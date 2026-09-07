package tekshot

import (
	"context"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// Partial collectors for scoped rewrites. Asking a model to echo a 12KB
// document byte-for-byte to change one section is fragile and expensive, so a
// scoped rewrite only ever receives the changed part; Go splices it into the
// original and re-validates the whole document.
const (
	blogSectionToolName      = "submit_blog_section"
	blogPresentationToolName = "submit_blog_presentation"
)

// blogCollector is what runBlogCollector drives: an ephemeral tool that keeps
// the last valid submission.
type blogCollector interface {
	tools.Tool
	Report() map[string]any
}

// BlogSectionCollector accepts one rewritten section whose id must match the
// scope; validation of the section itself happens once it is spliced in.
type BlogSectionCollector struct {
	sectionID string
	report    map[string]any
}

func NewBlogSectionCollector(sectionID string) *BlogSectionCollector {
	return &BlogSectionCollector{sectionID: sectionID}
}

func (t *BlogSectionCollector) Name() string { return blogSectionToolName }

func (t *BlogSectionCollector) Description() string {
	return "Submit the rewritten section (id \"" + t.sectionID + "\") of the article. Send only that section; every other part of the article is kept as it is."
}

func (t *BlogSectionCollector) Parameters() map[string]any {
	document := blogSubmissionParameters()["properties"].(map[string]any)["document"].(map[string]any)
	section := document["properties"].(map[string]any)["sections"].(map[string]any)["items"]
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"reply":   map[string]any{"type": "string", "description": "Short note to the editor about what changed, in the article language"},
			"section": section,
		},
		"required": []string{"reply", "section"},
	}
}

func (t *BlogSectionCollector) Execute(_ context.Context, args map[string]any) *tools.Result {
	if strings.TrimSpace(stringFromMap(args, "reply")) == "" {
		return tools.ErrorResult("MODEL_OUTPUT_INVALID: reply is required")
	}
	section, ok := args["section"].(map[string]any)
	if !ok {
		return tools.ErrorResult("MODEL_OUTPUT_INVALID: section must be an object")
	}
	if id := strings.TrimSpace(stringFromMap(section, "id")); id != "" && id != t.sectionID {
		return tools.ErrorResult(fmt.Sprintf("MODEL_OUTPUT_INVALID: section.id must be %q, got %q", t.sectionID, id))
	}
	section["id"] = t.sectionID
	t.report = map[string]any{"reply": stringFromMap(args, "reply"), "section": section}
	return tools.SilentResult("Section captured.")
}

func (t *BlogSectionCollector) Report() map[string]any { return cloneJSON(t.report) }

// BlogPresentationCollector accepts a template choice alone.
type BlogPresentationCollector struct {
	snapshot blogSnapshot
	report   map[string]any
}

func NewBlogPresentationCollector(snapshot blogSnapshot) *BlogPresentationCollector {
	return &BlogPresentationCollector{snapshot: snapshot}
}

func (t *BlogPresentationCollector) Name() string { return blogPresentationToolName }

func (t *BlogPresentationCollector) Description() string {
	return "Submit the presentation template for the article. The text is not touched."
}

func (t *BlogPresentationCollector) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"reply":        map[string]any{"type": "string", "description": "Short note to the editor: which template and why, in the article language"},
			"presentation": blogSubmissionParameters()["properties"].(map[string]any)["presentation"],
		},
		"required": []string{"reply", "presentation"},
	}
}

func (t *BlogPresentationCollector) Execute(_ context.Context, args map[string]any) *tools.Result {
	if strings.TrimSpace(stringFromMap(args, "reply")) == "" {
		return tools.ErrorResult("MODEL_OUTPUT_INVALID: reply is required")
	}
	raw, _ := args["presentation"].(map[string]any)
	key := strings.TrimSpace(stringFromMap(raw, "template"))
	if key == "" {
		if len(t.snapshot.TemplateKeys) > 0 {
			return tools.ErrorResult("MODEL_OUTPUT_INVALID: presentation.template is required (allowed: " + strings.Join(t.snapshot.TemplateKeys, ", ") + ")")
		}
		t.report = map[string]any{"reply": stringFromMap(args, "reply"), "presentation": nil}
		return tools.SilentResult("Presentation captured.")
	}
	if !t.snapshot.hasTemplate(key) {
		return tools.ErrorResult(fmt.Sprintf("MODEL_OUTPUT_INVALID: presentation.template %q is not synced on this site (allowed: %s)", key, strings.Join(t.snapshot.TemplateKeys, ", ")))
	}
	t.report = map[string]any{
		"reply":        stringFromMap(args, "reply"),
		"presentation": map[string]any{"template": key, "options": map[string]any{}},
	}
	return tools.SilentResult("Presentation captured.")
}

func (t *BlogPresentationCollector) Report() map[string]any { return cloneJSON(t.report) }

// spliceBlogSection returns a copy of original with the section whose id
// matches replaced; the id must exist and the order is preserved.
func spliceBlogSection(original map[string]any, section map[string]any) (map[string]any, error) {
	doc := cloneJSON(original)
	if doc == nil {
		return nil, fmt.Errorf("document could not be cloned")
	}
	id := stringFromMap(section, "id")
	sections, _ := doc["sections"].([]any)
	for i, raw := range sections {
		existing, _ := raw.(map[string]any)
		if stringFromMap(existing, "id") == id {
			sections[i] = section
			doc["sections"] = sections
			return doc, nil
		}
	}
	return nil, fmt.Errorf("section %s does not exist in the document", id)
}

// normalizedPresentation re-checks the presentation React sent back against
// the snapshot, so a template that was un-synced since the article was
// written cannot ride along.
func normalizedPresentation(raw any, snap blogSnapshot) (map[string]any, error) {
	entry, _ := raw.(map[string]any)
	key := strings.TrimSpace(stringFromMap(entry, "template"))
	if key == "" {
		return nil, nil
	}
	if !snap.hasTemplate(key) {
		return nil, fmt.Errorf("presentation.template %q is not synced on this site", key)
	}
	return map[string]any{"template": key, "options": map[string]any{}}, nil
}
