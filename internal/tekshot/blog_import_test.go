package tekshot

import (
	"context"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"
)

// importMarkup is a hand-written article as Gutenberg stores it: comments,
// classes, an image with a caption and a nested list.
const importMarkup = `<!-- wp:paragraph -->
<p>Camera AI giúp nhà máy <strong>giảm lỗi</strong> ngay từ ca đầu.</p>
<!-- /wp:paragraph -->

<!-- wp:heading -->
<h2 class="wp-block-heading">Vì sao cần camera AI</h2>
<!-- /wp:heading -->

<!-- wp:paragraph -->
<p>Mắt người mỏi sau tám giờ; camera thì không.</p>
<!-- /wp:paragraph -->

<!-- wp:image {"id":110,"sizeSlug":"large"} -->
<figure class="wp-block-image size-large"><img src="/a.jpg" alt="Dây chuyền"/><figcaption class="wp-element-caption">Dây chuyền đóng gói</figcaption></figure>
<!-- /wp:image -->

<!-- wp:list -->
<ul class="wp-block-list"><!-- wp:list-item -->
<li>Đếm sản phẩm</li>
<!-- /wp:list-item -->

<!-- wp:list-item -->
<li>Phát hiện lỗi</li>
<!-- /wp:list-item --></ul>
<!-- /wp:list -->`

// importArgs is a faithful conversion of importMarkup.
func importArgs() map[string]any {
	return map[string]any{
		"reply": "Đã chuyển xong.",
		"document": map[string]any{
			"version":       float64(1),
			"title":         "Camera AI trong nhà máy",
			"summary":       "",
			"language":      "vi",
			"lead":          map[string]any{"paragraphs": []any{"Camera AI giúp nhà máy **giảm lỗi** ngay từ ca đầu."}},
			"key_takeaways": []any{},
			"sections": []any{map[string]any{
				"id": "s1", "heading": "Vì sao cần camera AI", "level": float64(2),
				"blocks": []any{
					map[string]any{"type": "paragraph", "text": "Mắt người mỏi sau tám giờ; camera thì không."},
					map[string]any{"type": "image", "file_id": float64(110), "alt": "Dây chuyền", "caption": "Dây chuyền đóng gói"},
					map[string]any{"type": "list", "ordered": false, "items": []any{"Đếm sản phẩm", "Phát hiện lỗi"}},
				},
			}},
			"quote":       map[string]any{"text": "", "cite": ""},
			"faq":         []any{},
			"sources":     []any{},
			"cta":         map[string]any{"heading": "", "text": "", "button": map[string]any{"label": "", "href": ""}},
			"images":      map[string]any{"featured_file_id": float64(0), "featured_alt": ""},
			"schema_type": "Article",
		},
		"unconverted": []any{},
	}
}

func importSection(args map[string]any) map[string]any {
	return args["document"].(map[string]any)["sections"].([]any)[0].(map[string]any)
}

func importBlocks(args map[string]any) []any {
	return importSection(args)["blocks"].([]any)
}

func importCollector(markup string) *BlogImportCollector {
	return NewBlogImportCollector(validBlogSnapshot(), parseBlogImportSource(markup))
}

func TestParseBlogImportSourceReadsImagesAndVisibleText(t *testing.T) {
	markup := `<!-- wp:paragraph --><p>Giá <strong>vàng</strong> &amp; bạc</p><!-- /wp:paragraph -->` +
		`<!-- wp:image {"id":12,"sizeSlug":"large"} --><figure><img src="/a.jpg" alt="ẩn"/><figcaption>Chú thích</figcaption></figure><!-- /wp:image -->` +
		`<!-- wp:gallery {"linkTo":"none"} --><figure><!-- wp:image {"id":34} --><figure><img src="/b.jpg"/></figure><!-- /wp:image -->` +
		`<!-- wp:image {"id":12} --><figure><img src="/a.jpg"/></figure><!-- /wp:image --></figure><!-- /wp:gallery -->` +
		`<!-- wp:image {"id": --><!-- /wp:image -->`
	source := parseBlogImportSource(markup)
	if len(source.ImageIDs) != 2 || source.ImageIDs[0] != 12 || source.ImageIDs[1] != 34 {
		t.Fatalf("image ids = %v, want [12 34]", source.ImageIDs)
	}
	if !strings.Contains(source.Comparable, "giávàng&bạc") || !strings.Contains(source.Comparable, "chúthích") {
		t.Fatalf("visible text not kept: %q", source.Comparable)
	}
	if strings.Contains(source.Comparable, "ẩn") || strings.Contains(source.Comparable, "sizeslug") {
		t.Fatalf("attributes and block comments must not count as text: %q", source.Comparable)
	}
}

func TestBlogImportCollectorAcceptsAFaithfulConversion(t *testing.T) {
	c := importCollector(importMarkup)
	if res := c.Execute(context.Background(), importArgs()); res.IsError {
		t.Fatalf("faithful conversion rejected: %s", res.ForLLM)
	}
	report := c.Report()
	if report["document"].(map[string]any)["title"] != "Camera AI trong nhà máy" {
		t.Fatal("document not kept")
	}
	if list, _ := report["unconverted"].([]any); len(list) != 0 {
		t.Fatalf("unconverted = %v, want empty", list)
	}
	if c.LastError() != "" {
		t.Fatal("an accepted submission clears the last error")
	}
}

func TestBlogImportCollectorRejectsRewrittenWording(t *testing.T) {
	args := importArgs()
	importBlocks(args)[0].(map[string]any)["text"] = "Mắt người mệt sau tám giờ; camera thì không."
	c := importCollector(importMarkup)
	res := c.Execute(context.Background(), args)
	if !res.IsError || !strings.Contains(res.ForLLM, "word for word") || !strings.Contains(res.ForLLM, "Mắt người mệt") {
		t.Fatalf("rewritten wording must be rejected naming the text, got %q", res.ForLLM)
	}
	if c.Report() != nil || c.LastError() == "" {
		t.Fatal("the rejection must be kept for the retry prompt")
	}
}

func TestBlogImportCollectorToleratesTypographyAndNormalisation(t *testing.T) {
	markup := "<p>" + norm.NFD.String("Nhà máy “thông minh”") + "&nbsp;– bước đầu</p><h2>Phần một</h2><p>Nội dung.</p>"
	args := importArgs()
	doc := args["document"].(map[string]any)
	doc["lead"] = map[string]any{"paragraphs": []any{`Nhà máy "thông minh" - bước đầu`}}
	importSection(args)["heading"] = "Phần một"
	importSection(args)["blocks"] = []any{map[string]any{"type": "paragraph", "text": "Nội dung."}}
	if res := importCollector(markup).Execute(context.Background(), args); res.IsError {
		t.Fatalf("typography, NBSP and NFD must not count as rewriting: %s", res.ForLLM)
	}
}

func TestBlogImportCollectorRejectsASilentlyDroppedImage(t *testing.T) {
	args := importArgs()
	blocks := importBlocks(args)
	importSection(args)["blocks"] = []any{blocks[0], blocks[2]}
	c := importCollector(importMarkup)
	if res := c.Execute(context.Background(), args); !res.IsError || !strings.Contains(res.ForLLM, "110") {
		t.Fatalf("a dropped image must be named, got %q", res.ForLLM)
	}
	args["unconverted"] = []any{map[string]any{"block_name": "core/image", "excerpt": "/a.jpg", "reason": "Ảnh để sau."}}
	if res := c.Execute(context.Background(), args); res.IsError {
		t.Fatalf("an image listed in unconverted is accounted for: %s", res.ForLLM)
	}
}

func TestBlogImportCollectorRejectsDroppedTextNobodyListed(t *testing.T) {
	args := importArgs()
	blocks := importBlocks(args)
	importSection(args)["blocks"] = []any{blocks[1]}
	c := importCollector(importMarkup)
	if res := c.Execute(context.Background(), args); !res.IsError || !strings.Contains(res.ForLLM, "unconverted is empty") {
		t.Fatalf("losing most of the text silently must be rejected, got %q", res.ForLLM)
	}
	args["unconverted"] = []any{map[string]any{"block_name": "core/list", "excerpt": "Đếm sản phẩm", "reason": "Để sau."}}
	if res := c.Execute(context.Background(), args); res.IsError {
		t.Fatalf("listed blocks are accounted for: %s", res.ForLLM)
	}
}

func TestBlogImportCollectorRejectsLinksTheRendererWouldNotKeep(t *testing.T) {
	args := importArgs()
	importBlocks(args)[0].(map[string]any)["text"] = "Mắt người mỏi sau tám giờ; [camera](http://vidu.example.com) thì không."
	res := importCollector(importMarkup).Execute(context.Background(), args)
	if !res.IsError || !strings.Contains(res.ForLLM, "http://vidu.example.com") {
		t.Fatalf("an http:// link must be refused, got %q", res.ForLLM)
	}
}

func TestBlogImportCollectorStillEnforcesDocumentV1(t *testing.T) {
	args := importArgs()
	importBlocks(args)[1].(map[string]any)["file_id"] = float64(999)
	res := importCollector(importMarkup).Execute(context.Background(), args)
	if !res.IsError || !strings.Contains(res.ForLLM, "999") {
		t.Fatalf("an image outside the snapshot must be refused, got %q", res.ForLLM)
	}
}

func TestNormalizeBlogUnconverted(t *testing.T) {
	entries := []any{map[string]any{"block_name": " core/embed ", "excerpt": strings.Repeat("ệ", 300), "reason": ""}}
	for range 150 {
		entries = append(entries, map[string]any{"block_name": "core/html", "excerpt": "x", "reason": "r"})
	}
	out, err := normalizeBlogUnconverted(entries)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != blogImportMaxUnconverted {
		t.Fatalf("len = %d, want %d (trim, never refuse)", len(out), blogImportMaxUnconverted)
	}
	first := out[0].(map[string]any)
	if first["block_name"] != "core/embed" || len([]rune(first["excerpt"].(string))) != blogImportExcerptRunes || first["reason"] != blogImportDefaultReason {
		t.Fatalf("unexpected first entry %#v", first)
	}
	if _, err := normalizeBlogUnconverted([]any{map[string]any{"block_name": " ", "excerpt": "x", "reason": "r"}}); err == nil {
		t.Fatal("an entry without block_name must be refused")
	}
}

func TestBlogImportPresentationKeepsOnlyASyncedTemplate(t *testing.T) {
	snap := validBlogSnapshot()
	if p, ok := blogImportPresentation(map[string]any{"template": "editorial"}, snap).(map[string]any); !ok || p["template"] != "editorial" {
		t.Fatal("a synced template is kept")
	}
	if blogImportPresentation(map[string]any{"template": "magazine"}, snap) != nil {
		t.Fatal("an unsynced template is dropped, not guessed")
	}
	if blogImportPresentation(nil, snap) != nil {
		t.Fatal("no template stays no template")
	}
}
