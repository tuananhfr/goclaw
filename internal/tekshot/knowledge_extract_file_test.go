package tekshot

import (
	"strings"
	"testing"
)

func TestBuildKnowledgeFileReportShape(t *testing.T) {
	extraction := &knowledgeExtraction{Kind: "pdf", Stats: knowledgeExtractorStats{Pages: 40, TextPages: 38, ScanPages: 2, ScanPagesRendered: 2}, Truncated: true, TruncatedReason: "PDF có 320 trang scan, chỉ đọc 300 trang đầu."}
	chunking := knowledgeChunking{Chunks: make([]knowledgeChunk, 3), Truncated: true, Dropped: 2, DroppedFrom: "Trang 39"}
	outcome := knowledgeLabelOutcome{
		Pages:      []map[string]any{{"url": "https://h/f.pdf#c001-trang-1-3", "title": "A", "summary": "", "keywords": []string{}, "markdown": "x"}},
		Labeled:    1,
		Failed:     0,
		UnreadRefs: []string{"Trang 40"},
		VisionUsed: true,
	}
	r := buildKnowledgeFileReport("Bang_gia-2026.pdf", extraction, chunking, outcome)
	if r["status"] != "ok" || r["title"] != "Bang gia 2026" || r["source_kind"] != "file" || r["source_pages"] != "1-40" {
		t.Fatalf("report head = %v", r)
	}
	if urls := r["pages_fetched"].([]string); len(urls) != 1 || urls[0] != "https://h/f.pdf#c001-trang-1-3" {
		t.Fatalf("pages_fetched = %v", r["pages_fetched"])
	}
	if r["truncated"] != true || !strings.Contains(r["truncated_reason"].(string), "300 trang đầu") || !strings.Contains(r["truncated_reason"].(string), "Trang 39") {
		t.Fatalf("truncation = %v / %v", r["truncated"], r["truncated_reason"])
	}
	if r["total_pages_discovered"] != 5 {
		t.Fatalf("total_pages_discovered = %v", r["total_pages_discovered"])
	}
	health := r["tool_health"].(map[string]any)
	if health["exec"] != "ok" || health["vision"] != "ok" {
		t.Fatalf("tool_health = %v", health)
	}
	if unread := r["unread_refs"].([]string); len(unread) != 1 || unread[0] != "Trang 40" {
		t.Fatalf("unread_refs = %v", r["unread_refs"])
	}
}

func TestKnowledgeFileEmptyReport(t *testing.T) {
	r := knowledgeFileEmptyReport("trang-trang.pdf", "File không có nội dung chữ nào để trích.")
	if r["status"] != "empty" || r["reason"] == "" || r["title"] != "trang trang" {
		t.Fatalf("report = %v", r)
	}
}

func TestKnowledgeFileStem(t *testing.T) {
	cases := map[string]string{"bao-cao_Q4.xlsx": "bao cao Q4", ".pdf": "Tài liệu", "": "Tài liệu", "Hồ sơ.docx": "Hồ sơ"}
	for in, want := range cases {
		if got := knowledgeFileStem(in); got != want {
			t.Errorf("knowledgeFileStem(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestKnowledgeLabelToolAllowKeepsReadImageForScans(t *testing.T) {
	// nil would mean "no restriction" to the loop's tool filter, so a text
	// chunk still has to carry a harmless tool rather than an empty list.
	if got := knowledgeLabelToolAllow(true); len(got) != 1 || got[0] != "read_image" {
		t.Fatalf("scanned allow = %v", got)
	}
	if got := knowledgeLabelToolAllow(false); len(got) == 0 {
		t.Fatal("text allow must not be empty")
	}
}

func TestKnowledgeDocHeadTakesFirstTextChunk(t *testing.T) {
	head := knowledgeDocHead([]knowledgeChunk{
		{Kind: "image", Ref: "Trang 1"},
		{Kind: "text", Ref: "Trang 2", Text: strings.Repeat("ữ", knowledgeLabelDocHeadChars+50)},
	})
	if len([]rune(head)) != knowledgeLabelDocHeadChars {
		t.Fatalf("doc head runes = %d, want %d", len([]rune(head)), knowledgeLabelDocHeadChars)
	}
	if knowledgeDocHead([]knowledgeChunk{{Kind: "image", Ref: "Trang 1"}}) != "" {
		t.Fatal("a scan-only document has no text head")
	}
}
