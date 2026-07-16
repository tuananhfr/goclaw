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
				"hashtags":       "#Planning",
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
				"hashtags":       "",
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

func TestValidateBatchSourceIndexesAcceptsExactMatch(t *testing.T) {
	batch, err := validateDraftBatch(map[string]any{
		"title":   "Chunk 1",
		"summary": "2 posts",
		"posts": []any{
			validPost(2),
			validPost(1),
		},
	})
	if err != nil {
		t.Fatalf("validateDraftBatch returned error: %v", err)
	}

	err = validateBatchSourceIndexes(batch, []SourceItem{
		{SourceIndex: 1, ChecklistItem: "A"},
		{SourceIndex: 2, ChecklistItem: "B"},
	})
	if err != nil {
		t.Fatalf("validateBatchSourceIndexes returned error: %v", err)
	}
}

func TestValidateBatchSourceIndexesRejectsMissingIndex(t *testing.T) {
	batch, err := validateDraftBatch(map[string]any{
		"title":   "Chunk 1",
		"summary": "1 post",
		"posts": []any{
			validPost(1),
		},
	})
	if err != nil {
		t.Fatalf("validateDraftBatch returned error: %v", err)
	}

	err = validateBatchSourceIndexes(batch, []SourceItem{
		{SourceIndex: 1, ChecklistItem: "A"},
		{SourceIndex: 2, ChecklistItem: "B"},
	})
	if err == nil {
		t.Fatal("expected count mismatch for missing source_index")
	}
}

func TestValidateBatchSourceIndexesRejectsDuplicateIndex(t *testing.T) {
	batch, err := validateDraftBatch(map[string]any{
		"title":   "Chunk 1",
		"summary": "2 posts",
		"posts": []any{
			validPost(1),
			validPost(1),
		},
	})
	if err != nil {
		t.Fatalf("validateDraftBatch returned error: %v", err)
	}

	err = validateBatchSourceIndexes(batch, []SourceItem{
		{SourceIndex: 1, ChecklistItem: "A"},
		{SourceIndex: 2, ChecklistItem: "B"},
	})
	if err == nil {
		t.Fatal("expected duplicate source_index error")
	}
}

func TestValidateBatchSourceIndexesRejectsOutsideIndex(t *testing.T) {
	batch, err := validateDraftBatch(map[string]any{
		"title":   "Chunk 1",
		"summary": "1 post",
		"posts": []any{
			validPost(99),
		},
	})
	if err != nil {
		t.Fatalf("validateDraftBatch returned error: %v", err)
	}

	err = validateBatchSourceIndexes(batch, []SourceItem{
		{SourceIndex: 1, ChecklistItem: "A"},
	})
	if err == nil {
		t.Fatal("expected outside source_index error")
	}
}

func validPost(sourceIndex int) map[string]any {
	return map[string]any{
		"title":          "Post",
		"brief":          "Brief",
		"pillar":         "Pillar",
		"content":        "Complete caption",
		"hashtags":       "#Planning",
		"publish_at":     "",
		"publish_date":   "",
		"publish_time":   "",
		"checklist_item": "Original row summary",
		"source_index":   sourceIndex,
	}
}
func TestTekshotDraftResearchToolAllowIncludesResearchTools(t *testing.T) {
	allowed := tekshotDraftResearchToolAllow()
	allowedSet := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		allowedSet[name] = true
	}

	required := []string{
		"skill_search",
		"vault_search",
		"vault_read",
		"web_search",
		"web_fetch",
		"memory_search",
		"memory_get",
		"memory_expand",
		"knowledge_graph_search",
		"read_document",
		"read_image",
		"datetime",
	}
	for _, name := range required {
		if !allowedSet[name] {
			t.Fatalf("expected %s in Tekshot draft research allow-list", name)
		}
	}
}

func TestTekshotDraftResearchToolAllowExcludesSideEffectTools(t *testing.T) {
	allowed := tekshotDraftResearchToolAllow()
	allowedSet := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		allowedSet[name] = true
	}

	denied := []string{"exec", "write_file", "edit", "message", "cron", "team_tasks", "create_image"}
	for _, name := range denied {
		if allowedSet[name] {
			t.Fatalf("did not expect %s in Tekshot draft research allow-list", name)
		}
	}
}
