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

func TestSpliceBlogBlockReplacesOnlyThatBlock(t *testing.T) {
	orig := blockBaseDocument()
	spliced, err := spliceBlogBlock(orig, "s1", 0, map[string]any{"type": "paragraph", "text": "Mới."})
	if err != nil {
		t.Fatal(err)
	}
	if err := enforceBlogRewriteScope(orig, spliced, "block:s1:0"); err != nil {
		t.Fatalf("splice must satisfy the scope guard: %v", err)
	}
	if orig["sections"].([]any)[0].(map[string]any)["blocks"].([]any)[0].(map[string]any)["text"] == "Mới." {
		t.Fatal("original must not be mutated")
	}
	for _, miss := range []struct {
		id    string
		index int
	}{{"s1", 2}, {"zz", 0}} {
		if _, err := spliceBlogBlock(orig, miss.id, miss.index, map[string]any{"type": "paragraph", "text": "x"}); err == nil {
			t.Errorf("%s:%d must fail", miss.id, miss.index)
		}
	}
	if _, err := spliceBlogBlock(orig, "s1", 0, nil); err == nil {
		t.Error("a nil block must fail")
	}
}

func TestBlogBlockCollectorKeepsTheBlockType(t *testing.T) {
	c := NewBlogBlockCollector("s1", 0, "paragraph", validBlogSnapshot())
	if res := c.Execute(nil, map[string]any{"reply": "x", "block": map[string]any{"type": "list", "items": []any{"a"}}}); !res.IsError {
		t.Fatal("another type must be rejected")
	}
	if res := c.Execute(nil, map[string]any{"reply": "", "block": map[string]any{"type": "paragraph", "text": "a"}}); !res.IsError {
		t.Fatal("an empty reply must be rejected")
	}
	if res := c.Execute(nil, map[string]any{"reply": "x", "block": map[string]any{"type": "paragraph", "text": " "}}); !res.IsError {
		t.Fatal("an empty paragraph must be rejected inside the run, where the model can still fix it")
	}
	if res := c.Execute(nil, map[string]any{"reply": "ok", "block": map[string]any{"text": "Gọn hơn."}}); res.IsError {
		t.Fatalf("a missing type defaults to the original: %s", res.ForLLM)
	}
	got := c.Report()["block"].(map[string]any)
	if got["type"] != "paragraph" || got["text"] != "Gọn hơn." {
		t.Fatalf("unexpected block: %v", got)
	}
	if !strings.Contains(c.Description(), "s1") {
		t.Fatal("description must name the section")
	}
	image := NewBlogBlockCollector("s1", 1, "image", validBlogSnapshot())
	if res := image.Execute(nil, map[string]any{"reply": "x", "block": map[string]any{"type": "image", "file_id": float64(999), "alt": "a"}}); !res.IsError {
		t.Fatal("an image outside the snapshot must be rejected")
	}
}
