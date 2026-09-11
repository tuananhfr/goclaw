package tekshot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// Research runs as its own pass before writing. Asking one run to decide
// whether to research, research, and write well made quality swing on every
// rule change: posts that needed a lookup came out complete one run and
// hollow the next. Production searches through DuckDuckGo, which blocks a
// busy host, so widely known technique may come from the model itself;
// laws, prices, statistics and specs still need a Vault or web source.
const (
	factSheetToolName = "submit_fact_sheet"
	factSheetMaxFacts = 30
	// Three searches, three fetches and the submission fit here; without a
	// budget the pass ran 15-25 lookups and hit its timeout.
	draftResearchIterations = 6
	// Own budget so a slow lookup never eats the writer's share of the job's
	// 12 minutes.
	draftResearchTimeout       = 150 * time.Second
	draftResearchSubmitTimeout = 45 * time.Second
	// Resolves to no tool: the writer and the editor only ever call the
	// collector, so they cannot drift into searching again.
	draftWriteNoTools = "draft-write/no-tools"
)

type draftFact struct {
	Fact   string
	Source string
}

type draftFactSheet struct {
	Facts []draftFact
	Gaps  []string
}

func validateFactSheet(args map[string]any) (draftFactSheet, error) {
	var sheet draftFactSheet
	rawFacts, ok := args["facts"].([]any)
	if !ok {
		return sheet, fmt.Errorf("facts must be an array (use [] when the checklist row already has everything)")
	}
	// Trim rather than refuse: a rejected sheet costs a whole resubmission.
	if len(rawFacts) > factSheetMaxFacts {
		rawFacts = rawFacts[:factSheetMaxFacts]
	}
	for i, raw := range rawFacts {
		entry, ok := raw.(map[string]any)
		if !ok {
			return sheet, fmt.Errorf("facts[%d] must be an object", i)
		}
		fact := strings.TrimSpace(stringFromMap(entry, "fact"))
		source := strings.TrimSpace(stringFromMap(entry, "source"))
		if fact == "" {
			return sheet, fmt.Errorf("facts[%d].fact must not be empty", i)
		}
		if source == "" {
			return sheet, fmt.Errorf("facts[%d].source must name where the fact came from (vault:<file>, a URL, or \"kiến thức chung\")", i)
		}
		sheet.Facts = append(sheet.Facts, draftFact{Fact: cutRunes(fact, 600), Source: cutRunes(source, 300)})
	}
	if rawGaps, ok := args["gaps"].([]any); ok {
		for _, raw := range rawGaps {
			if gap, ok := raw.(string); ok && strings.TrimSpace(gap) != "" {
				sheet.Gaps = append(sheet.Gaps, cutRunes(strings.TrimSpace(gap), 300))
			}
		}
	}
	return sheet, nil
}

// FactSheetCollector receives the research pass's findings.
type FactSheetCollector struct {
	sheet *draftFactSheet
}

func NewFactSheetCollector() *FactSheetCollector { return &FactSheetCollector{} }

func (t *FactSheetCollector) Name() string { return factSheetToolName }

func (t *FactSheetCollector) Description() string {
	return "Submit the facts you found for this post, each with its source, plus what you could not find. Call exactly once."
}

func (t *FactSheetCollector) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"facts": map[string]any{
				"type":        "array",
				"description": "Facts the post needs that the checklist row does not already give. Empty when the row is already complete.",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"fact":   map[string]any{"type": "string", "description": "One concrete fact in Vietnamese, in the units Vietnamese readers use (g, ml, muỗng canh, muỗng cà phê, °C)."},
						"source": map[string]any{"type": "string", "description": "Where it came from: vault:<file path>, the URL you fetched, or \"kiến thức chung\" for a widely known technique. Laws, prices, statistics and specs need a vault or URL source."},
					},
					"required": []string{"fact", "source"},
				},
			},
			"gaps": map[string]any{
				"type":        "array",
				"description": "Things the title promises that you looked for and could not find.",
				"items":       map[string]any{"type": "string"},
			},
		},
		"required": []string{"facts", "gaps"},
	}
}

func (t *FactSheetCollector) Execute(_ context.Context, args map[string]any) *tools.Result {
	sheet, err := validateFactSheet(args)
	if err != nil {
		return tools.ErrorResult("MODEL_OUTPUT_INVALID: " + err.Error())
	}
	t.sheet = &sheet
	return tools.SilentResult("Fact sheet captured.")
}

func (t *FactSheetCollector) Sheet() *draftFactSheet { return t.sheet }

func buildDraftResearchPrompt(args map[string]any) string {
	var sb strings.Builder
	sb.WriteString("Bạn là người tra cứu dữ kiện cho một bài đăng Facebook. Không viết bài.\n\n")
	sb.WriteString("1. Đọc dòng checklist bên dưới và liệt kê những gì tiêu đề hứa mà dòng đó chưa có: bước làm, định lượng, nhiệt độ, thời gian, ngưỡng hay mốc của quy định, thông số sản phẩm, ví dụ cụ thể.\n")
	sb.WriteString("2. Lấy từng thứ còn thiếu theo thứ tự nguồn:\n")
	sb.WriteString("   a. vault_search — dữ kiện riêng của page.\n")
	sb.WriteString("   b. web_search / web_fetch — luật, quy định, giá, số liệu thống kê, thông số sản phẩm, tin mới. Ưu tiên nguồn chính thức hoặc chuyên ngành.\n")
	sb.WriteString("   c. kiến thức phổ thông của bạn — kỹ thuật ai làm nghề cũng biết: các bước sơ chế, nấu, ủ, pha, bảo quản, cách làm chuẩn, định nghĩa phổ biến. Ghi source là \"kiến thức chung\".\n")
	sb.WriteString("   Luật, quy định, giá, số liệu, thông số và thứ thay đổi theo thời gian bắt buộc có nguồn Vault hoặc web; không có thì ghi vào gaps, không dùng kiến thức chung cho chúng.\n")
	sb.WriteString("   Ngân sách cho cả lượt: tối đa 3 lần web_search và 3 lần web_fetch. Tra đúng thứ còn thiếu, đủ dùng thì nộp ngay; không tra thêm cho chắc.\n")
	sb.WriteString("   web_search báo lỗi một lần (ví dụ \"all search providers failed\") thì không gọi lại nữa — mỗi lần gọi lại mất trọn thời gian chờ; dùng Vault và kiến thức chung rồi nộp.\n")
	sb.WriteString("   Với công thức: lấy bước làm chung (sơ chế, nấu, ủ, pha, bảo quản) từ công thức uy tín kể cả khi định lượng của họ khác; định lượng của page trong dòng checklist luôn thắng — không ghi con số của nguồn ngoài khi nó mâu thuẫn.\n")
	sb.WriteString("3. Gọi " + factSheetToolName + " MỘT lần: mỗi dữ kiện một dòng kèm nguồn; đổi sang đơn vị và từ ngữ của người đọc Việt (g, ml, muỗng canh, muỗng cà phê, °C). Chỉ ghi dữ kiện dòng checklist chưa có và giúp người đọc làm hoặc hiểu điều tiêu đề hứa.\n")
	sb.WriteString("4. Thứ đã tra mà không ra thì ghi vào gaps. Không đoán, không suy ra con số.\n")
	sb.WriteString("Nếu dòng checklist đã đủ những gì tiêu đề hứa, gọi " + factSheetToolName + " ngay với facts rỗng.\n\n")
	sb.WriteString("DÒNG CHECKLIST:\n")
	for _, item := range sourceItemsArg(args["source_items"]) {
		sb.WriteString("Title: " + item.SourceTitle + "\n")
		if item.SourceBrief != "" {
			sb.WriteString("Brief: " + item.SourceBrief + "\n")
		}
		if supporting := sourceSupportingText(item); supporting != "" {
			sb.WriteString(supporting + "\n")
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func draftResearchRequest(args map[string]any, userID, sessionKey string, collector *FactSheetCollector) agent.RunRequest {
	return agent.RunRequest{
		SessionKey:     sessionKey + ":research",
		Message:        buildDraftResearchPrompt(args),
		Channel:        "tekshot_job",
		ChannelType:    "tekshot",
		ChatID:         userID,
		PeerKind:       "direct",
		Addressed:      true,
		RunID:          uuid.NewString(),
		UserID:         userID,
		SenderID:       userID,
		ToolAllow:      tekshotDraftResearchToolAllow(),
		EphemeralTools: []tools.Tool{collector},
		MaxIterations:  draftResearchIterations,
		LightContext:   true,
		TraceName:      "tekshot draft research",
		TraceTags:      []string{"tekshot", "draft_posts", "research"},
	}
}

// researchDraftFacts is best-effort: no sheet means the writer works from the
// checklist row alone, never that the job fails.
func researchDraftFacts(ctx context.Context, ag agent.Agent, args map[string]any, userID, sessionKey string) *draftFactSheet {
	collector := NewFactSheetCollector()
	req := draftResearchRequest(args, userID, sessionKey, collector)

	runCtx, cancel := context.WithTimeout(ctx, draftResearchTimeout)
	defer cancel()
	if _, err := ag.Run(runCtx, req); err != nil && collector.Sheet() == nil {
		slog.Warn("tekshot.draft.research_failed", "session", sessionKey, "error", err)
	}
	if collector.Sheet() == nil && ctx.Err() == nil {
		// The model researched but never submitted: force the submission from
		// what the session already holds, on a fresh budget.
		submitCtx, cancelSubmit := context.WithTimeout(ctx, draftResearchSubmitTimeout)
		defer cancelSubmit()
		forced := req
		forced.RunID = uuid.NewString()
		forced.Message = fmt.Sprintf("Gọi %s ngay bây giờ với những dữ kiện đã tra được. Không trả lời bằng văn bản.", factSheetToolName)
		forced.MaxIterations = 1
		forced.ToolChoice = &providers.ToolChoice{Mode: "function", Name: factSheetToolName}
		if _, err := ag.Run(submitCtx, forced); err != nil && collector.Sheet() == nil {
			slog.Warn("tekshot.draft.research_submit_failed", "session", sessionKey, "error", err)
		}
	}
	if sheet := collector.Sheet(); sheet != nil {
		slog.Info("tekshot.draft.researched", "session", sessionKey, "facts", len(sheet.Facts), "gaps", len(sheet.Gaps))
	}
	return collector.Sheet()
}

// renderFactSheet leaves the sources out: the writer only needs the facts,
// and a URL in its prompt invites it to cite one in the post.
func renderFactSheet(sheet *draftFactSheet) string {
	if sheet == nil || (len(sheet.Facts) == 0 && len(sheet.Gaps) == 0) {
		return ""
	}
	var sb strings.Builder
	if len(sheet.Facts) > 0 {
		sb.WriteString("RESEARCHED FACTS (already verified — use the ones that help the reader, as if you knew them; the checklist row wins any disagreement; never say where they came from):\n")
		for _, fact := range sheet.Facts {
			sb.WriteString("- " + fact.Fact + "\n")
		}
	}
	if len(sheet.Gaps) > 0 {
		sb.WriteString("NOT FOUND (leave these out; never invent them):\n")
		for _, gap := range sheet.Gaps {
			sb.WriteString("- " + gap + "\n")
		}
	}
	return sb.String()
}
