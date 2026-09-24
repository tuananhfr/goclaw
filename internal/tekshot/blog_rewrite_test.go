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
