package tekshot

import (
	"strings"
	"testing"
)

func rewriteBaseDocument() map[string]any {
	orig := validBlogSubmission()["document"].(map[string]any)
	orig["sections"] = []any{
		map[string]any{"id": "s1", "heading": "A", "level": float64(2), "blocks": []any{map[string]any{"type": "paragraph", "text": "a"}}},
		map[string]any{"id": "s2", "heading": "B", "level": float64(2), "blocks": []any{map[string]any{"type": "paragraph", "text": "b"}}},
	}
	return orig
}

func TestEnforceBlogRewriteScope(t *testing.T) {
	orig := rewriteBaseDocument()
	changed := func(mut func(d map[string]any)) map[string]any {
		d := cloneJSON(orig)
		mut(d)
		return d
	}
	s2Only := changed(func(d map[string]any) { d["sections"].([]any)[1].(map[string]any)["heading"] = "B2" })
	s1Too := changed(func(d map[string]any) {
		d["sections"].([]any)[0].(map[string]any)["heading"] = "A2"
		d["sections"].([]any)[1].(map[string]any)["heading"] = "B2"
	})
	titleToo := changed(func(d map[string]any) { d["title"] = "Khác" })
	fewer := changed(func(d map[string]any) { d["sections"] = d["sections"].([]any)[:1] })
	reordered := changed(func(d map[string]any) {
		s := d["sections"].([]any)
		d["sections"] = []any{s[1], s[0]}
	})

	if err := enforceBlogRewriteScope(orig, s2Only, "section:s2"); err != nil {
		t.Fatalf("s2 only should pass: %v", err)
	}
	if err := enforceBlogRewriteScope(orig, s1Too, "section:s2"); err == nil {
		t.Fatal("s1 changed must fail")
	}
	if err := enforceBlogRewriteScope(orig, titleToo, "section:s2"); err == nil {
		t.Fatal("title changed must fail")
	}
	if err := enforceBlogRewriteScope(orig, fewer, "section:s2"); err == nil {
		t.Fatal("dropping a section must fail")
	}
	if err := enforceBlogRewriteScope(orig, reordered, "section:s2"); err == nil {
		t.Fatal("reordering must fail")
	}
	if err := enforceBlogRewriteScope(orig, titleToo, "presentation"); err == nil {
		t.Fatal("presentation scope must keep the text")
	}
	if err := enforceBlogRewriteScope(orig, cloneJSON(orig), "presentation"); err != nil {
		t.Fatal(err)
	}
	if err := enforceBlogRewriteScope(orig, titleToo, "all"); err != nil {
		t.Fatal(err)
	}
	if err := enforceBlogRewriteScope(orig, s2Only, "section:zzz"); err == nil {
		t.Fatal("unknown section must fail")
	}
	if err := enforceBlogRewriteScope(orig, s2Only, "sections"); err == nil {
		t.Fatal("unknown scope must fail")
	}
	if err := enforceBlogRewriteScope(orig, nil, "all"); err == nil {
		t.Fatal("nil document must fail")
	}
}

func TestEnforceBlogRewriteScopeIgnoresNumberEncoding(t *testing.T) {
	orig := rewriteBaseDocument()
	// The validator emits int for level/version, JSON decoding gives float64.
	normalised := cloneJSON(orig)
	normalised["version"] = 1
	normalised["sections"].([]any)[0].(map[string]any)["level"] = 2
	if err := enforceBlogRewriteScope(orig, normalised, "presentation"); err != nil {
		t.Fatalf("int vs float64 must compare equal: %v", err)
	}
}

func TestBuildBlogRewritePromptNamesTheScopedTool(t *testing.T) {
	request := map[string]any{
		"instruction":  "Ngắn lại",
		"document":     rewriteBaseDocument(),
		"presentation": map[string]any{"template": "editorial"},
		"snapshot":     map[string]any{"website": map[string]any{"language": "vi"}},
	}
	section := buildBlogRewritePrompt(request, "section:s2")
	for _, want := range []string{"section:s2", "CURRENT DOCUMENT", "\"id\":\"s2\"", "USER INSTRUCTION:\nNgắn lại", blogSectionToolName} {
		if !strings.Contains(section, want) {
			t.Errorf("section prompt lacks %q", want)
		}
	}
	if strings.Contains(section, "calling "+blogFinalToolName) {
		t.Error("section prompt must not ask for the whole document")
	}
	presentation := buildBlogRewritePrompt(request, "presentation")
	if !strings.Contains(presentation, blogPresentationToolName) || strings.Contains(presentation, "calling "+blogFinalToolName) {
		t.Error("presentation prompt must name the presentation tool only")
	}
	all := buildBlogRewritePrompt(request, "all")
	if !strings.Contains(all, "calling "+blogFinalToolName) {
		t.Error("all prompt must ask for the whole document")
	}
	for _, prompt := range []string{section, presentation, all} {
		if strings.Contains(prompt, "create_image") {
			t.Fatal("prompt must never mention image generation")
		}
	}
}

func TestBlogToolAllowHasNoImageTools(t *testing.T) {
	for _, name := range blogToolAllow() {
		if strings.Contains(name, "image") {
			t.Fatalf("image tool %q must not be allowed for blog jobs", name)
		}
	}
}

func blockBaseDocument() map[string]any {
	doc := rewriteBaseDocument()
	doc["sections"].([]any)[0].(map[string]any)["blocks"] = []any{
		map[string]any{"type": "paragraph", "text": "Camera AI giúp **đếm** sản phẩm mỗi ca."},
		map[string]any{"type": "list", "ordered": false, "items": []any{"Nhanh", "Chính xác"}},
	}
	return doc
}

func TestBlogBlockScopeParsing(t *testing.T) {
	valid := map[string]blogBlockScope{
		"block:s1:0":           {SectionID: "s1", Index: 0},
		"fragment:s-2:12":      {Fragment: true, SectionID: "s-2", Index: 12},
		"block:gioi-thieu:999": {SectionID: "gioi-thieu", Index: 999},
	}
	for scope, want := range valid {
		got, ok := parseBlogBlockScope(scope)
		if !ok || got != want {
			t.Errorf("%s: got %+v %v, want %+v", scope, got, ok, want)
		}
		if err := validateBlogScope(scope); err != nil {
			t.Errorf("%s must be a valid scope: %v", scope, err)
		}
	}
	invalid := []string{
		"block:s1", "block:S1:0", "block:s1:01", "block:s1:-1", "block::0",
		"fragment:s1:1000", "block:s1:0:1", "block:1s:0", "blocks:s1:0",
		"fragment:" + strings.Repeat("a", 33) + ":0",
	}
	for _, scope := range invalid {
		if _, ok := parseBlogBlockScope(scope); ok {
			t.Errorf("%s must not parse", scope)
		}
		if err := validateBlogScope(scope); err == nil {
			t.Errorf("%s must be rejected", scope)
		}
	}
}

func TestBlogBlockAt(t *testing.T) {
	doc := blockBaseDocument()
	block, ok := blogBlockAt(doc, "s1", 1)
	if !ok || block["type"] != "list" {
		t.Fatalf("block s1:1 must be the list, got %v %v", block, ok)
	}
	for _, miss := range []struct {
		id    string
		index int
	}{{"s1", 2}, {"s1", -1}, {"zz", 0}} {
		if _, ok := blogBlockAt(doc, miss.id, miss.index); ok {
			t.Errorf("%s:%d must not exist", miss.id, miss.index)
		}
	}
}

func TestEnforceBlogRewriteScopeForABlock(t *testing.T) {
	orig := blockBaseDocument()
	changed := func(mut func(d map[string]any)) map[string]any {
		d := cloneJSON(orig)
		mut(d)
		return d
	}
	block := func(d map[string]any, s, i int) map[string]any {
		return d["sections"].([]any)[s].(map[string]any)["blocks"].([]any)[i].(map[string]any)
	}
	onlyTarget := changed(func(d map[string]any) { block(d, 0, 0)["text"] = "Camera AI đếm sản phẩm." })
	if err := enforceBlogRewriteScope(orig, onlyTarget, "block:s1:0"); err != nil {
		t.Fatalf("the target block alone must pass: %v", err)
	}
	mustFail := map[string]map[string]any{
		"another block": changed(func(d map[string]any) {
			block(d, 0, 0)["text"] = "x"
			block(d, 0, 1)["items"] = []any{"Khác"}
		}),
		"block type": changed(func(d map[string]any) { block(d, 0, 0)["type"] = "callout" }),
		"section heading": changed(func(d map[string]any) {
			block(d, 0, 0)["text"] = "x"
			d["sections"].([]any)[0].(map[string]any)["heading"] = "A2"
		}),
		"another section": changed(func(d map[string]any) { d["sections"].([]any)[1].(map[string]any)["heading"] = "B2" }),
		"title":           changed(func(d map[string]any) { d["title"] = "Khác" }),
		"an extra block": changed(func(d map[string]any) {
			s := d["sections"].([]any)[0].(map[string]any)
			s["blocks"] = append(s["blocks"].([]any), map[string]any{"type": "paragraph", "text": "thêm"})
		}),
	}
	for name, doc := range mustFail {
		if err := enforceBlogRewriteScope(orig, doc, "block:s1:0"); err == nil {
			t.Errorf("%s changed: the guard must fail", name)
		}
	}
	if err := enforceBlogRewriteScope(orig, onlyTarget, "block:s1:5"); err == nil {
		t.Error("an index past the end must fail")
	}
	if err := enforceBlogRewriteScope(orig, onlyTarget, "block:zz:0"); err == nil {
		t.Error("an unknown section must fail")
	}
	if err := enforceBlogRewriteScope(orig, onlyTarget, "fragment:s1:0"); err == nil {
		t.Error("a fragment scope has no document to compare")
	}
}

func TestNewBlogRewriteCollectorChecksTheTarget(t *testing.T) {
	orig := blockBaseDocument()
	withSelection := func(text string) map[string]any {
		return map[string]any{"selection": map[string]any{"start": float64(0), "end": float64(len([]rune(text))), "text": text}}
	}
	cases := []struct {
		scope   string
		request map[string]any
		tool    string
	}{
		{"all", nil, blogFinalToolName},
		{"presentation", nil, blogPresentationToolName},
		{"section:s2", nil, blogSectionToolName},
		{"block:s1:1", nil, blogBlockToolName},
		{"fragment:s1:0", withSelection("Camera AI"), blogFragmentToolName},
	}
	for _, c := range cases {
		collector, err := newBlogRewriteCollector(c.scope, orig, c.request, validBlogSnapshot())
		if err != nil {
			t.Errorf("%s: %v", c.scope, err)
			continue
		}
		if collector.Name() != c.tool {
			t.Errorf("%s: tool %s, want %s", c.scope, collector.Name(), c.tool)
		}
	}
	failing := []struct {
		name    string
		scope   string
		request map[string]any
	}{
		{"unknown section", "section:zz", nil},
		{"block past the end", "block:s1:2", nil},
		{"fragment without selection", "fragment:s1:0", nil},
		{"selection no longer in the block", "fragment:s1:0", withSelection("không có trong đoạn")},
		{"fragment inside a list", "fragment:s1:1", withSelection("Nhanh")},
	}
	for _, c := range failing {
		if _, err := newBlogRewriteCollector(c.scope, orig, c.request, validBlogSnapshot()); err == nil {
			t.Errorf("%s: expected an error", c.name)
		}
	}
}

func TestAssembleBlogRewriteResult(t *testing.T) {
	snap := validBlogSnapshot()
	original, err := validateBlogDocument(blockBaseDocument(), snap)
	if err != nil {
		t.Fatal(err)
	}
	current := map[string]any{"template": "editorial", "options": map[string]any{}}

	out, err := assembleBlogRewriteResult("block:s1:0", original, map[string]any{
		"reply": "Gọn lại.",
		"block": map[string]any{"type": "paragraph", "text": "Camera AI đếm sản phẩm."},
	}, current, snap)
	if err != nil {
		t.Fatal(err)
	}
	doc := out["document"].(map[string]any)
	if got := doc["sections"].([]any)[0].(map[string]any)["blocks"].([]any)[0].(map[string]any)["text"]; got != "Camera AI đếm sản phẩm." {
		t.Fatalf("block not spliced: %v", got)
	}
	if out["presentation"].(map[string]any)["template"] != "editorial" {
		t.Fatal("the current presentation must ride along")
	}
	if out["seo"].(map[string]any)["meta_title"] != "" {
		t.Fatal("a scoped result carries an empty seo block")
	}

	if _, err := assembleBlogRewriteResult("block:s1:0", original, map[string]any{
		"reply": "x", "block": map[string]any{"type": "callout", "text": "y"},
	}, nil, snap); err == nil {
		t.Fatal("a block of another type must fail the guard")
	}

	fragment, err := assembleBlogRewriteResult("fragment:s1:0", original, map[string]any{"reply": "Gọn lại.", "text": "Camera"}, current, snap)
	if err != nil {
		t.Fatal(err)
	}
	if _, has := fragment["document"]; has || fragment["text"] != "Camera" || fragment["reply"] != "Gọn lại." {
		t.Fatalf("a fragment result is the passage only: %v", fragment)
	}

	section, err := assembleBlogRewriteResult("section:s2", original, map[string]any{
		"reply":   "x",
		"section": map[string]any{"id": "s2", "heading": "B2", "level": float64(2), "blocks": []any{map[string]any{"type": "paragraph", "text": "b2"}}},
	}, nil, snap)
	if err != nil {
		t.Fatal(err)
	}
	if section["presentation"] != nil {
		t.Fatal("no current presentation must stay nil")
	}
}

func TestBuildBlogRewritePromptForABlockAndAFragment(t *testing.T) {
	request := map[string]any{
		"instruction":  "Gọn lại",
		"document":     blockBaseDocument(),
		"presentation": nil,
		"snapshot":     map[string]any{"website": map[string]any{"language": "vi"}},
		"selection":    map[string]any{"start": float64(0), "end": float64(9), "text": "Camera AI"},
	}
	block := buildBlogRewritePrompt(request, "block:s1:1")
	for _, want := range []string{blogBlockToolName, "## TARGET BLOCK", "\"type\":\"list\"", "block:s1:1", "USER INSTRUCTION:\nGọn lại"} {
		if !strings.Contains(block, want) {
			t.Errorf("block prompt lacks %q", want)
		}
	}
	fragment := buildBlogRewritePrompt(request, "fragment:s1:0")
	for _, want := range []string{blogFragmentToolName, "## TARGET BLOCK", "## SELECTED PASSAGE\n\"\"\"\nCamera AI\n\"\"\"", blogFragmentDeleteToken} {
		if !strings.Contains(fragment, want) {
			t.Errorf("fragment prompt lacks %q", want)
		}
	}
	for _, prompt := range []string{block, fragment} {
		if strings.Contains(prompt, "calling "+blogFinalToolName) {
			t.Error("a scoped prompt must not ask for the whole document")
		}
		if strings.Contains(prompt, "create_image") {
			t.Fatal("prompt must never mention image generation")
		}
	}
}
