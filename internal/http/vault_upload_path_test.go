package http

import "testing"

func TestSafeUploadRelPathKeepsOneSubfolder(t *testing.T) {
	cases := map[string]string{
		"note.md":                       "note.md",
		"tekshot-file-ho-so/index.md":   "tekshot-file-ho-so/index.md",
		"tekshot-web-lpc-vn/lien-he.md": "tekshot-web-lpc-vn/lien-he.md",
		"./tekshot-file-ho-so/c001.md":  "tekshot-file-ho-so/c001.md",
		"tekshot-file-ho-so//c001.md":   "tekshot-file-ho-so/c001.md",
		"tekshot-file-ho-so\\c001.md":   "tekshot-file-ho-so/c001.md",
	}
	for in, want := range cases {
		got, ok := safeUploadRelPath(in)
		if !ok || got != want {
			t.Errorf("safeUploadRelPath(%q) = %q, %v; want %q, true", in, got, ok, want)
		}
	}
}

// Anything deeper than one subfolder, or absolute, collapses to the bare
// filename — the behaviour that filepath.Base gave before subfolders existed.
func TestSafeUploadRelPathCollapsesDeepAndAbsolutePaths(t *testing.T) {
	cases := map[string]string{
		"a/b/c.md":                      "c.md",
		"/etc/passwd":                   "passwd",
		"C:\\Users\\admin\\bang gia.md": "bang gia.md",
	}
	for in, want := range cases {
		got, ok := safeUploadRelPath(in)
		if !ok || got != want {
			t.Errorf("safeUploadRelPath(%q) = %q, %v; want %q, true", in, got, ok, want)
		}
	}
}

func TestSafeUploadRelPathRejectsTraversalAndJunk(t *testing.T) {
	for _, in := range []string{
		"",
		".",
		"..",
		"../secret.md",
		"folder/../../secret.md",
		"..\\secret.md",
		"nul\x00.md",
		"   ",
	} {
		if got, ok := safeUploadRelPath(in); ok {
			t.Errorf("safeUploadRelPath(%q) = %q, true; want rejected", in, got)
		}
	}
}
