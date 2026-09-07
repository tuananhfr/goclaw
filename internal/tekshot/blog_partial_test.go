package tekshot

import (
	"strings"
	"testing"
)

func TestSpliceBlogSectionReplacesOnlyThatSection(t *testing.T) {
	orig := rewriteBaseDocument()
	replacement := map[string]any{"id": "s2", "heading": "B2", "level": float64(3), "blocks": []any{map[string]any{"type": "paragraph", "text": "b2"}}}
	spliced, err := spliceBlogSection(orig, replacement)
	if err != nil {
		t.Fatal(err)
	}
	if err := enforceBlogRewriteScope(orig, spliced, "section:s2"); err != nil {
		t.Fatalf("splice must satisfy the scope guard: %v", err)
	}
	if spliced["sections"].([]any)[1].(map[string]any)["heading"] != "B2" {
		t.Fatal("section not replaced")
	}
	if orig["sections"].([]any)[1].(map[string]any)["heading"] != "B" {
		t.Fatal("original must not be mutated")
	}
	if _, err := spliceBlogSection(orig, map[string]any{"id": "zzz"}); err == nil {
		t.Fatal("unknown section must fail")
	}
}

func TestBlogSectionCollectorPinsTheId(t *testing.T) {
	c := NewBlogSectionCollector("s2")
	if res := c.Execute(nil, map[string]any{"reply": "x", "section": map[string]any{"id": "s1", "heading": "h"}}); !res.IsError {
		t.Fatal("wrong id must be rejected")
	}
	if res := c.Execute(nil, map[string]any{"reply": "", "section": map[string]any{"id": "s2"}}); !res.IsError {
		t.Fatal("empty reply must be rejected")
	}
	if res := c.Execute(nil, map[string]any{"reply": "ok", "section": map[string]any{"heading": "h"}}); res.IsError {
		t.Fatalf("missing id defaults to the scope: %s", res.ForLLM)
	}
	if c.Report()["section"].(map[string]any)["id"] != "s2" {
		t.Fatal("id must be pinned to the scope")
	}
	if !strings.Contains(c.Description(), "s2") {
		t.Fatal("description must name the section")
	}
}

func TestBlogPresentationCollectorChecksTheSnapshot(t *testing.T) {
	c := NewBlogPresentationCollector(validBlogSnapshot())
	if res := c.Execute(nil, map[string]any{"reply": "x", "presentation": map[string]any{"template": "magazine"}}); !res.IsError {
		t.Fatal("unsynced template must be rejected")
	}
	if res := c.Execute(nil, map[string]any{"reply": "x", "presentation": map[string]any{"template": ""}}); !res.IsError {
		t.Fatal("empty template must be rejected when templates exist")
	}
	if res := c.Execute(nil, map[string]any{"reply": "x", "presentation": map[string]any{"template": "tutorial"}}); res.IsError {
		t.Fatalf("valid template rejected: %s", res.ForLLM)
	}
	if c.Report()["presentation"].(map[string]any)["template"] != "tutorial" {
		t.Fatal("template not captured")
	}
	none := NewBlogPresentationCollector(blogSnapshot{})
	if res := none.Execute(nil, map[string]any{"reply": "x", "presentation": map[string]any{"template": ""}}); res.IsError {
		t.Fatal("no templates synced: empty is the only valid answer")
	}
	if none.Report()["presentation"] != nil {
		t.Fatal("expected nil presentation")
	}
}

func TestNormalizedPresentation(t *testing.T) {
	snap := validBlogSnapshot()
	if p, err := normalizedPresentation(map[string]any{"template": "editorial", "options": map[string]any{"x": 1}}, snap); err != nil || p["template"] != "editorial" {
		t.Fatalf("unexpected: %v %v", p, err)
	}
	if p, err := normalizedPresentation(nil, snap); err != nil || p != nil {
		t.Fatalf("nil must stay nil: %v %v", p, err)
	}
	if _, err := normalizedPresentation(map[string]any{"template": "magazine"}, snap); err == nil {
		t.Fatal("unsynced template must fail")
	}
}
