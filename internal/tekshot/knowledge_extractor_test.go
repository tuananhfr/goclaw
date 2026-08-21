package tekshot

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// stubExtractor returns a Command that prints stdout, writes to stderr and exits with code.
func stubExtractor(t *testing.T, stdout string, code int) []string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "out.json")
	if err := os.WriteFile(path, []byte(stdout), 0o600); err != nil {
		t.Fatal(err)
	}
	return []string{"sh", "-c", "cat " + path + "; echo boom >&2; exit " + strconv.Itoa(code)}
}

func TestKnowledgeExtractorArgsContract(t *testing.T) {
	got := knowledgeExtractorArgs(knowledgeExtractorOptions{Input: "/tmp/a.pdf", Mime: "application/pdf", OutDir: "/tmp/out", MaxScanPages: 300, DPI: 150})
	want := []string{"--input", "/tmp/a.pdf", "--mime", "application/pdf", "--out-dir", "/tmp/out", "--max-scan-pages", "300", "--dpi", "150"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
}

func TestRunKnowledgeExtractorParsesUnits(t *testing.T) {
	stdout := `{"ok":true,"kind":"pdf","units":[{"index":0,"kind":"text","ref":"Trang 1","text":"Bảng giá"},{"index":1,"kind":"image","ref":"Trang 2","image_path":"/tmp/page-0002.png"}],"stats":{"pages":2,"text_pages":1,"scan_pages":1,"scan_pages_rendered":1},"truncated":false,"truncated_reason":""}`
	out, err := runKnowledgeExtractor(context.Background(), knowledgeExtractorOptions{OutDir: t.TempDir(), Command: stubExtractor(t, stdout, 0)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Kind != "pdf" || len(out.Units) != 2 || out.Units[1].ImagePath != "/tmp/page-0002.png" || out.Stats.ScanPages != 1 {
		t.Fatalf("parsed wrong: %+v", out)
	}
}

func TestRunKnowledgeExtractorTurnsOKFalseIntoVietnameseError(t *testing.T) {
	stdout := `{"ok":false,"error":"encrypted_pdf","message":"PDF có mật khẩu — hãy gỡ mật khẩu rồi tải lại."}`
	_, err := runKnowledgeExtractor(context.Background(), knowledgeExtractorOptions{OutDir: t.TempDir(), Command: stubExtractor(t, stdout, 0)})
	if err == nil || err.Error() != "PDF có mật khẩu — hãy gỡ mật khẩu rồi tải lại." {
		t.Fatalf("want the script's message verbatim, got %v", err)
	}
}

func TestRunKnowledgeExtractorSurfacesStderrOnCrash(t *testing.T) {
	_, err := runKnowledgeExtractor(context.Background(), knowledgeExtractorOptions{OutDir: t.TempDir(), Command: stubExtractor(t, "not json", 3)})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("crash must carry stderr tail, got %v", err)
	}
}

func TestKnowledgeExtractScriptIsEmbedded(t *testing.T) {
	if !strings.Contains(knowledgeExtractScript, "--max-scan-pages") || !strings.Contains(knowledgeExtractScript, "pypdfium2") {
		t.Fatal("embedded extractor script lost its CLI contract or its renderer")
	}
}
