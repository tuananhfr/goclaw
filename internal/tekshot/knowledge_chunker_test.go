package tekshot

import (
	"strconv"
	"strings"
	"testing"
)

func textUnit(ref string, size int) knowledgeUnit {
	return knowledgeUnit{Kind: "text", Ref: ref, Text: strings.Repeat("a", size)}
}

func imageUnit(ref string) knowledgeUnit {
	return knowledgeUnit{Kind: "image", Ref: ref, ImagePath: "/tmp/" + strings.ReplaceAll(ref, " ", "-") + ".png"}
}

func TestChunkAccumulatesSmallUnitsAndJoinsRefs(t *testing.T) {
	got := chunkKnowledgeUnits([]knowledgeUnit{textUnit("Trang 1", 600), textUnit("Trang 2", 600), textUnit("Trang 3", 600)})
	if len(got.Chunks) != 1 {
		t.Fatalf("want 1 chunk, got %d", len(got.Chunks))
	}
	c := got.Chunks[0]
	if c.Ref != "Trang 1–3" || c.Index != 0 || c.Kind != "text" || len(c.Text) != 600*3+4 {
		t.Fatalf("chunk = %+v", c)
	}
}

func TestChunkFlushesWhenNextUnitWouldOverflow(t *testing.T) {
	got := chunkKnowledgeUnits([]knowledgeUnit{textUnit("Trang 1", 3000), textUnit("Trang 2", 3000)})
	if len(got.Chunks) != 2 || got.Chunks[1].Ref != "Trang 2" || got.Chunks[1].Index != 1 {
		t.Fatalf("chunks = %+v", got.Chunks)
	}
}

func TestChunkHeadingStartsNewChunkOnlyPastMin(t *testing.T) {
	small := chunkKnowledgeUnits([]knowledgeUnit{textUnit("Phần 1", 600), {Kind: "text", Ref: "Phần 2", Heading: "Giá", Text: strings.Repeat("b", 600)}})
	if len(small.Chunks) != 1 {
		t.Fatalf("a heading must not break a chunk below min, got %d chunks", len(small.Chunks))
	}
	big := chunkKnowledgeUnits([]knowledgeUnit{textUnit("Phần 1", 1600), {Kind: "text", Ref: "Phần 2", Heading: "Giá", Text: strings.Repeat("b", 600)}})
	if len(big.Chunks) != 2 || big.Chunks[1].Heading != "Giá" {
		t.Fatalf("a heading past min must open a new chunk, got %+v", big.Chunks)
	}
}

func TestChunkSplitsOversizedUnitAtParagraphs(t *testing.T) {
	paras := make([]string, 5)
	for i := range paras {
		paras[i] = strings.Repeat("c", 2000)
	}
	got := chunkKnowledgeUnits([]knowledgeUnit{{Kind: "text", Ref: "Trang 7", Text: strings.Join(paras, "\n\n")}})
	if len(got.Chunks) < 2 {
		t.Fatalf("a 10k unit must split, got %d chunks", len(got.Chunks))
	}
	for _, c := range got.Chunks {
		if len(c.Text) > knowledgeChunkMaxChars {
			t.Fatalf("chunk %d is %d bytes, over max", c.Index, len(c.Text))
		}
		if c.Ref != "Trang 7" {
			t.Fatalf("pieces of one page keep its ref, got %q", c.Ref)
		}
	}
}

func TestChunkRepeatsTableHeaderAcrossPieces(t *testing.T) {
	rows := []string{"| Mã | Giá |", "|---|---|"}
	for i := 0; i < 700; i++ {
		rows = append(rows, "| A1 | 25.000 |")
	}
	got := chunkKnowledgeUnits([]knowledgeUnit{{Kind: "text", Ref: "Sheet Giá", Text: strings.Join(rows, "\n")}})
	if len(got.Chunks) < 2 {
		t.Fatalf("a 10k table must split, got %d", len(got.Chunks))
	}
	for _, c := range got.Chunks {
		if !strings.HasPrefix(c.Text, "| Mã | Giá |\n|---|---|") {
			t.Fatalf("piece %d lost the table header: %q", c.Index, c.Text[:40])
		}
	}
}

func TestChunkGroupsScanPagesBySix(t *testing.T) {
	var units []knowledgeUnit
	for i := 1; i <= 7; i++ {
		units = append(units, imageUnit("Trang "+strconv.Itoa(i)))
	}
	got := chunkKnowledgeUnits(units)
	if len(got.Chunks) != 2 || len(got.Chunks[0].ImagePaths) != 6 || got.Chunks[0].Ref != "Trang 1–6" || got.Chunks[1].Ref != "Trang 7" {
		t.Fatalf("chunks = %+v", got.Chunks)
	}
	if got.Chunks[0].Kind != "image" {
		t.Fatalf("kind = %q", got.Chunks[0].Kind)
	}
}

func TestChunkTextBetweenImagesBreaksTheGroup(t *testing.T) {
	got := chunkKnowledgeUnits([]knowledgeUnit{imageUnit("Trang 1"), imageUnit("Trang 2"), textUnit("Trang 3", 600), imageUnit("Trang 4")})
	if len(got.Chunks) != 3 || got.Chunks[0].Kind != "image" || got.Chunks[1].Kind != "text" || got.Chunks[2].Kind != "image" {
		t.Fatalf("chunks = %+v", got.Chunks)
	}
}

func TestChunkCapsAtMaxChunks(t *testing.T) {
	var units []knowledgeUnit
	for i := 1; i <= 450; i++ {
		units = append(units, textUnit("Trang "+strconv.Itoa(i), 3000))
	}
	got := chunkKnowledgeUnits(units)
	if len(got.Chunks) != knowledgeFileMaxChunks || !got.Truncated || got.Dropped != 50 || got.DroppedFrom != "Trang 401" {
		t.Fatalf("len=%d truncated=%v dropped=%d from=%q", len(got.Chunks), got.Truncated, got.Dropped, got.DroppedFrom)
	}
}

func TestJoinRefs(t *testing.T) {
	cases := []struct{ a, b, want string }{
		{"Trang 12", "Trang 15", "Trang 12–15"},
		{"Trang 12", "Trang 12", "Trang 12"},
		{"Sheet Giá", "Sheet Liên hệ", "Sheet Giá – Sheet Liên hệ"},
		{"Slide 3", "", "Slide 3"},
	}
	for _, c := range cases {
		if got := joinRefs(c.a, c.b); got != c.want {
			t.Errorf("joinRefs(%q,%q) = %q, want %q", c.a, c.b, got, c.want)
		}
	}
}
