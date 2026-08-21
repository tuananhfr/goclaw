package vault

import (
	"strings"
	"testing"
)

func TestExcerptFromBytesPassesThroughText(t *testing.T) {
	if got := ExcerptFromBytes([]byte("bảng giá 25.000đ")); got != "bảng giá 25.000đ" {
		t.Fatalf("text must pass through, got %q", got)
	}
}

func TestExcerptFromBytesCapsAtMax(t *testing.T) {
	long := strings.Repeat("a", ExcerptMaxBytes+500)
	if got := ExcerptFromBytes([]byte(long)); len(got) != ExcerptMaxBytes {
		t.Fatalf("must cap at %d, got %d", ExcerptMaxBytes, len(got))
	}
}

func TestExcerptFromBytesTrimsClippedRune(t *testing.T) {
	// "đ" là 2 byte — cắt giữa chừng phải lùi về biên rune hợp lệ.
	b := []byte("giá đ")
	clipped := b[:len(b)-1]
	got := ExcerptFromBytes(clipped)
	if got != "giá " {
		t.Fatalf("clipped rune must be trimmed, got %q", got)
	}
}

func TestExcerptFromBytesRejectsBinary(t *testing.T) {
	if got := ExcerptFromBytes([]byte{0x00, 0x01, 'a'}); got != "" {
		t.Fatalf("NUL bytes mean binary, want empty, got %q", got)
	}
}

func TestExcerptFromBytesEmpty(t *testing.T) {
	if got := ExcerptFromBytes(nil); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}
