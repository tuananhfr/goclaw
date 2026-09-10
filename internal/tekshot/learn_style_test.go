package tekshot

import (
	"strings"
	"testing"
)

func TestLearnStyleSamplesParsesDrupalShape(t *testing.T) {
	request := map[string]any{
		"sample_posts": []any{
			map[string]any{"title": "Món mới", "content": "Bún bò đặc biệt tuần này!", "hashtags": "#bunbo #monngon"},
			map[string]any{"title": "", "content": "Khách quen ơi, cuối tuần ghé nha.", "hashtags": ""},
		},
	}
	samples := learnStyleSamples(request)
	if len(samples) != 2 {
		t.Fatalf("expected 2 samples, got %d", len(samples))
	}
	if samples[0].Title != "Món mới" || samples[0].Hashtags != "#bunbo #monngon" {
		t.Fatalf("sample[0] parsed wrong: %+v", samples[0])
	}
	if samples[1].Content != "Khách quen ơi, cuối tuần ghé nha." {
		t.Fatalf("sample[1] parsed wrong: %+v", samples[1])
	}
}

func TestLearnStyleSamplesSkipsEmptyAndMalformed(t *testing.T) {
	request := map[string]any{
		"sample_posts": []any{
			map[string]any{"title": "chỉ có title", "content": "   "},
			"not a map",
			map[string]any{"content": "bài hợp lệ"},
		},
	}
	samples := learnStyleSamples(request)
	if len(samples) != 1 {
		t.Fatalf("expected 1 sample, got %d", len(samples))
	}
	if samples[0].Content != "bài hợp lệ" {
		t.Fatalf("unexpected sample: %+v", samples[0])
	}
}

func TestLearnStyleSamplesMissingKey(t *testing.T) {
	if samples := learnStyleSamples(map[string]any{}); samples != nil {
		t.Fatalf("expected nil for missing sample_posts, got %+v", samples)
	}
	if samples := learnStyleSamples(map[string]any{"sample_posts": "oops"}); samples != nil {
		t.Fatalf("expected nil for non-array sample_posts, got %+v", samples)
	}
}

func TestBuildLearnStylePrompt(t *testing.T) {
	request := map[string]any{
		"style_source":              "page_posts",
		"facebook_page_name":        "Quán Bún Bò Hạnh",
		"facebook_page_description": "Bún bò Huế chuẩn vị.",
	}
	samples := []learnStyleSample{
		{Title: "Món mới", Content: "Bún bò đặc biệt tuần này!", Hashtags: "#bunbo"},
		{Content: "Khách quen ơi, cuối tuần ghé nha."},
	}
	prompt := buildLearnStylePrompt(request, samples)

	for _, want := range []string{
		"Quán Bún Bò Hạnh",
		"Bún bò Huế chuẩn vị.",
		"Sample source: page_posts",
		"## Sample posts (2)",
		"Bún bò đặc biệt tuần này!",
		"Hashtags: #bunbo",
		"Khách quen ơi, cuối tuần ghé nha.",
		"ENTIRE style guide in Vietnamese",
		"ONLY the style guide text",
		"Bài mẫu tham chiếu",
		"đúng 1 bài ĐẦY ĐỦ nguyên văn",
		"khoảng 4000 ký tự",
		"tối đa 5000 ký tự",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q\n---\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "Title: \n") {
		t.Fatalf("prompt should omit empty titles\n---\n%s", prompt)
	}
	if strings.Contains(prompt, "Current style guide") {
		t.Fatalf("fresh learn must not carry an update block\n---\n%s", prompt)
	}
}

func TestBuildLearnStylePromptUpdatesCurrentGuide(t *testing.T) {
	request := map[string]any{
		"style_source":        "page_posts",
		"current_style_guide": "## Giọng văn\n- Thân thiện, xưng \"nhà mình\".",
	}
	prompt := buildLearnStylePrompt(request, []learnStyleSample{{Content: "Bài mới nè."}})

	for _, want := range []string{
		"## Current style guide",
		"xưng \"nhà mình\"",
		"## New sample posts (1)",
		"Giữ nguyên cấu trúc và tên các mục",
		"Ít bài mẫu thì thay đổi ít",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("update prompt missing %q\n---\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "## Sample posts (") {
		t.Fatalf("update prompt should label samples as new\n---\n%s", prompt)
	}
}

func TestBuildLearnStylePromptAsksToShrinkAnOversizedGuide(t *testing.T) {
	request := map[string]any{
		"current_style_guide":   strings.Repeat("á", 120),
		"style_guide_max_chars": float64(100),
	}
	prompt := buildLearnStylePrompt(request, []learnStyleSample{{Content: "Bài mới."}})
	if !strings.Contains(prompt, "đang dài hơn giới hạn") {
		t.Fatalf("prompt should ask to shrink a guide over the limit\n---\n%s", prompt)
	}
	if !strings.Contains(prompt, "tối đa 100 ký tự") {
		t.Fatalf("prompt should state the limit from the request\n---\n%s", prompt)
	}
}

func TestLearnStyleMaxChars(t *testing.T) {
	cases := map[string]struct {
		request map[string]any
		want    int
	}{
		"missing uses default":  {map[string]any{}, learnStyleDefaultMaxChars},
		"json number":           {map[string]any{"style_guide_max_chars": float64(4000)}, 4000},
		"zero uses default":     {map[string]any{"style_guide_max_chars": float64(0)}, learnStyleDefaultMaxChars},
		"negative uses default": {map[string]any{"style_guide_max_chars": float64(-5)}, learnStyleDefaultMaxChars},
	}
	for name, tc := range cases {
		if got := learnStyleMaxChars(tc.request); got != tc.want {
			t.Fatalf("%s: got %d, want %d", name, got, tc.want)
		}
	}
}

func TestStyleGuideLengthCountsCharactersNotBytes(t *testing.T) {
	// PHP đo bằng mb_strlen, nên Go phải đếm ký tự chứ không đếm byte UTF-8.
	if got := styleGuideLength("Tiếng Việt"); got != 10 {
		t.Fatalf("got %d, want 10", got)
	}
}

func TestLearnStyleTargetCharsLeavesHeadroomUnderTheLimit(t *testing.T) {
	// Model hay viết lố con số được giao, nên nhắm 80% giới hạn.
	if got := learnStyleTargetChars(5000); got != 4000 {
		t.Fatalf("got %d, want 4000", got)
	}
}

func TestShorterGuideKeepsTheShortestNonEmptyVersion(t *testing.T) {
	cases := map[string]struct {
		current, candidate, want string
	}{
		"shorter candidate wins":   {"dài dòng lắm", "gọn", "gọn"},
		"longer candidate ignored": {"gọn", "dài dòng lắm", "gọn"},
		"empty candidate ignored":  {"gọn", "   ", "gọn"},
	}
	for name, tc := range cases {
		if got := shorterGuide(tc.current, tc.candidate); got != tc.want {
			t.Fatalf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

func TestBuildLearnStyleShortenPrompt(t *testing.T) {
	prompt := buildLearnStyleShortenPrompt("## Giọng văn\n- dài dòng", 6200, 5000)
	for _, want := range []string{"6200", "5000", "4000", "## Giọng văn", "Giữ nguyên cấu trúc", "ONLY the style guide text"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("shorten prompt missing %q\n---\n%s", want, prompt)
		}
	}
}
