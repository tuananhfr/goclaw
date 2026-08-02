package tekshot

import (
	"strings"
	"testing"
)

func TestBuildDescribeImagePrompt(t *testing.T) {
	request := map[string]any{
		"facebook_page_name":        "Quán Bún Bò Hạnh",
		"facebook_page_description": "Bún bò Huế chuẩn vị.",
	}
	prompt := buildDescribeImagePrompt(request)

	for _, want := range []string{
		"Quán Bún Bò Hạnh",
		"Bún bò Huế chuẩn vị.",
		"read_image",
		"Vietnamese",
		"ONLY the description text",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q\n---\n%s", want, prompt)
		}
	}
}

func TestBuildDescribeImagePromptWithoutPageContext(t *testing.T) {
	prompt := buildDescribeImagePrompt(map[string]any{})
	if strings.Contains(prompt, "## Page context") {
		t.Fatalf("prompt should omit empty page context block\n---\n%s", prompt)
	}
	if !strings.Contains(prompt, "## Hard rules") {
		t.Fatalf("prompt missing hard rules\n---\n%s", prompt)
	}
}
