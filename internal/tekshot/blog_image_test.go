package tekshot

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
)

func sectionSet() map[string]bool { return map[string]bool{"s1": true, "s2": true} }

func validPlan() []any {
	return []any{
		map[string]any{"target": "featured", "prompt": "Ảnh bìa xưởng gỗ", "alt": "Xưởng gỗ", "caption": ""},
		map[string]any{"target": "section:s2", "prompt": "Điểm đếm trên dây chuyền", "alt": "Camera đếm", "caption": "Một điểm đếm"},
	}
}

func TestValidateBlogImagePlanAcceptsValid(t *testing.T) {
	out, err := validateBlogImagePlan(validPlan(), sectionSet())
	if err != nil {
		t.Fatalf("expected valid: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(out))
	}
	first := out[0].(map[string]any)
	if first["media"] != nil {
		t.Fatal("media must start nil")
	}
	if first["target"] != "featured" || first["alt"] != "Xưởng gỗ" {
		t.Fatalf("unexpected entry: %v", first)
	}
}

func TestValidateBlogImagePlanRejects(t *testing.T) {
	cases := map[string]any{
		"unknown target":  []any{map[string]any{"target": "footer", "prompt": "x", "alt": "y"}},
		"missing section": []any{map[string]any{"target": "section:s9", "prompt": "x", "alt": "y"}},
		"empty prompt":    []any{map[string]any{"target": "featured", "prompt": " ", "alt": "y"}},
		"empty alt":       []any{map[string]any{"target": "featured", "prompt": "x", "alt": ""}},
		"two featured": []any{
			map[string]any{"target": "featured", "prompt": "x", "alt": "y"},
			map[string]any{"target": "featured", "prompt": "x", "alt": "y"},
		},
		"same section twice": []any{
			map[string]any{"target": "section:s1", "prompt": "x", "alt": "y"},
			map[string]any{"target": "section:s1", "prompt": "x", "alt": "y"},
		},
		"not a list": map[string]any{"target": "featured"},
	}
	for name, plan := range cases {
		if _, err := validateBlogImagePlan(plan, sectionSet()); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestValidateBlogImagePlanCapsAtSix(t *testing.T) {
	plan := []any{map[string]any{"target": "featured", "prompt": "x", "alt": "y"}}
	for i := 0; i < 8; i++ {
		plan = append(plan, map[string]any{"target": "section:s1", "prompt": "x", "alt": "y"})
	}
	if _, err := validateBlogImagePlan(plan, sectionSet()); err == nil {
		t.Fatal("over the cap must be rejected")
	}
}

func TestValidateBlogImagePlanEmptyIsAllowed(t *testing.T) {
	out, err := validateBlogImagePlan(nil, sectionSet())
	if err != nil || len(out) != 0 {
		t.Fatalf("empty plan must pass: %v %v", out, err)
	}
}

func TestBlogImageMediaEntry(t *testing.T) {
	if blogImageMediaEntry(nil) != nil {
		t.Fatal("no media means nil")
	}
	entry := blogImageMediaEntry([]agent.MediaResult{{Path: "a/b.png", ContentType: "image/png"}})
	got, ok := entry.(map[string]any)
	if !ok {
		t.Fatalf("expected a map, got %T", entry)
	}
	if got["path"] != "a/b.png" || got["mime_type"] != "image/png" || got["filename"] != "b.png" {
		t.Fatalf("unexpected: %v", got)
	}
}

func TestBlogImageMediaEntryKeepsTheLastImage(t *testing.T) {
	entry := blogImageMediaEntry([]agent.MediaResult{{Path: "a.png"}, {Path: "b.png"}}).(map[string]any)
	if entry["path"] != "b.png" {
		t.Fatalf("expected the last image, got %v", entry["path"])
	}
}

func TestBlogImagePromptForbidsTextAndLogos(t *testing.T) {
	prompt := buildBlogImagePrompt(map[string]any{"prompt": "a small woodworking shop", "alt": "x"})
	for _, want := range []string{"a small woodworking shop", "create_image", "No text", "no logo"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
}
