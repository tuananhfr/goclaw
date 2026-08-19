package tekshot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/security"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const (
	knowledgeFinalToolName        = "submit_knowledge_markdown"
	knowledgeExtractMaxIterations = 30
	knowledgeExtractMaxScanPages  = 20
)

// knowledgeExtractToolAllow: exec drives markitdown/pdfplumber/pdftoppm,
// read_image covers scanned pages (routed to the tenant's codex vision),
// web_fetch covers website imports. read_document is deliberately absent:
// its provider chain has no codex entry, so it is dead on this tenant.
func knowledgeExtractToolAllow() []string {
	return []string{"exec", "read_image", "web_fetch", "datetime"}
}

type KnowledgeExtractCollectorTool struct {
	report map[string]any
}

func NewKnowledgeExtractCollectorTool() *KnowledgeExtractCollectorTool {
	return &KnowledgeExtractCollectorTool{}
}

func (t *KnowledgeExtractCollectorTool) Name() string { return knowledgeFinalToolName }

func (t *KnowledgeExtractCollectorTool) Description() string {
	return "Submit the final extracted knowledge as validated structured JSON. Call this exactly once when extraction is complete (or when you determined the source is empty)."
}

func (t *KnowledgeExtractCollectorTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"title": map[string]any{
				"type":        "string",
				"description": "Short Vietnamese title for the knowledge document, derived from the content (not the filename) when possible.",
			},
			"markdown": map[string]any{
				"type":        "string",
				"description": "The full extracted content as clean Markdown. Tables stay Markdown tables with values copied VERBATIM — never round or reformat numbers. Keep sheet names as `## <sheet>` headings, PDF pages as `## Trang N`. NO frontmatter — the caller adds it.",
			},
			"language": map[string]any{"type": "string", "description": "Primary language of the extracted content, e.g. 'vi'."},
			"status": map[string]any{
				"type":        "string",
				"description": "'ok' when content was extracted; 'empty' when the source genuinely has no extractable content. Never fabricate content to avoid 'empty'.",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "Required when status is 'empty': short honest Vietnamese reason (e.g. blank pages, image contains no text).",
			},
			"source_pages": map[string]any{"type": "string", "description": "Page range actually extracted, e.g. '1-4'. Empty for non-paged sources."},
			"truncated":    map[string]any{"type": "boolean", "description": "true when the source had more pages than the 20-page scan cap."},
			"tool_health": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"exec":   map[string]any{"type": "string", "description": "'ok' when shell conversion tools worked; 'dead' when exec was unavailable or every conversion tool failed. Be honest — 'dead' fails the job, which is CORRECT when tools are broken."},
					"vision": map[string]any{"type": "string", "description": "'ok' when image reading worked; 'dead' when it was needed but unavailable; 'unused' when the source needed no vision."},
				},
				"required": []string{"exec", "vision"},
			},
		},
		"required": []string{"title", "markdown", "language", "status", "tool_health"},
	}
}

func (t *KnowledgeExtractCollectorTool) Execute(_ context.Context, args map[string]any) *tools.Result {
	report, err := validateKnowledgeExtractReport(args)
	if err != nil {
		return tools.ErrorResult("MODEL_OUTPUT_INVALID: " + err.Error())
	}
	t.report = report
	return tools.SilentResult("Structured knowledge extraction captured.")
}

func (t *KnowledgeExtractCollectorTool) Report() map[string]any {
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

func validateKnowledgeExtractReport(args map[string]any) (map[string]any, error) {
	status := strings.TrimSpace(stringFromMap(args, "status"))
	if status != "ok" && status != "empty" {
		return nil, fmt.Errorf("status must be 'ok' or 'empty', got %q", status)
	}
	title := strings.TrimSpace(stringFromMap(args, "title"))
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	markdown := strings.TrimSpace(stringFromMap(args, "markdown"))
	if status == "ok" && markdown == "" {
		return nil, fmt.Errorf("markdown must be non-empty when status is 'ok'")
	}
	if status == "empty" && strings.TrimSpace(stringFromMap(args, "reason")) == "" {
		return nil, fmt.Errorf("reason is required when status is 'empty'")
	}
	health, ok := args["tool_health"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("tool_health is required")
	}
	execHealth := strings.TrimSpace(stringFromMap(health, "exec"))
	if execHealth != "ok" && execHealth != "dead" {
		return nil, fmt.Errorf("tool_health.exec must be 'ok' or 'dead', got %q", execHealth)
	}
	vision := strings.TrimSpace(stringFromMap(health, "vision"))
	if vision != "ok" && vision != "dead" && vision != "unused" {
		return nil, fmt.Errorf("tool_health.vision must be 'ok', 'dead' or 'unused', got %q", vision)
	}
	return args, nil
}

func buildKnowledgeExtractPrompt(request map[string]any) string {
	websiteURL := strings.TrimSpace(stringFromMap(request, "website_url"))
	mime := strings.TrimSpace(stringFromMap(request, "mime"))
	filename := strings.TrimSpace(stringFromMap(request, "filename"))

	var sb strings.Builder
	sb.WriteString("Extract the content of the attached source into clean Vietnamese-friendly Markdown for a store knowledge base.\n")
	sb.WriteString("The Markdown is later searched by an AI assistant to answer questions about prices, products and rules — completeness and VERBATIM numbers matter more than prose quality.\n\n")

	if websiteURL != "" {
		sb.WriteString("## Source: website\n")
		sb.WriteString("- URL: " + websiteURL + "\n")
		sb.WriteString("- Read it with web_fetch. Extract the main content only: no navigation, no footer, no cookie banners.\n\n")
	} else {
		sb.WriteString("## Source: file attached to this message\n")
		if filename != "" {
			sb.WriteString("- Filename: " + filename + "\n")
		}
		if mime != "" {
			sb.WriteString("- MIME: " + mime + "\n")
		}
		sb.WriteString("- The file was downloaded into this run's workspace; its path is stamped on the message media tag.\n\n")
		sb.WriteString("## Extraction strategy (follow in order)\n")
		sb.WriteString("1. Office files (docx/xlsx/pptx): run `markitdown <path>` via exec. Keep sheet names as `## <sheet>` headings; tables stay Markdown tables.\n")
		sb.WriteString("2. PDF: try text first — `pdftotext <path> -` via exec (or python3 with pdfplumber for tables).\n")
		sb.WriteString(fmt.Sprintf("3. PDF with an empty text layer (scan): render pages with `pdftoppm -r 150 -png <path> page` via exec, then read each page image with read_image. Hard cap: %d pages — past it, stop and set truncated=true.\n", knowledgeExtractMaxScanPages))
		sb.WriteString("4. Image files: read the image directly (it is attached; use read_image if it is not visible inline). Transcribe ALL text, prices and tables you see.\n\n")
	}

	sb.WriteString("## Hard rules\n")
	sb.WriteString("- Copy numbers, prices and units VERBATIM. Never round, never convert currencies.\n")
	sb.WriteString("- Keep the source's own language for content; headings may be Vietnamese.\n")
	sb.WriteString("- PDF pages become `## Trang N` sections.\n")
	sb.WriteString("- Do NOT invent content. A blank source is submitted as status='empty' with an honest reason.\n")
	sb.WriteString("- When every conversion tool fails, submit tool_health.exec='dead' — do not hand-write a fake extraction.\n")
	sb.WriteString(fmt.Sprintf("- Finish by calling %s exactly once with the full result. Do not answer with plain text.\n", knowledgeFinalToolName))
	return sb.String()
}

// runKnowledgeExtract turns one uploaded file (Drupal public URL) or one
// website URL into a validated Markdown knowledge report. The file arrives as
// a URL; the agent loop's persistMedia downloads URL media into the run
// workspace, so no download code lives here (same as describe_image).
func (s *JobService) runKnowledgeExtract(ctx context.Context, job *store.TekshotJob, request map[string]any) (any, string, error) {
	if s.agents == nil {
		return nil, "", fmt.Errorf("agent router is not configured")
	}
	fileURL := strings.TrimSpace(stringFromMap(request, "file_url"))
	websiteURL := strings.TrimSpace(stringFromMap(request, "website_url"))
	if (fileURL == "") == (websiteURL == "") {
		return nil, "", fmt.Errorf("knowledge_extract requires exactly one of file_url or website_url")
	}
	probeURL := fileURL
	if probeURL == "" {
		probeURL = websiteURL
	}
	if !strings.HasPrefix(probeURL, "http://") && !strings.HasPrefix(probeURL, "https://") {
		return nil, "", fmt.Errorf("knowledge_extract source must be an http(s) URL")
	}

	// The probe runs before the agent, so it runs before web_fetch's own SSRF
	// protection too — without its own check here, the probe is a way to walk
	// around it and read back internal services (postgres, redis, chrome, the
	// host) as "extracted knowledge" handed straight to the requesting user.
	// security.Validate + WithPinnedIP + NewSafeClient (internal/security/ssrf.go)
	// pin the resolved IP against DNS rebinding, re-check it at dial time, and
	// refuse to follow a redirect into a blocked range.
	_, pinnedIP, err := security.Validate(probeURL)
	if err != nil {
		return nil, "", fmt.Errorf("knowledge_extract: source url rejected: %w", err)
	}

	// Fail fast on a dead URL — without this the agent runs with no
	// attachment and "extracts" nothing plausible-looking (same trap
	// describe_image hit with stale media rows).
	probeCtx, cancelProbe := context.WithTimeout(ctx, 20*time.Second)
	defer cancelProbe()
	probeCtx = security.WithPinnedIP(probeCtx, pinnedIP)
	probeReq, err := http.NewRequestWithContext(probeCtx, http.MethodGet, probeURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("knowledge_extract: invalid source url: %w", err)
	}
	// Match web_fetch's User-Agent (internal/tools/web_fetch.go) — sites that
	// block Go's default UA (e.g. Wikipedia) still 403 a bare probe even
	// though the agent's web_fetch tool would have fetched them fine.
	probeReq.Header.Set("User-Agent", tools.FetchUserAgent)
	probeResp, err := security.NewSafeClient(20 * time.Second).Do(probeReq)
	if err != nil {
		return nil, "", fmt.Errorf("knowledge_extract: source unreachable: %w", err)
	}
	probeResp.Body.Close()
	// The safe client never follows redirects (CheckRedirect returns
	// ErrUseLastResponse), so an ordinary http->https or trailing-slash
	// redirect now surfaces here as a 3xx rather than a 2xx. The probe only
	// needs to know the host is alive and not internal — following redirects
	// and reading the body is web_fetch's job, and it re-validates every hop
	// itself — so 3xx counts as reachable too. Do not narrow this back to
	// 200-299 only, or every redirecting URL fails the probe.
	if probeResp.StatusCode < 200 || probeResp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("knowledge_extract: source returned HTTP %d", probeResp.StatusCode)
	}

	loop, err := s.agents.Get(store.WithTenantID(ctx, store.MasterTenantID), job.AgentKey)
	if err != nil {
		return nil, "", err
	}

	userID := "tekshot-" + job.ExternalUserID
	runCtx := store.WithTenantID(ctx, store.MasterTenantID)
	runCtx = store.WithUserID(runCtx, userID)
	runCtx = store.WithAgentKey(runCtx, job.AgentKey)

	collector := NewKnowledgeExtractCollectorTool()
	message := buildKnowledgeExtractPrompt(request)

	runReq := agent.RunRequest{
		SessionKey:     job.SessionKey,
		Message:        message,
		Channel:        "tekshot_job",
		ChannelType:    "tekshot",
		ChatID:         userID,
		PeerKind:       "direct",
		Addressed:      true,
		RunID:          uuid.NewString(),
		UserID:         userID,
		SenderID:       userID,
		ToolAllow:      knowledgeExtractToolAllow(),
		EphemeralTools: []tools.Tool{collector},
		MaxIterations:  knowledgeExtractMaxIterations,
		TraceName:      "tekshot knowledge extract",
		TraceTags:      []string{"tekshot", "knowledge_extract"},
	}

	// File sources ride the media pipeline so persistMedia drops the file
	// into the run workspace where exec can reach it. Website sources carry
	// no media — web_fetch reads the URL from the prompt.
	if fileURL != "" {
		mime := strings.TrimSpace(stringFromMap(request, "mime"))
		filename := strings.TrimSpace(stringFromMap(request, "filename"))
		if filename == "" {
			filename = "knowledge-source"
		}
		mediaType := media.TypeDocument
		if strings.HasPrefix(mime, "image/") {
			mediaType = media.TypeImage
		}
		if tags := media.BuildMediaTags([]media.MediaInfo{{
			Type:        mediaType,
			FilePath:    fileURL,
			ContentType: mime,
			FileName:    filename,
		}}); tags != "" {
			runReq.Message = tags + "\n\n" + message
		}
		runReq.Media = []bus.MediaFile{{
			Path:     fileURL,
			Filename: filename,
		}}
	}

	if _, err := loop.Run(runCtx, runReq); err != nil && collector.Report() == nil {
		return nil, "", err
	}

	// Forced pass, mirror market_research: the extraction already sits in
	// session history; the forced turn only re-asserts structured submission.
	if collector.Report() == nil {
		finalReq := runReq
		finalReq.RunID = uuid.NewString()
		finalReq.MaxIterations = 3
		finalReq.Message = fmt.Sprintf("Submit the final extraction now by calling %s with the complete structured result. Do not answer with plain text.", knowledgeFinalToolName)
		finalReq.ToolChoice = &providers.ToolChoice{Mode: "function", Name: knowledgeFinalToolName}
		// The model already saw the file in session history from the first
		// pass; persistMedia has no dedup, so re-sending Media here would
		// re-download the URL and write a second temp file for nothing.
		finalReq.Media = nil
		if _, err := loop.Run(runCtx, finalReq); err != nil && collector.Report() == nil {
			return nil, "", fmt.Errorf("final structured submission failed: %w", err)
		}
	}

	report := collector.Report()
	if report == nil {
		return nil, "", fmt.Errorf("MODEL_OUTPUT_INVALID: agent did not submit a valid knowledge extraction")
	}

	// Dead exec must FAIL the job, never complete hollow — Drupal treats a
	// failed job as a retryable outage, a completed one as trusted knowledge.
	if health, ok := report["tool_health"].(map[string]any); ok {
		if stringFromMap(health, "exec") == "dead" {
			return nil, "", fmt.Errorf("extraction tools unavailable: exec reported dead")
		}
		if stringFromMap(health, "vision") == "dead" {
			return nil, "", fmt.Errorf("extraction tools unavailable: vision needed but dead")
		}
	}

	return report, "Knowledge extracted", nil
}
