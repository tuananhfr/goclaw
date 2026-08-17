package tekshot

import "testing"

func TestSeedCommentsCollectorRejectsEmptyList(t *testing.T) {
	collector := NewSeedCommentsCollectorTool()
	result := collector.Execute(t.Context(), map[string]any{"comments": []any{}})
	if result == nil || !result.IsError {
		t.Fatalf("expected an error result for an empty comment list, got %#v", result)
	}
	if collector.Report() != nil {
		t.Fatalf("collector must not capture an invalid submission")
	}
}

func TestSeedCommentsCollectorRejectsNonPositiveDelay(t *testing.T) {
	collector := NewSeedCommentsCollectorTool()
	result := collector.Execute(t.Context(), map[string]any{
		"comments": []any{
			map[string]any{"message": "Menu đầy đủ ở đây nhé", "delay_minutes": 0},
		},
	})
	if result == nil || !result.IsError {
		t.Fatalf("expected an error result for delay_minutes = 0, got %#v", result)
	}
}

func TestSeedCommentsCollectorRejectsEmptyMessage(t *testing.T) {
	collector := NewSeedCommentsCollectorTool()
	result := collector.Execute(t.Context(), map[string]any{
		"comments": []any{
			map[string]any{"message": "   ", "delay_minutes": 5},
		},
	})
	if result == nil || !result.IsError {
		t.Fatalf("expected an error result for a blank message, got %#v", result)
	}
}

func TestSeedCommentsCollectorCapturesValidSubmission(t *testing.T) {
	collector := NewSeedCommentsCollectorTool()
	result := collector.Execute(t.Context(), map[string]any{
		"comments": []any{
			map[string]any{"message": "Menu đầy đủ ở đây nhé cả nhà", "delay_minutes": 4},
			map[string]any{"message": "Quán mở tới 22h ạ", "delay_minutes": 11},
		},
	})
	if result == nil || result.IsError {
		t.Fatalf("expected a successful capture, got %#v", result)
	}
	report := collector.Report()
	if report == nil {
		t.Fatal("collector did not capture the submission")
	}
	comments, _ := report["comments"].([]any)
	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(comments))
	}
}
