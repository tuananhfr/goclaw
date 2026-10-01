package tekshot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseStudioImageRequest_OrdersBaseFirstAndNormalises(t *testing.T) {
	req, err := parseStudioImageRequest(map[string]any{
		"instructions": "You are the image designer.",
		"prompt":       "Yêu cầu: làm poster",
		"message":      "làm poster",
		"size":         "999x999",
		"media": []any{
			map[string]any{"path": "/tmp/ref.png", "role": "reference"},
			map[string]any{"path": "/tmp/base.png", "role": "base"},
			map[string]any{"role": "reference"},
		},
		"tagged_skills": []any{" poster ", "poster", ""},
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if req.Prompt != "Yêu cầu: làm poster" || req.Size != "auto" {
		t.Fatalf("prompt=%q size=%q", req.Prompt, req.Size)
	}
	if len(req.Media) != 2 || req.Media[0].Role != "base" || req.Media[0].Path != "/tmp/base.png" {
		t.Fatalf("media = %+v, want base first and pathless entries dropped", req.Media)
	}
	if len(req.TaggedSkills) != 1 || req.TaggedSkills[0] != "poster" {
		t.Fatalf("skills = %v", req.TaggedSkills)
	}
}

func TestParseStudioImageRequest_FallsBackToMessageAndRejectsEmpty(t *testing.T) {
	req, err := parseStudioImageRequest(map[string]any{"message": "vẽ bánh mì", "size": "1024x1360"})
	if err != nil || req.Prompt != "vẽ bánh mì" || req.Size != "1024x1360" {
		t.Fatalf("req=%+v err=%v", req, err)
	}
	if _, err := parseStudioImageRequest(map[string]any{}); err == nil {
		t.Fatal("want error for empty prompt")
	}
}

func TestAppendSkillsBlock(t *testing.T) {
	got := appendSkillsBlock("Yêu cầu: x", []loadedSkill{{Name: "poster", Content: "Dùng chữ to."}})
	want := "Yêu cầu: x\n\nKỹ năng người dùng gắn:\n### poster\nDùng chữ to."
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if appendSkillsBlock("x", nil) != "x" {
		t.Fatal("no skills must leave the prompt unchanged")
	}
}

func TestAppendLibraryLine(t *testing.T) {
	got := appendLibraryLine("Yêu cầu: x", referenceLibraryItem{ID: 3, Description: "Ảnh chụp combo bánh mì trên bàn gỗ"})
	if !strings.HasSuffix(got, "\n\nẢnh kho (đính kèm cuối): Ảnh chụp combo bánh mì trên bàn gỗ") {
		t.Fatalf("got %q", got)
	}
}

func TestResolveStudioMediaPath(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "a.png")
	if err := os.WriteFile(inside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveStudioMediaPath(inside, []string{root}); err != nil || got != inside {
		t.Fatalf("inside: got %q err %v", got, err)
	}
	if _, err := resolveStudioMediaPath(filepath.Join(root, "..", "evil.png"), []string{root}); err == nil {
		t.Fatal("path outside roots must be rejected")
	}
	if _, err := resolveStudioMediaPath("relative.png", []string{root}); err == nil {
		t.Fatal("relative path must be rejected")
	}
}
