package tekshot

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

func chunkPara(text string) string {
	return "<!-- wp:paragraph -->\n<p>" + text + "</p>\n<!-- /wp:paragraph -->\n\n"
}

func chunkHeading(text string, level int) string {
	attrs := ""
	if level != 2 {
		attrs = fmt.Sprintf(` {"level":%d}`, level)
	}
	return fmt.Sprintf("<!-- wp:heading%s -->\n<h%d class=\"wp-block-heading\">%s</h%d>\n<!-- /wp:heading -->\n\n", attrs, level, text, level)
}

// longImportMarkup: a lead paragraph, then n h2 sections of two paragraphs.
func longImportMarkup(n int) string {
	var sb strings.Builder
	sb.WriteString(chunkPara("Mở bài của một bài viết rất dài."))
	for i := 1; i <= n; i++ {
		sb.WriteString(chunkHeading(fmt.Sprintf("Phần số %d", i), 2))
		sb.WriteString(chunkPara(fmt.Sprintf("Đoạn thứ nhất của phần %d. %s", i, strings.Repeat("Nội dung ", 20))))
		sb.WriteString(chunkPara(fmt.Sprintf("Đoạn thứ hai của phần %d. %s", i, strings.Repeat("Chi tiết ", 20))))
	}
	return sb.String()
}

func TestSplitBlogImportMarkupKeepsShortArticlesWhole(t *testing.T) {
	if chunks := splitBlogImportMarkup(importMarkup, 8000); len(chunks) != 1 || chunks[0] != importMarkup {
		t.Fatalf("a short article is one chunk, got %d", len(chunks))
	}
}

func TestSplitBlogImportMarkupCutsBeforeSectionHeadings(t *testing.T) {
	markup := longImportMarkup(12)
	chunks := splitBlogImportMarkup(markup, 1500)
	if len(chunks) < 3 {
		t.Fatalf("expected several chunks, got %d", len(chunks))
	}
	if strings.Join(chunks, "") != markup {
		t.Fatal("chunks must concatenate back to the original byte for byte")
	}
	for i, chunk := range chunks[1:] {
		if !strings.HasPrefix(strings.TrimSpace(chunk), "<!-- wp:heading") {
			t.Fatalf("chunk %d does not open with a section heading: %.60q", i+1, chunk)
		}
	}
}

func TestSplitBlogImportMarkupNeverCutsInsideABlockOrBeforeAnH4(t *testing.T) {
	group := `<!-- wp:group --><div class="wp-block-group">` + chunkHeading("Tiêu đề trong nhóm", 2) + chunkPara(strings.Repeat("Trong nhóm ", 80)) + `</div><!-- /wp:group -->` + "\n\n"
	markup := chunkPara(strings.Repeat("Mở bài ", 80)) + group + chunkHeading("Tiêu đề nhỏ", 4) + chunkPara(strings.Repeat("Sau h4 ", 80)) + chunkHeading("Phần tiếp", 3) + chunkPara("Cuối.")
	chunks := splitBlogImportMarkup(markup, 300)
	for _, chunk := range chunks {
		if strings.Count(chunk, "<!-- wp:group") != strings.Count(chunk, "<!-- /wp:group") {
			t.Fatalf("a group was split: %.80q", chunk)
		}
		if strings.HasPrefix(strings.TrimSpace(chunk), `<!-- wp:heading {"level":4}`) {
			t.Fatal("an h4 becomes a bold paragraph, so it cannot open a part")
		}
	}
	if last := chunks[len(chunks)-1]; !strings.HasPrefix(strings.TrimSpace(last), `<!-- wp:heading {"level":3}`) {
		t.Fatalf("expected the h3 to open the last part, got %.60q", last)
	}
}

func partArgs(heading string, texts ...string) map[string]any {
	blocks := []any{}
	for _, text := range texts {
		blocks = append(blocks, map[string]any{"type": "paragraph", "text": text})
	}
	return map[string]any{
		"sections":    []any{map[string]any{"id": "s1", "heading": heading, "level": float64(2), "blocks": blocks}},
		"faq":         []any{},
		"sources":     []any{},
		"unconverted": []any{},
	}
}

func TestBlogImportPartCollectorChecksAgainstItsOwnPart(t *testing.T) {
	part := chunkHeading("Phần hai", 2) + chunkPara("Đoạn đúng nguyên văn của phần hai.")
	c := NewBlogImportPartCollector(validBlogSnapshot(), parseBlogImportSource(part), "Bài dài", "vi")
	if res := c.Execute(context.Background(), partArgs("Phần hai", "Đoạn đúng nguyên văn của phần hai.")); res.IsError {
		t.Fatalf("a faithful part is accepted: %s", res.ForLLM)
	}
	if res := c.Execute(context.Background(), partArgs("Phần hai", "Đoạn đã bị viết lại của phần hai.")); !res.IsError || !strings.Contains(res.ForLLM, "word for word") {
		t.Fatalf("a rewritten part is refused, got %q", res.ForLLM)
	}
	if res := c.Execute(context.Background(), partArgs("Phần hai", "Đoạn")); !res.IsError || !strings.Contains(res.ForLLM, "keeps only") {
		t.Fatalf("a part that drops its text is refused, got %q", res.ForLLM)
	}
}

func TestStitchBlogImportRenumbersAndMerges(t *testing.T) {
	head := map[string]any{
		"reply": "Phần đầu xong.",
		"document": map[string]any{
			"title": "Bài", "lead": map[string]any{"paragraphs": []any{"Mở"}},
			"sections": []any{map[string]any{"id": "s1", "heading": "A", "level": 2, "blocks": []any{map[string]any{"type": "paragraph", "text": "a"}}}},
			"quote":    map[string]any{"text": "Trích đầu", "cite": ""},
			"faq":      []any{map[string]any{"q": "Q1", "a": "A1"}},
			"sources":  []any{},
		},
		"unconverted": []any{},
	}
	part := map[string]any{
		"sections":    []any{map[string]any{"id": "s1", "heading": "B", "level": 2, "blocks": []any{map[string]any{"type": "paragraph", "text": "b"}}}},
		"quote":       map[string]any{"text": "Trích sau", "cite": ""},
		"faq":         []any{map[string]any{"q": "Q2", "a": "A2"}},
		"sources":     []any{},
		"unconverted": []any{map[string]any{"block_name": "core/embed", "excerpt": "x", "reason": "r"}},
	}
	out := stitchBlogImport(head, []map[string]any{part})
	doc := out["document"].(map[string]any)
	sections := doc["sections"].([]any)
	if len(sections) != 2 || sections[1].(map[string]any)["id"] != "s2" {
		t.Fatalf("sections not appended and renumbered: %v", sections)
	}
	blocks := sections[1].(map[string]any)["blocks"].([]any)
	if last := blocks[len(blocks)-1].(map[string]any); last["type"] != "callout" || last["text"] != "Trích sau" {
		t.Fatalf("a later quote becomes a callout where it was, got %v", last)
	}
	if doc["quote"].(map[string]any)["text"] != "Trích đầu" {
		t.Fatal("the first quote stays the quote")
	}
	if len(doc["faq"].([]any)) != 2 || len(out["unconverted"].([]any)) != 1 {
		t.Fatal("faq and unconverted are concatenated")
	}
}

// scriptedPass answers each forced tool call with the next scripted args.
func scriptedPass(t *testing.T, answers map[string][]map[string]any) (blogImportPass, *[]string) {
	var prompts []string
	calls := map[string]int{}
	return func(_ context.Context, prompt string, tool tools.Tool) *providers.Usage {
		prompts = append(prompts, prompt)
		list := answers[tool.Name()]
		i := calls[tool.Name()]
		calls[tool.Name()]++
		if i >= len(list) {
			t.Fatalf("unexpected extra call to %s", tool.Name())
		}
		tool.Execute(context.Background(), list[i])
		return nil
	}, &prompts
}

func TestConvertBlogImportRunsOnePassPerPart(t *testing.T) {
	markup := longImportMarkup(3)
	chunks := splitBlogImportMarkup(markup, 500)
	if len(chunks) != 3 {
		t.Fatalf("fixture expects 3 chunks, got %d", len(chunks))
	}
	para := func(i int, which string, filler string) string {
		return fmt.Sprintf("Đoạn thứ %s của phần %d. %s", which, i, strings.TrimSpace(strings.Repeat(filler+" ", 20)))
	}
	section := func(i int) map[string]any {
		return map[string]any{"id": "s1", "heading": fmt.Sprintf("Phần số %d", i), "level": float64(2), "blocks": []any{
			map[string]any{"type": "paragraph", "text": para(i, "nhất", "Nội dung")},
			map[string]any{"type": "paragraph", "text": para(i, "hai", "Chi tiết")},
		}}
	}
	headArgs := map[string]any{
		"reply": "Xong.",
		"document": map[string]any{
			"version": float64(1), "title": "Bài dài", "summary": "", "language": "vi",
			"lead": map[string]any{"paragraphs": []any{"Mở bài của một bài viết rất dài."}}, "key_takeaways": []any{},
			"sections": []any{section(1)}, "faq": []any{}, "sources": []any{},
			"images": map[string]any{"featured_file_id": float64(0), "featured_alt": ""}, "schema_type": "Article",
		},
		"unconverted": []any{},
	}
	part := func(i int) map[string]any {
		return map[string]any{"sections": []any{section(i)}, "faq": []any{}, "sources": []any{}, "unconverted": []any{}}
	}
	pass, prompts := scriptedPass(t, map[string][]map[string]any{
		blogImportToolName:     {headArgs},
		blogImportPartToolName: {part(2), part(3)},
	})
	request := map[string]any{"gutenberg_markup": markup, "title": "Bài dài"}

	report, err := convertBlogImport(context.Background(), request, validBlogSnapshot(), pass, func(string) {}, 500)
	if err != nil {
		t.Fatal(err)
	}
	if report["reply"] != "Đã chuyển bài dài theo 3 phần." {
		t.Fatalf("the first part's reply only speaks for part 1, got %q", report["reply"])
	}
	sections := report["document"].(map[string]any)["sections"].([]any)
	if len(sections) != 3 || sections[2].(map[string]any)["id"] != "s3" {
		t.Fatalf("expected 3 stitched sections, got %v", sections)
	}
	if len(*prompts) != 3 || !strings.Contains((*prompts)[1], "PART 2 OF 3") || strings.Contains((*prompts)[1], "Phần số 1") {
		t.Fatalf("each later pass sees only its own part")
	}
}

func TestConvertBlogImportFailsWhenAPartNeverPasses(t *testing.T) {
	markup := longImportMarkup(3)
	bad := partArgs("Phần số 2", "Viết lại hoàn toàn khác.")
	headOnly := map[string]any{
		"reply": "x",
		"document": map[string]any{
			"version": float64(1), "title": "Bài dài", "language": "vi",
			"lead":     map[string]any{"paragraphs": []any{"Mở bài của một bài viết rất dài."}},
			"sections": []any{map[string]any{"id": "s1", "heading": "Phần số 1", "level": float64(2), "blocks": []any{map[string]any{"type": "paragraph", "text": "Đoạn thứ nhất của phần 1. " + strings.TrimSpace(strings.Repeat("Nội dung ", 20))}, map[string]any{"type": "paragraph", "text": "Đoạn thứ hai của phần 1. " + strings.TrimSpace(strings.Repeat("Chi tiết ", 20))}}}},
			"faq":      []any{}, "sources": []any{}, "images": map[string]any{"featured_file_id": float64(0)}, "schema_type": "Article",
		},
		"unconverted": []any{},
	}
	pass, _ := scriptedPass(t, map[string][]map[string]any{
		blogImportToolName:     {headOnly},
		blogImportPartToolName: {bad, bad, bad},
	})
	_, err := convertBlogImport(context.Background(), map[string]any{"gutenberg_markup": markup, "title": "Bài dài"}, validBlogSnapshot(), pass, func(string) {}, 500)
	if err == nil || !strings.Contains(err.Error(), "part 2 of 3") {
		t.Fatalf("a part that never passes fails the job naming it, got %v", err)
	}
}

func TestRunBlogImportAttemptsSaysWhenTheRunTimedOut(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tool := NewBlogImportCollector(validBlogSnapshot(), parseBlogImportSource(importMarkup))
	err := runBlogImportAttempts(ctx, func(context.Context, string, tools.Tool) *providers.Usage { return nil }, tool, "p", "bài", func(string) {}, &providers.Usage{})
	if err == nil || !strings.Contains(err.Error(), "ran out of time") {
		t.Fatalf("a cancelled run must say so, not blame the model, got %v", err)
	}
}
