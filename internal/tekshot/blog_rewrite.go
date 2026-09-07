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

func (s *JobService) runBlogRewrite(ctx context.Context, job *store.TekshotJob, request map[string]any) (any, string, error) {
	if strings.TrimSpace(stringFromMap(request, "instruction")) == "" {
		return nil, "", fmt.Errorf("instruction is required")
	}
	original, ok := request["document"].(map[string]any)
	if !ok || len(original) == 0 {
		return nil, "", fmt.Errorf("document is required for a rewrite")
	}
	scope := strings.TrimSpace(stringFromMap(request, "scope"))
	if scope == "" {
		scope = blogScopeAll
	}
	if err := validateBlogScope(scope); err != nil {
		return nil, "", err
	}

	report, usage, err := s.runBlogCollector(ctx, job, request, buildBlogRewritePrompt(request, scope), "tekshot blog rewrite", []string{"tekshot", "blog", "rewrite"})
	if err != nil {
		return nil, "", err
	}
	updated, _ := report["document"].(map[string]any)
	if err := enforceBlogRewriteScope(original, updated, scope); err != nil {
		return nil, "", fmt.Errorf("MODEL_OUTPUT_INVALID: %w", err)
	}
	if scope == blogScopePresentation && report["presentation"] == nil && len(blogSnapshotFromRequest(request).TemplateKeys) > 0 {
		return nil, "", fmt.Errorf("MODEL_OUTPUT_INVALID: presentation scope must choose a template")
	}
	if usage != nil {
		report["usage"] = usage
	}
	return report, "Blog document rewritten (" + scope + ")", nil
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
	writeBlogContract(&sb, request)
	sb.WriteString("TASK: revise an existing article according to the USER INSTRUCTION, within SCOPE.\n\n")
	writeBlogSnapshot(&sb, request)
	sb.WriteString("## CURRENT DOCUMENT\n")
	sb.WriteString(compactJSON(request["document"]))
	sb.WriteString("\n\n## CURRENT PRESENTATION\n")
	sb.WriteString(compactJSON(request["presentation"]))
	sb.WriteString("\n\n## SCOPE\n")
	switch {
	case scope == blogScopePresentation:
		sb.WriteString("presentation — change ONLY presentation.template. Return the CURRENT DOCUMENT byte-for-byte unchanged (same fields, same order, same text). Any textual change is rejected.\n")
	case strings.HasPrefix(scope, blogScopeSectionPrfx):
		id := strings.TrimPrefix(scope, blogScopeSectionPrfx)
		sb.WriteString("section:" + id + " — rewrite ONLY the section whose id is \"" + id + "\". Return every other section and every other document field exactly as in CURRENT DOCUMENT (same ids, same order, same text). Keep presentation unless the instruction says otherwise. Changes outside that section are rejected.\n")
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
