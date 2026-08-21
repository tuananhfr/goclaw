package tekshot

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func downloadTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *knowledgeSourceDownloader) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	// Tests inject the plain server client: the production safe client refuses loopback by design.
	return srv, &knowledgeSourceDownloader{client: srv.Client(), maxBytes: 100}
}

func TestKnowledgeSourceDownloadWritesFileWithSafeName(t *testing.T) {
	srv, d := downloadTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	})
	dir := t.TempDir()
	path, n, err := d.download(context.Background(), srv.URL+"/x", dir, "bang gia (1).pdf")
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if n != 5 {
		t.Fatalf("size = %d, want 5", n)
	}
	if filepath.Base(path) != "bang-gia-1-.pdf" {
		t.Fatalf("unsafe chars kept: %s", path)
	}
	if body, _ := os.ReadFile(path); string(body) != "hello" {
		t.Fatalf("body = %q", body)
	}
}

func TestKnowledgeSourceDownloadRejectsOversizeByContentLength(t *testing.T) {
	srv, d := downloadTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 1000))) // net/http sets Content-Length for a small finished body
	})
	dir := t.TempDir()
	if _, _, err := d.download(context.Background(), srv.URL, dir, "a.pdf"); !errors.Is(err, errKnowledgeSourceTooLarge) {
		t.Fatalf("want errKnowledgeSourceTooLarge, got %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("oversize download must leave no file, found %d", len(entries))
	}
}

func TestKnowledgeSourceDownloadRejectsOversizeStreamedBody(t *testing.T) {
	srv, d := downloadTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush() // forces chunked encoding: no Content-Length, the streamed check must catch it
		_, _ = w.Write([]byte(strings.Repeat("x", 101)))
	})
	dir := t.TempDir()
	if _, _, err := d.download(context.Background(), srv.URL, dir, "a.pdf"); !errors.Is(err, errKnowledgeSourceTooLarge) {
		t.Fatalf("want errKnowledgeSourceTooLarge, got %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("oversize download must leave no file, found %d", len(entries))
	}
}

func TestKnowledgeSourceDownloadRejectsHTTPError(t *testing.T) {
	srv, d := downloadTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	if _, _, err := d.download(context.Background(), srv.URL, t.TempDir(), "a.pdf"); err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("want HTTP 404 error, got %v", err)
	}
}

func TestSafeSourceFilename(t *testing.T) {
	cases := map[string]string{
		"bao-cao.pdf":      "bao-cao.pdf",
		"../../etc/passwd": "passwd",
		"":                 "source",
		"..":               "source",
		"Báo cáo Q4.xlsx":  "B-o-c-o-Q4.xlsx",
	}
	for in, want := range cases {
		if got := safeSourceFilename(in); got != want {
			t.Errorf("safeSourceFilename(%q) = %q, want %q", in, got, want)
		}
	}
}
