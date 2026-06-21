package tekshot

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const (
	toolName           = "tekshot_generate_draft_posts"
	finalToolName      = "submit_draft_batch"
	defaultTimezone    = "Asia/Ho_Chi_Minh"
	maxStructuredPosts = 200
)

var (
	isoDateTimePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}$`)
	isoDatePattern     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	hourMinutePattern  = regexp.MustCompile(`^\d{2}:\d{2}$`)
)

type DraftPostsTool struct {
	router *agent.Router
}

func NewDraftPostsTool(router *agent.Router) *DraftPostsTool {
	return &DraftPostsTool{router: router}
}

func (t *DraftPostsTool) Name() string { return toolName }

func (t *DraftPostsTool) Description() string {
	return "Generate a strict structured Tekshot draft post batch by orchestrating an agent and returning validated JSON."
}

func (t *DraftPostsTool) HiddenFromLLM() bool { return true }

func (t *DraftPostsTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"agent_key": map[string]any{
				"type":        "string",
				"description": "Target GoClaw agent key that should plan the batch.",
			},
			"session_key": map[string]any{
				"type":        "string",
				"description": "Stable session key for the delegated planning run.",
			},
			"source_type": map[string]any{
				"type":        "string",
				"description": "Checklist source type: text, link, or file.",
			},
			"source_text": map[string]any{
				"type":        "string",
				"description": "Resolved source text when available.",
			},
			"source_url": map[string]any{
				"type":        "string",
				"description": "Original cloud document URL when the source came from a link.",
			},
			"source_media": map[string]any{
				"type":        "array",
				"description": "Uploaded GoClaw media references for file-based source material.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path":      map[string]any{"type": "string"},
						"mime_type": map[string]any{"type": "string"},
						"filename":  map[string]any{"type": "string"},
					},
				},
			},
			"file_name": map[string]any{
				"type":        "string",
				"description": "Human-readable file name for file-based checklist source.",
			},
			"instructions": map[string]any{
				"type":        "string",
				"description": "Full Tekshot instructions telling the agent how to plan the posts.",
			},
			"timezone": map[string]any{
				"type":        "string",
				"description": "IANA timezone used when the agent schedules posts.",
			},
		},
		"required":             []string{"agent_key", "instructions"},
		"additionalProperties": false,
	}
}

func (t *DraftPostsTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	agentKey := strings.TrimSpace(stringArg(args, "agent_key"))
	if agentKey == "" {
		return tools.ErrorResult("agent_key is required")
	}
	if t.router == nil {
		return tools.ErrorResult("agent router is not configured")
	}

	ag, err := t.router.Get(ctx, agentKey)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("failed to resolve agent %q: %v", agentKey, err))
	}

	sessionKey := strings.TrimSpace(stringArg(args, "session_key"))
	if sessionKey == "" {
		sessionKey = "tekshot:draft:" + uuid.NewString()
	}
	timezone := strings.TrimSpace(stringArg(args, "timezone"))
	if timezone == "" {
		timezone = defaultTimezone
	}

	collector := NewDraftBatchCollectorTool()
	runReq := agent.RunRequest{
		SessionKey:     sessionKey,
		Message:        buildPrompt(args, timezone),
		Media:          mediaFilesArg(args["source_media"]),
		Channel:        "http",
		ChatID:         "api",
		PeerKind:       "direct",
		Addressed:      true,
		RunID:          uuid.NewString(),
		UserID:         store.UserIDFromContext(ctx),
		ToolAllow:      []string{"web_fetch", "read_document"},
		EphemeralTools: []tools.Tool{collector},
		MaxIterations:  6,
		TraceName:      "tekshot draft posts",
		TraceTags:      []string{"tekshot", "draft_posts"},
	}

	if _, err := ag.Run(ctx, runReq); err != nil && collector.Batch() == nil {
		return tools.ErrorResult(fmt.Sprintf("draft generation run failed: %v", err))
	}

	if collector.Batch() == nil {
		finalReq := runReq
		finalReq.RunID = uuid.NewString()
		finalReq.Message = fmt.Sprintf("Submit the final Tekshot batch now by calling %s with the complete structured result. Do not answer with plain text.", finalToolName)
		finalReq.MaxIterations = 2
		finalReq.ToolChoice = &providers.ToolChoice{
			Mode: "function",
			Name: finalToolName,
		}
		if _, err := ag.Run(ctx, finalReq); err != nil && collector.Batch() == nil {
			return tools.ErrorResult(fmt.Sprintf("final structured submission failed: %v", err))
		}
	}

	batch := collector.Batch()
	if batch == nil {
		return tools.ErrorResult("MODEL_OUTPUT_INVALID: agent did not submit a valid structured draft batch")
	}

	encoded, err := json.Marshal(batch)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("failed to encode structured draft batch: %v", err))
	}

	return &tools.Result{
		ForLLM:            string(encoded),
		StructuredContent: batch,
		Metadata: map[string]any{
			"agent_key":   agentKey,
			"session_key": sessionKey,
			"source_type": stringArg(args, "source_type"),
			"timezone":    timezone,
		},
	}
}

type DraftBatchCollectorTool struct {
	batch map[string]any
}

func NewDraftBatchCollectorTool() *DraftBatchCollectorTool {
	return &DraftBatchCollectorTool{}
}

func (t *DraftBatchCollectorTool) Name() string { return finalToolName }

func (t *DraftBatchCollectorTool) Description() string {
	return "Submit the final Tekshot draft post batch as validated structured JSON. Call this once when the batch is complete."
}

func (t *DraftBatchCollectorTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title": map[string]any{
				"type":        "string",
				"description": "Short batch title.",
			},
			"summary": map[string]any{
				"type":        "string",
				"description": "Short summary of what this batch covers.",
			},
			"posts": map[string]any{
				"type":        "array",
				"description": "Complete set of generated posts.",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"title":          map[string]any{"type": "string"},
						"brief":          map[string]any{"type": "string"},
						"pillar":         map[string]any{"type": "string"},
						"content":        map[string]any{"type": "string"},
						"hashtags":       map[string]any{"type": "string", "description": "3-5 contextual hashtags for this post, space-separated, each starting with #. Example: '#BIMFrance #DessinTechnique #DesignSupport'. Use empty string if none."},
						"publish_at":     map[string]any{"type": "string", "description": "Publish date and time. Must strictly match 'YYYY-MM-DDTHH:MM:SS' (e.g. 2026-06-21T15:30:00). Do NOT include timezone offsets or Z suffix."},
						"publish_date":   map[string]any{"type": "string", "description": "Publish date. Must match 'YYYY-MM-DD' (e.g. 2026-06-21)."},
						"publish_time":   map[string]any{"type": "string", "description": "Publish time in 24-hour format. Must match 'HH:MM' (e.g. 15:30)."},
						"checklist_item": map[string]any{"type": "string"},
					},
					"required": []string{
						"title", "brief", "pillar", "content", "hashtags",
						"publish_at", "publish_date", "publish_time", "checklist_item",
					},
				},
			},
		},
		"required":             []string{"title", "summary", "posts"},
		"additionalProperties": false,
	}
}

func (t *DraftBatchCollectorTool) Execute(_ context.Context, args map[string]any) *tools.Result {
	batch, err := validateDraftBatch(args)
	if err != nil {
		return tools.ErrorResult("MODEL_OUTPUT_INVALID: " + err.Error())
	}
	t.batch = batch
	return tools.SilentResult("Structured draft batch captured.")
}

func (t *DraftBatchCollectorTool) Batch() map[string]any {
	if t.batch == nil {
		return nil
	}
	return cloneBatch(t.batch)
}

func buildPrompt(args map[string]any, timezone string) string {
	var sb strings.Builder
	sb.WriteString("You are generating Tekshot Studio draft social posts.\n")
	sb.WriteString("Study the provided source carefully, plan the batch, and when the batch is complete you must call submit_draft_batch exactly once with the final structured result.\n")
	sb.WriteString("Do not return the final batch as plain text.\n")
	sb.WriteString("Keep every post grounded in the source material. Use empty strings for unknown optional fields, never omit required fields.\n")
	sb.WriteString("Scheduling timezone: ")
	sb.WriteString(timezone)
	sb.WriteString("\n\n")

	if instructions := strings.TrimSpace(stringArg(args, "instructions")); instructions != "" {
		sb.WriteString("Tekshot instructions:\n")
		sb.WriteString(instructions)
		sb.WriteString("\n\n")
	}

	if sourceType := strings.TrimSpace(stringArg(args, "source_type")); sourceType != "" {
		sb.WriteString("Source type: ")
		sb.WriteString(sourceType)
		sb.WriteString("\n")
	}
	if sourceURL := strings.TrimSpace(stringArg(args, "source_url")); sourceURL != "" {
		sb.WriteString("Source URL: ")
		sb.WriteString(sourceURL)
		sb.WriteString("\n")
	}
	if fileName := strings.TrimSpace(stringArg(args, "file_name")); fileName != "" {
		sb.WriteString("Source file name: ")
		sb.WriteString(fileName)
		sb.WriteString("\n")
	}
	if sourceText := strings.TrimSpace(stringArg(args, "source_text")); sourceText != "" {
		sb.WriteString("\nResolved source text:\n")
		sb.WriteString(sourceText)
		sb.WriteString("\n")
	}

	sb.WriteString("\nIMPORTANT content rules:\n")
	sb.WriteString("- The 'content' field must contain ONLY the core post body text. Do NOT include any footer, contact information, company address, phone number, email, website URL, or brand hashtags in the content field.\n")
	sb.WriteString("- The footer/signature block is managed separately by the system and will be appended automatically.\n")
	sb.WriteString("- Generate 3-5 contextual hashtags relevant to each post's topic and place them in the 'hashtags' field, space-separated, each starting with #. Example: '#BIMFrance #DessinTechnique #DesignSupport'.\n")
	sb.WriteString("- Do NOT include the brand/company hashtag (e.g. #LPCFrance) in the hashtags field — it is already in the footer.\n")
	sb.WriteString("\nFinal output schema requirements:\n")
	sb.WriteString("- title: short batch title\n")
	sb.WriteString("- summary: short batch summary\n")
	sb.WriteString("- posts: array of objects with exactly these string fields: title, brief, pillar, content, hashtags, publish_at, publish_date, publish_time, checklist_item\n")
	sb.WriteString("  * publish_at must strictly use format 'YYYY-MM-DDTHH:MM:SS' (e.g., 2026-06-21T18:00:00). NO timezone offset (+07:00) or Z suffix allowed.\n")
	sb.WriteString("  * publish_date must use format 'YYYY-MM-DD' (e.g., 2026-06-21).\n")
	sb.WriteString("  * publish_time must use format 'HH:MM' (e.g., 18:00).\n")
	sb.WriteString("- If scheduling data is unavailable, set publish_at, publish_date, and publish_time to empty strings.\n")
	return sb.String()
}

func mediaFilesArg(raw any) []bus.MediaFile {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	media := make([]bus.MediaFile, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		path := strings.TrimSpace(stringArg(entry, "path"))
		if path == "" {
			continue
		}
		media = append(media, bus.MediaFile{
			Path:     path,
			MimeType: strings.TrimSpace(stringArg(entry, "mime_type")),
			Filename: strings.TrimSpace(stringArg(entry, "filename")),
		})
	}
	return media
}

func validateDraftBatch(args map[string]any) (map[string]any, error) {
	requiredRootKeys := map[string]bool{
		"title": true, "summary": true, "posts": true,
	}
	if !sameKeys(args, requiredRootKeys) {
		return nil, fmt.Errorf("batch must contain exactly title, summary, and posts")
	}

	title := strings.TrimSpace(stringArg(args, "title"))
	summary := strings.TrimSpace(stringArg(args, "summary"))
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if summary == "" {
		return nil, fmt.Errorf("summary is required")
	}

	postsRaw, ok := args["posts"].([]any)
	if !ok || len(postsRaw) == 0 {
		return nil, fmt.Errorf("posts must be a non-empty array")
	}
	if len(postsRaw) > maxStructuredPosts {
		return nil, fmt.Errorf("posts exceeds the maximum of %d items", maxStructuredPosts)
	}

	posts := make([]map[string]any, 0, len(postsRaw))
	for index, rawPost := range postsRaw {
		postMap, ok := rawPost.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("posts[%d] must be an object", index)
		}
		post, err := validateDraftPost(postMap, index)
		if err != nil {
			return nil, err
		}
		posts = append(posts, post)
	}

	return map[string]any{
		"title":   title,
		"summary": summary,
		"posts":   posts,
	}, nil
}

func validateDraftPost(post map[string]any, index int) (map[string]any, error) {
	requiredPostKeys := map[string]bool{
		"title": true, "brief": true, "pillar": true, "content": true, "hashtags": true,
		"publish_at": true, "publish_date": true, "publish_time": true, "checklist_item": true,
	}
	if !sameKeys(post, requiredPostKeys) {
		return nil, fmt.Errorf("posts[%d] must contain exactly title, brief, pillar, content, hashtags, publish_at, publish_date, publish_time, checklist_item", index)
	}

	title := strings.TrimSpace(stringArg(post, "title"))
	content := strings.TrimSpace(stringArg(post, "content"))
	checklistItem := strings.TrimSpace(stringArg(post, "checklist_item"))
	if title == "" {
		return nil, fmt.Errorf("posts[%d].title is required", index)
	}
	if content == "" {
		return nil, fmt.Errorf("posts[%d].content is required", index)
	}
	if checklistItem == "" {
		return nil, fmt.Errorf("posts[%d].checklist_item is required", index)
	}

	publishAt := strings.TrimSpace(stringArg(post, "publish_at"))
	publishDate := strings.TrimSpace(stringArg(post, "publish_date"))
	publishTime := strings.TrimSpace(stringArg(post, "publish_time"))
	if err := validateSchedule(index, publishAt, publishDate, publishTime); err != nil {
		return nil, err
	}

	return map[string]any{
		"title":          title,
		"brief":          strings.TrimSpace(stringArg(post, "brief")),
		"pillar":         strings.TrimSpace(stringArg(post, "pillar")),
		"content":        content,
		"hashtags":       strings.TrimSpace(stringArg(post, "hashtags")),
		"publish_at":     publishAt,
		"publish_date":   publishDate,
		"publish_time":   publishTime,
		"checklist_item": checklistItem,
	}, nil
}

func validateSchedule(index int, publishAt, publishDate, publishTime string) error {
	emptyCount := 0
	for _, value := range []string{publishAt, publishDate, publishTime} {
		if value == "" {
			emptyCount++
		}
	}
	if emptyCount == 3 {
		return nil
	}
	if emptyCount != 0 {
		return fmt.Errorf("posts[%d] schedule fields must all be present or all empty", index)
	}

	if !isoDateTimePattern.MatchString(publishAt) {
		return fmt.Errorf("posts[%d].publish_at must match YYYY-MM-DDTHH:MM:SS", index)
	}
	if !isoDatePattern.MatchString(publishDate) {
		return fmt.Errorf("posts[%d].publish_date must match YYYY-MM-DD", index)
	}
	if !hourMinutePattern.MatchString(publishTime) {
		return fmt.Errorf("posts[%d].publish_time must match HH:MM", index)
	}
	parsedAt, err := time.Parse("2006-01-02T15:04:05", publishAt)
	if err != nil {
		return fmt.Errorf("posts[%d].publish_at is invalid: %v", index, err)
	}
	parsedDate, err := time.Parse("2006-01-02", publishDate)
	if err != nil {
		return fmt.Errorf("posts[%d].publish_date is invalid: %v", index, err)
	}
	parsedTime, err := time.Parse("15:04", publishTime)
	if err != nil {
		return fmt.Errorf("posts[%d].publish_time is invalid: %v", index, err)
	}
	if parsedAt.Format("2006-01-02") != parsedDate.Format("2006-01-02") {
		return fmt.Errorf("posts[%d] publish_at and publish_date do not match", index)
	}
	if parsedAt.Format("15:04") != parsedTime.Format("15:04") {
		return fmt.Errorf("posts[%d] publish_at and publish_time do not match", index)
	}
	return nil
}

func sameKeys(values map[string]any, required map[string]bool) bool {
	if len(values) != len(required) {
		return false
	}
	for key := range values {
		if !required[key] {
			return false
		}
	}
	return true
}

func stringArg(values map[string]any, key string) string {
	raw, ok := values[key]
	if !ok || raw == nil {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	return value
}

func cloneBatch(batch map[string]any) map[string]any {
	clone := make(map[string]any, len(batch))
	maps.Copy(clone, batch)
	if posts, ok := batch["posts"].([]map[string]any); ok {
		copiedPosts := make([]map[string]any, len(posts))
		for i, post := range posts {
			postClone := make(map[string]any, len(post))
			maps.Copy(postClone, post)
			copiedPosts[i] = postClone
		}
		clone["posts"] = copiedPosts
	}
	return clone
}
