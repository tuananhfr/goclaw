package tools

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const facebookCommentMaxChars = 8000

type FacebookPostWithCommentsTool struct {
	registry *Registry
	cron     store.CronStore
}

type fbCommentPlan struct {
	Enabled        bool
	PolicyFound    bool
	Count          int
	WindowMS       int64
	RandomOrder    bool
	RandomOrderSet bool
	MinGapMS       int64
	Comments       []fbPlannedComment
}

type fbPlannedComment struct {
	Message   string `json:"message"`
	Rationale string `json:"rationale,omitempty"`
}

type fbScheduledComment struct {
	JobID     string `json:"job_id"`
	RunAtMS   int64  `json:"run_at_ms"`
	RunAt     string `json:"run_at"`
	Message   string `json:"message"`
	Rationale string `json:"rationale,omitempty"`
}

type fbAutoCommentHookSkipKey struct{}

func NewFacebookPostWithCommentsTool(registry *Registry, cron store.CronStore) *FacebookPostWithCommentsTool {
	return &FacebookPostWithCommentsTool{registry: registry, cron: cron}
}

func (t *FacebookPostWithCommentsTool) Name() string { return "facebook_post_with_comments" }

func (t *FacebookPostWithCommentsTool) Description() string {
	return "Create a Facebook post through MCP and schedule final top-level comments in GoClaw cron without running the agent again. Reads scheduled-comment policy from Facebook MCP when available; callers must still provide final context-aware comment text."
}

func (t *FacebookPostWithCommentsTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"post_kind": map[string]any{
				"type":        "string",
				"enum":        []string{"text", "photo", "media"},
				"description": "Post type: text=fb_create_post, photo=fb_create_photo_post, media=fb_create_post_with_media",
			},
			"post_args": map[string]any{
				"type":                 "object",
				"description":          "Arguments for the selected Facebook MCP post tool, e.g. image_url/caption or message/media_ids.",
				"additionalProperties": true,
			},
			"page_id": map[string]any{
				"type":        "string",
				"description": "Optional Facebook page ID. Added to post and scheduled comment tool args.",
			},
			"post_comments": map[string]any{
				"type":                 "object",
				"description":          "Final public comments to schedule after the post is created. May contain only comments; scheduling policy is read from Facebook MCP when omitted.",
				"additionalProperties": true,
			},
			"mcp_post_tool_name": map[string]any{
				"type":        "string",
				"description": "Optional explicit MCP post tool name when multiple Facebook MCP servers expose the same tool.",
			},
			"mcp_comment_tool_name": map[string]any{
				"type":        "string",
				"description": "Optional explicit MCP fb_create_post_comment tool name.",
			},
			"mcp_comment_schedule_tool_name": map[string]any{
				"type":        "string",
				"description": "Optional explicit MCP fb_get_comment_schedule_config tool name.",
			},
		},
		"required": []string{"post_kind", "post_args"},
	}
}

func (t *FacebookPostWithCommentsTool) Execute(ctx context.Context, args map[string]any) *Result {
	reg := RegistryFromContext(ctx)
	if reg == nil {
		reg = t.registry
	}
	activeTool := t.withRegistry(reg)
	if activeTool.registry == nil {
		return ErrorResult("tool registry not available")
	}
	if activeTool.cron == nil {
		return ErrorResult("cron store not available")
	}

	postKind, _ := args["post_kind"].(string)
	postSuffix, err := facebookPostToolSuffix(postKind)
	if err != nil {
		return ErrorResult(err.Error())
	}
	postArgs, ok := args["post_args"].(map[string]any)
	if !ok {
		return ErrorResult("post_args object is required")
	}
	postArgs = cloneMap(postArgs)
	pageID, _ := args["page_id"].(string)
	if pageID != "" {
		if _, exists := postArgs["page_id"]; !exists {
			postArgs["page_id"] = pageID
		}
	}

	postTool, err := activeTool.resolveTool(stringArg(args, "mcp_post_tool_name"), postSuffix)
	if err != nil {
		return ErrorResult(err.Error())
	}

	plan, planSource, err := activeTool.resolveCommentPlan(ctx, args, pageID)
	if err != nil {
		return ErrorResult(err.Error())
	}

	internalCtx := context.WithValue(ctx, fbAutoCommentHookSkipKey{}, true)
	postResult := activeTool.registry.ExecuteWithContext(internalCtx, postTool, postArgs, ToolChannelFromCtx(ctx), ToolChatIDFromCtx(ctx), ToolPeerKindFromCtx(ctx), ToolSessionKeyFromCtx(ctx), nil)
	if postResult == nil {
		return ErrorResult(fmt.Sprintf("post tool %q returned nil result", postTool))
	}
	if postResult.IsError {
		return postResult
	}

	postID, parsed, err := extractFacebookPostID(postResult)
	if err != nil {
		return ErrorResult(err.Error())
	}

	var scheduled []fbScheduledComment
	if plan.Enabled && len(plan.Comments) > 0 {
		commentTool, err := activeTool.resolveTool(stringArg(args, "mcp_comment_tool_name"), "__fb_create_post_comment")
		if err != nil {
			return ErrorResult(err.Error())
		}
		scheduled, err = activeTool.scheduleComments(ctx, postID, pageID, commentTool, plan)
		if err != nil {
			return ErrorResult(err.Error())
		}
	}

	out := map[string]any{
		"post_id":             postID,
		"post_tool":           postTool,
		"post_result":         parsed,
		"comment_plan_source": planSource,
		"scheduled_comments":  scheduled,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return NewResult(string(b))
}

func (t *FacebookPostWithCommentsTool) BeforeExecute(ctx context.Context, reg *Registry, name string, args map[string]any) *Result {
	if ctx.Value(fbAutoCommentHookSkipKey{}) == true || !isFacebookMCPPostTool(name) {
		return nil
	}
	if reg == nil {
		reg = t.registry
	}
	plan, _, err := t.withRegistry(reg).resolveCommentPlan(ctx, t.autoHookArgs(reg, name, args), stringArg(args, "page_id"))
	if err != nil {
		return ErrorResult(err.Error())
	}
	if plan.Enabled && len(plan.Comments) == 0 {
		return ErrorResult(fmt.Sprintf("Facebook MCP comment schedule is enabled for this page; include %d final context-aware comments in this same %s call using post_comments.comments", fbMaxInt(plan.Count, 1), name))
	}
	return nil
}

func (t *FacebookPostWithCommentsTool) AfterExecute(ctx context.Context, reg *Registry, name string, args map[string]any, result *Result) *Result {
	if ctx.Value(fbAutoCommentHookSkipKey{}) == true || !isFacebookMCPPostTool(name) || result == nil || result.IsError {
		return nil
	}
	if reg == nil {
		reg = t.registry
	}
	activeTool := t.withRegistry(reg)
	plan, source, err := activeTool.resolveCommentPlan(ctx, activeTool.autoHookArgs(reg, name, args), stringArg(args, "page_id"))
	if err != nil || !plan.Enabled || len(plan.Comments) == 0 {
		return nil
	}
	postID, _, err := extractFacebookPostID(result)
	if err != nil {
		return nil
	}
	commentTool := companionFacebookMCPTool(name, "__fb_create_post_comment")
	if _, ok := reg.Get(commentTool); !ok {
		return nil
	}
	scheduled, err := activeTool.scheduleComments(ctx, postID, stringArg(args, "page_id"), commentTool, plan)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Facebook post was created but scheduling comments failed: %v", err))
	}
	data := map[string]any{
		"post_id":             postID,
		"comment_plan_source": source,
		"scheduled_comments":  scheduled,
	}
	b, _ := json.MarshalIndent(data, "", "  ")
	suffix := "\n\nGoClaw scheduled Facebook comments:\n" + string(b)
	next := *result
	if next.ForLLM != "" {
		next.ForLLM += suffix
	} else {
		next.ForLLM = suffix
	}
	if next.ForUser != "" {
		next.ForUser += suffix
	}
	return &next
}

func (t *FacebookPostWithCommentsTool) withRegistry(reg *Registry) *FacebookPostWithCommentsTool {
	if reg == nil || reg == t.registry {
		return t
	}
	return &FacebookPostWithCommentsTool{registry: reg, cron: t.cron}
}

func (t *FacebookPostWithCommentsTool) autoHookArgs(reg *Registry, postTool string, args map[string]any) map[string]any {
	out := map[string]any{}
	if v, ok := args["post_comments"]; ok {
		out["post_comments"] = v
	}
	if configTool := companionFacebookMCPTool(postTool, "__fb_get_comment_schedule_config"); configTool != "" {
		if reg == nil {
			reg = t.registry
		}
		if _, ok := reg.Get(configTool); ok {
			out["mcp_comment_schedule_tool_name"] = configTool
		}
	}
	return out
}

func (t *FacebookPostWithCommentsTool) resolveCommentPlan(ctx context.Context, args map[string]any, pageID string) (fbCommentPlan, string, error) {
	localPlan, hasLocal, err := parseFBCommentPlan(args["post_comments"])
	if err != nil {
		return fbCommentPlan{}, "", err
	}

	configTool, err := t.resolveTool(stringArg(args, "mcp_comment_schedule_tool_name"), "__fb_get_comment_schedule_config")
	if err != nil {
		if hasLocal {
			if localPlan.Enabled && len(localPlan.Comments) == 0 {
				return fbCommentPlan{}, "", fmt.Errorf("post_comments enabled but no comments were provided")
			}
			return localPlan, "request", nil
		}
		return fbCommentPlan{}, "none", nil
	}

	configArgs := map[string]any{}
	if pageID != "" {
		configArgs["page_id"] = pageID
	}
	configResult := t.registry.ExecuteWithContext(ctx, configTool, configArgs, ToolChannelFromCtx(ctx), ToolChatIDFromCtx(ctx), ToolPeerKindFromCtx(ctx), ToolSessionKeyFromCtx(ctx), nil)
	if configResult == nil {
		return fbCommentPlan{}, "", fmt.Errorf("comment schedule config tool %q returned nil result", configTool)
	}
	if configResult.IsError {
		if hasLocal {
			return localPlan, "request", nil
		}
		return fbCommentPlan{}, "", fmt.Errorf("comment schedule config tool %q failed: %s", configTool, firstNonEmpty(configResult.ForLLM, configResult.ForUser))
	}
	policy, err := extractFBCommentPolicy(configResult)
	if err != nil {
		if hasLocal {
			return localPlan, "request", nil
		}
		return fbCommentPlan{}, "", err
	}
	plan := mergeFBCommentPolicy(policy, localPlan, hasLocal)
	if !plan.Enabled {
		return fbCommentPlan{}, "mcp", nil
	}
	if len(plan.Comments) == 0 {
		return fbCommentPlan{}, "", fmt.Errorf("Facebook MCP comment schedule is enabled for this page but no final comments were provided; generate %d context-aware comments and pass them in post_comments.comments", fbMaxInt(plan.Count, 1))
	}
	if plan.Count > 0 && len(plan.Comments) != plan.Count {
		return fbCommentPlan{}, "", fmt.Errorf("Facebook MCP comment schedule expects %d comments, got %d", plan.Count, len(plan.Comments))
	}
	if err := validateFBCommentPlan(plan); err != nil {
		return fbCommentPlan{}, "", err
	}
	return plan, "mcp", nil
}

func (t *FacebookPostWithCommentsTool) scheduleComments(ctx context.Context, postID, pageID, commentTool string, plan fbCommentPlan) ([]fbScheduledComment, error) {
	comments := append([]fbPlannedComment(nil), plan.Comments...)
	if plan.RandomOrder {
		shuffleComments(comments)
	}
	runAt, err := randomCommentTimes(time.Now(), plan.WindowMS, plan.MinGapMS, len(comments))
	if err != nil {
		return nil, err
	}

	agentID := resolveAgentIDString(ctx)
	userID := store.UserIDFromContext(ctx)
	scheduled := make([]fbScheduledComment, 0, len(comments))
	shortID := sanitizeJobName(shortPostID(postID))
	for i, c := range comments {
		args := map[string]any{"post_id": postID, "message": c.Message}
		if pageID != "" {
			args["page_id"] = pageID
		}
		name := fmt.Sprintf("fb-comment-%s-%02d", shortID, i+1)
		job, err := t.cron.AddToolCallJob(ctx, name, runAt[i].UnixMilli(), commentTool, args, agentID, userID)
		if err != nil {
			return nil, err
		}
		scheduled = append(scheduled, fbScheduledComment{
			JobID:     job.ID,
			RunAtMS:   runAt[i].UnixMilli(),
			RunAt:     runAt[i].Format(time.RFC3339),
			Message:   c.Message,
			Rationale: c.Rationale,
		})
	}
	return scheduled, nil
}

func facebookPostToolSuffix(kind string) (string, error) {
	switch kind {
	case "text":
		return "__fb_create_post", nil
	case "photo":
		return "__fb_create_photo_post", nil
	case "media":
		return "__fb_create_post_with_media", nil
	default:
		return "", fmt.Errorf("invalid post_kind %q (must be text, photo, or media)", kind)
	}
}

func isFacebookMCPPostTool(name string) bool {
	return strings.HasPrefix(name, "mcp_") &&
		(strings.HasSuffix(name, "__fb_create_post") ||
			strings.HasSuffix(name, "__fb_create_photo_post") ||
			strings.HasSuffix(name, "__fb_create_post_with_media"))
}

func companionFacebookMCPTool(name, suffix string) string {
	idx := strings.LastIndex(name, "__")
	if idx < 0 {
		return ""
	}
	return name[:idx] + suffix
}

func (t *FacebookPostWithCommentsTool) resolveTool(explicit, suffix string) (string, error) {
	if explicit != "" {
		if _, ok := t.registry.Get(explicit); ok {
			return explicit, nil
		}
		if t.registry.TryActivateDeferred(explicit) {
			if _, ok := t.registry.Get(explicit); ok {
				return explicit, nil
			}
		}
		return "", fmt.Errorf("explicit MCP tool %q is not registered", explicit)
	}
	var matches []string
	for _, name := range t.registry.List() {
		if strings.HasSuffix(name, suffix) {
			matches = append(matches, name)
		}
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no MCP tool found with suffix %q; pass explicit MCP tool name if it is deferred or unavailable", suffix)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("multiple MCP tools match suffix %q: %s; pass explicit MCP tool name", suffix, strings.Join(matches, ", "))
	}
	return matches[0], nil
}

func parseFBCommentPlan(raw any) (fbCommentPlan, bool, error) {
	if raw == nil {
		return fbCommentPlan{}, false, nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return fbCommentPlan{}, true, fmt.Errorf("post_comments must be an object")
	}
	enabled, _ := m["enabled"].(bool)
	windowMS := int64(numberOrZero(m["window_ms"]))
	if windowMS == 0 {
		windowMS = int64(numberOrZero(m["windowMs"]))
	}
	minGapMS := int64(numberOrZero(m["min_gap_ms"]))
	if minGapMS == 0 {
		minGapMS = int64(numberOrZero(m["minGapMs"]))
	}
	randomOrder := true
	randomOrderSet := false
	if v, ok := m["random_order"].(bool); ok {
		randomOrder = v
		randomOrderSet = true
	} else if v, ok := m["randomOrder"].(bool); ok {
		randomOrder = v
		randomOrderSet = true
	}

	count := int(numberOrZero(m["comment_count"]))
	if count == 0 {
		count = int(numberOrZero(m["commentCount"]))
	}

	var comments []fbPlannedComment
	if rawComments, ok := m["comments"].([]any); ok {
		comments = make([]fbPlannedComment, 0, len(rawComments))
		for i, raw := range rawComments {
			c, err := parsePlannedComment(raw)
			if err != nil {
				return fbCommentPlan{}, true, fmt.Errorf("post_comments.comments[%d]: %w", i, err)
			}
			comments = append(comments, c)
		}
	}
	plan := fbCommentPlan{Enabled: enabled, Count: count, WindowMS: windowMS, RandomOrder: randomOrder, RandomOrderSet: randomOrderSet, MinGapMS: minGapMS, Comments: comments}
	if enabled || len(comments) > 0 {
		if err := validateFBCommentPlan(plan); err != nil {
			return fbCommentPlan{}, true, err
		}
	}
	return plan, true, nil
}

func validateFBCommentPlan(plan fbCommentPlan) error {
	if !plan.Enabled {
		return nil
	}
	if plan.WindowMS <= 0 {
		return fmt.Errorf("post_comments.window_ms must be positive")
	}
	if len(plan.Comments) == 0 {
		return fmt.Errorf("post_comments.comments must contain at least one comment")
	}
	seen := map[string]bool{}
	unique := 0
	for i, c := range plan.Comments {
		if err := validateFacebookCommentMessage(c.Message); err != nil {
			return fmt.Errorf("post_comments.comments[%d]: %w", i, err)
		}
		key := strings.ToLower(strings.TrimSpace(c.Message))
		if !seen[key] {
			unique++
			seen[key] = true
		}
	}
	if unique <= 1 && len(plan.Comments) > 1 {
		return fmt.Errorf("post_comments.comments must not all be duplicates")
	}
	if plan.MinGapMS > 0 && int64(len(plan.Comments)-1)*plan.MinGapMS > plan.WindowMS {
		return fmt.Errorf("post_comments.min_gap_ms impossible: need at least %dms for %d comments in %dms window", int64(len(plan.Comments)-1)*plan.MinGapMS, len(plan.Comments), plan.WindowMS)
	}
	return nil
}

func mergeFBCommentPolicy(policy fbCommentPlan, local fbCommentPlan, hasLocal bool) fbCommentPlan {
	plan := policy
	if hasLocal {
		if local.WindowMS > 0 {
			plan.WindowMS = local.WindowMS
		}
		if local.MinGapMS > 0 {
			plan.MinGapMS = local.MinGapMS
		}
		if local.Count > 0 {
			plan.Count = local.Count
		}
		if local.RandomOrderSet {
			plan.RandomOrder = local.RandomOrder
		}
		if len(local.Comments) > 0 {
			plan.Comments = local.Comments
		}
	}
	return plan
}

func extractFBCommentPolicy(result *Result) (fbCommentPlan, error) {
	text := firstNonEmpty(result.ForLLM, result.ForUser)
	for _, candidate := range jsonObjectCandidates(text) {
		var m map[string]any
		if json.Unmarshal([]byte(candidate), &m) != nil {
			continue
		}
		if nested, ok := m["comment_schedule"].(map[string]any); ok {
			plan := fbCommentPolicyFromMap(nested)
			if enabled, ok := m["enabled"].(bool); ok {
				plan.Enabled = enabled
			}
			return plan, nil
		}
		if nested, ok := m["data"].(map[string]any); ok {
			if schedule, ok := nested["comment_schedule"].(map[string]any); ok {
				plan := fbCommentPolicyFromMap(schedule)
				if enabled, ok := nested["enabled"].(bool); ok {
					plan.Enabled = enabled
				}
				return plan, nil
			}
		}
		return fbCommentPolicyFromMap(m), nil
	}
	return fbCommentPlan{}, fmt.Errorf("comment schedule config result did not contain JSON")
}

func fbCommentPolicyFromMap(m map[string]any) fbCommentPlan {
	enabled, _ := m["enabled"].(bool)
	count := int(numberOrZero(m["comment_count"]))
	if count == 0 {
		count = int(numberOrZero(m["commentCount"]))
	}
	windowMS := int64(numberOrZero(m["window_ms"]))
	if windowMS == 0 {
		windowMS = int64(numberOrZero(m["windowMs"]))
	}
	minGapMS := int64(numberOrZero(m["min_gap_ms"]))
	if minGapMS == 0 {
		minGapMS = int64(numberOrZero(m["minGapMs"]))
	}
	randomOrder := true
	randomOrderSet := false
	if v, ok := m["random_order"].(bool); ok {
		randomOrder = v
		randomOrderSet = true
	} else if v, ok := m["randomOrder"].(bool); ok {
		randomOrder = v
		randomOrderSet = true
	}
	return fbCommentPlan{Enabled: enabled, PolicyFound: true, Count: count, WindowMS: windowMS, MinGapMS: minGapMS, RandomOrder: randomOrder, RandomOrderSet: randomOrderSet}
}

func parsePlannedComment(raw any) (fbPlannedComment, error) {
	switch v := raw.(type) {
	case string:
		return fbPlannedComment{Message: strings.TrimSpace(v)}, nil
	case map[string]any:
		msg, _ := v["message"].(string)
		rationale, _ := v["rationale"].(string)
		return fbPlannedComment{Message: strings.TrimSpace(msg), Rationale: strings.TrimSpace(rationale)}, nil
	default:
		return fbPlannedComment{}, fmt.Errorf("must be a string or object with message")
	}
}

func validateFacebookCommentMessage(message string) error {
	if strings.TrimSpace(message) == "" {
		return fmt.Errorf("message is required")
	}
	if len([]rune(message)) > facebookCommentMaxChars {
		return fmt.Errorf("message exceeds %d characters", facebookCommentMaxChars)
	}
	lower := strings.ToLower(message)
	markers := []string{
		"g\u1ee3i \u00fd reply", "goi y reply", "suggested reply",
		"n\u1ebfu anh mu\u1ed1n", "neu anh muon",
		"n\u1ebfu b\u1ea1n mu\u1ed1n", "neu ban muon",
		"m\u00ecnh c\u00f3 th\u1ec3 l\u00e0m ti\u1ebfp", "minh co the lam tiep",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return fmt.Errorf("message contains internal draft marker %q", marker)
		}
	}
	if strings.Contains(message, "\n- ") && strings.Contains(lower, "m\u1eabu reply") {
		return fmt.Errorf("message looks like an internal proposal list")
	}
	return nil
}

func randomCommentTimes(now time.Time, windowMS, minGapMS int64, count int) ([]time.Time, error) {
	if count == 0 {
		return nil, nil
	}
	offsets := make([]int64, count)
	if minGapMS > 0 {
		free := windowMS - int64(count-1)*minGapMS
		if free < 0 {
			return nil, fmt.Errorf("min_gap_ms is too large for the window")
		}
		for i := range offsets {
			offsets[i] = randomInt64(free + 1)
		}
		sort.Slice(offsets, func(i, j int) bool { return offsets[i] < offsets[j] })
		for i := range offsets {
			offsets[i] += int64(i) * minGapMS
		}
	} else {
		for i := range offsets {
			offsets[i] = randomInt64(windowMS + 1)
		}
		sort.Slice(offsets, func(i, j int) bool { return offsets[i] < offsets[j] })
	}
	out := make([]time.Time, count)
	for i, offset := range offsets {
		out[i] = now.Add(time.Duration(offset) * time.Millisecond)
	}
	return out, nil
}

func shuffleComments(comments []fbPlannedComment) {
	for i := len(comments) - 1; i > 0; i-- {
		j := int(randomInt64(int64(i + 1)))
		comments[i], comments[j] = comments[j], comments[i]
	}
}

func randomInt64(max int64) int64 {
	if max <= 1 {
		return 0
	}
	n, err := rand.Int(rand.Reader, big.NewInt(max))
	if err != nil {
		return time.Now().UnixNano() % max
	}
	return n.Int64()
}

func extractFacebookPostID(result *Result) (string, map[string]any, error) {
	text := result.ForLLM
	if text == "" {
		text = result.ForUser
	}
	parsed := map[string]any{}
	for _, candidate := range jsonObjectCandidates(text) {
		var m map[string]any
		if json.Unmarshal([]byte(candidate), &m) == nil {
			if id := stringField(m, "post_id"); id != "" {
				return id, m, nil
			}
			if id := stringField(m, "id"); id != "" {
				return id, m, nil
			}
			if nested, ok := m["data"].(map[string]any); ok {
				if id := stringField(nested, "post_id"); id != "" {
					return id, m, nil
				}
				if id := stringField(nested, "id"); id != "" {
					return id, m, nil
				}
			}
			parsed = m
		}
	}
	return "", parsed, fmt.Errorf("Facebook post tool result did not contain post_id or id")
}

func jsonObjectCandidates(text string) []string {
	var out []string
	for start := strings.Index(text, "{"); start >= 0; {
		depth := 0
		inString := false
		escape := false
		for i := start; i < len(text); i++ {
			ch := text[i]
			if inString {
				if escape {
					escape = false
				} else if ch == '\\' {
					escape = true
				} else if ch == '"' {
					inString = false
				}
				continue
			}
			switch ch {
			case '"':
				inString = true
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					out = append(out, text[start:i+1])
					goto next
				}
			}
		}
	next:
		nextStart := strings.Index(text[start+1:], "{")
		if nextStart < 0 {
			break
		}
		start += nextStart + 1
	}
	return out
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func stringArg(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

func numberOrZero(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	default:
		return 0
	}
}

func stringField(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func fbMaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var unsafeJobName = regexp.MustCompile(`[^a-zA-Z0-9-]+`)

func sanitizeJobName(s string) string {
	s = unsafeJobName.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "post"
	}
	return s
}

func shortPostID(postID string) string {
	if idx := strings.LastIndex(postID, "_"); idx >= 0 && idx < len(postID)-1 {
		postID = postID[idx+1:]
	}
	if len(postID) > 24 {
		return postID[len(postID)-24:]
	}
	return postID
}
