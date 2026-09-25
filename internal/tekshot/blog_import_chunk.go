package tekshot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// A long article is converted one part at a time: the model stopped copying
// tekshot.vn nodes 114 and 118 (23 and 29 KB) about halfway through a single
// answer, well under the output token ceiling.
const (
	blogImportChunkBytes   = 8000
	blogImportPartToolName = "submit_blog_import_part"
	blogImportPartLead     = "-"
)

// Top-level block comments; attributes cannot contain '>' (Gutenberg escapes it).
var blogImportBlockComment = regexp.MustCompile(`<!--\s*(/?)wp:([a-z0-9/-]+)\s*(\{[^>]*\})?\s*(/?)-->`)

// splitBlogImportMarkup cuts only between top-level blocks and only before an
// h2/h3, so every later part opens a section and no block is ever split.
func splitBlogImportMarkup(markup string, target int) []string {
	if len(markup) <= target {
		return []string{markup}
	}
	var chunks []string
	start, depth := 0, 0
	for _, m := range blogImportBlockComment.FindAllStringSubmatchIndex(markup, -1) {
		closing := m[3] > m[2]
		selfClosing := m[9] > m[8]
		if !closing && depth == 0 && m[0]-start >= target && blogImportOpensSection(markup, m) {
			chunks = append(chunks, markup[start:m[0]])
			start = m[0]
		}
		switch {
		case closing:
			if depth > 0 {
				depth--
			}
		case !selfClosing:
			depth++
		}
	}
	return append(chunks, markup[start:])
}

func blogImportOpensSection(markup string, m []int) bool {
	name := markup[m[4]:m[5]]
	if name != "heading" && name != "core/heading" {
		return false
	}
	level := 2
	if m[7] > m[6] {
		var attrs map[string]any
		if json.Unmarshal([]byte(markup[m[6]:m[7]]), &attrs) == nil {
			if l := int(numberFromMap(attrs, "level")); l > 0 {
				level = l
			}
		}
	}
	return level <= 3
}

// BlogImportPartCollector takes one later part of a long article: sections
// and the few top-level roles a part can hold, checked against that part only.
type BlogImportPartCollector struct {
	snapshot blogSnapshot
	source   blogImportSource
	title    string
	language string
	report   map[string]any
	lastErr  string
}

func NewBlogImportPartCollector(snapshot blogSnapshot, source blogImportSource, title, language string) *BlogImportPartCollector {
	return &BlogImportPartCollector{snapshot: snapshot, source: source, title: title, language: language}
}

func (t *BlogImportPartCollector) Name() string { return blogImportPartToolName }

func (t *BlogImportPartCollector) Description() string {
	return "Submit this part of the article re-arranged as sections (v1), with every block that has no place listed in unconverted. Call exactly once."
}

func (t *BlogImportPartCollector) Parameters() map[string]any {
	document, _ := blogSubmissionParameters()["properties"].(map[string]any)["document"].(map[string]any)
	docProps, _ := document["properties"].(map[string]any)
	unconverted := NewBlogImportCollector(t.snapshot, t.source).Parameters()["properties"].(map[string]any)["unconverted"]
	props := map[string]any{"unconverted": unconverted}
	for _, key := range []string{"sections", "key_takeaways", "quote", "faq", "cta", "sources"} {
		props[key] = docProps[key]
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           props,
		"required":             []string{"sections", "key_takeaways", "quote", "faq", "cta", "sources", "unconverted"},
	}
}

func (t *BlogImportPartCollector) Execute(_ context.Context, args map[string]any) *tools.Result {
	report, err := validateBlogImportPart(args, t.snapshot, t.source, t.title, t.language)
	if err != nil {
		t.lastErr = err.Error()
		return tools.ErrorResult("MODEL_OUTPUT_INVALID: " + err.Error())
	}
	t.report, t.lastErr = report, ""
	return tools.SilentResult("Imported part captured.")
}

func (t *BlogImportPartCollector) Report() map[string]any { return cloneJSON(t.report) }

func (t *BlogImportPartCollector) LastError() string { return t.lastErr }

// validateBlogImportPart wraps the part in a stand-in document so every check
// of a whole import (v1 shape, verbatim, duplicates, images, coverage) applies.
func validateBlogImportPart(args map[string]any, snap blogSnapshot, source blogImportSource, title, language string) (map[string]any, error) {
	if strings.TrimSpace(title) == "" {
		title = blogImportPartLead
	}
	doc := map[string]any{
		"version": float64(blogSchemaVersion), "title": title, "summary": "", "language": language,
		"lead":          map[string]any{"paragraphs": []any{blogImportPartLead}},
		"key_takeaways": args["key_takeaways"], "sections": args["sections"],
		"quote": args["quote"], "faq": args["faq"], "cta": args["cta"], "sources": args["sources"],
		"images":      map[string]any{"featured_file_id": float64(0), "featured_alt": ""},
		"schema_type": "Article",
	}
	checked, err := validateBlogImport(map[string]any{"document": doc, "unconverted": args["unconverted"], "reply": blogImportPartLead}, snap, source)
	if err != nil {
		return nil, err
	}
	d := checked["document"].(map[string]any)
	return map[string]any{
		"sections": d["sections"], "key_takeaways": d["key_takeaways"], "quote": d["quote"],
		"faq": d["faq"], "cta": d["cta"], "sources": d["sources"], "unconverted": checked["unconverted"],
	}, nil
}

func blogImportQuoteText(raw any) string {
	quote, _ := raw.(map[string]any)
	return strings.TrimSpace(stringFromMap(quote, "text"))
}

func blogImportCTALabel(raw any) string {
	cta, _ := raw.(map[string]any)
	button, _ := cta["button"].(map[string]any)
	return strings.TrimSpace(stringFromMap(button, "label"))
}

func blogImportAppendList(base any, more any) []any {
	out, _ := base.([]any)
	extra, _ := more.([]any)
	return append(append([]any{}, out...), extra...)
}

// stitchBlogImport appends the later parts to the first: sections renumbered,
// lists concatenated. v1 holds one quote and one CTA, so a later quote becomes
// a callout where its part ends and a later CTA is listed as unconverted.
func stitchBlogImport(head map[string]any, parts []map[string]any) map[string]any {
	out := cloneJSON(head)
	doc, _ := out["document"].(map[string]any)
	sections, _ := doc["sections"].([]any)
	unconverted, _ := out["unconverted"].([]any)
	for _, part := range parts {
		partSections, _ := part["sections"].([]any)
		if text := blogImportQuoteText(part["quote"]); text != "" {
			if blogImportQuoteText(doc["quote"]) == "" {
				doc["quote"] = part["quote"]
			} else if len(partSections) > 0 {
				last, _ := partSections[len(partSections)-1].(map[string]any)
				last["blocks"] = blogImportAppendList(last["blocks"], []any{map[string]any{"type": "callout", "text": text}})
			}
		}
		for _, raw := range partSections {
			section, _ := raw.(map[string]any)
			section["id"] = fmt.Sprintf("s%d", len(sections)+1)
			sections = append(sections, section)
		}
		doc["key_takeaways"] = blogImportAppendList(doc["key_takeaways"], part["key_takeaways"])
		doc["faq"] = blogImportAppendList(doc["faq"], part["faq"])
		doc["sources"] = blogImportAppendList(doc["sources"], part["sources"])
		if label := blogImportCTALabel(part["cta"]); label != "" {
			if blogImportCTALabel(doc["cta"]) == "" {
				doc["cta"] = part["cta"]
			} else {
				unconverted = append(unconverted, map[string]any{"block_name": "core/buttons", "excerpt": label, "reason": "Bài chỉ giữ một lời kêu gọi hành động."})
			}
		}
		unconverted = blogImportAppendList(unconverted, part["unconverted"])
	}
	doc["sections"] = sections
	out["unconverted"] = unconverted
	return out
}

// blogImportPass runs the model once with tool as the only, forced tool.
type blogImportPass func(ctx context.Context, prompt string, tool tools.Tool) *providers.Usage

type blogImportTool interface {
	tools.Tool
	Report() map[string]any
	LastError() string
}

func runBlogImportAttempts(ctx context.Context, pass blogImportPass, tool blogImportTool, prompt, label string, progress func(string), usage *providers.Usage) error {
	for attempt := 1; attempt <= blogImportAttempts && tool.Report() == nil && ctx.Err() == nil; attempt++ {
		message := prompt
		if rejected := tool.LastError(); rejected != "" {
			message += "\n## YOUR PREVIOUS SUBMISSION WAS REJECTED\n" + rejected + "\nSubmit it all again and fix exactly that.\n"
		}
		progress(fmt.Sprintf("Đang chuyển %s (lần %d/%d)", label, attempt, blogImportAttempts))
		if u := pass(ctx, message, tool); u != nil {
			usage.PromptTokens += u.PromptTokens
			usage.CompletionTokens += u.CompletionTokens
			usage.TotalTokens += u.TotalTokens
			usage.ThinkingTokens += u.ThinkingTokens
		}
	}
	if tool.Report() != nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("the job ran out of time (%v) — try again, or edit this article as raw markup", err)
	}
	if reason := tool.LastError(); reason != "" {
		return errors.New(reason)
	}
	return fmt.Errorf("agent did not call %s", tool.Name())
}

// convertBlogImport: the first part yields the whole document frame, each later
// part its sections; the stitched article is checked again against the whole
// original, which also catches a block placed in two parts.
func convertBlogImport(ctx context.Context, request map[string]any, snap blogSnapshot, pass blogImportPass, progress func(string), chunkBytes int) (map[string]any, error) {
	markup := stringFromMap(request, "gutenberg_markup")
	chunks := splitBlogImportMarkup(markup, chunkBytes)
	total := len(chunks)
	label := func(i int) string {
		if total == 1 {
			return "bài"
		}
		return fmt.Sprintf("phần %d/%d", i, total)
	}
	failed := func(i int, err error) error {
		if total == 1 {
			return fmt.Errorf("MODEL_OUTPUT_INVALID: %s", err)
		}
		return fmt.Errorf("MODEL_OUTPUT_INVALID: part %d of %d: %s", i, total, err)
	}

	var usage providers.Usage
	headRequest := cloneJSON(request)
	headRequest["gutenberg_markup"] = chunks[0]
	frame := &blogImportFrame{
		Title:       stringFromMap(request, "title"),
		Summary:     stringFromMap(request, "summary"),
		FeaturedID:  int(numberFromMap(request, "featured_file_id")),
		FeaturedAlt: stringFromMap(request, "featured_alt"),
	}
	headSource := parseBlogImportSource(chunks[0])
	headSource.Frame = frame
	head := NewBlogImportCollector(snap, headSource)
	if err := runBlogImportAttempts(ctx, pass, head, buildBlogImportHeadPrompt(headRequest, total), label(1), progress, &usage); err != nil {
		return nil, failed(1, err)
	}
	report := head.Report()
	if total > 1 {
		title := stringFromMap(report["document"].(map[string]any), "title")
		parts := make([]map[string]any, 0, total-1)
		for i := 1; i < total; i++ {
			part := NewBlogImportPartCollector(snap, parseBlogImportSource(chunks[i]), title, snap.Language)
			if err := runBlogImportAttempts(ctx, pass, part, buildBlogImportPartPrompt(request, chunks[i], i+1, total), label(i+1), progress, &usage); err != nil {
				return nil, failed(i+1, err)
			}
			parts = append(parts, part.Report())
		}
		whole := parseBlogImportSource(markup)
		whole.Frame = frame
		checked, err := validateBlogImport(stitchBlogImport(report, parts), snap, whole)
		if err != nil {
			return nil, fmt.Errorf("MODEL_OUTPUT_INVALID: stitched article: %s", err)
		}
		report = checked
		// The first pass only saw part 1, so its own reply would describe part 1.
		report["reply"] = fmt.Sprintf("Đã chuyển bài dài theo %d phần.", total)
	}
	if usage.TotalTokens > 0 {
		report["usage"] = usage
	}
	return report, nil
}

func buildBlogImportPartPrompt(request map[string]any, chunk string, index, total int) string {
	language := blogSnapshotFromRequest(request).Language
	var images any
	if snapshot, ok := request["snapshot"].(map[string]any); ok {
		images = snapshot["images"]
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("You convert PART %d OF %d of ONE existing article from Gutenberg HTML into sections of the structured blog document v1. Earlier parts are converted separately; convert ONLY the markup below, completely. This is a lossless re-arrangement, not an edit.\n", index, total))
	sb.WriteString("Deliver the result by calling " + blogImportPartToolName + " exactly once. Never answer with plain text.\n\n")
	sb.WriteString("RULES — code checks every one of them and rejects the submission otherwise:\n")
	sb.WriteString(blogImportRuleVerbatim)
	sb.WriteString(blogImportRuleEmphasis)
	sb.WriteString("3. This part opens with a heading. Every h2/h3 opens a section (level 2 or 3) whose heading is the original heading text. " + blogImportRuleMinorHeadings + "\n")
	sb.WriteString(blogImportRuleMapping)
	sb.WriteString("5. Section ids are s1, s2, … in order within this part. Every block appears exactly once.\n")
	sb.WriteString(blogImportRuleLeftovers(language))
	sb.WriteString("7. key_takeaways = [] unless this part has an explicit takeaway list. sources = [] unless this part lists sources with https URLs. quote, faq and cta only when this part holds such blocks; otherwise empty.\n\n")
	writeChecklistChatValue(&sb, "AVAILABLE IMAGES", images)
	sb.WriteString(fmt.Sprintf("## PART %d OF %d MARKUP\n", index, total))
	sb.WriteString(chunk)
	sb.WriteString("\n")
	return sb.String()
}
