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

// Fixtures lấy nguyên văn từ batch chạy thật 2026-08-16 (workspace 22, post
// 2144): reviewer cho pass sạch trong khi content chỉ là brief viết lại. Lint
// phải bắt được ca này mà không cần đốt lượt LLM nào.
const realBrief2144 = `Hook: CHỈ 59K CHO BỮA TỐI CỰC ĐỈNH TẠI PIZZA HIP'S RESTO THANH TRÌ

Mở đầu: Tối nay đi làm về mệt mỏi, ngại nấu nướng mà chưa biết ăn gì? Đừng lo các bác ơi, Hip'S đã có ngay bữa tối vừa gọn vừa ngon lại cực êm ví

Combo Độc Thân chỉ 59K: 1 Mỳ Ý Bò Băm + 1 Coca Tươi.

Sợi mỳ Ý dai ngon: Quyện đẫm sốt bò băm đậm đà, béo thơm chuẩn vị.

Coca tươi nổ bọt: Mát lịm sảng khoái, đánh tan mọi mệt mỏi ngày dài.

Kết & CTA: Nhấc máy alo hoặc ghé ngay Pizza Hip'S Resto Thanh Trì để sở hữu bữa ăn nhanh-gọn tiện`

const realContent2144 = `Một phần mỳ Ý bò băm với sợi mỳ dai, quyện sốt bò băm đậm đà, béo thơm — ăn nhanh mà vẫn đủ vị cho buổi tối bận rộn.

Đi kèm là Coca tươi nổ bọt mát lạnh, giúp bữa ăn thêm sảng khoái sau một ngày dài.

1 Mỳ Ý Bò Băm + 1 Coca Tươi, vừa gọn phần vừa êm ví. Alo hoặc ghé Pizza Hip'S Resto Thanh Trì để có bữa tối nhanh-gọn tiện nhé!`

func TestLintDraftPostFlagsBriefParaphrasedSentenceBySentence(t *testing.T) {
	item := SourceItem{SourceTitle: "Chỉ 59K cho bữa tối cực đỉnh", SourceBrief: realBrief2144}
	post := lintPost(item.SourceTitle, realBrief2144, realContent2144, "#pizzahips #combo59k #thanhtri")
	codes := lintCodes(LintDraftPost(post, item))
	if !codes["brief_paraphrase"] {
		t.Fatalf("expected brief_paraphrase on a caption that mirrors the brief line by line, got %+v", LintDraftPost(post, item))
	}
	// Whole-brief substring không khớp vì model diễn đạt lại từng câu — đó chính
	// là lý do brief_copy một mình không đủ.
	if codes["brief_copy"] {
		t.Fatal("brief_copy must not fire here; the brief is paraphrased, not pasted")
	}
}

func TestLintDraftPostDevelopedContentIsNotFlaggedAsParaphrase(t *testing.T) {
	item := SourceItem{
		SourceTitle: "Bí quyết chọn máy lạnh",
		SourceBrief: "Hook: hóa đơn điện tăng gấp đôi mùa nóng.\n\nNội dung: máy inverter tiết kiệm 40% điện năng.\n\nKết & CTA: ghé showroom đo công suất phòng miễn phí.",
	}
	post := lintPost(item.SourceTitle, item.SourceBrief,
		"Tháng nào cũng giật mình khi mở phong bì tiền điện? Thủ phạm thường không phải cái tủ lạnh, mà là chiếc máy cũ chạy suốt đêm với công suất cố định. Loại đời mới tự hạ tải khi phòng đã đủ mát, nhờ vậy cắt được gần một nửa lượng điện tiêu thụ mỗi tháng. Muốn biết phòng mình cần công suất bao nhiêu thì cứ ghé cửa hàng, bọn mình đo giúp không tính phí.",
		"#maylanh #inverter #tietkiemdien")
	for _, f := range LintDraftPost(post, item) {
		if f.Code == "brief_paraphrase" {
			t.Fatal("content that develops the brief must not be flagged as paraphrase")
		}
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
