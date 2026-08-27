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
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/security"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const (
	knowledgeFinalToolName = "submit_knowledge_markdown"
	// 45 fits: entry + sitemap probes + 20 page fetches + splits + submit.
	// web_fetch also halves its own char budget past 50% of this (web_fetch.go).
	knowledgeExtractMaxIterations = 45
	// Scanned PDF pages rendered per job by knowledge_extract.py; each page is
	// one vision read, so this is the cost brake.
	knowledgeExtractMaxScanPages = 300
	// The binding limit is NOT MaxIterations: the loop kills a run after 36
	// consecutive read-only tool calls (readOnlyExplorationCritical in
	// internal/agent/toolloop.go) and web_fetch is read-only. 12 pages plus
	// sitemap probes stays clear of that ceiling.
	knowledgeExtractMaxSitePages = 12
	// One tool call carries the whole submission, so the pages together must
	// stay inside the model's output budget.
	knowledgeExtractMaxTotalChars = 40000
)

// knowledgeExtractToolAllow is the website crawl's tool list: web_fetch reads
// pages and exec reads web_fetch's spill files (cat <path>). File sources never
// reach this run — they branch off to runKnowledgeExtractFile.
func knowledgeExtractToolAllow() []string {
	return []string{"exec", "read_image", "web_fetch", "datetime"}
}

type KnowledgeExtractCollectorTool struct {
	report map[string]any
	// Website sources submit per-page entries instead of one markdown blob: the
	// vault indexes and reads whole documents, so a page is the unit that fits.
	websiteSource bool
}

func NewKnowledgeExtractCollectorTool(websiteSource bool) *KnowledgeExtractCollectorTool {
	return &KnowledgeExtractCollectorTool{websiteSource: websiteSource}
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
				"description": "File sources ONLY: the full extracted content as clean Markdown. Tables stay Markdown tables with values copied VERBATIM — never round or reformat numbers. Keep sheet names as `## <sheet>` headings, PDF pages as `## Trang N`. NO frontmatter — the caller adds it. Website sources leave this empty and submit `pages` instead.",
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
			"pages": map[string]any{
				"type":        "array",
				"description": "Website sources ONLY: one entry per content page you extracted, in read order. File sources leave this empty and use markdown instead.",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"url":     map[string]any{"type": "string", "description": "The page URL you fetched."},
						"title":   map[string]any{"type": "string", "description": "Specific Vietnamese title naming brand + topic (+ time/version when the page has one), e.g. 'Cát Tường – Bảng giá NOXH Smart City (đợt 3/2026)'. Never submit the site's bare generic H1."},
						"summary": map[string]any{"type": "string", "description": "One Vietnamese sentence: what facts this page holds."},
						"keywords": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "3-8 terms a person would search for: product/project names, price words, unit codes, phone numbers, policy terms.",
						},
						"markdown": map[string]any{"type": "string", "description": "The page content as clean Markdown, numbers VERBATIM. Target 1500-6000 characters."},
					},
					"required": []string{"url", "title", "markdown"},
				},
			},
			"total_pages_discovered": map[string]any{"type": "number", "description": "Website sources: how many candidate content pages you discovered (sitemap or internal links), including ones you did not read."},
			"truncated":              map[string]any{"type": "boolean", "description": "true when the source had more than you could read: more PDF pages than the 20-page scan cap, or more site pages than the 20-page crawl cap."},
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
	report, err := validateKnowledgeExtractReport(args, t.websiteSource)
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

// normalizePages validates and canonicalizes the per-page entries a website
// extraction submits: trims, dedupes by url (first wins), caps keywords at 8.
func normalizePages(raw any) ([]map[string]any, error) {
	items, ok := raw.([]any)
	if !ok {
		return nil, nil
	}
	seen := make(map[string]bool, len(items))
	pages := make([]map[string]any, 0, len(items))
	for i, item := range items {
		entry, isMap := item.(map[string]any)
		if !isMap {
			return nil, fmt.Errorf("pages[%d] must be an object", i)
		}
		url := strings.TrimSpace(stringFromMap(entry, "url"))
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			return nil, fmt.Errorf("pages[%d].url must be an http(s) URL", i)
		}
		if seen[url] {
			continue
		}
		title := strings.TrimSpace(stringFromMap(entry, "title"))
		if title == "" {
			return nil, fmt.Errorf("pages[%d].title is required", i)
		}
		md := strings.TrimSpace(stringFromMap(entry, "markdown"))
		if md == "" {
			return nil, fmt.Errorf("pages[%d].markdown is required", i)
		}
		seen[url] = true
		pages = append(pages, map[string]any{
			"url":      url,
			"title":    title,
			"summary":  strings.TrimSpace(stringFromMap(entry, "summary")),
			"keywords": normalizeKeywords(entry["keywords"]),
			"markdown": md,
		})
	}
	return pages, nil
}

func normalizeKeywords(raw any) []string {
	items, ok := raw.([]any)
	if !ok {
		return []string{}
	}
	out := make([]string, 0, 8)
	for _, item := range items {
		s, isString := item.(string)
		if !isString {
			continue
		}
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = append(out, s)
		if len(out) == 8 {
			break
		}
	}
	return out
}

func validateKnowledgeExtractReport(args map[string]any, websiteSource bool) (map[string]any, error) {
	status := strings.TrimSpace(stringFromMap(args, "status"))
	if status != "ok" && status != "empty" {
		return nil, fmt.Errorf("status must be 'ok' or 'empty', got %q", status)
	}
	title := strings.TrimSpace(stringFromMap(args, "title"))
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	markdown := strings.TrimSpace(stringFromMap(args, "markdown"))
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
	if websiteSource {
		pages, pagesErr := normalizePages(args["pages"])
		if pagesErr != nil {
			return nil, pagesErr
		}
		if status == "ok" && len(pages) == 0 {
			return nil, fmt.Errorf("pages must hold at least one extracted page for a website source")
		}
		args["pages"] = pages
		urls := make([]string, 0, len(pages))
		for _, page := range pages {
			urls = append(urls, page["url"].(string))
		}
		args["pages_fetched"] = urls
	} else {
		if status == "ok" && markdown == "" {
			return nil, fmt.Errorf("markdown must be non-empty when status is 'ok'")
		}
		delete(args, "pages")
		delete(args, "pages_fetched")
	}
	return args, nil
}

// buildKnowledgeExtractPrompt drives the website crawl. A file source never
// gets here — runKnowledgeExtractFile extracts it without an agent.
func buildKnowledgeExtractPrompt(request map[string]any) string {
	websiteURL := strings.TrimSpace(stringFromMap(request, "website_url"))

	var sb strings.Builder
	sb.WriteString("Extract the content of the attached source into clean Vietnamese-friendly Markdown for a store knowledge base.\n")
	sb.WriteString("The Markdown is later searched by an AI assistant to answer questions about prices, products and rules — completeness and VERBATIM numbers matter more than prose quality.\n\n")

	if websiteURL != "" {
		sb.WriteString("## Source: website\n")
		sb.WriteString("- Entry URL: " + websiteURL + "\n")
		sb.WriteString("- This imports the SITE, not one page. Each content page becomes its own knowledge document, so submit each page as its own entry.\n\n")
		sb.WriteString("## Crawl strategy (follow in order)\n")
		sb.WriteString("1. web_fetch the entry URL. Use it to discover the site; include it as a content page ONLY when it carries unique facts no subpage has.\n")
		sb.WriteString("2. Build the page list. Try `<origin>/sitemap.xml` (then `/sitemap_index.xml`) first — the cheapest full list. No sitemap: internal links of the entry page, same host only.\n")
		sb.WriteString("3. Rank by fact density: bảng giá / sản phẩm / dịch vụ / dự án / giới thiệu / liên hệ / chính sách, then article pages. Skip tag, category, author, search and pagination URLs.\n")
		sb.WriteString("   HTML pages only. NEVER web_fetch an attachment or binary URL — .pdf, .doc(x), .xls(x), .ppt(x), .zip, .jpg, .png, /wp-content/uploads/ and the like. Fetching one wastes the whole iteration budget on binary noise. A page that links an attachment: keep the link inside that page's markdown as `[tên file](url)` and move on.\n")
		sb.WriteString(fmt.Sprintf("4. web_fetch the ranked pages, up to %d content pages. Never fetch the same URL twice.\n", knowledgeExtractMaxSitePages))
		sb.WriteString("5. Submit each page as one entry in `pages`: its own rewritten title, a one-sentence summary, 3-8 business keywords, and its full Markdown.\n")
		sb.WriteString("6. Set total_pages_discovered to how many candidate content pages you found. Stopped at the cap, out of fetches or over the size budget → set truncated=true.\n\n")
		sb.WriteString("## Budget — the run is killed if you overrun it\n")
		sb.WriteString("- You get about 30 web_fetch calls TOTAL, sitemap probes included. Spend them on content pages and submit while you still have budget: a run that keeps reading is killed before it can submit, and the whole extraction is lost.\n")
		sb.WriteString(fmt.Sprintf("- The whole submission travels in ONE tool call, so keep every page's markdown together under ~%d characters. Over budget: keep the highest-value pages (giá, sản phẩm/dự án, điều kiện, liên hệ), drop the rest and set truncated=true.\n\n", knowledgeExtractMaxTotalChars))
		sb.WriteString("## Per-page contract\n")
		sb.WriteString("- title: specific and self-describing — brand + topic (+ time/version when the page has one). A reader must know what the page holds from the title alone; never submit a bare generic H1 like \"406 CĂN HỘ\".\n")
		sb.WriteString("- markdown: target 1500-6000 characters. A page over ~8000 characters: split it by its H2 sections into two entries titled \"... (phần 1/2)\" and \"... (phần 2/2)\", same url on both.\n")
		sb.WriteString("- Drop nav menus, cookie banners, share widgets, pagination controls and related-post teasers.\n")
		sb.WriteString("- KEEP the contact block wherever it sits, footer included: địa chỉ, số điện thoại, email, mã số thuế, giờ mở cửa, danh sách chi nhánh. That is what the assistant gets asked about.\n")
		sb.WriteString("- A teaser the site itself cut with \"…\" is worth nothing: open the linked page and take the full text, or leave the teaser out.\n\n")
	}

	sb.WriteString("## Hard rules\n")
	sb.WriteString("- Copy numbers, prices and units VERBATIM. Never round, never convert currencies.\n")
	sb.WriteString("- Keep the source's own language for content; headings may be Vietnamese.\n")
	sb.WriteString("- Do NOT invent content. A blank source is submitted as status='empty' with an honest reason.\n")
	sb.WriteString("- web_fetch spills anything past its char limit into a temp file and names the path: read the rest with exec (`cat <path>`). read_file is NOT available in this run, so skipping that step silently loses the tail.\n")
	sb.WriteString("- When web_fetch itself is unusable, submit tool_health.exec='dead' — do not hand-write a fake extraction.\n")
	sb.WriteString(fmt.Sprintf("- Finish by calling %s exactly once with the full result. Do not answer with plain text.\n", knowledgeFinalToolName))
	return sb.String()
}

// runKnowledgeExtract validates the source, then splits: a file goes to
// runKnowledgeExtractFile (deterministic extraction, never through an agent
// run), a website into the crawl below.
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

	if fileURL != "" {
		fileReport, message, fileErr := s.runKnowledgeExtractFile(ctx, job, request, fileURL, pinnedIP)
		if fileErr != nil {
			return nil, "", fileErr
		}
		// Prompt E chạy trên cả hai nhánh: không trang nào vào vault mà chưa
		// qua thẩm định, dù nó đến từ file hay từ crawl.
		screened, ok := fileReport.(map[string]any)
		if !ok {
			return fileReport, message, nil
		}
		screened = s.applyKnowledgeScreening(ctx, job, request, screened)
		return screened, knowledgeScreenMessage(screened, message), nil
	}

	loop, err := s.agents.Get(store.WithTenantID(ctx, store.MasterTenantID), job.AgentKey)
	if err != nil {
		return nil, "", err
	}

	userID := "tekshot-" + job.ExternalUserID
	runCtx := store.WithTenantID(ctx, store.MasterTenantID)
	runCtx = store.WithUserID(runCtx, userID)
	runCtx = store.WithAgentKey(runCtx, job.AgentKey)

	collector := NewKnowledgeExtractCollectorTool(websiteURL != "")
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

	// The website run carries no media: web_fetch reads the URL from the prompt.
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

	report = s.applyKnowledgeScreening(ctx, job, request, report)
	return report, knowledgeScreenMessage(report, "Knowledge extracted"), nil
}

// knowledgeScreenMessage nói thẳng số trang bị giữ lại trong dòng trạng thái
// job, để panel không phải mở khối screening mới biết import bị cắt bớt.
func knowledgeScreenMessage(report map[string]any, fallback string) string {
	screening, ok := report["screening"].(map[string]any)
	if !ok {
		return fallback
	}
	held := intFromAny(screening["held"])
	if held == 0 {
		return fallback
	}
	return fmt.Sprintf("%s — %d tài liệu bị giữ lại chờ duyệt", fallback, held)
}

func intFromAny(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}
