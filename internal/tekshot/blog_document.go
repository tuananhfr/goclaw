package tekshot

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// Blog document v1 — the contract shared with Drupal's BlogDocument::fromArray()
// (tekshot_studio_web) and the React type. Changing it is a three-repo edit.
const (
	blogFinalToolName = "submit_blog_document"
	blogSchemaVersion = 1
)

var (
	blogBlockTypes  = map[string]bool{"paragraph": true, "callout": true, "list": true, "image": true, "table": true}
	blogSchemaTypes = map[string]bool{"Article": true, "NewsArticle": true, "BlogPosting": true}
	blogSectionID   = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	blogHTTPS       = regexp.MustCompile(`^https://[^\s"<>]+$`)
	blogHTTPSOrPath = regexp.MustCompile(`^(https://[^\s"<>]+|/[^\s"<>]*)$`)
)

// blogSnapshot is the part of the Drupal snapshot the validator needs: what the
// target site can actually render (synced templates) and which images exist
// there. Anything outside these lists is rejected, never guessed.
type blogSnapshot struct {
	TemplateKeys []string
	ImageIDs     map[int]bool
	Language     string
}

func blogSnapshotFromRequest(request map[string]any) blogSnapshot {
	snap := blogSnapshot{ImageIDs: map[int]bool{}, Language: "vi"}
	raw, _ := request["snapshot"].(map[string]any)
	if raw == nil {
		return snap
	}
	if website, ok := raw["website"].(map[string]any); ok {
		if lang := strings.TrimSpace(stringFromMap(website, "language")); lang != "" {
			snap.Language = lang
		}
	}
	if templates, ok := raw["templates"].([]any); ok {
		for _, item := range templates {
			if entry, ok := item.(map[string]any); ok {
				if key := strings.TrimSpace(stringFromMap(entry, "key")); key != "" {
					snap.TemplateKeys = append(snap.TemplateKeys, key)
				}
			}
		}
	}
	if images, ok := raw["images"].([]any); ok {
		for _, item := range images {
			if entry, ok := item.(map[string]any); ok {
				if id := int(numberFromMap(entry, "id")); id > 0 {
					snap.ImageIDs[id] = true
				}
			}
		}
	}
	return snap
}

func (s blogSnapshot) hasTemplate(key string) bool {
	for _, k := range s.TemplateKeys {
		if k == key {
			return true
		}
	}
	return false
}

// blogSubmissionParameters is the tool schema. Every key of every object is
// required so strict providers accept it; "no value" is an empty string, 0 or
// an empty array, and validateBlogSubmission normalises those away.
func blogSubmissionParameters() map[string]any {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	strArr := func(desc string) map[string]any {
		return map[string]any{"type": "array", "description": desc, "items": map[string]any{"type": "string"}}
	}
	obj := func(props map[string]any, desc string) map[string]any {
		required := make([]string, 0, len(props))
		for key := range props {
			required = append(required, key)
		}
		return map[string]any{"type": "object", "additionalProperties": false, "description": desc, "properties": props, "required": required}
	}
	block := obj(map[string]any{
		"type":    map[string]any{"type": "string", "enum": []string{"paragraph", "callout", "list", "image", "table"}},
		"text":    str("paragraph/callout body; inline **bold**, *italic*, [text](https://…) only. Empty for other types."),
		"ordered": map[string]any{"type": "boolean", "description": "list only"},
		"items":   strArr("list items; empty for other types"),
		"file_id": map[string]any{"type": "integer", "description": "image only: an id from AVAILABLE IMAGES; 0 otherwise"},
		"alt":     str("image only: required alt text"),
		"caption": str("image only; may be empty"),
		"header":  map[string]any{"type": "boolean", "description": "table only: first row is a header"},
		"rows": map[string]any{"type": "array", "description": "table only: rows of cell strings; empty for other types",
			"items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
	}, "One content block")
	section := obj(map[string]any{
		"id":      str("slug, ^[a-z][a-z0-9-]*$, e.g. s1"),
		"heading": str("section heading"),
		"level":   map[string]any{"type": "integer", "enum": []int{2, 3}},
		"blocks":  map[string]any{"type": "array", "minItems": 1, "items": block},
	}, "One section")
	document := obj(map[string]any{
		"version":       map[string]any{"type": "integer", "enum": []int{1}},
		"title":         str("article title, max 255"),
		"summary":       str("1-2 sentence summary, max 1000"),
		"language":      str("BCP-47 code the article is written in"),
		"lead":          obj(map[string]any{"paragraphs": strArr("1-3 opening paragraphs")}, "Lead"),
		"key_takeaways": strArr("3-5 one-line takeaways; may be empty"),
		"sections":      map[string]any{"type": "array", "minItems": 1, "items": section},
		"quote":         obj(map[string]any{"text": str("pull quote; empty = none"), "cite": str("attribution; may be empty")}, "Pull quote"),
		"faq": map[string]any{"type": "array", "description": "2-4 Q&A; may be empty",
			"items": obj(map[string]any{"q": str("question"), "a": str("answer")}, "FAQ item")},
		"cta": obj(map[string]any{
			"heading": str("may be empty"),
			"text":    str("may be empty"),
			"button":  obj(map[string]any{"label": str("empty = no CTA"), "href": str("https://… or /path")}, "CTA button"),
		}, "Call to action"),
		"sources": map[string]any{"type": "array", "description": "only https URLs you actually fetched; may be empty",
			"items": obj(map[string]any{"title": str("source title"), "url": str("https URL")}, "Source")},
		"images": obj(map[string]any{
			"featured_file_id": map[string]any{"type": "integer", "description": "id from AVAILABLE IMAGES, 0 = none"},
			"featured_alt":     str("alt for the featured image; may be empty"),
		}, "Featured image"),
		"schema_type": map[string]any{"type": "string", "enum": []string{"Article", "NewsArticle", "BlogPosting"}},
	}, "Blog document v1")
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"reply":    str("Short note to the editor: what was written and why, in the article language"),
			"document": document,
			"presentation": obj(map[string]any{
				"template": str("a key from TEMPLATES, or empty for the site default"),
				"options":  map[string]any{"type": "object", "description": "reserved; send {}"},
			}, "Presentation"),
			"seo": obj(map[string]any{
				"meta_title":       str("≤ 60 chars"),
				"meta_description": str("≤ 160 chars"),
				"keywords":         str("comma separated; may be empty"),
				"focus_keyword":    str("one focus keyword"),
			}, "SEO"),
		},
		"required": []string{"reply", "document", "presentation", "seo"},
	}
}

// validateBlogSubmission mirrors BlogDocument::fromArray() in Drupal and returns
// the normalised submission. Fail closed: any doubt is an error, never a
// degraded document.
func validateBlogSubmission(args map[string]any, snap blogSnapshot) (map[string]any, error) {
	reply := strings.TrimSpace(stringFromMap(args, "reply"))
	rawDoc, ok := args["document"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("document must be an object")
	}
	document, err := validateBlogDocument(rawDoc, snap)
	if err != nil {
		return nil, err
	}
	// reply chỉ là câu ghi chú cho biên tập viên. Bỏ cả bài 11KB vì thiếu một
	// câu là fail-closed sai chỗ, nên tự điền từ tiêu đề khi model quên.
	if reply == "" {
		reply = "Đã viết xong bài: " + stringFromMap(document, "title")
	}

	var presentation map[string]any
	if raw, ok := args["presentation"].(map[string]any); ok {
		if key := strings.TrimSpace(stringFromMap(raw, "template")); key != "" {
			if !snap.hasTemplate(key) {
				return nil, fmt.Errorf("presentation.template %q is not synced on this site (allowed: %s)", key, strings.Join(snap.TemplateKeys, ", "))
			}
			presentation = map[string]any{"template": key, "options": map[string]any{}}
		}
	}

	seo := map[string]any{}
	rawSEO, _ := args["seo"].(map[string]any)
	for key, limit := range map[string]int{"meta_title": 255, "meta_description": 320, "keywords": 255, "focus_keyword": 100} {
		seo[key] = cutRunes(strings.TrimSpace(stringFromMap(rawSEO, key)), limit)
	}
	// Meta trống thì suy ra từ bài: bỏ cả bài vì thiếu một dòng meta là
	// fail-closed sai chỗ, và tiêu đề vẫn là meta_title tốt hơn ô trống.
	if seo["meta_title"] == "" {
		seo["meta_title"] = cutRunes(stringFromMap(document, "title"), 60)
	}
	if seo["meta_description"] == "" {
		seo["meta_description"] = cutRunes(stringFromMap(document, "summary"), 160)
	}

	out := map[string]any{
		"reply":    cutRunes(reply, 2000),
		"document": document,
		"seo":      seo,
	}
	if presentation != nil {
		out["presentation"] = presentation
	} else {
		out["presentation"] = nil
	}
	return out, nil
}

func validateBlogDocument(d map[string]any, snap blogSnapshot) (map[string]any, error) {
	if int(numberFromMap(d, "version")) != blogSchemaVersion {
		return nil, fmt.Errorf("document.version must be 1")
	}
	title, err := blogText(d["title"], "document.title", true, 255)
	if err != nil {
		return nil, err
	}
	summary, err := blogText(d["summary"], "document.summary", false, 1000)
	if err != nil {
		return nil, err
	}
	language := strings.TrimSpace(stringFromMap(d, "language"))
	if language == "" {
		language = snap.Language
	}
	if language == "" {
		language = "vi"
	}

	leadRaw, _ := d["lead"].(map[string]any)
	lead, err := blogStringList(leadRaw["paragraphs"], "document.lead.paragraphs", true)
	if err != nil {
		return nil, err
	}
	takeaways, err := blogStringList(d["key_takeaways"], "document.key_takeaways", false)
	if err != nil {
		return nil, err
	}

	rawSections, _ := d["sections"].([]any)
	sections := make([]any, 0, len(rawSections))
	for i, raw := range rawSections {
		section, _ := raw.(map[string]any)
		normalised, err := validateBlogSection(section, i, snap)
		if err != nil {
			return nil, err
		}
		sections = append(sections, normalised)
	}
	if len(sections) == 0 {
		return nil, fmt.Errorf("document.sections must not be empty")
	}

	out := map[string]any{
		"version":       blogSchemaVersion,
		"title":         title,
		"summary":       summary,
		"language":      language,
		"lead":          map[string]any{"paragraphs": lead},
		"key_takeaways": takeaways,
		"sections":      sections,
	}

	if quote, ok := d["quote"].(map[string]any); ok && strings.TrimSpace(stringFromMap(quote, "text")) != "" {
		text, err := blogText(quote["text"], "document.quote.text", true, 1000)
		if err != nil {
			return nil, err
		}
		cite, err := blogText(quote["cite"], "document.quote.cite", false, 255)
		if err != nil {
			return nil, err
		}
		out["quote"] = map[string]any{"text": text, "cite": cite}
	}

	faq := []any{}
	if rawFAQ, ok := d["faq"].([]any); ok {
		for i, raw := range rawFAQ {
			item, _ := raw.(map[string]any)
			q, err := blogText(item["q"], fmt.Sprintf("document.faq[%d].q", i), true, 500)
			if err != nil {
				return nil, err
			}
			a, err := blogText(item["a"], fmt.Sprintf("document.faq[%d].a", i), true, 2000)
			if err != nil {
				return nil, err
			}
			faq = append(faq, map[string]any{"q": q, "a": a})
		}
	}
	out["faq"] = faq

	if cta, ok := d["cta"].(map[string]any); ok {
		button, _ := cta["button"].(map[string]any)
		if label := strings.TrimSpace(stringFromMap(button, "label")); label != "" {
			href := strings.TrimSpace(stringFromMap(button, "href"))
			if !blogHTTPSOrPath.MatchString(href) {
				return nil, fmt.Errorf("document.cta.button.href must be https:// or a /path")
			}
			heading, err := blogText(cta["heading"], "document.cta.heading", false, 255)
			if err != nil {
				return nil, err
			}
			text, err := blogText(cta["text"], "document.cta.text", false, 1000)
			if err != nil {
				return nil, err
			}
			out["cta"] = map[string]any{"heading": heading, "text": text, "button": map[string]any{"label": cutRunes(label, 100), "href": href}}
		}
	}

	sources := []any{}
	if rawSources, ok := d["sources"].([]any); ok {
		for i, raw := range rawSources {
			item, _ := raw.(map[string]any)
			url := strings.TrimSpace(stringFromMap(item, "url"))
			if !blogHTTPS.MatchString(url) {
				return nil, fmt.Errorf("document.sources[%d].url must be https://", i)
			}
			sourceTitle, err := blogText(item["title"], fmt.Sprintf("document.sources[%d].title", i), true, 255)
			if err != nil {
				return nil, err
			}
			sources = append(sources, map[string]any{"title": sourceTitle, "url": url})
		}
	}
	out["sources"] = sources

	images := map[string]any{"featured_file_id": nil, "featured_alt": ""}
	if rawImages, ok := d["images"].(map[string]any); ok {
		if id := int(numberFromMap(rawImages, "featured_file_id")); id > 0 {
			if !snap.ImageIDs[id] {
				return nil, fmt.Errorf("document.images.featured_file_id %d is not an image on this site", id)
			}
			images["featured_file_id"] = id
			images["featured_alt"] = cutRunes(strings.TrimSpace(stringFromMap(rawImages, "featured_alt")), 512)
		}
	}
	out["images"] = images

	schemaType := strings.TrimSpace(stringFromMap(d, "schema_type"))
	if schemaType == "" {
		schemaType = "Article"
	}
	if !blogSchemaTypes[schemaType] {
		return nil, fmt.Errorf("document.schema_type %q is not supported", schemaType)
	}
	out["schema_type"] = schemaType
	return out, nil
}

func validateBlogSection(s map[string]any, index int, snap blogSnapshot) (map[string]any, error) {
	id := strings.TrimSpace(stringFromMap(s, "id"))
	if id == "" {
		id = fmt.Sprintf("s%d", index+1)
	}
	if len(id) > 32 || !blogSectionID.MatchString(id) {
		return nil, fmt.Errorf("document.sections[%d].id %q is invalid (use ^[a-z][a-z0-9-]*$)", index, id)
	}
	heading, err := blogText(s["heading"], fmt.Sprintf("document.sections[%d].heading", index), true, 255)
	if err != nil {
		return nil, err
	}
	level := int(numberFromMap(s, "level"))
	if level == 0 {
		level = 2
	}
	if level != 2 && level != 3 {
		return nil, fmt.Errorf("document.sections[%d].level must be 2 or 3", index)
	}
	rawBlocks, _ := s["blocks"].([]any)
	blocks := make([]any, 0, len(rawBlocks))
	for _, raw := range rawBlocks {
		block, _ := raw.(map[string]any)
		normalised, err := validateBlogBlock(block, id, snap)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, normalised)
	}
	if len(blocks) == 0 {
		return nil, fmt.Errorf("document.sections[%d].blocks must not be empty", index)
	}
	return map[string]any{"id": id, "heading": heading, "level": level, "blocks": blocks}, nil
}

func validateBlogBlock(b map[string]any, sectionID string, snap blogSnapshot) (map[string]any, error) {
	kind := strings.TrimSpace(stringFromMap(b, "type"))
	if !blogBlockTypes[kind] {
		return nil, fmt.Errorf("block type %q is not supported (section %s)", kind, sectionID)
	}
	switch kind {
	case "paragraph", "callout":
		text, err := blogText(b["text"], fmt.Sprintf("%s block in section %s", kind, sectionID), true, 5000)
		if err != nil {
			return nil, err
		}
		return map[string]any{"type": kind, "text": text}, nil
	case "list":
		items, err := blogStringList(b["items"], fmt.Sprintf("list in section %s", sectionID), true)
		if err != nil {
			return nil, err
		}
		ordered, _ := b["ordered"].(bool)
		return map[string]any{"type": "list", "ordered": ordered, "items": items}, nil
	case "image":
		id := int(numberFromMap(b, "file_id"))
		if id < 1 || !snap.ImageIDs[id] {
			return nil, fmt.Errorf("image in section %s uses file_id %d which is not an image on this site", sectionID, id)
		}
		alt := strings.TrimSpace(stringFromMap(b, "alt"))
		if alt == "" {
			return nil, fmt.Errorf("image in section %s needs alt text", sectionID)
		}
		return map[string]any{"type": "image", "file_id": id, "alt": cutRunes(alt, 512), "caption": cutRunes(strings.TrimSpace(stringFromMap(b, "caption")), 512)}, nil
	default: // table
		rawRows, _ := b["rows"].([]any)
		rows := make([]any, 0, len(rawRows))
		for _, rawRow := range rawRows {
			cellsRaw, _ := rawRow.([]any)
			cells := make([]any, 0, len(cellsRaw))
			for _, cell := range cellsRaw {
				if s, ok := cell.(string); ok {
					cells = append(cells, strings.TrimSpace(s))
				} else {
					cells = append(cells, strings.TrimSpace(fmt.Sprint(cell)))
				}
			}
			if len(cells) > 0 {
				rows = append(rows, cells)
			}
		}
		if len(rows) == 0 {
			return nil, fmt.Errorf("table in section %s has no rows", sectionID)
		}
		header := true
		if v, ok := b["header"].(bool); ok {
			header = v
		}
		return map[string]any{"type": "table", "header": header, "rows": rows}, nil
	}
}

func blogText(value any, path string, required bool, max int) (string, error) {
	text := ""
	switch v := value.(type) {
	case string:
		text = strings.TrimSpace(v)
	case nil:
	default:
		text = strings.TrimSpace(fmt.Sprint(v))
	}
	if required && text == "" {
		return "", fmt.Errorf("%s must not be empty", path)
	}
	if len([]rune(text)) > max {
		return "", fmt.Errorf("%s exceeds %d characters", path, max)
	}
	return text, nil
}

func blogStringList(value any, path string, required bool) ([]any, error) {
	items := []any{}
	if raw, ok := value.([]any); ok {
		for _, item := range raw {
			if s, ok := item.(string); ok {
				if t := strings.TrimSpace(s); t != "" {
					items = append(items, t)
				}
			}
		}
	}
	if required && len(items) == 0 {
		return nil, fmt.Errorf("%s must not be empty", path)
	}
	return items, nil
}

// BlogDocumentCollector is the ephemeral tool the blog jobs hand the agent:
// the only way a document leaves the run is through this validator.
type BlogDocumentCollector struct {
	snapshot blogSnapshot
	report   map[string]any
}

func NewBlogDocumentCollector(snapshot blogSnapshot) *BlogDocumentCollector {
	return &BlogDocumentCollector{snapshot: snapshot}
}

func (t *BlogDocumentCollector) Name() string { return blogFinalToolName }

func (t *BlogDocumentCollector) Description() string {
	return "Submit the finished blog article as a structured document (v1) with its presentation template and SEO fields. Call exactly once when the article is complete."
}

func (t *BlogDocumentCollector) Parameters() map[string]any {
	return blogSubmissionParameters()
}

func (t *BlogDocumentCollector) Execute(_ context.Context, args map[string]any) *tools.Result {
	report, err := validateBlogSubmission(args, t.snapshot)
	if err != nil {
		return tools.ErrorResult("MODEL_OUTPUT_INVALID: " + err.Error())
	}
	t.report = report
	return tools.SilentResult("Blog document captured.")
}

func (t *BlogDocumentCollector) Report() map[string]any {
	if t.report == nil {
		return nil
	}
	encoded, err := json.Marshal(t.report)
	if err != nil {
		return nil
	}
	var clone map[string]any
	if err := json.Unmarshal(encoded, &clone); err != nil {
		return nil
	}
	return clone
}
