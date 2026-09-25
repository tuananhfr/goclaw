package tekshot

import (
	"context"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"

	"github.com/nextlevelbuilder/goclaw/internal/store"
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
	if res := c.Execute(context.Background(), args); !res.IsError || !strings.Contains(res.ForLLM, "keeps only") {
		t.Fatalf("losing most of the text silently must be rejected, got %q", res.ForLLM)
	}
}

// Node 118/114 on tekshot.vn: the model stopped two thirds in and listed the
// rest as "markup was cut off". A paragraph, heading or list always fits v1.
func TestBlogImportCollectorRejectsTextBlocksListedAsUnconverted(t *testing.T) {
	args := importArgs()
	args["unconverted"] = []any{map[string]any{"block_name": "core/paragraph", "excerpt": "Mắt người mỏi", "reason": "Markup gốc bị cắt."}}
	res := importCollector(importMarkup).Execute(context.Background(), args)
	if !res.IsError || !strings.Contains(res.ForLLM, "core/paragraph") {
		t.Fatalf("a paragraph listed as unconverted must be refused, got %q", res.ForLLM)
	}
}

func TestBlogImportCollectorRejectsATailDroppedIntoUnconverted(t *testing.T) {
	tail := strings.Repeat(`<!-- wp:embed {"url":"https://youtube.com/watch?v=1"} --><figure><div>https://youtube.com/watch?v=1</div></figure><!-- /wp:embed -->`, 1) +
		strings.Repeat("<!-- wp:html --><div>Một đoạn dài nằm trong khối html mà bản cấu trúc không chứa được, lặp lại nhiều lần cho đủ dài.</div><!-- /wp:html -->", 6)
	args := importArgs()
	args["unconverted"] = []any{map[string]any{"block_name": "core/embed", "excerpt": "https://youtube.com/watch?v=1", "reason": "Chưa có khối video."}}
	res := importCollector(importMarkup+tail).Execute(context.Background(), args)
	if !res.IsError || !strings.Contains(res.ForLLM, "keeps only") {
		t.Fatalf("coverage must hold even when unconverted is not empty, got %q", res.ForLLM)
	}
}

func TestBlogImportCollectorAcceptsAnEmbedListedAsUnconverted(t *testing.T) {
	markup := importMarkup + `<!-- wp:embed {"url":"https://youtube.com/watch?v=1"} --><figure><div>https://youtube.com/watch?v=1</div></figure><!-- /wp:embed -->`
	args := importArgs()
	args["unconverted"] = []any{map[string]any{"block_name": "core/embed", "excerpt": "https://youtube.com/watch?v=1", "reason": "Chưa có khối video."}}
	if res := importCollector(markup).Execute(context.Background(), args); res.IsError {
		t.Fatalf("a genuinely unconvertible block is accounted for: %s", res.ForLLM)
	}
}

// Node 118: eight images had no alt, and v1 refuses an image block without one.
func TestBlogImportCollectorFillsAMissingImageAlt(t *testing.T) {
	args := importArgs()
	importBlocks(args)[1].(map[string]any)["alt"] = ""
	c := importCollector(importMarkup)
	if res := c.Execute(context.Background(), args); res.IsError {
		t.Fatalf("a missing alt is filled, not refused: %s", res.ForLLM)
	}
	image := c.Report()["document"].(map[string]any)["sections"].([]any)[0].(map[string]any)["blocks"].([]any)[1].(map[string]any)
	if image["alt"] != "Dây chuyền đóng gói" {
		t.Fatalf("alt falls back to the caption, got %q", image["alt"])
	}

	args = importArgs()
	block := importBlocks(args)[1].(map[string]any)
	block["alt"], block["caption"] = "", ""
	markup := strings.Replace(importMarkup, `<figcaption class="wp-element-caption">Dây chuyền đóng gói</figcaption>`, "", 1)
	c = importCollector(markup)
	if res := c.Execute(context.Background(), args); res.IsError {
		t.Fatalf("no caption either: %s", res.ForLLM)
	}
	image = c.Report()["document"].(map[string]any)["sections"].([]any)[0].(map[string]any)["blocks"].([]any)[1].(map[string]any)
	if image["alt"] != "Camera AI trong nhà máy" {
		t.Fatalf("alt falls back to the title, got %q", image["alt"])
	}
}

// Node 8758: the one paragraph landed in the lead and again in the section.
func TestBlogImportCollectorRejectsDuplicatedText(t *testing.T) {
	args := importArgs()
	doc := args["document"].(map[string]any)
	doc["lead"] = map[string]any{"paragraphs": []any{
		"Camera AI giúp nhà máy **giảm lỗi** ngay từ ca đầu.",
		"Mắt người mỏi sau tám giờ; camera thì không.",
	}}
	res := importCollector(importMarkup).Execute(context.Background(), args)
	if !res.IsError || !strings.Contains(res.ForLLM, "more often than") || !strings.Contains(res.ForLLM, "Mắt người mỏi") {
		t.Fatalf("a text placed twice must be refused, got %q", res.ForLLM)
	}
}

func TestBlogImportCollectorAllowsTheOnlyParagraphTwice(t *testing.T) {
	markup := "<!-- wp:paragraph --><p>Bài chỉ có đúng một đoạn văn này thôi.</p><!-- /wp:paragraph -->"
	args := importArgs()
	doc := args["document"].(map[string]any)
	doc["lead"] = map[string]any{"paragraphs": []any{"Bài chỉ có đúng một đoạn văn này thôi."}}
	importSection(args)["heading"] = "Camera AI trong nhà máy"
	importSection(args)["blocks"] = []any{map[string]any{"type": "paragraph", "text": "Bài chỉ có đúng một đoạn văn này thôi."}}
	if res := importCollector(markup).Execute(context.Background(), args); res.IsError {
		t.Fatalf("v1 needs a lead and a section block, so a one-paragraph article repeats it: %s", res.ForLLM)
	}
}

func TestBuildBlogImportPromptResolvesTheNoHeadingRule(t *testing.T) {
	prompt := buildBlogImportPrompt(map[string]any{"gutenberg_markup": "<p>x</p>"})
	for _, want := range []string{"never list core/paragraph, core/heading or core/list", "every paragraph after it"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt misses %q", want)
		}
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

func TestBuildBlogImportPromptCarriesTheOriginal(t *testing.T) {
	request := map[string]any{
		"gutenberg_markup": importMarkup,
		"title":            "Camera AI trong nhà máy",
		"summary":          "Tóm tắt cũ",
		"featured_file_id": float64(110),
		"featured_alt":     "Bìa",
		"snapshot": map[string]any{
			"website": map[string]any{"language": "vi"},
			"images":  []any{map[string]any{"id": float64(110), "url": "/a.jpg"}},
		},
	}
	prompt := buildBlogImportPrompt(request)
	for _, want := range []string{
		blogImportToolName,
		"Never rewrite",
		"unconverted",
		"## TITLE\nCamera AI trong nhà máy",
		"## SUMMARY\nTóm tắt cũ",
		"## AVAILABLE IMAGES",
		"## ORIGINAL MARKUP\n" + importMarkup,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt misses %q", want)
		}
	}
}

func TestRunBlogImportRefusesMissingOrOversizedMarkup(t *testing.T) {
	s := &JobService{}
	if _, _, err := s.runBlogImport(context.Background(), &store.TekshotJob{}, map[string]any{}); err == nil || !strings.Contains(err.Error(), "gutenberg_markup is required") {
		t.Fatalf("missing markup: %v", err)
	}
	big := map[string]any{"gutenberg_markup": strings.Repeat("a", blogImportMaxMarkupBytes+1)}
	if _, _, err := s.runBlogImport(context.Background(), &store.TekshotJob{}, big); err == nil || !strings.Contains(err.Error(), "BLOG_IMPORT_TOO_LARGE") {
		t.Fatalf("oversized markup: %v", err)
	}
}

func TestBlogImportJobTypeIsWired(t *testing.T) {
	if TekshotJobTypeBlogImport != "blog_import" {
		t.Fatal("the job type string is the contract with Drupal's BlogJobRepository::JOB_TYPES")
	}
	if !isSupportedTekshotJobType(TekshotJobTypeBlogImport) {
		t.Fatal("Create must accept blog_import")
	}
	if jobRunTimeout(TekshotJobTypeBlogImport) != defaultJobRunTimeout {
		t.Fatal("blog_import runs on the default 12-minute budget")
	}
}
