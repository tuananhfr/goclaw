package tekshot

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// blog_import converts one hand-written Gutenberg article into document v1,
// once. The model only places the existing wording into roles; the code below
// refuses any submission that rewrites a sentence or loses a block (images
// included) without listing it in unconverted.
const (
	blogImportToolName = "submit_blog_import"
	// Mirrors BlogImportPreparer::MAX_MARKUP_BYTES in tekshot_studio_web.
	blogImportMaxMarkupBytes = 60000
	blogImportMaxUnconverted = 100
	blogImportExcerptRunes   = 200
	// Below this share of the original's visible text, with nothing listed in
	// unconverted, the model has dropped content rather than re-arranged it.
	blogImportMinCoverage   = 0.8
	blogImportDefaultReason = "Không có vai trò tương ứng trong bản cấu trúc."
)

var (
	blogImportComment = regexp.MustCompile(`(?s)<!--.*?-->`)
	blogImportTag     = regexp.MustCompile(`(?s)<[^>]*>`)
	// Gutenberg escapes < and > inside block attributes, so [^>] cannot run
	// past the end of the comment into the next block.
	blogImportImageComment = regexp.MustCompile(`<!--\s*wp:image\s+(\{[^>]*\})\s*/?-->`)
	blogImportLink         = regexp.MustCompile(`\[([^\]\n]+)\]\(([^)\s]+)\)`)
	blogImportFold         = strings.NewReplacer("“", `"`, "”", `"`, "„", `"`, "‘", "'", "’", "'", "–", "-", "—", "-", "…", "...", "*", "")
)

type blogImportSource struct {
	Comparable string
	ImageIDs   []int
}

func parseBlogImportSource(markup string) blogImportSource {
	source := blogImportSource{Comparable: blogImportComparable(markup)}
	seen := map[int]bool{}
	for _, match := range blogImportImageComment.FindAllStringSubmatch(markup, -1) {
		var attrs map[string]any
		if json.Unmarshal([]byte(match[1]), &attrs) != nil {
			continue
		}
		if id := int(numberFromMap(attrs, "id")); id > 0 && !seen[id] {
			seen[id] = true
			source.ImageIDs = append(source.ImageIDs, id)
		}
	}
	return source
}

// blogImportComparable reduces HTML or inline markdown to what a reader sees,
// so "copied verbatim" survives tags, entities, curly quotes, dashes, Unicode
// normalisation and whitespace — and nothing else.
func blogImportComparable(s string) string {
	s = blogImportComment.ReplaceAllString(s, " ")
	s = blogImportTag.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = blogImportLink.ReplaceAllString(s, "$1")
	s = strings.ToLower(blogImportFold.Replace(norm.NFC.String(s)))
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || r == '\u200b' || r == '\u00ad' {
			return -1
		}
		return r
	}, s)
}

// blogImportCheckedTexts are the body texts that must come from the original
// word for word. Title and summary come from the node, a v1 section needs a
// heading the original may lack, and v1 requires an alt the original may not
// have — so those are exempt.
func blogImportCheckedTexts(document map[string]any) []string {
	var texts []string
	add := func(values ...any) {
		for _, value := range values {
			if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
				texts = append(texts, s)
			}
		}
	}
	if lead, ok := document["lead"].(map[string]any); ok {
		if paragraphs, ok := lead["paragraphs"].([]any); ok {
			add(paragraphs...)
		}
	}
	if takeaways, ok := document["key_takeaways"].([]any); ok {
		add(takeaways...)
	}
	sections, _ := document["sections"].([]any)
	for _, rawSection := range sections {
		section, _ := rawSection.(map[string]any)
		blocks, _ := section["blocks"].([]any)
		for _, rawBlock := range blocks {
			block, _ := rawBlock.(map[string]any)
			switch stringFromMap(block, "type") {
			case "paragraph", "callout":
				add(block["text"])
			case "list":
				if items, ok := block["items"].([]any); ok {
					add(items...)
				}
			case "image":
				add(block["caption"])
			case "table":
				rows, _ := block["rows"].([]any)
				for _, row := range rows {
					if cells, ok := row.([]any); ok {
						add(cells...)
					}
				}
			}
		}
	}
	if quote, ok := document["quote"].(map[string]any); ok {
		add(quote["text"], quote["cite"])
	}
	if faq, ok := document["faq"].([]any); ok {
		for _, raw := range faq {
			if item, ok := raw.(map[string]any); ok {
				add(item["q"], item["a"])
			}
		}
	}
	if cta, ok := document["cta"].(map[string]any); ok {
		add(cta["heading"], cta["text"])
		if button, ok := cta["button"].(map[string]any); ok {
			add(button["label"])
		}
	}
	return texts
}

func blogImportDocumentRunes(document map[string]any) int {
	total := 0
	for _, text := range blogImportCheckedTexts(document) {
		total += utf8.RuneCountInString(blogImportComparable(text))
	}
	sections, _ := document["sections"].([]any)
	for _, raw := range sections {
		if section, ok := raw.(map[string]any); ok {
			total += utf8.RuneCountInString(blogImportComparable(stringFromMap(section, "heading")))
		}
	}
	return total
}

func blogImportRewritten(document map[string]any, source blogImportSource) []string {
	var rewritten []string
	for _, text := range blogImportCheckedTexts(document) {
		comparable := blogImportComparable(text)
		if utf8.RuneCountInString(comparable) < 3 {
			continue
		}
		if !strings.Contains(source.Comparable, comparable) {
			rewritten = append(rewritten, blogImportRunes(strings.TrimSpace(text), 120))
		}
	}
	return rewritten
}

// blogImportUnsafeLinks: InlineMarkdown in Drupal prints any other href as
// literal "[text](url)", so such a link would show up as raw markdown.
func blogImportUnsafeLinks(document map[string]any) []string {
	var unsafe []string
	for _, text := range blogImportCheckedTexts(document) {
		for _, match := range blogImportLink.FindAllStringSubmatch(text, -1) {
			if !blogHTTPSOrPath.MatchString(match[2]) {
				unsafe = append(unsafe, match[2])
			}
		}
	}
	return unsafe
}

func blogImportMissingImages(document map[string]any, source blogImportSource, unconverted []any) []int {
	for _, raw := range unconverted {
		name := strings.ToLower(stringFromMap(raw.(map[string]any), "block_name"))
		for _, media := range []string{"image", "gallery", "media", "cover"} {
			if strings.Contains(name, media) {
				return nil
			}
		}
	}
	used := map[int]bool{}
	if images, ok := document["images"].(map[string]any); ok {
		used[int(numberFromMap(images, "featured_file_id"))] = true
	}
	sections, _ := document["sections"].([]any)
	for _, rawSection := range sections {
		section, _ := rawSection.(map[string]any)
		blocks, _ := section["blocks"].([]any)
		for _, rawBlock := range blocks {
			if block, ok := rawBlock.(map[string]any); ok && stringFromMap(block, "type") == "image" {
				used[int(numberFromMap(block, "file_id"))] = true
			}
		}
	}
	var missing []int
	for _, id := range source.ImageIDs {
		if !used[id] {
			missing = append(missing, id)
		}
	}
	return missing
}

func normalizeBlogUnconverted(raw any) ([]any, error) {
	list, _ := raw.([]any)
	out := make([]any, 0, len(list))
	for i, item := range list {
		entry, _ := item.(map[string]any)
		name := strings.TrimSpace(stringFromMap(entry, "block_name"))
		if name == "" {
			return nil, fmt.Errorf("unconverted[%d].block_name must not be empty (e.g. core/embed)", i)
		}
		reason := strings.TrimSpace(stringFromMap(entry, "reason"))
		if reason == "" {
			reason = blogImportDefaultReason
		}
		out = append(out, map[string]any{
			"block_name": blogImportRunes(name, 100),
			"excerpt":    blogImportRunes(strings.TrimSpace(stringFromMap(entry, "excerpt")), blogImportExcerptRunes),
			"reason":     blogImportRunes(reason, 300),
		})
		// A long tail of leftovers is trimmed, never a reason to lose the import.
		if len(out) == blogImportMaxUnconverted {
			break
		}
	}
	return out, nil
}

func blogImportRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) > max {
		return string(runes[:max])
	}
	return s
}

func blogImportQuoted(texts []string) string {
	if len(texts) > 5 {
		texts = texts[:5]
	}
	quoted := make([]string, 0, len(texts))
	for _, text := range texts {
		quoted = append(quoted, fmt.Sprintf("%q", text))
	}
	return strings.Join(quoted, "; ")
}

func validateBlogImport(args map[string]any, snap blogSnapshot, source blogImportSource) (map[string]any, error) {
	rawDoc, ok := args["document"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("document must be an object")
	}
	document, err := validateBlogDocument(rawDoc, snap)
	if err != nil {
		return nil, err
	}
	unconverted, err := normalizeBlogUnconverted(args["unconverted"])
	if err != nil {
		return nil, err
	}
	if links := blogImportUnsafeLinks(document); len(links) > 0 {
		return nil, fmt.Errorf("links must point to https:// or a /path; for %s keep only the link text", strings.Join(links, ", "))
	}
	if rewritten := blogImportRewritten(document, source); len(rewritten) > 0 {
		return nil, fmt.Errorf("%d text(s) are not in the original article word for word — copy the original wording exactly, never rewrite: %s", len(rewritten), blogImportQuoted(rewritten))
	}
	if missing := blogImportMissingImages(document, source, unconverted); len(missing) > 0 {
		return nil, fmt.Errorf("original images %v are neither in the document nor listed in unconverted", missing)
	}
	if len(unconverted) == 0 {
		kept, total := blogImportDocumentRunes(document), utf8.RuneCountInString(source.Comparable)
		if total > 0 && float64(kept) < blogImportMinCoverage*float64(total) {
			return nil, fmt.Errorf("the document keeps only %d%% of the original text and unconverted is empty — place every remaining block or list it in unconverted", kept*100/total)
		}
	}
	reply := strings.TrimSpace(stringFromMap(args, "reply"))
	if reply == "" {
		reply = "Đã chuyển bài sang bản cấu trúc."
	}
	return map[string]any{"reply": cutRunes(reply, 2000), "document": document, "unconverted": unconverted}, nil
}

// blogImportPresentation keeps the article's current template when the site
// still has it synced; an import never chooses a new layout.
func blogImportPresentation(raw any, snap blogSnapshot) any {
	presentation, err := normalizedPresentation(raw, snap)
	if err != nil || presentation == nil {
		return nil
	}
	return presentation
}

// BlogImportCollector is the only way an imported document leaves the run.
// The last rejection is kept so the next attempt can be told what to fix.
type BlogImportCollector struct {
	snapshot blogSnapshot
	source   blogImportSource
	report   map[string]any
	lastErr  string
}

func NewBlogImportCollector(snapshot blogSnapshot, source blogImportSource) *BlogImportCollector {
	return &BlogImportCollector{snapshot: snapshot, source: source}
}

func (t *BlogImportCollector) Name() string { return blogImportToolName }

func (t *BlogImportCollector) Description() string {
	return "Submit the original article re-arranged as a structured document (v1), with every block that has no place listed in unconverted. Call exactly once."
}

func (t *BlogImportCollector) Parameters() map[string]any {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	document := blogSubmissionParameters()["properties"].(map[string]any)["document"]
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"reply":    str("One or two sentences to the editor, in the article language: what was converted and what could not be"),
			"document": document,
			"unconverted": map[string]any{
				"type":        "array",
				"description": "every original block that has no place in the document; empty when everything fit",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"block_name": str("Gutenberg block name, e.g. core/embed"),
						"excerpt":    str("first words of its visible text, or its URL; at most 200 characters"),
						"reason":     str("one short sentence in the article language"),
					},
					"required": []string{"block_name", "excerpt", "reason"},
				},
			},
		},
		"required": []string{"reply", "document", "unconverted"},
	}
}

func (t *BlogImportCollector) Execute(_ context.Context, args map[string]any) *tools.Result {
	report, err := validateBlogImport(args, t.snapshot, t.source)
	if err != nil {
		t.lastErr = err.Error()
		return tools.ErrorResult("MODEL_OUTPUT_INVALID: " + err.Error())
	}
	t.report, t.lastErr = report, ""
	return tools.SilentResult("Imported document captured.")
}

func (t *BlogImportCollector) Report() map[string]any { return cloneJSON(t.report) }

func (t *BlogImportCollector) LastError() string { return t.lastErr }
