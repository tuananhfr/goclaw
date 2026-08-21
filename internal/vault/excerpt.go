package vault

import (
	"bytes"
	"os"
	"unicode/utf8"
)

// ExcerptMaxBytes caps how much of a document's head is copied into the
// content_excerpt column for FTS indexing (see migration 000065).
const ExcerptMaxBytes = 16 * 1024

// ExcerptFromFile reads the head of a file for FTS indexing.
// Returns "" on any error or non-text content.
func ExcerptFromFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, ExcerptMaxBytes)
	n, _ := f.Read(buf)
	return ExcerptFromBytes(buf[:n])
}

// ExcerptFromBytes caps b at ExcerptMaxBytes and trims a clipped trailing
// rune. NUL bytes mean binary (and Postgres TEXT rejects them) → "".
func ExcerptFromBytes(b []byte) string {
	if len(b) == 0 || bytes.IndexByte(b, 0) >= 0 {
		return ""
	}
	if len(b) > ExcerptMaxBytes {
		b = b[:ExcerptMaxBytes]
	}
	// A utf8 rune is at most 4 bytes — more than 4 backtracks means not text.
	for i := 0; i < 4 && len(b) > 0; i++ {
		if utf8.Valid(b) {
			return string(b)
		}
		b = b[:len(b)-1]
	}
	return ""
}
