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
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q\n---\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "Title: \n") {
		t.Fatalf("prompt should omit empty titles\n---\n%s", prompt)
	}
}
