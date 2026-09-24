package tekshot

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	blogScopeAll          = "all"
	blogScopePresentation = "presentation"
	blogScopeSectionPrfx  = "section:"
	blogScopeBlockPrfx    = "block:"
	blogScopeFragmentPrfx = "fragment:"
)

// No leading zeros so one block has exactly one spelling; 999 is far past any real section.
var blogBlockIndex = regexp.MustCompile(`^(0|[1-9][0-9]{0,2})$`)

// blogBlockScope is a parsed block:/fragment: scope. Document v1 has no block
// ids, so a block is addressed by its section id and its position.
type blogBlockScope struct {
	Fragment  bool
	SectionID string
	Index     int
}

func parseBlogBlockScope(scope string) (blogBlockScope, bool) {
	var rest string
	fragment := false
	switch {
	case strings.HasPrefix(scope, blogScopeBlockPrfx):
		rest = strings.TrimPrefix(scope, blogScopeBlockPrfx)
	case strings.HasPrefix(scope, blogScopeFragmentPrfx):
		rest, fragment = strings.TrimPrefix(scope, blogScopeFragmentPrfx), true
	default:
		return blogBlockScope{}, false
	}
	cut := strings.LastIndex(rest, ":")
	if cut <= 0 {
		return blogBlockScope{}, false
	}
	sectionID, digits := rest[:cut], rest[cut+1:]
	if len(sectionID) > 32 || !blogSectionID.MatchString(sectionID) || !blogBlockIndex.MatchString(digits) {
		return blogBlockScope{}, false
	}
	index, err := strconv.Atoi(digits)
	if err != nil {
		return blogBlockScope{}, false
	}
	return blogBlockScope{Fragment: fragment, SectionID: sectionID, Index: index}, true
}

func blogBlockAt(document map[string]any, sectionID string, index int) (map[string]any, bool) {
	sections, _ := document["sections"].([]any)
	for _, raw := range sections {
		section, _ := raw.(map[string]any)
		if stringFromMap(section, "id") != sectionID {
			continue
		}
		blocks, _ := section["blocks"].([]any)
		if index < 0 || index >= len(blocks) {
			return nil, false
		}
		block, ok := blocks[index].(map[string]any)
		return block, ok
	}
	return nil, false
}

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
	case strings.HasPrefix(scope, blogScopeBlockPrfx), strings.HasPrefix(scope, blogScopeFragmentPrfx):
		if _, ok := parseBlogBlockScope(scope); !ok {
			return fmt.Errorf("scope %q is invalid (use block:<section id>:<index> or fragment:<section id>:<index>)", scope)
		}
		return nil
	default:
		return fmt.Errorf("scope %q is not supported (all | section:<id> | block:<id>:<n> | fragment:<id>:<n> | presentation)", scope)
	}
}

// enforceBlogRewriteScope is the guarantee the chat makes to the editor: an
// edit scoped to one section or one block cannot silently rewrite the rest,
// and a layout change cannot touch a word. Compared on canonical JSON, not on trust.
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
	if target, ok := parseBlogBlockScope(scope); ok {
		if target.Fragment {
			return fmt.Errorf("fragment scope returns a passage, not a document")
		}
		return enforceBlogBlockScope(original, updated, target)
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
	return blogTopLevelUnchanged(original, updated, "section scope "+sectionID)
}

func enforceBlogBlockScope(original, updated map[string]any, target blogBlockScope) error {
	label := fmt.Sprintf("block scope %s:%d", target.SectionID, target.Index)
	origSections, _ := original["sections"].([]any)
	newSections, _ := updated["sections"].([]any)
	if len(origSections) != len(newSections) {
		return fmt.Errorf("%s changed the number of sections", label)
	}
	found := false
	for i := range origSections {
		origSec, _ := origSections[i].(map[string]any)
		newSec, _ := newSections[i].(map[string]any)
		id := stringFromMap(origSec, "id")
		if stringFromMap(newSec, "id") != id {
			return fmt.Errorf("%s changed section order or ids", label)
		}
		if id != target.SectionID {
			if canonicalJSON(origSec) != canonicalJSON(newSec) {
				return fmt.Errorf("%s changed section %s", label, id)
			}
			continue
		}
		found = true
		if err := blogSectionOnlyBlockChanged(origSec, newSec, target.Index, label); err != nil {
			return err
		}
	}
	if !found {
		return fmt.Errorf("section %s does not exist in the document", target.SectionID)
	}
	return blogTopLevelUnchanged(original, updated, label)
}

func blogSectionOnlyBlockChanged(origSec, newSec map[string]any, index int, label string) error {
	for _, key := range []string{"heading", "level"} {
		if canonicalJSON(origSec[key]) != canonicalJSON(newSec[key]) {
			return fmt.Errorf("%s changed the section %s", label, key)
		}
	}
	origBlocks, _ := origSec["blocks"].([]any)
	newBlocks, _ := newSec["blocks"].([]any)
	if len(origBlocks) != len(newBlocks) {
		return fmt.Errorf("%s changed the number of blocks", label)
	}
	if index < 0 || index >= len(origBlocks) {
		return fmt.Errorf("%s points past the end of the section", label)
	}
	for i := range origBlocks {
		if i != index {
			if canonicalJSON(origBlocks[i]) != canonicalJSON(newBlocks[i]) {
				return fmt.Errorf("%s changed block %d", label, i)
			}
			continue
		}
		origBlock, _ := origBlocks[i].(map[string]any)
		newBlock, _ := newBlocks[i].(map[string]any)
		if stringFromMap(origBlock, "type") != stringFromMap(newBlock, "type") {
			return fmt.Errorf("%s changed the block type", label)
		}
	}
	return nil
}

func blogTopLevelUnchanged(original, updated map[string]any, label string) error {
	for key, value := range original {
		if key == "sections" {
			continue
		}
		if canonicalJSON(value) != canonicalJSON(updated[key]) {
			return fmt.Errorf("%s changed document.%s", label, key)
		}
	}
	for key := range updated {
		if key == "sections" {
			continue
		}
		if _, exists := original[key]; !exists && updated[key] != nil {
			return fmt.Errorf("%s added document.%s", label, key)
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
