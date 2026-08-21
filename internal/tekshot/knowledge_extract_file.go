package tekshot

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/security"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// runKnowledgeExtractFile is the file branch of knowledge_extract: download →
// deterministic extraction → chunking → one short label run per chunk. The
// file itself never enters an agent run, which is what lets it be 100MB.
func (s *JobService) runKnowledgeExtractFile(ctx context.Context, job *store.TekshotJob, request map[string]any, fileURL string, pinnedIP net.IP) (any, string, error) {
	filename := strings.TrimSpace(stringFromMap(request, "filename"))
	if filename == "" {
		filename = "knowledge-source"
	}
	mime := strings.TrimSpace(stringFromMap(request, "mime"))

	workDir, err := os.MkdirTemp("", "goclaw-kx-")
	if err != nil {
		return nil, "", fmt.Errorf("knowledge_extract: create work dir: %w", err)
	}
	// Source plus every rendered page leaves the container with the job,
	// success or failure — 100MB + 300 PNGs must not accumulate in /tmp.
	defer os.RemoveAll(workDir)

	s.reportKnowledgeProgress(ctx, job, "Đang tải file về…")
	dlCtx, cancelDownload := context.WithTimeout(security.WithPinnedIP(ctx, pinnedIP), knowledgeSourceDownloadTimeout)
	defer cancelDownload()
	downloader := &knowledgeSourceDownloader{client: security.NewSafeClient(knowledgeSourceDownloadTimeout), maxBytes: knowledgeFileMaxBytes}
	srcPath, _, err := downloader.download(dlCtx, fileURL, workDir, filename)
	if err != nil {
		return nil, "", err
	}

	s.reportKnowledgeProgress(ctx, job, "Đang trích xuất nội dung…")
	extraction, err := runKnowledgeExtractor(ctx, knowledgeExtractorOptions{
		Input: srcPath, Mime: mime, OutDir: workDir,
		MaxScanPages: knowledgeExtractMaxScanPages, DPI: knowledgeExtractorDPI,
	})
	if err != nil {
		return nil, "", err
	}
	chunking := chunkKnowledgeUnits(extraction.Units)
	if len(chunking.Chunks) == 0 {
		return knowledgeFileEmptyReport(filename, "File không có nội dung chữ nào để trích."), "Nguồn rỗng", nil
	}

	loop, err := s.agents.Get(store.WithTenantID(ctx, store.MasterTenantID), job.AgentKey)
	if err != nil {
		return nil, "", err
	}
	userID := "tekshot-" + job.ExternalUserID
	runCtx := store.WithTenantID(ctx, store.MasterTenantID)
	runCtx = store.WithUserID(runCtx, userID)
	runCtx = store.WithAgentKey(runCtx, job.AgentKey)

	lctx := knowledgeLabelContext{Filename: filename, Kind: extraction.Kind, ChunkCount: len(chunking.Chunks), DocHead: knowledgeDocHead(chunking.Chunks)}
	outcome, err := labelKnowledgeChunks(runCtx, chunking.Chunks, lctx, fileURL, s.knowledgeLabelRunner(loop, job, userID), func(done, total int) {
		s.reportKnowledgeProgress(ctx, job, fmt.Sprintf("Đã gán nhãn %d/%d đoạn", done, total))
	})
	if err != nil {
		return nil, "", err
	}
	if len(outcome.Pages) == 0 {
		if outcome.VisionUsed {
			// Same honesty rule as the website branch: a scan nobody could read
			// must FAIL, never complete as hollow knowledge.
			return nil, "", fmt.Errorf("extraction tools unavailable: vision needed but dead")
		}
		return knowledgeFileEmptyReport(filename, "Không đoạn nào gán nhãn được."), "Nguồn rỗng", nil
	}
	report := buildKnowledgeFileReport(filename, extraction, chunking, outcome)
	return report, fmt.Sprintf("Đã nhập %d tài liệu", len(outcome.Pages)), nil
}

// reportKnowledgeProgress refreshes the job lock and pushes a running
// callback; Drupal's handleCallback writes progress_message on non-final
// statuses and the panel already renders it, so nothing else changes.
func (s *JobService) reportKnowledgeProgress(ctx context.Context, job *store.TekshotJob, msg string) {
	_ = s.store.MarkRunning(ctx, job.ID, msg, defaultJobLockTTL)
	s.sendCallback(ctx, job, store.TekshotJobRunning, msg, "", nil)
}

func knowledgeDocHead(chunks []knowledgeChunk) string {
	for _, c := range chunks {
		if c.Kind != "text" {
			continue
		}
		r := []rune(c.Text)
		if len(r) > knowledgeLabelDocHeadChars {
			r = r[:knowledgeLabelDocHeadChars]
		}
		return string(r)
	}
	return ""
}

// knowledgeLabelToolAllow: scanned chunks keep read_image for file-ref vision
// mode (see describeImageToolAllow); text chunks need no tool, and nil would
// mean "no restriction", so they carry only the harmless datetime.
func knowledgeLabelToolAllow(scanned bool) []string {
	if scanned {
		return []string{"read_image"}
	}
	return []string{"datetime"}
}

func (s *JobService) knowledgeLabelRunner(loop agent.Agent, job *store.TekshotJob, userID string) knowledgeLabelRunner {
	return func(ctx context.Context, chunk knowledgeChunk, prompt string) (map[string]any, error) {
		scanned := chunk.Kind == "image"
		collector := NewKnowledgePageLabelTool(scanned)
		req := agent.RunRequest{
			SessionKey:     job.SessionKey,
			Message:        prompt,
			Channel:        "tekshot_job",
			ChannelType:    "tekshot",
			ChatID:         userID,
			PeerKind:       "direct",
			Addressed:      true,
			RunID:          uuid.NewString(),
			UserID:         userID,
			SenderID:       userID,
			ToolAllow:      knowledgeLabelToolAllow(scanned),
			EphemeralTools: []tools.Tool{collector},
			MaxIterations:  knowledgeLabelMaxIterations,
			// Hundreds of label turns share one session: keep only the current
			// turn in context and skip context files/skills — every run is
			// self-contained and has to stay cheap.
			HistoryLimit: 1,
			LightContext: true,
			SkillFilter:  []string{},
			TraceName:    "tekshot knowledge label",
			TraceTags:    []string{"tekshot", "knowledge_extract", "label"},
		}
		if scanned {
			for _, p := range chunk.ImagePaths {
				req.Media = append(req.Media, bus.MediaFile{Path: p, MimeType: "image/png", Filename: filepath.Base(p)})
			}
		}
		if _, err := loop.Run(ctx, req); err != nil && collector.Report() == nil {
			return nil, err
		}
		if collector.Report() == nil {
			final := req
			final.RunID = uuid.NewString()
			final.MaxIterations = 2
			final.Media = nil
			final.Message = fmt.Sprintf("Submit the label now by calling %s. Do not answer with plain text.", knowledgeLabelToolName)
			final.ToolChoice = &providers.ToolChoice{Mode: "function", Name: knowledgeLabelToolName}
			if _, err := loop.Run(ctx, final); err != nil && collector.Report() == nil {
				return nil, err
			}
		}
		if collector.Report() == nil {
			return nil, fmt.Errorf("MODEL_OUTPUT_INVALID: no label submitted for %s", chunk.Ref)
		}
		return collector.Report(), nil
	}
}

// buildKnowledgeFileReport is the job result Drupal's createFromExtraction
// reads: website-shaped pages[] plus file-branch bookkeeping.
func buildKnowledgeFileReport(filename string, extraction *knowledgeExtraction, chunking knowledgeChunking, outcome knowledgeLabelOutcome) map[string]any {
	urls := make([]string, 0, len(outcome.Pages))
	for _, p := range outcome.Pages {
		urls = append(urls, stringFromMap(p, "url"))
	}
	var reasons []string
	if extraction.Truncated && strings.TrimSpace(extraction.TruncatedReason) != "" {
		reasons = append(reasons, strings.TrimSpace(extraction.TruncatedReason))
	}
	if chunking.Truncated {
		reasons = append(reasons, fmt.Sprintf("Tài liệu dài quá %d đoạn, đã bỏ %d đoạn từ %s.", knowledgeFileMaxChunks, chunking.Dropped, chunking.DroppedFrom))
	}
	vision := "unused"
	if outcome.VisionUsed {
		vision = "ok"
	}
	sourcePages := ""
	if extraction.Stats.Pages > 0 {
		sourcePages = fmt.Sprintf("1-%d", extraction.Stats.Pages)
	}
	unread := outcome.UnreadRefs
	if unread == nil {
		unread = []string{}
	}
	return map[string]any{
		"title":                  knowledgeFileStem(filename),
		"language":               "vi",
		"status":                 "ok",
		"source_kind":            "file",
		"source_pages":           sourcePages,
		"pages":                  outcome.Pages,
		"pages_fetched":          urls,
		"total_pages_discovered": len(chunking.Chunks) + chunking.Dropped,
		"truncated":              extraction.Truncated || chunking.Truncated,
		"truncated_reason":       strings.Join(reasons, " "),
		"unread_refs":            unread,
		"tool_health":            map[string]any{"exec": "ok", "vision": vision},
		"extractor": map[string]any{
			"kind": extraction.Kind, "pages": extraction.Stats.Pages, "text_pages": extraction.Stats.TextPages,
			"scan_pages": extraction.Stats.ScanPages, "scan_pages_rendered": extraction.Stats.ScanPagesRendered,
			"sheets": extraction.Stats.Sheets, "slides": extraction.Stats.Slides,
			"chunks": len(chunking.Chunks), "labeled": outcome.Labeled, "label_failed": outcome.Failed,
		},
	}
}

func knowledgeFileEmptyReport(filename, reason string) map[string]any {
	return map[string]any{
		"title":       knowledgeFileStem(filename),
		"language":    "vi",
		"status":      "empty",
		"reason":      reason,
		"source_kind": "file",
		"tool_health": map[string]any{"exec": "ok", "vision": "unused"},
	}
}

// knowledgeFileStem turns "Bang_gia-2026.pdf" into "Bang gia 2026" for the
// index document title; chunk titles come from the model.
func knowledgeFileStem(filename string) string {
	stem := strings.TrimSuffix(filename, filepath.Ext(filename))
	stem = strings.TrimSpace(strings.NewReplacer("_", " ", "-", " ").Replace(stem))
	if stem == "" {
		return "Tài liệu"
	}
	return stem
}
