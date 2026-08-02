package tekshot

import (
	"strings"
	"testing"
)

func TestReferenceLibraryFromRequestParsesManifest(t *testing.T) {
	request := map[string]any{
		"reference_library": []any{
			map[string]any{"id": float64(12), "url": "https://tekshot.localhost/files/ref/a.jpg", "description": "Tô bún bò trên bàn gỗ."},
			map[string]any{"id": float64(15), "url": "http://tekshot.localhost/files/ref/b.png", "description": "Mặt tiền quán buổi tối."},
		},
	}
	items := referenceLibraryFromRequest(request)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].ID != 12 || items[0].Description != "Tô bún bò trên bàn gỗ." {
		t.Fatalf("item[0] parsed wrong: %+v", items[0])
	}
	if items[1].URL != "http://tekshot.localhost/files/ref/b.png" {
		t.Fatalf("item[1] parsed wrong: %+v", items[1])
	}
}

func TestReferenceLibraryFromRequestSkipsInvalidEntries(t *testing.T) {
	request := map[string]any{
		"reference_library": []any{
			map[string]any{"id": float64(0), "url": "https://x/a.jpg", "description": "id không hợp lệ"},
			map[string]any{"id": float64(3), "url": "", "description": "thiếu url"},
			map[string]any{"id": float64(4), "url": "ftp://x/a.jpg", "description": "sai scheme"},
			map[string]any{"id": float64(5), "url": "https://x/a.jpg", "description": "   "},
			"not a map",
			map[string]any{"id": float64(6), "url": "https://x/ok.jpg", "description": "hợp lệ"},
		},
	}
	items := referenceLibraryFromRequest(request)
	if len(items) != 1 || items[0].ID != 6 {
		t.Fatalf("expected only item 6 to survive, got %+v", items)
	}
}

func TestReferenceLibraryFromRequestMissing(t *testing.T) {
	if items := referenceLibraryFromRequest(map[string]any{}); items != nil {
		t.Fatalf("expected nil for missing key, got %+v", items)
	}
	if items := referenceLibraryFromRequest(map[string]any{"reference_library": "oops"}); items != nil {
		t.Fatalf("expected nil for non-array, got %+v", items)
	}
}

func TestReferenceLibraryMediaFiles(t *testing.T) {
	items := []referenceLibraryItem{
		{ID: 12, URL: "https://x/files/a.jpg", Description: "a"},
		{ID: 15, URL: "https://x/files/b", Description: "b"},
	}
	files := referenceLibraryMediaFiles(items)
	if len(files) != 2 {
		t.Fatalf("expected 2 media files, got %d", len(files))
	}
	if files[0].Filename != "ref-lib-12" || files[0].Path != "https://x/files/a.jpg" {
		t.Fatalf("files[0] wrong: %+v", files[0])
	}
	for i, f := range files {
		if !strings.HasPrefix(f.MimeType, "image/") {
			t.Fatalf("files[%d] mime must be image/* for the media tag, got %q", i, f.MimeType)
		}
	}
}

func TestBuildReferenceLibraryBlock(t *testing.T) {
	items := []referenceLibraryItem{
		{ID: 12, URL: "https://x/a.jpg", Description: "Tô bún bò trên bàn gỗ."},
	}
	block := buildReferenceLibraryBlock(items)
	for _, want := range []string{
		"ref-lib-12: Tô bún bò trên bàn gỗ.",
		"reference_image_path",
		"<media:image>",
		"at most ONE",
		"EDITING",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("block missing %q\n---\n%s", want, block)
		}
	}
}
