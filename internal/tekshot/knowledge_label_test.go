package tekshot

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
)

func TestValidateKnowledgePageLabel(t *testing.T) {
	if _, err := validateKnowledgePageLabel(map[string]any{"status": "ok", "title": "", "summary": "x"}, false); err == nil {
		t.Fatal("ok without title must fail")
	}
	if _, err := validateKnowledgePageLabel(map[string]any{"status": "ok", "title": "A"}, true); err == nil {
		t.Fatal("scanned page without markdown must fail")
	}
	if _, err := validateKnowledgePageLabel(map[string]any{"status": "empty"}, false); err == nil {
		t.Fatal("empty without reason must fail")
	}
	got, err := validateKnowledgePageLabel(map[string]any{"status": "ok", "title": " Cát Tường – Bảng giá ", "keywords": []any{"giá", "", "NOXH"}, "markdown": "ignored"}, false)
	if err != nil || got["title"] != "Cát Tường – Bảng giá" || len(got["keywords"].([]string)) != 2 {
		t.Fatalf("got %v err %v", got, err)
	}
}

func TestKnowledgeLabelPromptTextChunkForbidsRewriting(t *testing.T) {
	p := buildKnowledgeLabelPrompt(knowledgeLabelContext{Filename: "bang-gia.pdf", Kind: "pdf", ChunkCount: 3, DocHead: "Đầu tài liệu"},
		knowledgeChunk{Index: 1, Kind: "text", Ref: "Trang 2", Text: "| Món | Giá |"})
	for _, want := range []string{"2/3", "bang-gia.pdf", "Trang 2", "| Món | Giá |", "leave markdown empty", "Đầu tài liệu", knowledgeLabelToolName} {
		if !strings.Contains(p, want) {
			t.Fatalf("text prompt missing %q", want)
		}
	}
}

func TestKnowledgeLabelPromptImageChunkAsksForTranscription(t *testing.T) {
	p := buildKnowledgeLabelPrompt(knowledgeLabelContext{Filename: "ho-so.pdf", Kind: "pdf", ChunkCount: 1},
		knowledgeChunk{Index: 0, Kind: "image", Ref: "Trang 1–3", ImagePaths: []string{"a", "b", "c"}})
	for _, want := range []string{"3 scanned page", "Trang 1–3", "VERBATIM", "status 'empty'"} {
		if !strings.Contains(p, want) {
			t.Fatalf("image prompt missing %q", want)
		}
	}
}

func TestKnowledgeChunkURLIsUniquePerChunkAndStable(t *testing.T) {
	a := knowledgeChunkURL("https://h/f.pdf", knowledgeChunk{Index: 0, Ref: "Trang 12"})
	b := knowledgeChunkURL("https://h/f.pdf", knowledgeChunk{Index: 1, Ref: "Trang 12"})
	if a == b || !strings.HasPrefix(a, "https://h/f.pdf#c001-trang-12") || !strings.HasPrefix(b, "https://h/f.pdf#c002-") {
		t.Fatalf("a=%s b=%s", a, b)
	}
}

func fakeRunner(calls *int, fail map[int]int, reports map[int]map[string]any) knowledgeLabelRunner {
	return func(_ context.Context, chunk knowledgeChunk, _ string) (map[string]any, error) {
		*calls++
		if n := fail[chunk.Index]; n > 0 {
			fail[chunk.Index] = n - 1
			return nil, errors.New("provider down")
		}
		if r, ok := reports[chunk.Index]; ok {
			return r, nil
		}
		return map[string]any{"status": "ok", "title": "T" + chunk.Ref, "summary": "s", "keywords": []string{"k"}, "markdown": "MODEL TEXT"}, nil
	}
}

func TestLabelKnowledgeChunksKeepsExtractedMarkdownForText(t *testing.T) {
	chunks := []knowledgeChunk{{Index: 0, Kind: "text", Ref: "Trang 1–3", Text: "EXTRACTED"}}
	calls := 0
	out, err := labelKnowledgeChunks(context.Background(), chunks, knowledgeLabelContext{Filename: "f.pdf"}, "https://h/f.pdf", fakeRunner(&calls, nil, nil), nil)
	if err != nil || len(out.Pages) != 1 || out.Labeled != 1 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	p := out.Pages[0]
	if p["markdown"] != "EXTRACTED" || p["title"] != "TTrang 1–3" || p["url"] != "https://h/f.pdf#c001-trang-1-3" {
		t.Fatalf("page = %v", p)
	}
}

func TestLabelKnowledgeChunksRetriesOnceThenFallsBack(t *testing.T) {
	chunks := []knowledgeChunk{{Index: 0, Kind: "text", Ref: "Trang 1", Text: "X"}}
	calls := 0
	out, err := labelKnowledgeChunks(context.Background(), chunks, knowledgeLabelContext{Filename: "f.pdf"}, "https://h/f.pdf", fakeRunner(&calls, map[int]int{0: 2}, nil), nil)
	if err != nil || calls != 2 || out.Failed != 1 || len(out.Pages) != 1 {
		t.Fatalf("calls=%d out=%+v err=%v", calls, out, err)
	}
	if out.Pages[0]["title"] != "f.pdf – Trang 1" || out.Pages[0]["markdown"] != "X" {
		t.Fatalf("fallback page = %v", out.Pages[0])
	}
}

func TestLabelKnowledgeChunksImageUsesModelMarkdownAndRecordsUnread(t *testing.T) {
	chunks := []knowledgeChunk{
		{Index: 0, Kind: "image", Ref: "Trang 1–2", ImagePaths: []string{"a", "b"}},
		{Index: 1, Kind: "image", Ref: "Trang 3", ImagePaths: []string{"c"}},
		{Index: 2, Kind: "image", Ref: "Trang 4", ImagePaths: []string{"d"}},
	}
	calls := 0
	reports := map[int]map[string]any{2: {"status": "empty", "reason": "trang trắng"}}
	out, err := labelKnowledgeChunks(context.Background(), chunks, knowledgeLabelContext{Filename: "f.pdf"}, "https://h/f.pdf", fakeRunner(&calls, map[int]int{1: 2}, reports), nil)
	if err != nil || !out.VisionUsed || len(out.Pages) != 1 || out.Pages[0]["markdown"] != "MODEL TEXT" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	if len(out.UnreadRefs) != 2 || out.UnreadRefs[0] != "Trang 3" || out.UnreadRefs[1] != "Trang 4" {
		t.Fatalf("unread = %v", out.UnreadRefs)
	}
}

func TestLabelKnowledgeChunksReportsProgressEveryFiveAndAtEnd(t *testing.T) {
	var chunks []knowledgeChunk
	for i := 0; i < 7; i++ {
		chunks = append(chunks, knowledgeChunk{Index: i, Kind: "text", Ref: "Trang " + strconv.Itoa(i+1), Text: "x"})
	}
	calls := 0
	var seen [][2]int
	_, err := labelKnowledgeChunks(context.Background(), chunks, knowledgeLabelContext{Filename: "f.pdf"}, "https://h/f.pdf", fakeRunner(&calls, nil, nil), func(done, total int) {
		seen = append(seen, [2]int{done, total})
	})
	if err != nil || len(seen) != 2 || seen[0] != [2]int{5, 7} || seen[1] != [2]int{7, 7} {
		t.Fatalf("progress = %v err=%v", seen, err)
	}
}

func TestLabelKnowledgeChunksStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	_, err := labelKnowledgeChunks(ctx, []knowledgeChunk{{Index: 0, Kind: "text", Ref: "Trang 1", Text: "x"}}, knowledgeLabelContext{}, "https://h/f", fakeRunner(&calls, nil, nil), nil)
	if err == nil || calls != 0 {
		t.Fatalf("cancelled ctx must stop before any run, calls=%d err=%v", calls, err)
	}
}
