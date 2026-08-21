package http

import "testing"

func TestSafeUploadFolderAcceptsOneSegment(t *testing.T) {
	cases := map[string]string{
		"":                     "",
		"   ":                  "",
		"tekshot-file-ho-so":   "tekshot-file-ho-so",
		"tekshot-web-lpc-vn":   "tekshot-web-lpc-vn",
		" tekshot-file-ho-so ": "tekshot-file-ho-so",
	}
	for in, want := range cases {
		got, ok := safeUploadFolder(in)
		if !ok || got != want {
			t.Errorf("safeUploadFolder(%q) = %q, %v; want %q, true", in, got, ok, want)
		}
	}
}

func TestSafeUploadFolderRejectsTraversalAndNesting(t *testing.T) {
	for _, in := range []string{
		"..",
		".",
		".hidden",
		"../secret",
		"a/b",
		"a\\b",
		"/abs",
		"nul\x00",
	} {
		if got, ok := safeUploadFolder(in); ok {
			t.Errorf("safeUploadFolder(%q) = %q, true; want rejected", in, got)
		}
	}
}
