package tekshot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const (
	// knowledgeFileMaxBytes is one side of a three-way contract: it mirrors
	// StudioKnowledgeSourceService::MAX_BYTES (Drupal) and the "100MB" label in
	// the React KnowledgePanel. Raise all three together or the panel lies.
	knowledgeFileMaxBytes int64 = 100 << 20
	// knowledgeSourceDownloadTimeout bounds the whole transfer; 100MB over a
	// slow link needs minutes, and persistMedia's 30s is exactly the trap we
	// are stepping around.
	knowledgeSourceDownloadTimeout = 15 * time.Minute
)

// errKnowledgeSourceTooLarge reaches the panel verbatim through the job's
// error_message, so it is a Vietnamese sentence, not a Go-style error.
var errKnowledgeSourceTooLarge = fmt.Errorf("file vượt %dMB — hãy cắt nhỏ rồi tải lại", knowledgeFileMaxBytes>>20)

var unsafeSourceNameChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// knowledgeSourceDownloader streams a source file to disk. It deliberately
// does NOT go through persistMedia: that path caps at 10MB inside an
// io.LimitReader and truncates silently, and every channel shares it.
type knowledgeSourceDownloader struct {
	client   *http.Client
	maxBytes int64
}

// download fetches rawURL into dir and returns the local path and byte size.
// Oversize is an error twice over — on Content-Length before reading and on
// the streamed count after — never a silently truncated file.
func (d *knowledgeSourceDownloader) download(ctx context.Context, rawURL, dir, filename string) (string, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", 0, fmt.Errorf("knowledge_extract: invalid source url: %w", err)
	}
	req.Header.Set("User-Agent", tools.FetchUserAgent)
	resp, err := d.client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("knowledge_extract: source download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", 0, fmt.Errorf("knowledge_extract: source returned HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > d.maxBytes {
		return "", 0, errKnowledgeSourceTooLarge
	}

	dst := filepath.Join(dir, safeSourceFilename(filename))
	out, err := os.Create(dst)
	if err != nil {
		return "", 0, fmt.Errorf("knowledge_extract: create source file: %w", err)
	}
	n, copyErr := io.Copy(out, io.LimitReader(resp.Body, d.maxBytes+1))
	closeErr := out.Close()
	switch {
	case copyErr != nil:
		os.Remove(dst)
		return "", 0, fmt.Errorf("knowledge_extract: source download interrupted: %w", copyErr)
	case closeErr != nil:
		os.Remove(dst)
		return "", 0, fmt.Errorf("knowledge_extract: write source file: %w", closeErr)
	case n > d.maxBytes:
		os.Remove(dst)
		return "", 0, errKnowledgeSourceTooLarge
	case n == 0:
		os.Remove(dst)
		return "", 0, errors.New("knowledge_extract: source is empty")
	}
	return dst, n, nil
}

// safeSourceFilename keeps the extension (the extractor falls back to it when
// the MIME is blank) and nothing that could escape dir.
func safeSourceFilename(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	base = unsafeSourceNameChars.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-.")
	if base == "" || base == "." || base == ".." {
		return "source"
	}
	return base
}
