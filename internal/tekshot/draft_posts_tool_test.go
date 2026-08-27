package tekshot

import (
	"strings"
	"testing"
)

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
	}, false)
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
	}, false)
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
	}, false)
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
	}, false)
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
	}, false)
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
	}, false)
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

func TestValidateDraftBatchRejectsFractionalSourceIndex(t *testing.T) {
	post := validPost(1)
	post["source_index"] = 1.5

	_, err := validateDraftBatch(map[string]any{
		"title":   "Chunk 1",
		"summary": "1 post",
		"posts":   []any{post},
	}, false)
	if err == nil {
		t.Fatal("expected fractional source_index to be rejected")
	}
}

func TestSourceItemsArgRejectsFractionalSourceIndex(t *testing.T) {
	items := sourceItemsArg([]any{map[string]any{
		"source_index":   1.5,
		"checklist_item": "A",
	}})
	if len(items) != 0 {
		t.Fatalf("expected no source items, got %#v", items)
	}
}

func TestBuildPromptIdentifiesSinglePostDraftMode(t *testing.T) {
	prompt := buildPrompt(map[string]any{
		"source_text": "This duplicate source must not be rendered.",
		"source_items": []any{map[string]any{
			"source_index":   1,
			"checklist_item": "Specific topic",
			"source_title":   "Specific topic",
			"source_brief":   "Hook: Specific hook\nBody: Specific detail",
			"source_text":    "Title: Specific topic\nBrief: Hook: Specific hook\nBody: Specific detail\nPILLAR: Daily post",
		}},
	}, defaultTimezone)
	if !strings.Contains(prompt, "SINGLE-POST CONTENT WRITER CONTRACT") {
		t.Fatalf("single-source prompt did not contain single-post guidance: %s", prompt)
	}
	for _, want := range []string{
		"The only creative output fields are content and hashtags.",
		"Do not flatten it into generic marketing copy",
		"Work in this order:",
		"only call submit_draft_batch after the self-check passes",
		"source_title (read-only): Specific topic",
		"source_brief (read-only): Hook: Specific hook",
		"PILLAR: Daily post",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("single-source prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, unwanted := range []string{
		"plan the batch",
		"contrarian observation",
		"This duplicate source must not be rendered.",
	} {
		if strings.Contains(prompt, unwanted) {
			t.Fatalf("single-source prompt should not contain %q:\n%s", unwanted, prompt)
		}
	}
}

func TestSourceItemsArgRetainsCanonicalTitleAndBrief(t *testing.T) {
	items := sourceItemsArg([]any{map[string]any{
		"source_index":   4,
		"checklist_item": "Checklist title",
		"source_title":   "Canonical title",
		"source_brief":   "Canonical brief",
		"source_text":    "Title: Canonical title\nBrief: Canonical brief\nPILLAR: Daily post",
	}})
	if len(items) != 1 {
		t.Fatalf("expected one source item, got %#v", items)
	}
	if items[0].SourceTitle != "Canonical title" || items[0].SourceBrief != "Canonical brief" {
		t.Fatalf("canonical title/brief were lost: %#v", items[0])
	}
	if got := sourceSupportingText(items[0]); got != "PILLAR: Daily post" {
		t.Fatalf("supporting source = %q, want PILLAR only", got)
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
