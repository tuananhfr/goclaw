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
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// blog_audit is a read-only, tool-free pass over a finished document: it
// scores SEO / AEO, lists issues and proposes rewrites the editor can apply
// as blog_rewrite jobs. It never touches the document.
const (
	blogAuditNoTools  = "blog-audit/no-tools"
	blogAuditTimeout  = 90 * time.Second
	blogAuditUnread   = "audit_unreadable"
	blogAuditToolName = "submit_blog_audit"
	// Vòng 1 gọi tool bắt buộc; vòng 2 là lần ép cuối khi vòng 1 bị từ chối.
	blogAuditIterations = 2
)

var blogAuditSeverities = map[string]bool{"error": true, "warning": true, "info": true}

func (s *JobService) runBlogAudit(ctx context.Context, job *store.TekshotJob, request map[string]any) (any, string, error) {
	if s.agents == nil {
		return nil, "", fmt.Errorf("agent router is not configured")
	}
	document, ok := request["document"].(map[string]any)
	if !ok || len(document) == 0 {
		return nil, "", fmt.Errorf("document is required for an audit")
	}
	loop, err := s.agents.Get(store.WithTenantID(ctx, store.MasterTenantID), job.AgentKey)
	if err != nil {
		return nil, "", err
	}
	userID := "tekshot-" + job.ExternalUserID
	runCtx := store.WithTenantID(ctx, store.MasterTenantID)
	runCtx = store.WithUserID(runCtx, userID)
	runCtx = store.WithAgentKey(runCtx, job.AgentKey)
	runCtx, cancel := context.WithTimeout(runCtx, blogAuditTimeout)
	defer cancel()

	// Tool bắt buộc thay vì đọc JSON trong câu trả lời: một lượt tự do đã từng
	// bị model tiêu vào list_files, để lại content rỗng và chặn xuất bản oan.
	collector := NewBlogAuditCollector(blogSectionIDs(document))
	runID := uuid.NewString()
	result, err := loop.Run(runCtx, agent.RunRequest{
		SessionKey:     job.SessionKey + ":audit:" + runID,
		Message:        buildBlogAuditPrompt(request),
		Channel:        "tekshot_job",
		ChannelType:    "tekshot",
		ChatID:         userID,
		PeerKind:       "direct",
		Addressed:      true,
		RunID:          runID,
		UserID:         userID,
		SenderID:       userID,
		ToolAllow:      []string{blogAuditNoTools},
		EphemeralTools: []tools.Tool{collector},
		ToolChoice:     &providers.ToolChoice{Mode: "function", Name: blogAuditToolName},
		MaxIterations:  blogAuditIterations,
		SkillFilter:    []string{},
		LightContext:   true,
		HistoryLimit:   1,
		TraceName:      "tekshot blog audit",
		TraceTags:      []string{"tekshot", "blog", "audit"},
	})
	out := collector.Report()
	if out == nil && result != nil {
		// Model đôi khi trả JSON thẳng trong câu trả lời; vẫn nhận, đừng chặn.
		if parsed, parseErr := parseBlogAuditReply(result.Content); parseErr == nil {
			out = normalizeBlogAudit(parsed, blogSectionIDs(document))
		}
	}
	if out == nil {
		reason := "agent did not call " + blogAuditToolName
		if err != nil {
			reason = err.Error()
		}
		slog.Warn("tekshot: blog audit unreadable, blocking publish", "job", job.ID.String(), "reason", reason)
		return auditUnreadableResult(reason), "Blog audit unreadable", nil
	}
	if result != nil && result.Usage != nil {
		out["usage"] = result.Usage
	}
	return out, "Blog audit completed", nil
}

func parseBlogAuditReply(reply string) (map[string]any, error) {
	object, err := extractJSONObject(reply)
	if err != nil {
		return nil, err
	}
	var parsed map[string]any
	if err := decodeJSONInto(object, &parsed); err != nil {
		return nil, err
	}
	if parsed == nil {
		return nil, fmt.Errorf("audit reply is not an object")
	}
	return parsed, nil
}

// auditUnreadableResult is the fail-closed answer: a blocking error issue,
// zero scores, so Drupal refuses to publish until a readable audit exists.
func auditUnreadableResult(reason string) map[string]any {
	return map[string]any{
		"seo_score":            0,
		"ai_readability_score": 0,
		"issues": []any{map[string]any{
			"code": blogAuditUnread, "severity": "error",
			"message": "Không đọc được kết quả audit AI: " + reason, "path": "audit",
		}},
		"suggestions": []any{},
	}
}

// normalizeBlogAudit clamps scores, forces unknown severities to error and
// unknown scopes to "all" — the unsafe reading, never the lenient one.
func normalizeBlogAudit(raw map[string]any, sectionIDs map[string]bool) map[string]any {
	issues := []any{}
	if list, ok := raw["issues"].([]any); ok {
		for _, item := range list {
			entry, _ := item.(map[string]any)
			message := strings.TrimSpace(stringFromMap(entry, "message"))
			if message == "" {
				continue
			}
			severity := strings.ToLower(strings.TrimSpace(stringFromMap(entry, "severity")))
			if !blogAuditSeverities[severity] {
				severity = "error"
			}
			code := strings.TrimSpace(stringFromMap(entry, "code"))
			if code == "" {
				code = "ai_issue"
			}
			issues = append(issues, map[string]any{
				"code": code, "severity": severity, "message": cutRunes(message, 500),
				"path": cutRunes(strings.TrimSpace(stringFromMap(entry, "path")), 120),
			})
		}
	}
	suggestions := []any{}
	if list, ok := raw["suggestions"].([]any); ok {
		for _, item := range list {
			entry, _ := item.(map[string]any)
			instruction := strings.TrimSpace(stringFromMap(entry, "instruction"))
			if instruction == "" {
				continue
			}
			scope := strings.TrimSpace(stringFromMap(entry, "scope"))
			if validateBlogScope(scope) != nil || (strings.HasPrefix(scope, blogScopeSectionPrfx) && !sectionIDs[strings.TrimPrefix(scope, blogScopeSectionPrfx)]) {
				scope = blogScopeAll
			}
			title := strings.TrimSpace(stringFromMap(entry, "title"))
			if title == "" {
				title = cutRunes(instruction, 80)
			}
			suggestions = append(suggestions, map[string]any{
				"title": cutRunes(title, 120), "instruction": cutRunes(instruction, 1000), "scope": scope,
			})
		}
	}
	return map[string]any{
		"seo_score":            clampScore(numberFromMap(raw, "seo_score")),
		"ai_readability_score": clampScore(numberFromMap(raw, "ai_readability_score")),
		"issues":               issues,
		"suggestions":          suggestions,
	}
}

// BlogAuditCollector là kênh ra duy nhất của một lượt audit; nó chỉ chuẩn hoá,
// không bao giờ từ chối, vì audit rỗng cũng là một kết quả hợp lệ.
type BlogAuditCollector struct {
	sectionIDs map[string]bool
	report     map[string]any
}

func NewBlogAuditCollector(sectionIDs map[string]bool) *BlogAuditCollector {
	return &BlogAuditCollector{sectionIDs: sectionIDs}
}

func (t *BlogAuditCollector) Name() string { return blogAuditToolName }

func (t *BlogAuditCollector) Description() string {
	return "Submit the SEO/AEO audit of the article: two scores, the issues found and the rewrite suggestions. Call exactly once."
}

func (t *BlogAuditCollector) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"seo_score":            map[string]any{"type": "integer", "description": "0-100: title, meta, keyword placement, heading structure, internal logic for search engines."},
			"ai_readability_score": map[string]any{"type": "integer", "description": "0-100: how well an answer engine could quote this article — direct answers, definitions up front, FAQ, takeaways, tables."},
			"issues": map[string]any{
				"type":        "array",
				"description": "What is wrong. Empty array when nothing is.",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"code":     map[string]any{"type": "string", "description": "snake_case identifier of the problem."},
						"severity": map[string]any{"type": "string", "enum": []string{"error", "warning", "info"}, "description": "error only for something that must be fixed before publishing."},
						"message":  map[string]any{"type": "string", "description": "What is wrong, in the article's language."},
						"path":     map[string]any{"type": "string", "description": "Where, e.g. document.sections[s2]; empty string when it is the whole article."},
					},
					"required": []string{"code", "severity", "message", "path"},
				},
			},
			"suggestions": map[string]any{
				"type":        "array",
				"description": "2-5 rewrites a writer could execute, most valuable first. Empty array when there is nothing to improve.",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"title":       map[string]any{"type": "string", "description": "Short name of the change."},
						"instruction": map[string]any{"type": "string", "description": "The instruction itself, in the article's language."},
						"scope":       map[string]any{"type": "string", "description": "\"all\", \"presentation\", or \"section:<id>\" of a section that exists."},
					},
					"required": []string{"title", "instruction", "scope"},
				},
			},
		},
		"required": []string{"seo_score", "ai_readability_score", "issues", "suggestions"},
	}
}

func (t *BlogAuditCollector) Execute(_ context.Context, args map[string]any) *tools.Result {
	t.report = normalizeBlogAudit(args, t.sectionIDs)
	return tools.SilentResult("Blog audit captured.")
}

func (t *BlogAuditCollector) Report() map[string]any {
	return t.report
}

func clampScore(value float64) int {
	switch {
	case value < 0:
		return 0
	case value > 100:
		return 100
	default:
		return int(value)
	}
}

func blogSectionIDs(document map[string]any) map[string]bool {
	ids := map[string]bool{}
	sections, _ := document["sections"].([]any)
	for _, raw := range sections {
		if section, ok := raw.(map[string]any); ok {
			if id := stringFromMap(section, "id"); id != "" {
				ids[id] = true
			}
		}
	}
	return ids
}

func buildBlogAuditPrompt(request map[string]any) string {
	language := blogSnapshotFromRequest(request).Language
	var sb strings.Builder
	sb.WriteString("You are an SEO and AEO (answer-engine optimisation) auditor for one website's blog. You only read; you never rewrite.\n")
	sb.WriteString("Deliver the audit by calling " + blogAuditToolName + " exactly once. Do not answer with plain text, and do not call any other tool.\n")
	sb.WriteString("Write messages, titles and instructions in language \"" + language + "\".\n")
	sb.WriteString("seo_score: title/meta/keyword/heading structure/internal logic for search engines. ai_readability_score: how well an answer engine could quote this article — direct answers, definitions up front, FAQ, takeaways, tables.\n")
	sb.WriteString("Use severity error only for something that must be fixed before publishing (misleading claim, missing answer to the title's question, keyword absent from title). Do not repeat SEO RULES already listed; add what a rule engine cannot see.\n")
	sb.WriteString("Every suggestion must name a scope that exists: section:<id> for one section, presentation for layout only, all otherwise. 2-5 suggestions, most valuable first.\n")
	sb.WriteString("Do not invent facts, sources or figures; if the article makes an unsupported claim, that is an issue.\n\n")
	snapshot, _ := request["snapshot"].(map[string]any)
	if snapshot != nil {
		writeChecklistChatValue(&sb, "WEBSITE", snapshot["website"])
	}
	writeChecklistChatValue(&sb, "BRAND PROFILE", request["brand_profile"])
	writeChecklistChatValue(&sb, "SEO RULES (already evaluated by the rule engine)", request["seo_rules"])
	writeChecklistChatValue(&sb, "SEO FIELDS", request["seo"])
	writeChecklistChatValue(&sb, "PRESENTATION", request["presentation"])
	sb.WriteString("## DOCUMENT\n")
	sb.WriteString(compactJSON(request["document"]))
	sb.WriteString("\n")
	return sb.String()
}
