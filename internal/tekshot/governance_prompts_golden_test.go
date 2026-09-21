package tekshot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	// autocrlf có thể checkout golden thành CRLF trên Windows.
	if got != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Fatalf("%s drifted from golden\n--- got ---\n%s", name, got)
	}
}

// Không có system_rules thì Prompt D và luật ảnh phải y như trước khi luật thành dữ liệu sửa được.
func TestCompliancePromptMatchesGolden(t *testing.T) {
	request := map[string]any{"page_profile": map[string]any{
		"profile":   []any{"P3", "P6"},
		"cam_rieng": []any{"Cấm A"},
		"phap_nhan": "Công ty Mẫu",
	}}
	post := map[string]any{"title": "Tiêu đề", "content": "Nội dung", "tu_cham_risk": "LOW"}
	assertGolden(t, "compliance_prompt.golden", buildCompliancePrompt(pageProfileFromRequest(request), post))
}

func TestImageGuidanceMatchesGolden(t *testing.T) {
	var sb strings.Builder
	for _, branch := range []string{"UPLOAD", "REF", "INFO", "AI", "PRODUCT"} {
		request := map[string]any{
			"page_profile": map[string]any{"profile": []any{"P1"}},
			"loai_anh":     branch,
		}
		sb.WriteString("#" + branch + "\n" + imageGuidanceFor(request))
	}
	assertGolden(t, "image_guidance.golden", sb.String())
}
