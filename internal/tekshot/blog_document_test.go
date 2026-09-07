package tekshot

import (
	"strings"
	"testing"
)

func validBlogSnapshot() blogSnapshot {
	return blogSnapshot{TemplateKeys: []string{"editorial", "tutorial"}, ImageIDs: map[int]bool{110: true}, Language: "vi"}
}

func validBlogSubmission() map[string]any {
	return map[string]any{
		"reply": "Đã viết xong.",
		"document": map[string]any{
			"version":       float64(1),
			"title":         "Camera AI trong nhà máy",
			"summary":       "Tóm tắt",
			"language":      "vi",
			"lead":          map[string]any{"paragraphs": []any{"Mở bài."}},
			"key_takeaways": []any{"Ý 1"},
			"sections": []any{map[string]any{
				"id": "s1", "heading": "Phần 1", "level": float64(2),
				"blocks": []any{
					map[string]any{"type": "paragraph", "text": "Đoạn."},
					map[string]any{"type": "image", "file_id": float64(110), "alt": "Ảnh", "caption": ""},
				},
			}},
			"quote":       map[string]any{"text": "", "cite": ""},
			"faq":         []any{},
			"sources":     []any{map[string]any{"title": "Nguồn", "url": "https://example.com/a"}},
			"cta":         map[string]any{"heading": "", "text": "", "button": map[string]any{"label": "Dùng thử", "href": "/dung-thu"}},
			"images":      map[string]any{"featured_file_id": float64(0), "featured_alt": ""},
			"schema_type": "Article",
		},
		"presentation": map[string]any{"template": "editorial", "options": map[string]any{}},
		"seo":          map[string]any{"meta_title": "t", "meta_description": "d", "keywords": "", "focus_keyword": "camera ai"},
	}
}

func blogSection(m map[string]any) map[string]any {
	return m["document"].(map[string]any)["sections"].([]any)[0].(map[string]any)
}

func TestValidateBlogSubmissionAcceptsValid(t *testing.T) {
	out, err := validateBlogSubmission(validBlogSubmission(), validBlogSnapshot())
	if err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
	doc := out["document"].(map[string]any)
	if _, has := doc["quote"]; has {
		t.Fatal("empty quote must be dropped")
	}
	if doc["images"].(map[string]any)["featured_file_id"] != nil {
		t.Fatal("featured 0 must become nil")
	}
	if doc["sections"].([]any)[0].(map[string]any)["blocks"].([]any)[1].(map[string]any)["file_id"] != 110 {
		t.Fatal("file_id must be an int")
	}
	if out["presentation"].(map[string]any)["template"] != "editorial" {
		t.Fatal("presentation kept")
	}
}

func TestValidateBlogSubmissionRejects(t *testing.T) {
	cases := map[string]func(m map[string]any){
		"missing lead": func(m map[string]any) {
			m["document"].(map[string]any)["lead"] = map[string]any{"paragraphs": []any{}}
		},
		"missing sections": func(m map[string]any) { m["document"].(map[string]any)["sections"] = []any{} },
		"unknown block": func(m map[string]any) {
			blogSection(m)["blocks"] = []any{map[string]any{"type": "html", "text": "<b>x</b>"}}
		},
		"image not in site": func(m map[string]any) {
			blogSection(m)["blocks"] = []any{map[string]any{"type": "image", "file_id": float64(999), "alt": "x"}}
		},
		"image without alt": func(m map[string]any) {
			blogSection(m)["blocks"] = []any{map[string]any{"type": "image", "file_id": float64(110), "alt": " "}}
		},
		"template not synced": func(m map[string]any) { m["presentation"].(map[string]any)["template"] = "magazine" },
		"cta href http": func(m map[string]any) {
			m["document"].(map[string]any)["cta"].(map[string]any)["button"].(map[string]any)["href"] = "http://x.com"
		},
		"source http": func(m map[string]any) {
			m["document"].(map[string]any)["sources"] = []any{map[string]any{"title": "n", "url": "http://x.com"}}
		},
		"bad section id": func(m map[string]any) { blogSection(m)["id"] = "Phần 1" },
		"bad level":      func(m map[string]any) { blogSection(m)["level"] = float64(4) },
		"empty blocks":   func(m map[string]any) { blogSection(m)["blocks"] = []any{} },
		"empty title":    func(m map[string]any) { m["document"].(map[string]any)["title"] = " " },
		"version 2":      func(m map[string]any) { m["document"].(map[string]any)["version"] = float64(2) },
		"empty reply":    func(m map[string]any) { m["reply"] = "" },
		"faq without answer": func(m map[string]any) {
			m["document"].(map[string]any)["faq"] = []any{map[string]any{"q": "Hỏi?", "a": ""}}
		},
		"featured not in site": func(m map[string]any) {
			m["document"].(map[string]any)["images"] = map[string]any{"featured_file_id": float64(5), "featured_alt": "x"}
		},
		"schema type": func(m map[string]any) { m["document"].(map[string]any)["schema_type"] = "Recipe" },
		"document missing": func(m map[string]any) { delete(m, "document") },
	}
	for name, mutate := range cases {
		m := validBlogSubmission()
		mutate(m)
		if _, err := validateBlogSubmission(m, validBlogSnapshot()); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestValidateBlogSubmissionNoTemplatesMeansNoPresentation(t *testing.T) {
	m := validBlogSubmission()
	m["presentation"] = map[string]any{"template": "", "options": map[string]any{}}
	out, err := validateBlogSubmission(m, blogSnapshot{ImageIDs: map[int]bool{110: true}})
	if err != nil {
		t.Fatal(err)
	}
	if out["presentation"] != nil {
		t.Fatal("expected nil presentation")
	}
	m = validBlogSubmission()
	if _, err := validateBlogSubmission(m, blogSnapshot{ImageIDs: map[int]bool{110: true}}); err == nil {
		t.Fatal("a template on a site with none synced must be rejected")
	}
}

func TestValidateBlogSubmissionDefaultsAndTrims(t *testing.T) {
	m := validBlogSubmission()
	doc := m["document"].(map[string]any)
	doc["language"] = ""
	delete(doc, "schema_type")
	blogSection(m)["id"] = ""
	m["seo"].(map[string]any)["meta_title"] = strings.Repeat("a", 300)
	out, err := validateBlogSubmission(m, validBlogSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	got := out["document"].(map[string]any)
	if got["language"] != "vi" || got["schema_type"] != "Article" {
		t.Fatalf("defaults not applied: %v %v", got["language"], got["schema_type"])
	}
	if got["sections"].([]any)[0].(map[string]any)["id"] != "s1" {
		t.Fatal("section id default s1")
	}
	if len(out["seo"].(map[string]any)["meta_title"].(string)) != 255 {
		t.Fatal("meta_title must be cut to 255")
	}
}

func TestBlogSnapshotFromRequest(t *testing.T) {
	snap := blogSnapshotFromRequest(map[string]any{"snapshot": map[string]any{
		"website":   map[string]any{"language": "en"},
		"templates": []any{map[string]any{"key": "editorial"}, map[string]any{"key": ""}},
		"images":    []any{map[string]any{"id": float64(7)}, map[string]any{"id": "8"}, map[string]any{"id": float64(0)}},
	}})
	if snap.Language != "en" || len(snap.TemplateKeys) != 1 || !snap.ImageIDs[7] || !snap.ImageIDs[8] || snap.ImageIDs[0] {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
}

func TestBlogCollectorKeepsLastValidReport(t *testing.T) {
	c := NewBlogDocumentCollector(validBlogSnapshot())
	if res := c.Execute(nil, map[string]any{"reply": "x"}); !res.IsError || !strings.Contains(res.ForLLM, "MODEL_OUTPUT_INVALID") {
		t.Fatalf("invalid submission must be an error result: %+v", res)
	}
	if c.Report() != nil {
		t.Fatal("no report yet")
	}
	if res := c.Execute(nil, validBlogSubmission()); res.IsError {
		t.Fatalf("valid rejected: %s", res.ForLLM)
	}
	report := c.Report()
	if report == nil || report["reply"] != "Đã viết xong." {
		t.Fatal("report expected")
	}
	report["reply"] = "mutated"
	if c.Report()["reply"] != "Đã viết xong." {
		t.Fatal("Report must return a clone")
	}
}
