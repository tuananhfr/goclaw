package tekshot

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const (
	knowledgeLabelToolName      = "submit_knowledge_page"
	knowledgeLabelMaxIterations = 4
	knowledgeLabelProgressEvery = 5
	knowledgeLabelDocHeadChars  = 300
)

// KnowledgePageLabelTool collects the label for ONE chunk. requireMarkdown is
// true for scanned pages, where the model's transcription IS the content; for
// text chunks the extracted Markdown is authoritative and whatever the model
// returns in markdown is dropped, so numbers never pass through the model.
type KnowledgePageLabelTool struct {
	report          map[string]any
	requireMarkdown bool
}

func NewKnowledgePageLabelTool(requireMarkdown bool) *KnowledgePageLabelTool {
	return &KnowledgePageLabelTool{requireMarkdown: requireMarkdown}
}

func (t *KnowledgePageLabelTool) Name() string { return knowledgeLabelToolName }

func (t *KnowledgePageLabelTool) Description() string {
	return "Submit the label for this knowledge chunk: title, summary, keywords — plus the transcribed Markdown for scanned pages. Call it exactly once."
}

func (t *KnowledgePageLabelTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"title": map[string]any{
				"type":        "string",
				"description": "Specific Vietnamese title: tên tài liệu/thương hiệu + chủ đề của đoạn (+ thời điểm/phiên bản nếu có). A reader must know what the chunk holds from the title alone; never a bare generic heading like \"Bảng giá\".",
			},
			"summary":  map[string]any{"type": "string", "description": "One Vietnamese sentence: what facts this chunk holds."},
			"keywords": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "3-8 terms a person would search for: product/project names, unit codes, price words, phone numbers, policy terms."},
			"markdown": map[string]any{"type": "string", "description": "Scanned pages ONLY: the full transcription as clean Markdown, numbers VERBATIM, tables as Markdown tables. Text chunks: leave empty — the content is already extracted."},
			"status":   map[string]any{"type": "string", "description": "'ok' normally; 'empty' when the chunk or page holds no readable content at all."},
			"reason":   map[string]any{"type": "string", "description": "Required when status is 'empty': short honest Vietnamese reason."},
		},
		"required": []string{"title", "status"},
	}
}

func (t *KnowledgePageLabelTool) Execute(_ context.Context, args map[string]any) *tools.Result {
	report, err := validateKnowledgePageLabel(args, t.requireMarkdown)
	if err != nil {
		return tools.ErrorResult("MODEL_OUTPUT_INVALID: " + err.Error())
	}
	t.report = report
	return tools.SilentResult("Knowledge chunk label captured.")
}

func (t *KnowledgePageLabelTool) Report() map[string]any { return t.report }

func validateKnowledgePageLabel(args map[string]any, requireMarkdown bool) (map[string]any, error) {
	status := strings.TrimSpace(stringFromMap(args, "status"))
	if status != "ok" && status != "empty" {
		return nil, fmt.Errorf("status must be 'ok' or 'empty', got %q", status)
	}
	if status == "empty" {
		reason := strings.TrimSpace(stringFromMap(args, "reason"))
		if reason == "" {
			return nil, fmt.Errorf("reason is required when status is 'empty'")
		}
		return map[string]any{"status": "empty", "reason": reason}, nil
	}
	title := strings.TrimSpace(stringFromMap(args, "title"))
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	markdown := strings.TrimSpace(stringFromMap(args, "markdown"))
	if requireMarkdown && markdown == "" {
		return nil, fmt.Errorf("markdown is required for a scanned page")
	}
	return map[string]any{
		"status":   "ok",
		"title":    title,
		"summary":  strings.TrimSpace(stringFromMap(args, "summary")),
		"keywords": normalizeKeywords(args["keywords"]),
		"markdown": markdown,
	}, nil
}

type knowledgeLabelContext struct {
	Filename   string
	Kind       string
	ChunkCount int
	// DocHead is the first 300 chars of the first text chunk, so later chunks
	// can name the document they belong to in their title.
	DocHead string
}

func buildKnowledgeLabelPrompt(lctx knowledgeLabelContext, chunk knowledgeChunk) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("This is chunk %d/%d of the file %q (%s), location: %s.\n", chunk.Index+1, lctx.ChunkCount, lctx.Filename, lctx.Kind, chunk.Ref))
	if lctx.DocHead != "" && chunk.Index > 0 {
		sb.WriteString("How the document opens (context only): " + lctx.DocHead + "\n")
	}
	sb.WriteString("\n")
	if chunk.Kind == "image" {
		sb.WriteString(fmt.Sprintf("The attached images are %d scanned page(s) (%s). Transcribe ALL text into clean Markdown: numbers, codes and phone numbers VERBATIM; tables stay Markdown tables; keep page order and open each page with `## <page ref>`. A blank page or one with no text at all → status 'empty' with a reason.\n", len(chunk.ImagePaths), chunk.Ref))
	} else {
		sb.WriteString("The chunk content below is already extracted. Do NOT rewrite or summarize it into markdown — leave markdown empty. Label only.\n\n---\n")
		sb.WriteString(chunk.Text)
		sb.WriteString("\n---\n")
	}
	sb.WriteString(fmt.Sprintf("\nCall %s exactly once with: a self-describing Vietnamese title (document/brand name + the chunk's topic + time/version when present — never a generic heading), a one-sentence summary, and 3-8 keywords a person would search for.\n", knowledgeLabelToolName))
	return sb.String()
}

type knowledgeLabelRunner func(ctx context.Context, chunk knowledgeChunk, prompt string) (map[string]any, error)

type knowledgeLabelOutcome struct {
	Pages      []map[string]any // website-shaped {url,title,summary,keywords,markdown}
	Labeled    int
	Failed     int
	UnreadRefs []string // scanned chunks that produced no transcription
	VisionUsed bool
}

var refSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

// knowledgeChunkURL builds the page URL the website schema requires:
// file_url plus a fragment that is unique per chunk and stable for the same
// file, so Drupal's filename hash overwrites on re-upload instead of piling up.
func knowledgeChunkURL(fileURL string, chunk knowledgeChunk) string {
	slug := strings.Trim(refSlugChars.ReplaceAllString(strings.ToLower(chunk.Ref), "-"), "-")
	return fmt.Sprintf("%s#c%03d-%s", fileURL, chunk.Index+1, slug)
}

// labelKnowledgeChunks runs one label pass per chunk. A failing run is retried
// once; a text chunk that still fails keeps its extracted content under a
// fallback title, a scanned chunk is recorded as unread — one bad chunk never
// fails the job.
func labelKnowledgeChunks(ctx context.Context, chunks []knowledgeChunk, lctx knowledgeLabelContext, fileURL string, run knowledgeLabelRunner, progress func(done, total int)) (knowledgeLabelOutcome, error) {
	var out knowledgeLabelOutcome
	total := len(chunks)
	for i, chunk := range chunks {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		prompt := buildKnowledgeLabelPrompt(lctx, chunk)
		report, err := run(ctx, chunk, prompt)
		if err != nil {
			report, err = run(ctx, chunk, prompt)
		}
		if chunk.Kind == "image" {
			out.VisionUsed = true
		}
		switch {
		case err != nil || report == nil:
			out.Failed++
			if chunk.Kind == "image" {
				out.UnreadRefs = append(out.UnreadRefs, chunk.Ref)
			} else {
				out.Pages = append(out.Pages, fallbackKnowledgePage(fileURL, lctx, chunk))
			}
		case stringFromMap(report, "status") == "empty":
			if chunk.Kind == "image" {
				out.UnreadRefs = append(out.UnreadRefs, chunk.Ref)
			} else {
				out.Pages = append(out.Pages, fallbackKnowledgePage(fileURL, lctx, chunk))
			}
		default:
			out.Labeled++
			markdown := chunk.Text
			if chunk.Kind == "image" {
				markdown = stringFromMap(report, "markdown")
			}
			keywords, _ := report["keywords"].([]string)
			if keywords == nil {
				keywords = []string{}
			}
			out.Pages = append(out.Pages, map[string]any{
				"url":      knowledgeChunkURL(fileURL, chunk),
				"title":    stringFromMap(report, "title"),
				"summary":  stringFromMap(report, "summary"),
				"keywords": keywords,
				"markdown": markdown,
			})
		}
		if progress != nil && ((i+1)%knowledgeLabelProgressEvery == 0 || i+1 == total) {
			progress(i+1, total)
		}
	}
	return out, nil
}

func fallbackKnowledgePage(fileURL string, lctx knowledgeLabelContext, chunk knowledgeChunk) map[string]any {
	return map[string]any{
		"url":      knowledgeChunkURL(fileURL, chunk),
		"title":    lctx.Filename + " – " + chunk.Ref,
		"summary":  "",
		"keywords": []string{},
		"markdown": chunk.Text,
	}
}
