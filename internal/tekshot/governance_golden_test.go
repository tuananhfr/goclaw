package tekshot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Prompt gửi cho model phải giữ nguyên từng byte khi luật được dựng từ dữ liệu
// thay vì chuỗi viết tay: màn hình Kim chỉ nam và model đọc chung một nguồn.
func TestBuildGovernancePromptMatchesGolden(t *testing.T) {
	profile := &pageProfile{
		Codes:         []string{"P1", "P4"},
		Purposes:      []string{"THONG_TIN", "THUONG_MAI"},
		Formats:       []string{"F1", "F2"},
		Forbidden:     []string{"Cấm A", "Cấm B"},
		Required:      []string{"Bắt buộc A"},
		ForbiddenTops: []string{"Chủ đề X"},
		BlockCTAPhone: true,
		LegalEntity:   "Công ty Mẫu",
		Rules:         defaultGovernanceRules(),
	}
	got := buildGovernancePrompt(profile)

	path := filepath.Join("testdata", "governance_prompt.golden")
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
		t.Fatalf("governance prompt drifted from golden\n--- got ---\n%s", got)
	}
}
