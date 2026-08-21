package http

import "strings"

// maxUploadPathSegments caps how deep an uploaded name may nest inside the
// agent/team folder: one subfolder plus the file. Tekshot groups each knowledge
// import into its own folder; nothing needs more, and every extra level is
// more surface to get path handling wrong.
const maxUploadPathSegments = 2

// safeUploadRelPath turns a client-supplied filename into a relative path that
// is safe to join onto the upload target directory. It keeps a single
// subfolder, collapses anything deeper or absolute to the bare filename (what
// filepath.Base used to do), and refuses traversal outright.
func safeUploadRelPath(name string) (string, bool) {
	s := strings.ReplaceAll(name, "\\", "/")
	if strings.ContainsRune(s, 0) {
		return "", false
	}
	absolute := strings.HasPrefix(s, "/") || hasDriveLetter(s)

	segments := make([]string, 0, maxUploadPathSegments)
	for _, seg := range strings.Split(s, "/") {
		seg = strings.TrimSpace(seg)
		switch seg {
		case "", ".":
			continue
		case "..":
			// Traversal is never a typo worth salvaging.
			return "", false
		}
		segments = append(segments, seg)
	}
	if len(segments) == 0 {
		return "", false
	}
	if absolute || len(segments) > maxUploadPathSegments {
		segments = segments[len(segments)-1:]
	}
	return strings.Join(segments, "/"), true
}

func hasDriveLetter(s string) bool {
	if len(s) < 2 || s[1] != ':' {
		return false
	}
	c := s[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
