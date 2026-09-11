package tekshot

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func draftContentDescription(t *testing.T) string {
	t.Helper()
	schema := NewDraftBatchCollectorTool().Parameters()
	posts := schema["properties"].(map[string]any)["posts"].(map[string]any)
	props := posts["items"].(map[string]any)["properties"].(map[string]any)
	return props["content"].(map[string]any)["description"].(string)
}

func TestDraftContentDescriptionDropsThePromoTemplate(t *testing.T) {
	description := draftContentDescription(t)
	// Khuôn cũ ép mọi bài thành bài quảng cáo món ~1.000 ký tự, độn tính từ
	// cảm quan cho từng mục và thêm một nhịp "bán cảm xúc".
	for _, unwanted := range []string{"800-1000", "will be rejected", "sells the feeling", "2-3 clauses of sensory detail"} {
		if strings.Contains(description, unwanted) {
			t.Fatalf("content description still carries %q:\n%s", unwanted, description)
		}
	}
}

func TestDraftContentDescriptionShapesByPostType(t *testing.T) {
	description := draftContentDescription(t)
	// Người viết không còn tự tra: bảng dữ kiện đến từ bước tra cứu riêng.
	for _, unwanted := range []string{"RESEARCH FIRST", "web_search", "vault_search"} {
		if strings.Contains(description, unwanted) {
			t.Fatalf("content description still sends the writer to research (%q):\n%s", unwanted, description)
		}
	}
	for _, want := range []string{
		"LENGTH FOLLOWS THE MATERIAL",
		"RESEARCHED FACTS",
		"never invent",
		"RECIPE / HOW-TO",
		"every step",
		"OFFER / MENU / PRICE",
		"KNOWLEDGE / B2B / RECRUITMENT / STORY",
		"EVERY SENTENCE does a job",
		"not the voice",
		"KEEP them verbatim",
		// Lần đo thứ hai: bài trà sữa in ra "Nguồn hiện có… chưa có bước…",
		// bài waffle giữ nguyên cup/tablespoon/°F của công thức Mỹ.
		"NEVER mention the source",
		"metric units",
		// Waffle r2 nhồi calo và thông số máy, rồi giải thích số phút lệch nhau.
		"skip trivia",
		"the checklist wins",
		"never discuss",
	} {
		if !strings.Contains(description, want) {
			t.Fatalf("content description missing %q:\n%s", want, description)
		}
	}
}

func TestDraftRunRequestUsesALeanWriterContext(t *testing.T) {
	args := map[string]any{
		"workspace":    map[string]any{"label": "Quán Mẫu"},
		"source_items": []any{map[string]any{"source_index": 1, "checklist_item": "A", "source_title": "A", "source_brief": "B"}},
	}
	req := draftRunRequest(args, defaultTimezone, "tekshot-drupal-user-1", "tekshot:draft:x", NewDraftBatchCollectorTool())

	// Persona trợ lý chat (SOUL/AGENTS/USER.md, luật NO_REPLY, cron…) không có
	// chỗ trong một lượt viết caption.
	if !req.LightContext {
		t.Fatalf("draft run must skip the chat persona context files")
	}
	if req.Channel != "tekshot_job" || req.ChannelType != "tekshot" {
		t.Fatalf("draft run should use the tekshot job channel, got %q/%q", req.Channel, req.ChannelType)
	}
	// Người viết chỉ có một việc là nộp bài: tool tra cứu bị gỡ, lượt nộp bị ép.
	if len(req.ToolAllow) != 1 || req.ToolAllow[0] != draftWriteNoTools {
		t.Fatalf("writer must not carry research tools: %v", req.ToolAllow)
	}
	if req.ToolChoice == nil || req.ToolChoice.Name != finalToolName || req.MaxIterations != 1 || len(req.EphemeralTools) != 1 {
		t.Fatalf("writer must be one forced submission: %+v", req)
	}
	if !strings.Contains(req.Message, "biên tập viên") || !strings.Contains(req.Message, "Quán Mẫu") {
		t.Fatalf("draft prompt should open with the page's editor persona:\n%s", req.Message)
	}
	if !strings.Contains(req.Message, "đã được tra sẵn") || strings.Contains(req.Message, "tra Vault rồi web") {
		t.Fatalf("persona should hand the writer researched facts, not send it searching:\n%s", req.Message)
	}
}

func TestDraftWriterPersonaWithoutPageName(t *testing.T) {
	persona := draftWriterPersona(map[string]any{})
	if !strings.Contains(persona, "biên tập viên") || strings.Contains(persona, `""`) {
		t.Fatalf("persona should read cleanly without a page name: %q", persona)
	}
}

func TestDraftReviewPromptCarriesConcreteCriteria(t *testing.T) {
	prompt := buildDraftReviewPrompt()
	if strings.Contains(prompt, "tra Vault rồi web") {
		t.Fatalf("editor must not search; research already ran:\n%s", prompt)
	}
	for _, want := range []string{
		"Giữ nguyên mọi dữ kiện đã tra được",
		"page khác",
		"RESEARCHED FACTS",
		"3 dòng đầu",
		"tính từ",
		"bảng kê",
		"nhắc tới nguồn",
		"chi tiết vặt",
		"lệch",
		"đơn vị",
		"ĐẠT",
		finalToolName,
		"Không bịa",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("review prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestDraftReviewRequestStaysInTheSameSession(t *testing.T) {
	// Gốc cố tình mang tool tra cứu: lượt biên tập phải tự gỡ, không dựa vào gốc.
	first := agent.RunRequest{SessionKey: "tekshot:draft:x", RunID: "run-1", Message: "viết bài", MaxIterations: 1, ToolAllow: tekshotDraftResearchToolAllow(), ToolChoice: &providers.ToolChoice{Mode: "function", Name: finalToolName}}
	review := draftReviewRequest(first)

	// Cùng session để model thấy lại nguồn và chính bài nó vừa nộp.
	if review.SessionKey != first.SessionKey {
		t.Fatalf("review must reuse the draft session, got %q", review.SessionKey)
	}
	if review.RunID == first.RunID || review.RunID == "" {
		t.Fatalf("review needs its own run id, got %q", review.RunID)
	}
	if review.ToolChoice != nil {
		t.Fatalf("review must be free to answer ĐẠT without calling a tool")
	}
	if len(review.ToolAllow) != 1 || review.ToolAllow[0] != draftWriteNoTools {
		t.Fatalf("editor must not carry research tools: %v", review.ToolAllow)
	}
	if review.MaxIterations != draftReviewIterations || review.Message != buildDraftReviewPrompt() {
		t.Fatalf("review request not built from the review prompt: %+v", review)
	}
}

func TestDraftContentChanged(t *testing.T) {
	before := map[string]any{"posts": []any{map[string]any{"content": "A"}}}
	same := map[string]any{"posts": []any{map[string]any{"content": "A"}}}
	edited := map[string]any{"posts": []any{map[string]any{"content": "B"}}}
	if draftContentChanged(before, same) {
		t.Fatalf("identical content reported as changed")
	}
	if !draftContentChanged(before, edited) {
		t.Fatalf("edited content not detected")
	}
}

func TestDraftContentChangedReadsTheCollectorsRealShape(t *testing.T) {
	// validateDraftBatch dựng posts là []map[string]any, không phải []any.
	before := map[string]any{"posts": []map[string]any{{"content": "A"}}}
	edited := map[string]any{"posts": []map[string]any{{"content": "B"}}}
	if !draftContentChanged(before, edited) {
		t.Fatalf("edited content not detected on the collector's batch shape")
	}
}
