package tekshot

import "testing"

func lintPost(title, brief, content, hashtags string) map[string]any {
	return map[string]any{
		"title": title, "brief": brief, "content": content, "hashtags": hashtags,
	}
}

func lintCodes(findings []LintFinding) map[string]bool {
	out := map[string]bool{}
	for _, f := range findings {
		out[f.Code] = true
	}
	return out
}

func TestLintDraftPostFlagsTitleEchoInFirstLine(t *testing.T) {
	item := SourceItem{SourceTitle: "Bí quyết chọn máy lạnh tiết kiệm điện"}
	post := lintPost(item.SourceTitle, "",
		"Bí quyết chọn máy lạnh tiết kiệm điện\nMùa hè này ba mẹ nào cũng đau đầu vì hóa đơn điện tăng vọt, và chiếc máy lạnh cũ chính là thủ phạm âm thầm nhất trong nhà.",
		"#maylanh #tietkiemdien #muahe")
	if !lintCodes(LintDraftPost(post, item))["title_echo"] {
		t.Fatal("expected title_echo finding")
	}
}

func TestLintDraftPostFlagsBriefCopiedVerbatim(t *testing.T) {
	brief := "Hook: hóa đơn điện tăng gấp đôi. Nội dung: inverter tiết kiệm 40%. CTA: inbox nhận tư vấn."
	item := SourceItem{SourceBrief: brief}
	post := lintPost("Tiêu đề", brief,
		"Mở đầu tự nhiên một chút. "+brief+" Và một câu kết.",
		"#dieuhoa #inverter #tuvan")
	if !lintCodes(LintDraftPost(post, item))["brief_copy"] {
		t.Fatal("expected brief_copy finding")
	}
}

func TestLintDraftPostFlagsBadHashtags(t *testing.T) {
	post := lintPost("Tiêu đề", "",
		"Một caption đủ dài để không dính lỗi content_too_short, nói về sản phẩm với đầy đủ lợi ích cụ thể cho người đọc, có số liệu và lời mời hành động rõ ràng ở cuối bài.",
		"#mot hai #ba")
	if !lintCodes(LintDraftPost(post, SourceItem{}))["hashtag_format"] {
		t.Fatal("expected hashtag_format finding: token 'hai' thiếu #")
	}
}

func TestLintDraftPostFlagsTooShortContent(t *testing.T) {
	post := lintPost("Tiêu đề", "", "Ngắn quá.", "#a #b #c")
	if !lintCodes(LintDraftPost(post, SourceItem{}))["content_too_short"] {
		t.Fatal("expected content_too_short finding")
	}
}

func TestLintDraftPostCleanPostHasNoFindings(t *testing.T) {
	item := SourceItem{
		SourceTitle: "Bí quyết chọn máy lạnh",
		SourceBrief: "Hook: hóa đơn điện. Benefit: inverter tiết kiệm 40%.",
	}
	post := lintPost(item.SourceTitle, item.SourceBrief,
		"Tháng này nhìn hóa đơn điện mà giật mình? Chiếc điều hòa 10 năm tuổi có thể đang ngốn gấp đôi lượng điện cần thiết. Công nghệ inverter thế hệ mới cắt tới 40% điện năng nhờ tự điều chỉnh công suất thay vì bật-tắt liên tục. Ghé showroom cuối tuần này để được đo công suất phòng miễn phí nhé!",
		"#maylanh #inverter #tietkiemdien")
	if findings := LintDraftPost(post, item); len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}
