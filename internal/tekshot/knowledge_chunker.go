package tekshot

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Budgets are bytes, not runes: Vietnamese UTF-8 runs ~1.3 bytes per character,
// so chunks land a little short of the spec's "ký tự" — still inside the
// 1–6k band the retrieval QA measured.
const (
	knowledgeChunkMinChars     = 1500
	knowledgeChunkMaxChars     = 6000
	knowledgeScanPagesPerChunk = 6
	knowledgeFileMaxChunks     = 400
)

type knowledgeChunk struct {
	Index      int
	Kind       string // "text" | "image"
	Ref        string // "Trang 12–15", "Sheet Giá", "Slide 3"
	Heading    string
	Text       string
	ImagePaths []string
}

type knowledgeChunking struct {
	Chunks      []knowledgeChunk
	Truncated   bool
	Dropped     int    // chunks cut past knowledgeFileMaxChunks
	DroppedFrom string // ref of the first dropped chunk, for the index doc
}

type chunkBuilder struct {
	out      []knowledgeChunk
	text     strings.Builder
	firstRef string
	lastRef  string
	heading  string
	images   []string
	imgFirst string
	imgLast  string
}

func (b *chunkBuilder) flushText() {
	if b.text.Len() == 0 {
		return
	}
	b.out = append(b.out, knowledgeChunk{
		Index:   len(b.out),
		Kind:    "text",
		Ref:     joinRefs(b.firstRef, b.lastRef),
		Heading: b.heading,
		Text:    strings.TrimSpace(b.text.String()),
	})
	b.text.Reset()
	b.firstRef, b.lastRef, b.heading = "", "", ""
}

func (b *chunkBuilder) flushImages() {
	if len(b.images) == 0 {
		return
	}
	b.out = append(b.out, knowledgeChunk{
		Index:      len(b.out),
		Kind:       "image",
		Ref:        joinRefs(b.imgFirst, b.imgLast),
		ImagePaths: b.images,
	})
	b.images, b.imgFirst, b.imgLast = nil, "", ""
}

func (b *chunkBuilder) addText(piece, ref, heading string) {
	if b.text.Len() > 0 {
		overflow := b.text.Len()+2+len(piece) > knowledgeChunkMaxChars
		headingBreak := heading != "" && b.text.Len() >= knowledgeChunkMinChars
		if overflow || headingBreak {
			b.flushText()
		}
	}
	if b.text.Len() == 0 {
		b.firstRef, b.heading = ref, heading
	} else {
		b.text.WriteString("\n\n")
	}
	b.text.WriteString(piece)
	b.lastRef = ref
}

// chunkKnowledgeUnits groups extractor units into vault-sized documents:
// 1500–6000 bytes of text, or up to 6 scanned pages, per chunk. Pure.
func chunkKnowledgeUnits(units []knowledgeUnit) knowledgeChunking {
	b := &chunkBuilder{}
	for _, u := range units {
		if u.Kind == "image" {
			b.flushText()
			if len(b.images) >= knowledgeScanPagesPerChunk {
				b.flushImages()
			}
			if len(b.images) == 0 {
				b.imgFirst = u.Ref
			}
			b.images = append(b.images, u.ImagePath)
			b.imgLast = u.Ref
			continue
		}
		b.flushImages()
		text := strings.TrimSpace(u.Text)
		if text == "" {
			continue
		}
		for i, piece := range splitOversizedText(text, knowledgeChunkMaxChars) {
			heading := ""
			if i == 0 {
				heading = u.Heading
			}
			b.addText(piece, u.Ref, heading)
		}
	}
	b.flushText()
	b.flushImages()

	result := knowledgeChunking{Chunks: b.out}
	if len(b.out) > knowledgeFileMaxChunks {
		result.Truncated = true
		result.Dropped = len(b.out) - knowledgeFileMaxChunks
		result.DroppedFrom = b.out[knowledgeFileMaxChunks].Ref
		result.Chunks = b.out[:knowledgeFileMaxChunks]
	}
	return result
}

// splitOversizedText cuts one unit that exceeds max at paragraph breaks, then
// at lines; a Markdown table split across pieces repeats its header so every
// piece stays a readable table.
func splitOversizedText(text string, max int) []string {
	if len(text) <= max {
		return []string{text}
	}
	var pieces []string
	var cur strings.Builder
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			pieces = append(pieces, s)
		}
		cur.Reset()
	}
	for _, para := range strings.Split(text, "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		if len(para) > max {
			flush()
			pieces = append(pieces, splitLongParagraph(para, max)...)
			continue
		}
		if cur.Len() > 0 && cur.Len()+2+len(para) > max {
			flush()
		}
		if cur.Len() > 0 {
			cur.WriteString("\n\n")
		}
		cur.WriteString(para)
	}
	flush()
	return pieces
}

func splitLongParagraph(para string, max int) []string {
	lines := strings.Split(para, "\n")
	header := ""
	if len(lines) >= 2 && isTableRow(lines[0]) && isTableSeparator(lines[1]) {
		header = lines[0] + "\n" + lines[1]
	}
	var pieces []string
	var cur strings.Builder
	start := func() {
		cur.Reset()
		if header != "" && len(pieces) > 0 {
			cur.WriteString(header)
		}
	}
	start()
	for _, line := range lines {
		line = cutRunes(line, max)
		if cur.Len() > 0 && cur.Len()+1+len(line) > max {
			pieces = append(pieces, cur.String())
			start()
		}
		if cur.Len() > 0 {
			cur.WriteString("\n")
		}
		cur.WriteString(line)
	}
	if cur.Len() > 0 {
		pieces = append(pieces, cur.String())
	}
	return pieces
}

// cutRunes trims s to at most max bytes without splitting a UTF-8 sequence —
// a single line longer than a whole document has nothing sensible left to keep.
func cutRunes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}

func isTableRow(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "|") && strings.HasSuffix(t, "|")
}

func isTableSeparator(line string) bool {
	t := strings.Trim(strings.TrimSpace(line), "|")
	if t == "" {
		return false
	}
	for _, r := range t {
		if r != '-' && r != ':' && r != '|' && r != ' ' {
			return false
		}
	}
	return true
}

// joinRefs renders the span a chunk covers: "Trang 12" + "Trang 15" → "Trang 12–15".
func joinRefs(first, last string) string {
	if first == last || last == "" {
		return first
	}
	p1, n1, ok1 := splitRefNumber(first)
	p2, n2, ok2 := splitRefNumber(last)
	if ok1 && ok2 && p1 == p2 {
		return fmt.Sprintf("%s %s–%s", p1, n1, n2)
	}
	return first + " – " + last
}

func splitRefNumber(ref string) (prefix, num string, ok bool) {
	i := strings.LastIndex(ref, " ")
	if i < 0 {
		return "", "", false
	}
	num = ref[i+1:]
	if num == "" {
		return "", "", false
	}
	for _, r := range num {
		if r < '0' || r > '9' {
			return "", "", false
		}
	}
	return ref[:i], num, true
}
