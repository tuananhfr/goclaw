package http

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractWorkspaceZipBrandKit(t *testing.T) {
	scopeDir := t.TempDir()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	writeZipEntry(t, zw, "brand-kits/pizza-hips/BRAND.md", "font: SVN-Bango")
	writeZipEntry(t, zw, "brand-kits/pizza-hips/assets/fonts/SVN-Bango.otf", "fake-font")
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	extracted, totalBytes, err := extractWorkspaceZip(bytes.NewReader(buf.Bytes()), scopeDir)
	if err != nil {
		t.Fatalf("extractWorkspaceZip returned error: %v", err)
	}
	if totalBytes == 0 {
		t.Fatalf("expected extracted byte count")
	}
	if len(extracted) != 2 {
		t.Fatalf("expected 2 extracted files, got %d: %#v", len(extracted), extracted)
	}

	brandPath := filepath.Join(scopeDir, "brand-kits", "pizza-hips", "BRAND.md")
	data, err := os.ReadFile(brandPath)
	if err != nil {
		t.Fatalf("read extracted BRAND.md: %v", err)
	}
	if string(data) != "font: SVN-Bango" {
		t.Fatalf("unexpected BRAND.md content: %q", data)
	}
	fontPath := filepath.Join(scopeDir, "brand-kits", "pizza-hips", "assets", "fonts", "SVN-Bango.otf")
	if _, err := os.Stat(fontPath); err != nil {
		t.Fatalf("expected extracted font path: %v", err)
	}
}

func TestExtractWorkspaceZipNormalizesWindowsPaths(t *testing.T) {
	scopeDir := t.TempDir()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	writeZipEntry(t, zw, `brand-kits\pizza-hips\BRAND.md`, "font: SVN-Bango")
	writeZipEntry(t, zw, `brand-kits\pizza-hips\assets\fonts\SVN-Bango.otf`, "fake-font")
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	extracted, _, err := extractWorkspaceZip(bytes.NewReader(buf.Bytes()), scopeDir)
	if err != nil {
		t.Fatalf("extractWorkspaceZip returned error: %v", err)
	}
	if len(extracted) != 2 {
		t.Fatalf("expected 2 extracted files, got %d: %#v", len(extracted), extracted)
	}
	if _, err := os.Stat(filepath.Join(scopeDir, "brand-kits", "pizza-hips", "assets", "fonts", "SVN-Bango.otf")); err != nil {
		t.Fatalf("expected normalized font path: %v", err)
	}
}

func TestExtractWorkspaceZipRejectsUnsafePath(t *testing.T) {
	scopeDir := t.TempDir()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	writeZipEntry(t, zw, "../evil.txt", "bad")
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	_, _, err := extractWorkspaceZip(bytes.NewReader(buf.Bytes()), scopeDir)
	if err == nil || !strings.Contains(err.Error(), "invalid path") {
		t.Fatalf("expected invalid path error, got %v", err)
	}
}

func TestExtractWorkspaceZipRejectsBlockedExtension(t *testing.T) {
	scopeDir := t.TempDir()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	writeZipEntry(t, zw, "brand-kits/pizza-hips/install.ps1", "bad")
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	_, _, err := extractWorkspaceZip(bytes.NewReader(buf.Bytes()), scopeDir)
	if err == nil || !strings.Contains(err.Error(), "blocked file type") {
		t.Fatalf("expected blocked extension error, got %v", err)
	}
}

func writeZipEntry(t *testing.T, zw *zip.Writer, name, body string) {
	t.Helper()
	w, err := zw.Create(name)
	if err != nil {
		t.Fatalf("create zip entry %s: %v", name, err)
	}
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatalf("write zip entry %s: %v", name, err)
	}
}
