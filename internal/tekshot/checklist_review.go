package tekshot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	TekshotJobTypeChecklistReview = "content_checklist_review"

	checklistReviewMaxIterations = 5
	checklistReviewMaxRows       = 30
)

// checklistReviewToolAllow chỉ đọc Vault: người chấm cần biết dữ kiện có thật,
// không cần lên web tìm thêm.
func checklistReviewToolAllow() []string {
	return []string{"vault_search", "vault_read"}
}

// runContentChecklistReview chấm điểm và soát lặp cho dòng người gõ tay hoặc dòng
// cũ chưa có điểm. Điểm chỉ là nhận xét cho người dùng, không chặn gì.
func (s *JobService) runContentChecklistReview(ctx context.Context, job *store.TekshotJob, request map[string]any) (any, string, error) {
	if s.agents == nil {
		return nil, "", fmt.Errorf("agent router is not configured")
	}
	rows := checklistReviewRows(request)
	if len(rows) == 0 {
		return nil, "", fmt.Errorf("content_checklist_review needs at least one row")
	}

	loop, err := s.agents.Get(store.WithTenantID(ctx, store.MasterTenantID), job.AgentKey)
	if err != nil {
		return nil, "", err
	}
	userID := "tekshot-" + job.ExternalUserID
	runCtx := store.WithTenantID(ctx, store.MasterTenantID)
	runCtx = store.WithUserID(runCtx, userID)
	runCtx = store.WithAgentKey(runCtx, job.AgentKey)

	// Phiên riêng, không thấy lịch sử của lượt lập kế hoạch: người chấm không
	// được biết người viết đã nghĩ gì.
	runID := uuid.NewString()
	result, err := loop.Run(runCtx, agent.RunRequest{
		SessionKey:    job.SessionKey + ":review:" + runID,
		Message:       buildChecklistReviewPrompt(request, rows),
		Channel:       "tekshot_job",
		ChannelType:   "tekshot",
		ChatID:        userID,
		PeerKind:      "direct",
		Addressed:     true,
		RunID:         runID,
		UserID:        userID,
		SenderID:      userID,
		ToolAllow:     checklistReviewToolAllow(),
		MaxIterations: checklistReviewMaxIterations,
		SkillFilter:   []string{},
		LightContext:  true,
		HistoryLimit:  1,
		TraceName:     "tekshot content checklist review",
		TraceTags:     []string{"tekshot", "content_checklist_review"},
	})
	if err != nil {
		return nil, "", err
	}
	reply := ""
	if result != nil {
		reply = result.Content
	}

	review, err := buildChecklistReviewResult(reply, rows, checklistHistoryFromRequest(request), checklistSiblingsFromRequest(request))
	if err != nil {
		return nil, "", err
	}
	scored, _ := review["rows"].([]any)
	return review, fmt.Sprintf("Reviewed %d checklist rows", len(scored)), nil
}

// checklistReviewRows đọc các dòng cần chấm; dòng không có id hoặc chủ đề bị bỏ.
func checklistReviewRows(request map[string]any) []map[string]any {
	raw, _ := request["rows"].([]any)
	rows := make([]map[string]any, 0, len(raw))
	for _, entry := range raw {
		row, ok := entry.(map[string]any)
		if !ok || int(numberFromMap(row, "id")) <= 0 || strings.TrimSpace(stringFromMap(row, "topic")) == "" {
			continue
		}
		rows = append(rows, row)
		if len(rows) == checklistReviewMaxRows {
			break
		}
	}
	return rows
}

func buildChecklistReviewPrompt(request map[string]any, rows []map[string]any) string {
	var sb strings.Builder
	sb.WriteString("You are an independent content editor reviewing planned Facebook post topics for one page. ")
	sb.WriteString("You did not plan these rows: judge only what is written, against the facts below.\n\n")

	profile := readBusinessProfile(request)
	sb.WriteString("## Page context\n")
	if pageName := strings.TrimSpace(stringFromMap(request, "page_name")); pageName != "" {
		sb.WriteString("- Page: " + pageName + "\n")
	}
	profile.writeProfile(&sb)
	if today := strings.TrimSpace(stringFromMap(request, "today")); today != "" {
		sb.WriteString("- Today: " + today + "\n")
	}
	sb.WriteString("\n")
	writeChecklistBlock(&sb, "## Reach facts (from the store's own Facebook pages)", request, "social_facts", true)
	writeChecklistBlock(&sb, "## Market research findings", request, "research", true)
	writeChecklistHistory(&sb, checklistHistoryFromRequest(request))
	writeChecklistSiblings(&sb, checklistSiblingsFromRequest(request))

	writeChecklistScoreRules(&sb)

	sb.WriteString("## Also classify\n")
	sb.WriteString("- kieu_hook, one of " + strings.Join(checklistHookTypes, ", ") + " — how the row's hook opens.\n")
	sb.WriteString("- cot_truyen, one of " + strings.Join(checklistStoryTypes, ", ") + " — the storyline.\n")
	sb.WriteString("When a row already carries a value, return it unchanged.\n\n")

	sb.WriteString("## Rows to review\n")
	for _, row := range rows {
		sb.WriteString(fmt.Sprintf("- id: %d\n", int(numberFromMap(row, "id"))))
		for _, field := range []string{"date", "topic", "hook", "body", "usp", "content_line", "tep_khach", "giai_doan", "kieu_hook", "cot_truyen"} {
			if value := strings.TrimSpace(stringFromMap(row, field)); value != "" {
				sb.WriteString("  " + field + ": " + value + "\n")
			}
		}
	}

	sb.WriteString("\n## Reply\n")
	sb.WriteString("Reply with JSON only, no preamble. Write every ly_do and nhan_xet in Vietnamese:\n")
	sb.WriteString(`{"rows":[{"id":0,"kieu_hook":"","cot_truyen":"","nhan_xet":"one sentence for the team","diem_chu_de":{"dung_luc":{"diem":0,"ly_do":""},"co_nguon":{"diem":0,"ly_do":""},"dung_thu_ban":{"diem":0,"ly_do":""},"hop_muc_tieu":{"diem":0,"ly_do":""},"khach_quan_tam":{"diem":0,"ly_do":""}}}]}`)
	sb.WriteString("\nOne entry per row id above.\n")
	return sb.String()
}

// buildChecklistReviewResult dựng kết quả trả về: điểm do model chấm nhưng tổng
// và cảnh báo lặp do code tính. Một dòng chấm hỏng bị bỏ điểm chứ không được đoán;
// không dòng nào chấm được thì cả job thất bại (fail closed).
func buildChecklistReviewResult(reply string, rows []map[string]any, history []checklistHistoryEntry, siblings []checklistSiblingPage) (map[string]any, error) {
	object, err := extractJSONObject(reply)
	if err != nil {
		return nil, fmt.Errorf("MODEL_OUTPUT_INVALID: %w", err)
	}
	var parsed struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := json.Unmarshal([]byte(object), &parsed); err != nil {
		return nil, fmt.Errorf("MODEL_OUTPUT_INVALID: %w", err)
	}
	byID := map[int]map[string]any{}
	for _, entry := range parsed.Rows {
		byID[int(numberFromMap(entry, "id"))] = entry
	}

	pick := func(given, proposed string, allowed []string) string {
		given = strings.ToUpper(strings.TrimSpace(given))
		if containsString(allowed, given) {
			return given
		}
		proposed = strings.ToUpper(strings.TrimSpace(proposed))
		if containsString(allowed, proposed) {
			return proposed
		}
		return ""
	}

	plan := make([]checklistPlanEntry, 0, len(rows))
	results := make([]map[string]any, 0, len(rows))
	scoredCount := 0
	for _, row := range rows {
		id := int(numberFromMap(row, "id"))
		entry := byID[id]
		result := map[string]any{"id": id}
		result["kieu_hook"] = pick(stringFromMap(row, "kieu_hook"), stringFromMap(entry, "kieu_hook"), checklistHookTypes)
		result["cot_truyen"] = pick(stringFromMap(row, "cot_truyen"), stringFromMap(entry, "cot_truyen"), checklistStoryTypes)

		if entry == nil {
			result["loi"] = "Người chấm không trả kết quả cho dòng này."
		} else if score, scoreErr := normalizeChecklistScore(entry["diem_chu_de"], fmt.Sprintf("rows[%d]", id)); scoreErr != nil {
			result["loi"] = scoreErr.Error()
		} else {
			result["diem_chu_de"] = score
			result["nhan_xet"] = strings.TrimSpace(stringFromMap(entry, "nhan_xet"))
			scoredCount++
		}
		results = append(results, result)

		plan = append(plan, checklistPlanEntry{
			Label:     fmt.Sprintf("rows[%d]", id),
			ID:        id,
			Date:      strings.TrimSpace(stringFromMap(row, "date")),
			Topic:     strings.TrimSpace(stringFromMap(row, "topic")),
			HookType:  result["kieu_hook"].(string),
			StoryType: result["cot_truyen"].(string),
			TimeSlot:  strings.TrimSpace(stringFromMap(row, "time_slot")),
			Audience:  strings.TrimSpace(stringFromMap(row, "tep_khach")),
			Keyword:   strings.ToUpper(strings.TrimSpace(stringFromMap(row, "tu_khoa_cta"))),
		})
	}
	if scoredCount == 0 {
		return nil, fmt.Errorf("MODEL_OUTPUT_INVALID: no row could be scored")
	}

	findings := mergeChecklistFindings(
		checklistRepetitionFindings(plan, history),
		checklistSiblingFindings(plan, siblings),
	)
	out := make([]any, 0, len(results))
	for i, result := range results {
		warnings := make([]any, len(findings[i]))
		for j, message := range findings[i] {
			warnings[j] = message
		}
		result["canh_bao_lap"] = warnings
		out = append(out, result)
	}
	return map[string]any{"rows": out}, nil
}
