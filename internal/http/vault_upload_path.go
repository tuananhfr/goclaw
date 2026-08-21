package http

import "strings"

// safeUploadFolder validates the optional "folder" form field of a vault
// upload: exactly one path segment, which becomes a subfolder of the agent or
// team directory. It cannot come in through the filename — Go's
// mime/multipart runs filepath.Base over that before a handler sees it — so a
// caller that wants to group an import sends this field instead.
//
// Returns ("", true) for an absent folder, ("", false) for a hostile one.
func safeUploadFolder(folder string) (string, bool) {
	s := strings.TrimSpace(strings.ReplaceAll(folder, "\\", "/"))
	if s == "" {
		return "", true
	}
	if strings.ContainsRune(s, 0) || strings.Contains(s, "/") {
		return "", false
	}
	if s == "." || s == ".." || strings.HasPrefix(s, ".") {
		return "", false
	}
	return s, true
}
