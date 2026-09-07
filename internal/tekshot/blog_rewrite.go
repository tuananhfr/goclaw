package tekshot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	blogScopeAll          = "all"
	blogScopePresentation = "presentation"
	blogScopeSectionPrfx  = "section:"
)

// runBlogRewrite: scope "all" gets the whole document back from the model;
// "section:<id>" and "presentation" only ever receive the changed part and
// splice it into the original — the model never has to echo the article.
func (s *JobService) runBlogRewrite(ctx context.Context, job *store.TekshotJob, request map[string]any) (any, string, error) {
	if strings.TrimSpace(stringFromMap(request, "instruction")) == "" {
		return nil, "", fmt.Errorf("instruction is required")
	}
	rawOriginal, ok := request["document"].(map[string]any)
	if !ok || len(rawOriginal) == 0 {
		return nil, "", fmt.Errorf("document is required for a rewrite")
	}
	scope := strings.TrimSpace(stringFromMap(request, "scope"))
	if scope == "" {
		scope = blogScopeAll
	}
	if err := validateBlogScope(scope); err != nil {
		return nil, "", err
	}
	snap := blogSnapshotFromRequest(request)
	original, err := validateBlogDocument(rawOriginal, snap)
	if err != nil {
		return nil, "", fmt.Errorf("current document is invalid: %w", err)
	}
	currentPresentation, err := normalizedPresentation(request["presentation"], snap)
	if err != nil {
		return nil, "", fmt.Errorf("current presentation is invalid: %w", err)
	}
	emptySEO := map[string]any{"meta_title": "", "meta_description": "", "keywords": "", "focus_keyword": ""}
	traceTags := []string{"tekshot", "blog", "rewrite"}

	var collector blogCollector
	switch {
	case scope == blogScopePresentation:
		collector = NewBlogPresentationCollector(snap)
	case strings.HasPrefix(scope, blogScopeSectionPrfx):
		id := strings.TrimPrefix(scope, blogScopeSectionPrfx)
		if !blogHasSection(original, id) {
			return nil, "", fmt.Errorf("section %s does not exist in the document", id)
		}
		collector = NewBlogSectionCollector(id)
	default:
		collector = NewBlogDocumentCollector(snap)
	}

	report, usage, err := s.runBlogCollector(ctx, job, buildBlogRewritePrompt(request, scope), "tekshot blog rewrite", traceTags, collector)
	if err != nil {
		return nil, "", err
	}

	var result map[string]any
	switch {
	case scope == blogScopePresentation:
		result = map[string]any{"reply": report["reply"], "document": original, "presentation": report["presentation"], "seo": emptySEO}
	case strings.HasPrefix(scope, blogScopeSectionPrfx):
		section, _ := report["section"].(map[string]any)
		spliced, err := spliceBlogSection(original, section)
		if err != nil {
			return nil, "", fmt.Errorf("MODEL_OUTPUT_INVALID: %w", err)
		}
		updated, err := validateBlogDocument(spliced, snap)
		if err != nil {
			return nil, "", fmt.Errorf("MODEL_OUTPUT_INVALID: %w", err)
		}
		if err := enforceBlogRewriteScope(original, updated, scope); err != nil {
			return nil, "", fmt.Errorf("MODEL_OUTPUT_INVALID: %w", err)
		}
		var presentation any
		if currentPresentation != nil {
			presentation = currentPresentation
		}
		result = map[string]any{"reply": report["reply"], "document": updated, "presentation": presentation, "seo": emptySEO}
	default:
		result = report
	}
	if usage != nil {
		result["usage"] = usage
	}
	return result, "Blog document rewritten (" + scope + ")", nil
}

func blogHasSection(document map[string]any, id string) bool {
	sections, _ := document["sections"].([]any)
	for _, raw := range sections {
		if section, ok := raw.(map[string]any); ok && stringFromMap(section, "id") == id {
			return true
		}
	}
	return false
}

func validateBlogScope(scope string) error {
	switch {
	case scope == blogScopeAll, scope == blogScopePresentation:
		return nil
	case strings.HasPrefix(scope, blogScopeSectionPrfx):
		id := strings.TrimPrefix(scope, blogScopeSectionPrfx)
		if !blogSectionID.MatchString(id) {
			return fmt.Errorf("scope section id %q is invalid", id)
		}
		return nil
	default:
		return fmt.Errorf("scope %q is not supported (all | section:<id> | presentation)", scope)
	}
}

// enforceBlogRewriteScope is the guarantee the chat makes to the editor: an
// edit scoped to one section cannot silently rewrite the rest, and a layout
// change cannot touch a word. Compared on canonical JSON, not on trust.
func enforceBlogRewriteScope(original, updated map[string]any, scope string) error {
	if err := validateBlogScope(scope); err != nil {
		return err
	}
	if updated == nil {
		return fmt.Errorf("rewrite returned no document")
	}
	switch {
	case scope == blogScopeAll:
		return nil
	case scope == blogScopePresentation:
		if canonicalJSON(original) != canonicalJSON(updated) {
			return fmt.Errorf("presentation scope changed the document text")
		}
		return nil
	}

	sectionID := strings.TrimPrefix(scope, blogScopeSectionPrfx)
	origSections, _ := original["sections"].([]any)
	newSections, _ := updated["sections"].([]any)
	if len(origSections) != len(newSections) {
		return fmt.Errorf("section scope %s changed the number of sections (%d → %d)", sectionID, len(origSections), len(newSections))
	}
	found := false
	for i := range origSections {
		origSec, _ := origSections[i].(map[string]any)
		newSec, _ := newSections[i].(map[string]any)
		origID := stringFromMap(origSec, "id")
		if stringFromMap(newSec, "id") != origID {
			return fmt.Errorf("section scope %s changed section order or ids", sectionID)
		}
		if origID == sectionID {
			found = true
			continue
		}
		if canonicalJSON(origSec) != canonicalJSON(newSec) {
			return fmt.Errorf("section scope %s changed section %s", sectionID, origID)
		}
	}
	if !found {
		return fmt.Errorf("section %s does not exist in the document", sectionID)
	}
	for key, value := range original {
		if key == "sections" {
			continue
		}
		if canonicalJSON(value) != canonicalJSON(updated[key]) {
			return fmt.Errorf("section scope %s changed document.%s", sectionID, key)
		}
	}
	for key := range updated {
		if key == "sections" {
			continue
		}
		if _, exists := original[key]; !exists && updated[key] != nil {
			return fmt.Errorf("section scope %s added document.%s", sectionID, key)
		}
	}
	return nil
}

// canonicalJSON: encoding/json sorts map keys, so two maps with the same
// content encode identically. Numbers pass through a float64 round-trip so an
// int from the validator and a float64 from JSON compare equal.
func canonicalJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	var generic any
	if err := json.Unmarshal(encoded, &generic); err != nil {
		return string(encoded)
	}
	again, err := json.Marshal(generic)
	if err != nil {
		return string(encoded)
	}
	return string(again)
}

func cloneJSON(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var clone map[string]any
	if err := json.Unmarshal(encoded, &clone); err != nil {
		return nil
	}
	return clone
}

func buildBlogRewritePrompt(request map[string]any, scope string) string {
	var sb strings.Builder
	toolName := blogFinalToolName
	switch {
	case scope == blogScopePresentation:
		toolName = blogPresentationToolName
	case strings.HasPrefix(scope, blogScopeSectionPrfx):
		toolName = blogSectionToolName
	}
	writeBlogContract(&sb, request, toolName)
	sb.WriteString("TASK: revise an existing article according to the USER INSTRUCTION, within SCOPE.\n\n")
	writeBlogSnapshot(&sb, request)
	sb.WriteString("## CURRENT DOCUMENT\n")
	sb.WriteString(compactJSON(request["document"]))
	sb.WriteString("\n\n## CURRENT PRESENTATION\n")
	sb.WriteString(compactJSON(request["presentation"]))
	sb.WriteString("\n\n## SCOPE\n")
	switch {
	case scope == blogScopePresentation:
		sb.WriteString("presentation — choose the template only. Call " + blogPresentationToolName + " with presentation.template; the article text is kept exactly as it is and must not be resubmitted.\n")
	case strings.HasPrefix(scope, blogScopeSectionPrfx):
		id := strings.TrimPrefix(scope, blogScopeSectionPrfx)
		sb.WriteString("section:" + id + " — rewrite ONLY the section whose id is \"" + id + "\". Call " + blogSectionToolName + " with that single section (keep id \"" + id + "\", heading and level may change if asked). Every other section and field is kept automatically; do not resubmit them.\n")
	default:
		sb.WriteString("all — you may change any part of the document and the presentation, but keep existing file_id values and keep the article on the same topic unless the instruction says otherwise.\n")
	}
	sb.WriteString("\n")
	writeChecklistChatValue(&sb, "CONVERSATION", request["conversation"])
	sb.WriteString("USER INSTRUCTION:\n")
	sb.WriteString(strings.TrimSpace(stringFromMap(request, "instruction")))
	sb.WriteString("\n")
	return sb.String()
}
