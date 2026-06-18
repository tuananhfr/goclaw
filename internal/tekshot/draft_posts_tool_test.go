package tekshot

import "testing"

func TestValidateDraftBatchAcceptsValidPayload(t *testing.T) {
	batch, err := validateDraftBatch(map[string]any{
		"title":   "Week 1",
		"summary": "3 posts from the imported checklist",
		"posts": []any{
			map[string]any{
				"title":          "Post 1",
				"brief":          "Short planning note",
				"pillar":         "Awareness",
				"content":        "Complete caption",
				"publish_at":     "2026-06-18T08:30:00",
				"publish_date":   "2026-06-18",
				"publish_time":   "08:30",
				"checklist_item": "Original row summary",
			},
		},
	})
	if err != nil {
		t.Fatalf("validateDraftBatch returned error: %v", err)
	}
	if batch["title"] != "Week 1" {
		t.Fatalf("unexpected title: %#v", batch["title"])
	}
}

func TestValidateDraftBatchRejectsExtraKeys(t *testing.T) {
	_, err := validateDraftBatch(map[string]any{
		"title":   "Week 1",
		"summary": "Summary",
		"posts": []any{
			map[string]any{
				"title":          "Post 1",
				"brief":          "",
				"pillar":         "",
				"content":        "Complete caption",
				"publish_at":     "",
				"publish_date":   "",
				"publish_time":   "",
				"checklist_item": "Original row summary",
				"unexpected":     "nope",
			},
		},
	})
	if err == nil {
		t.Fatal("expected validation error for unexpected post key")
	}
}
