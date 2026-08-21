package tekshot

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//go:embed knowledge_extract.py
var knowledgeExtractScript string

const (
	knowledgeExtractorTimeout   = 10 * time.Minute
	knowledgeExtractorDPI       = 150
	knowledgeExtractorPython    = "python3"
	knowledgeExtractorStderrCap = 4096
)

type knowledgeUnit struct {
	Index     int    `json:"index"`
	Kind      string `json:"kind"` // "text" | "image"
	Ref       string `json:"ref"`  // "Trang 12", "Sheet Giá", "Slide 3", "Phần 2"
	Text      string `json:"text,omitempty"`
	ImagePath string `json:"image_path,omitempty"`
	Heading   string `json:"heading,omitempty"`
}

type knowledgeExtractorStats struct {
	Pages             int `json:"pages"`
	TextPages         int `json:"text_pages"`
	ScanPages         int `json:"scan_pages"`
	ScanPagesRendered int `json:"scan_pages_rendered"`
	Sheets            int `json:"sheets"`
	Slides            int `json:"slides"`
}

// knowledgeExtraction is the stdout contract of knowledge_extract.py.
type knowledgeExtraction struct {
	OK              bool                    `json:"ok"`
	Kind            string                  `json:"kind"`
	Units           []knowledgeUnit         `json:"units"`
	Stats           knowledgeExtractorStats `json:"stats"`
	Truncated       bool                    `json:"truncated"`
	TruncatedReason string                  `json:"truncated_reason"`
	Error           string                  `json:"error"`
	Message         string                  `json:"message"`
}

type knowledgeExtractorOptions struct {
	Input        string
	Mime         string
	OutDir       string
	MaxScanPages int
	DPI          int
	// Command replaces "python3 <script>"; tests point it at a stub that
	// prints a fixture. The CLI args below are appended either way.
	Command []string
}

// knowledgeExtractorArgs is the CLI contract with knowledge_extract.py.
func knowledgeExtractorArgs(opts knowledgeExtractorOptions) []string {
	return []string{
		"--input", opts.Input,
		"--mime", opts.Mime,
		"--out-dir", opts.OutDir,
		"--max-scan-pages", strconv.Itoa(opts.MaxScanPages),
		"--dpi", strconv.Itoa(opts.DPI),
	}
}

// runKnowledgeExtractor runs the embedded Python extractor on one file. The
// script never raises: every failure arrives as {"ok":false,...} whose
// message is already a Vietnamese sentence for the panel.
func runKnowledgeExtractor(ctx context.Context, opts knowledgeExtractorOptions) (*knowledgeExtraction, error) {
	if opts.DPI <= 0 {
		opts.DPI = knowledgeExtractorDPI
	}
	command := opts.Command
	if len(command) == 0 {
		scriptPath := filepath.Join(opts.OutDir, "knowledge_extract.py")
		if err := os.WriteFile(scriptPath, []byte(knowledgeExtractScript), 0o600); err != nil {
			return nil, fmt.Errorf("knowledge_extract: write extractor script: %w", err)
		}
		command = []string{knowledgeExtractorPython, scriptPath}
	}
	runCtx, cancel := context.WithTimeout(ctx, knowledgeExtractorTimeout)
	defer cancel()

	args := append(append([]string{}, command[1:]...), knowledgeExtractorArgs(opts)...)
	cmd := exec.CommandContext(runCtx, command[0], args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return nil, errors.New("trích xuất quá 10 phút — file quá lớn hoặc quá phức tạp, hãy cắt nhỏ rồi tải lại")
	}

	var out knowledgeExtraction
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &out); err != nil {
		tail := strings.TrimSpace(stderr.String())
		if len(tail) > knowledgeExtractorStderrCap {
			tail = tail[len(tail)-knowledgeExtractorStderrCap:]
		}
		if runErr != nil {
			return nil, fmt.Errorf("knowledge_extract: extractor failed: %v: %s", runErr, tail)
		}
		return nil, fmt.Errorf("knowledge_extract: extractor returned invalid JSON: %w: %s", err, tail)
	}
	if !out.OK {
		msg := strings.TrimSpace(out.Message)
		if msg == "" {
			msg = "Không trích xuất được file (" + out.Error + ")."
		}
		return nil, errors.New(msg)
	}
	return &out, nil
}
