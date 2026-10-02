package tekshot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	TekshotJobTypeChecklistGuide = "content_checklist_guide"

	checklistGuideMaxIterations = 3
	checklistGuideMaxRows       = 60
	checklistGuideCellMaxRunes  = 300
	// Chỉ dùng khi Drupal không gửi giới hạn; Drupal giữ con số thật vì nó là bên
	// chèn bản nếp vào mọi lượt lập kế hoạch.
	checklistGuideDefaultMaxChars  = 1500
	checklistGuideMaxShortenPasses = 2
	// Ngắn hơn ngần này là một câu từ chối hoặc lời chào, chưa phải bản nếp.
	checklistGuideMinChars = 120
	// Bản nếp người dùng sửa tay có thể dài hơn bản AI viết; chặn trên để một ô
	// dán nhầm không nuốt cả prompt.
	checklistGuidePromptMaxRunes = 4000
)

// checklistGuideToolAllow giữ lượt học đóng sách: bản nếp chỉ được rút từ bảng
// được đưa, lên web hay vào Vault là mời thêm "thói quen" bảng không hề có.
func checklistGuideToolAllow() []string {
	return []string{"datetime"}
}

// checklistGuideTable là bảng kế hoạch đội đang dùng, Drupal đã gọt sẵn.
type checklistGuideTable struct {
	Name    string
	Headers []string
	Rows    [][]string
}

func checklistGuideTableFromRequest(request map[string]any) checklistGuideTable {
	raw, _ := request["table"].(map[string]any)
	table := checklistGuideTable{Name: strings.TrimSpace(stringFromMap(raw, "name"))}

	headers, _ := raw["headers"].([]any)
	for _, header := range headers {
		table.Headers = append(table.Headers, checklistGuideCell(header))
	}

	rows, _ := raw["rows"].([]any)
	for _, entry := range rows {
		cells, ok := entry.([]any)
		if !ok {
			continue
		}
		row := make([]string, len(cells))
		filled := false
		for i, cell := range cells {
			row[i] = checklistGuideCell(cell)
			filled = filled || row[i] != ""
		}
		if !filled {
			continue
		}
		table.Rows = append(table.Rows, row)
		if len(table.Rows) == checklistGuideMaxRows {
			break
		}
	}
	return table
}

// checklistGuideCell đưa một ô về một dòng chữ ngắn.
func checklistGuideCell(value any) string {
	text, _ := value.(string)
	text = strings.Join(strings.Fields(text), " ")
	if runes := []rune(text); len(runes) > checklistGuideCellMaxRunes {
		text = string(runes[:checklistGuideCellMaxRunes]) + "…"
	}
	return text
}

func checklistGuideMaxChars(request map[string]any) int {
	if limit := int(numberFromMap(request, "guide_max_chars")); limit > 0 {
		return limit
	}
	return checklistGuideDefaultMaxChars
}

// runContentChecklistGuide đọc bảng kế hoạch đội đang dùng MỘT lần và rút ra
// cách đội lên kế hoạch. Các lượt lập kế hoạch sau chỉ nhận bản nếp này, không
// nhận bảng: nhận bảng thì kế hoạch mới cứ bám theo chủ đề cũ.
func (s *JobService) runContentChecklistGuide(ctx context.Context, job *store.TekshotJob, request map[string]any) (any, string, error) {
	if s.agents == nil {
		return nil, "", fmt.Errorf("agent router is not configured")
	}
	table := checklistGuideTableFromRequest(request)
	if len(table.Rows) == 0 {
		return nil, "", fmt.Errorf("content_checklist_guide needs a table with at least one row")
	}

	loop, err := s.agents.Get(store.WithTenantID(ctx, store.MasterTenantID), job.AgentKey)
	if err != nil {
		return nil, "", err
	}
	userID := "tekshot-" + job.ExternalUserID
	runCtx := store.WithTenantID(ctx, store.MasterTenantID)
	runCtx = store.WithUserID(runCtx, userID)
	runCtx = store.WithAgentKey(runCtx, job.AgentKey)

	maxChars := checklistGuideMaxChars(request)
	runReq := agent.RunRequest{
		SessionKey:    job.SessionKey,
		Message:       buildChecklistGuidePrompt(request, table, maxChars),
		Channel:       "tekshot_job",
		ChannelType:   "tekshot",
		ChatID:        userID,
		PeerKind:      "direct",
		Addressed:     true,
		RunID:         uuid.NewString(),
		UserID:        userID,
		SenderID:      userID,
		ToolAllow:     checklistGuideToolAllow(),
		MaxIterations: checklistGuideMaxIterations,
		SkillFilter:   []string{},
		LightContext:  true,
		HistoryLimit:  1,
		TraceName:     "tekshot content checklist guide",
		TraceTags:     []string{"tekshot", "content_checklist_guide"},
	}

	result, err := loop.Run(runCtx, runReq)
	if err != nil {
		return nil, "", err
	}
	guide := ""
	if result != nil {
		guide = cleanChecklistGuide(result.Content)
	}
	if styleGuideLength(guide) < checklistGuideMinChars {
		return nil, "", fmt.Errorf("MODEL_OUTPUT_INVALID: agent did not return a usable planning-habits note")
	}

	// Giới hạn là mục tiêu, không phải cổng chặn: vẫn trả bản ngắn nhất có được.
	for pass := 1; pass <= checklistGuideMaxShortenPasses && styleGuideLength(guide) > maxChars; pass++ {
		shortenReq := runReq
		shortenReq.SessionKey = fmt.Sprintf("%s:shorten:%d", job.SessionKey, pass)
		shortenReq.RunID = uuid.NewString()
		shortenReq.Message = buildChecklistGuideShortenPrompt(guide, maxChars)
		// Giữ nguyên số vòng của lượt chính: model hay gọi datetime trước khi viết,
		// một vòng duy nhất bị tool đó ăn mất và lượt rút gọn trả về rỗng.
		shortened, err := loop.Run(runCtx, shortenReq)
		if err != nil {
			if ctx.Err() != nil {
				return nil, "", err
			}
			slog.Warn("tekshot.checklist_guide.shorten_failed", "job_id", job.ID, "pass", pass, "error", err)
			break
		}
		if shortened == nil {
			break
		}
		candidate := cleanChecklistGuide(shortened.Content)
		if styleGuideLength(candidate) < checklistGuideMinChars {
			break
		}
		next := shorterGuide(guide, candidate)
		if next == guide {
			break
		}
		guide = next
	}
	if length := styleGuideLength(guide); length > maxChars {
		slog.Warn("tekshot.checklist_guide.over_limit", "job_id", job.ID, "chars", length, "limit", maxChars)
	}

	return map[string]any{"guide": guide, "rows": len(table.Rows)}, "Checklist planning habits learned", nil
}

// cleanChecklistGuide bỏ hàng rào ``` model hay bọc quanh câu trả lời.
func cleanChecklistGuide(reply string) string {
	lines := strings.Split(strings.TrimSpace(reply), "\n")
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "```") {
		lines = lines[1:]
	}
	if n := len(lines); n > 0 && strings.TrimSpace(lines[n-1]) == "```" {
		lines = lines[:n-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func buildChecklistGuidePrompt(request map[string]any, table checklistGuideTable, maxChars int) string {
	var sb strings.Builder
	sb.WriteString("You are studying the content plan a Facebook page's team already uses.\n")
	sb.WriteString("Describe HOW this team plans its posts, not WHAT it posted. Another planner will read your note as background before drafting a brand-new plan for the same page.\n\n")

	sb.WriteString("## Page\n")
	if pageName := strings.TrimSpace(stringFromMap(request, "page_name")); pageName != "" {
		sb.WriteString("- Name: " + pageName + "\n")
	}
	sb.WriteString("\n")

	heading := "## The team's plan table"
	if table.Name != "" {
		heading += " \"" + table.Name + "\""
	}
	sb.WriteString(fmt.Sprintf("%s (%d rows)\n", heading, len(table.Rows)))
	for i, row := range table.Rows {
		sb.WriteString(fmt.Sprintf("Row %d\n", i+1))
		for column, cell := range row {
			if cell == "" {
				continue
			}
			label := fmt.Sprintf("Column %d", column+1)
			if column < len(table.Headers) && table.Headers[column] != "" {
				label = table.Headers[column]
			}
			sb.WriteString("- " + label + ": " + cell + "\n")
		}
	}

	sb.WriteString("\n## Write these sections, in this order, under these exact headings\n")
	sb.WriteString("1. \"Tuyến nội dung\" — the content pillars in use and roughly how often each one appears.\n")
	sb.WriteString("2. \"Nhịp đăng\" — posts per week, which weekdays, which time slots.\n")
	sb.WriteString("3. \"Độ chi tiết của một dòng\" — which columns the team fills, how specific a topic and its main content are, how long they run.\n")
	sb.WriteString("4. \"CTA và ảnh\" — the kinds of call-to-action and the image conventions that recur.\n")
	sb.WriteString("5. \"Người duyệt hay nhắc\" — only when the table carries reviewer comments or status notes: the corrections that recur. Leave this section out entirely when it does not.\n")

	sb.WriteString("\n## Hard rules\n")
	sb.WriteString("- Write the whole note in Vietnamese — store staff read and edit it by hand.\n")
	// Bản nếp mà chép chủ đề cũ thì lượt lập kế hoạch sau sẽ viết lại đúng kế hoạch cũ.
	sb.WriteString("- Describe patterns only. Never list, quote or paraphrase an individual topic, hook or row of the table: a reader must not be able to rebuild the old plan from your note.\n")
	sb.WriteString("- Name a pillar, a weekday or a time slot only when the table shows it. When a column is missing or too sparse to judge, write \"bảng không cho thấy\" for that point instead of guessing.\n")
	sb.WriteString("- Never state a product, price, promotion or brand fact.\n")
	sb.WriteString(fmt.Sprintf("- Aim for about %d characters and never more than %d: short bullets, no repeated ideas.\n", learnStyleTargetChars(maxChars), maxChars))
	sb.WriteString("- Reply with ONLY the note text: no preamble, no meta commentary, no code fences.\n")
	return sb.String()
}

// buildChecklistGuideShortenPrompt tự đủ, không dựa vào lịch sử phiên của lượt đầu.
func buildChecklistGuideShortenPrompt(guide string, maxChars int) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("The planning-habits note below is %d characters, over the %d-character limit.\n", styleGuideLength(guide), maxChars))
	sb.WriteString(fmt.Sprintf("Rewrite it to about %d characters, never more than %d.\n\n", learnStyleTargetChars(maxChars), maxChars))
	sb.WriteString("## Hard rules\n")
	sb.WriteString("- Giữ nguyên tên và thứ tự các mục; gộp ý trùng, viết gạch đầu dòng ngắn.\n")
	sb.WriteString("- Không thêm chủ đề, câu hook hay dòng nào của bảng gốc.\n")
	sb.WriteString("- This is a pure rewrite: call no tool.\n")
	sb.WriteString("- Reply with ONLY the note text: no preamble, no meta commentary, no code fences.\n\n")
	sb.WriteString("## Note to shorten\n")
	sb.WriteString(guide)
	sb.WriteString("\n")
	return sb.String()
}

// checklistGuideFromRequest đọc bản nếp đã lưu của page; rỗng nghĩa là page chưa học.
func checklistGuideFromRequest(request map[string]any) string {
	guide := strings.TrimSpace(stringFromMap(request, "checklist_guide"))
	if runes := []rune(guide); len(runes) > checklistGuidePromptMaxRunes {
		guide = strings.TrimSpace(string(runes[:checklistGuidePromptMaxRunes]))
	}
	return guide
}

// writeChecklistGuide đưa bản nếp vào prompt lập kế hoạch dưới dạng tham khảo.
// Page chưa có bản nếp thì không viết gì, prompt giữ nguyên như trước.
func writeChecklistGuide(sb *strings.Builder, guide string) {
	if guide == "" {
		return
	}
	sb.WriteString("## How this page has planned its posts so far (reference only)\n")
	sb.WriteString("This note was distilled from a plan the team used before. It is a reference, not a rule and not a topic list: keep the pillars, posting rhythm and level of detail the team is used to when they still fit, but let the facts above decide the topics. Add or drop a pillar when those facts call for it, and never steer the plan back to what the page posted before.\n")
	sb.WriteString(guide + "\n\n")
}
