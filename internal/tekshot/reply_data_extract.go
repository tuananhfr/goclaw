package tekshot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/security"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const (
	TekshotJobTypeReplyDataExtract = "reply_data_extract"
	replyDataMaxColumns            = 30
	replyDataMaxRows               = 2000
	replyDataMaxCellRunes          = 500
	replyDataMaxColumnRunes        = 120
	replyDataMaxNameRunes          = 255
	replyDataMaxImages             = 20
	replyDataToolName              = "submit_data_table"
)

// replyDataTable là một bảng dữ liệu trả lời Messenger; Drupal lưu thành bảng nháp chờ duyệt.
type replyDataTable struct {
	Name      string     `json:"name"`
	Source    string     `json:"source"`
	Columns   []string   `json:"columns"`
	Rows      [][]string `json:"rows"`
	Uncertain [][2]int   `json:"uncertain"`
}

// clampRunes cuts s to exactly max runes with no marker: reply-data cells are stored
// data Drupal parses back against an exact contract, not prompt text, so headRunes'
// ellipsis (used by every other job) would push cells/columns/names one rune over.
func clampRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) > max {
		return string(runes[:max])
	}
	return string(runes)
}

func clampReplyDataTable(t replyDataTable) (replyDataTable, bool) {
	if len(t.Columns) == 0 || len(t.Rows) == 0 {
		return t, false
	}
	width := min(len(t.Columns), replyDataMaxColumns)
	out := replyDataTable{Name: clampRunes(strings.TrimSpace(t.Name), replyDataMaxNameRunes), Source: t.Source, Uncertain: [][2]int{}}
	for _, c := range t.Columns[:width] {
		out.Columns = append(out.Columns, clampRunes(strings.TrimSpace(c), replyDataMaxColumnRunes))
	}
	for _, r := range t.Rows[:min(len(t.Rows), replyDataMaxRows)] {
		row := make([]string, width)
		for i := 0; i < width && i < len(r); i++ {
			row[i] = clampRunes(strings.TrimSpace(r[i]), replyDataMaxCellRunes)
		}
		out.Rows = append(out.Rows, row)
	}
	for _, u := range t.Uncertain {
		if u[0] >= 0 && u[0] < len(out.Rows) && u[1] >= 0 && u[1] < width {
			out.Uncertain = append(out.Uncertain, u)
		}
	}
	return out, true
}

// ReplyDataTableTool thu bảng model đọc từ ảnh; ô không chắc phải để trống thay vì đoán.
type ReplyDataTableTool struct {
	table     *replyDataTable
	submitted bool
}

func NewReplyDataTableTool() *ReplyDataTableTool { return &ReplyDataTableTool{} }

func (t *ReplyDataTableTool) Name() string { return replyDataToolName }

func (t *ReplyDataTableTool) Description() string {
	return "Submit the table read from the image. Set has_table=false when the image holds no table or list of items with values."
}

func (t *ReplyDataTableTool) Parameters() map[string]any {
	strs := map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"has_table": map[string]any{"type": "boolean"},
			"name":      map[string]any{"type": "string"},
			"columns":   strs,
			"rows":      map[string]any{"type": "array", "items": strs},
			"uncertain": map[string]any{"type": "array", "items": map[string]any{
				"type": "object", "additionalProperties": false,
				"properties": map[string]any{"row": map[string]any{"type": "integer"}, "column": map[string]any{"type": "integer"}},
				"required":   []string{"row", "column"},
			}},
		},
		"required": []string{"has_table", "name", "columns", "rows", "uncertain"},
	}
}

func (t *ReplyDataTableTool) Execute(_ context.Context, args map[string]any) *tools.Result {
	t.submitted = true
	if has, _ := args["has_table"].(bool); !has {
		t.table = nil
		return tools.SilentResult("No table.")
	}
	table := replyDataTable{Name: stringFromMap(args, "name"), Source: "vision", Columns: anyStrings(args["columns"])}
	rawRows, _ := args["rows"].([]any)
	for _, r := range rawRows {
		row := anyStrings(r)
		for len(row) < len(table.Columns) {
			row = append(row, "")
		}
		table.Rows = append(table.Rows, row)
	}
	rawUncertain, _ := args["uncertain"].([]any)
	for _, u := range rawUncertain {
		m, _ := u.(map[string]any)
		r, rok := m["row"].(float64)
		c, cok := m["column"].(float64)
		if rok && cok {
			table.Uncertain = append(table.Uncertain, [2]int{int(r), int(c)})
		}
	}
	if len(table.Columns) == 0 {
		t.submitted = false
		return tools.ErrorResult("MODEL_OUTPUT_INVALID: columns required when has_table=true")
	}
	t.table = &table
	return tools.SilentResult("Table captured.")
}

func (t *ReplyDataTableTool) Table() *replyDataTable { return t.table }
func (t *ReplyDataTableTool) Submitted() bool        { return t.submitted }

func anyStrings(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, _ := item.(string)
		out = append(out, strings.TrimSpace(s))
	}
	return out
}

type replyDataVisionRunner func(ctx context.Context, img extractedImage) (*replyDataTable, error)

// readTablesFromImages: ảnh đọc hỏng được liệt kê chứ không làm hỏng cả file.
func readTablesFromImages(ctx context.Context, images []extractedImage, run replyDataVisionRunner) ([]replyDataTable, []string) {
	var tables []replyDataTable
	var unread []string
	for _, img := range images {
		if ctx.Err() != nil {
			unread = append(unread, img.Ref)
			continue
		}
		table, err := run(ctx, img)
		if err != nil {
			slog.Warn("reply_data_extract: image read failed", "ref", img.Ref, "error", err)
			unread = append(unread, img.Ref)
			continue
		}
		if table == nil {
			continue
		}
		table.Source = "vision"
		table.Name = strings.TrimSpace(img.Ref + " · " + table.Name)
		tables = append(tables, *table)
	}
	return tables, unread
}

const replyDataVisionPrompt = `The attached image comes from a shop's own file (menu, price list, product list, schedule).
If it contains a table or a list of items with values (for example dish + price), transcribe it with submit_data_table:
- columns: the header labels as printed; invent short labels only when the image has none.
- rows: one array per item, cells copied exactly as printed (numbers keep their digits; drop currency symbols only if the header already says the unit).
- A cell you cannot read with certainty: leave it "" and list it in uncertain as {row, column} (0-based, rows exclude the header). Never guess a number.
- name: a short title for the table in Vietnamese.
If there is no such table, call submit_data_table with has_table=false.`

func (s *JobService) replyDataVisionRunner(job *store.TekshotJob) replyDataVisionRunner {
	return func(ctx context.Context, img extractedImage) (*replyDataTable, error) {
		loop, err := s.agents.Get(store.WithTenantID(ctx, store.MasterTenantID), job.AgentKey)
		if err != nil {
			return nil, err
		}
		userID := "tekshot-" + job.ExternalUserID
		collector := NewReplyDataTableTool()
		req := agent.RunRequest{
			SessionKey: job.SessionKey + ":reply-data:" + uuid.NewString(), Message: replyDataVisionPrompt,
			Channel: "tekshot_job", ChannelType: "tekshot", ChatID: userID, PeerKind: "direct", Addressed: true,
			RunID: uuid.NewString(), UserID: userID, SenderID: userID,
			ToolAllow: []string{"read_image"}, EphemeralTools: []tools.Tool{collector},
			MaxIterations: 3, HistoryLimit: 1, LightContext: true, SkillFilter: []string{},
			Media:     []bus.MediaFile{{Path: img.ImagePath, MimeType: "image/png", Filename: filepath.Base(img.ImagePath)}},
			TraceName: "tekshot reply data vision", TraceTags: []string{"tekshot", TekshotJobTypeReplyDataExtract},
		}
		if _, err := loop.Run(ctx, req); err != nil && !collector.Submitted() {
			return nil, err
		}
		if !collector.Submitted() {
			final := req
			final.RunID = uuid.NewString()
			final.MaxIterations = 2
			final.Message = "Submit the table now by calling " + replyDataToolName + ". Do not answer with plain text."
			final.ToolChoice = &providers.ToolChoice{Mode: "function", Name: replyDataToolName}
			if _, err := loop.Run(ctx, final); err != nil && !collector.Submitted() {
				return nil, err
			}
		}
		if !collector.Submitted() {
			return nil, errors.New("model did not submit a table")
		}
		return collector.Table(), nil
	}
}

func (s *JobService) runReplyDataExtract(ctx context.Context, job *store.TekshotJob, request map[string]any) (any, string, error) {
	fileURL := strings.TrimSpace(stringFromMap(request, "file_url"))
	parsed, err := url.Parse(fileURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, "", errors.New("reply_data_extract: file_url must be an http(s) URL")
	}
	_, pinnedIP, err := security.Validate(fileURL)
	if err != nil {
		return nil, "", fmt.Errorf("reply_data_extract: %w", err)
	}
	workDir, err := os.MkdirTemp("", "goclaw-rd-")
	if err != nil {
		return nil, "", err
	}
	defer os.RemoveAll(workDir)

	filename := stringFromMap(request, "filename")
	if filename == "" {
		filename = "reply-data-source"
	}
	dlCtx, cancel := context.WithTimeout(security.WithPinnedIP(ctx, pinnedIP), knowledgeSourceDownloadTimeout)
	defer cancel()
	downloader := &knowledgeSourceDownloader{client: security.NewSafeClient(knowledgeSourceDownloadTimeout), maxBytes: knowledgeFileMaxBytes}
	srcPath, _, err := downloader.download(dlCtx, fileURL, workDir, filename)
	if err != nil {
		return nil, "", err
	}
	ext, err := runTableExtractor(ctx, knowledgeExtractorOptions{
		Input: srcPath, Mime: stringFromMap(request, "mime"), OutDir: workDir,
		MaxScanPages: replyDataMaxImages, DPI: knowledgeExtractorDPI,
	})
	if err != nil {
		return nil, "", err
	}
	return s.replyDataResult(ctx, ext, s.replyDataVisionRunner(job))
}

func (s *JobService) replyDataResult(ctx context.Context, ext *tableExtraction, run replyDataVisionRunner) (any, string, error) {
	var tables []replyDataTable
	for _, t := range ext.Tables {
		t.Source = "parsed"
		if c, ok := clampReplyDataTable(t); ok {
			tables = append(tables, c)
		}
	}
	vision, unread := readTablesFromImages(ctx, ext.Images, run)
	// A cancelled/timed-out run must fail the job, not complete it with whatever
	// images happened to finish — process() would otherwise MarkCompleted over a
	// job the caller already cancelled.
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	for _, t := range vision {
		if c, ok := clampReplyDataTable(t); ok {
			tables = append(tables, c)
		}
	}
	if tables == nil {
		tables = []replyDataTable{}
	}
	if unread == nil {
		unread = []string{}
	}
	return map[string]any{
		"tables": tables, "truncated": ext.Truncated, "truncated_reason": ext.TruncatedReason,
		"unread_images": unread,
	}, fmt.Sprintf("Đã tách %d bảng", len(tables)), nil
}
