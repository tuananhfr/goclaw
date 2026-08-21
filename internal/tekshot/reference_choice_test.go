package tekshot

import (
	"strings"
	"testing"
)

func choiceFixture() []referenceLibraryItem {
	return []referenceLibraryItem{
		{ID: 12, URL: "https://x/a.jpg", Description: "Tô bún bò trên bàn gỗ."},
		{ID: 15, URL: "https://x/b.jpg", Description: "Mặt tiền quán buổi tối."},
	}
}

func TestCapReferenceItems(t *testing.T) {
	items := choiceFixture()
	if got := capReferenceItems(items, 5); len(got) != 2 {
		t.Fatalf("under the cap must pass through, got %d", len(got))
	}
	if got := capReferenceItems(items, 1); len(got) != 1 || got[0].ID != 12 {
		t.Fatalf("cap must keep the head of the list, got %+v", got)
	}
	if got := capReferenceItems(items, 0); len(got) != 2 {
		t.Fatalf("a non-positive cap must disable capping, got %d", len(got))
	}
}

func TestBuildReferenceChoicePromptListsEveryItem(t *testing.T) {
	prompt := buildReferenceChoicePrompt("Ảnh combo buổi sáng", choiceFixture())
	for _, want := range []string{
		"Ảnh combo buổi sáng",
		"id 12: Tô bún bò trên bàn gỗ.",
		"id 15: Mặt tiền quán buổi tối.",
		`{"id": 0}`,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q\n---\n%s", want, prompt)
		}
	}
	// URL không được lộ: lượt này chọn bằng mô tả, không được tải gì.
	if strings.Contains(prompt, "https://x/a.jpg") {
		t.Fatalf("prompt must not carry image URLs\n---\n%s", prompt)
	}
}

func TestParseReferenceChoiceAcceptsPlainJSON(t *testing.T) {
	if got := parseReferenceChoice(`{"id": 15}`, choiceFixture()); got != 15 {
		t.Fatalf("expected 15, got %d", got)
	}
}

func TestParseReferenceChoiceAcceptsFencedJSON(t *testing.T) {
	reply := "Ảnh này hợp nhất.\n```json\n{\"id\": 12}\n```\n"
	if got := parseReferenceChoice(reply, choiceFixture()); got != 12 {
		t.Fatalf("expected 12, got %d", got)
	}
}

func TestParseReferenceChoiceRejectsUnknownID(t *testing.T) {
	if got := parseReferenceChoice(`{"id": 99}`, choiceFixture()); got != 0 {
		t.Fatalf("an id outside the manifest must be refused, got %d", got)
	}
}

func TestParseReferenceChoiceHandlesNoFit(t *testing.T) {
	for _, reply := range []string{`{"id": 0}`, "Không ảnh nào hợp cả.", ""} {
		if got := parseReferenceChoice(reply, choiceFixture()); got != 0 {
			t.Fatalf("reply %q must yield 0, got %d", reply, got)
		}
	}
}
