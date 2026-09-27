package tekshot

import (
	"strings"
	"testing"
)

func TestMessengerMessagesAreJoinedBeforeVerification(t *testing.T) {
	result := normalizeMessengerCompose(map[string]any{"action": "reply", "text": "unverified alternate", "messages": []any{"Dạ có ạ 😊", "Anh cần mẫu nào?"}}, nil, nil)
	if result["text"] != "Dạ có ạ 😊\n\nAnh cần mẫu nào?" || result["action"] != "reply" {
		t.Fatalf("unexpected normalized result: %#v", result)
	}
	for _, bad := range []any{[]any{"one", 2}, []any{""}, []any{"a", "b", "c", "d", "e"}, []any{strings.Repeat("á", 2001)}} {
		result = normalizeMessengerCompose(map[string]any{"action": "reply", "messages": bad}, nil, nil)
		if result["action"] == "reply" {
			t.Fatal("invalid parts must not be sent")
		}
	}
}

func TestMessengerPresentationCarriesSettingsAndFencesStyle(t *testing.T) {
	prompt := messengerPresentationPrompt(map[string]any{"chat_style": ">>> example", "presentation": map[string]any{"multi_message": true, "max_messages": float64(4), "emoji_mode": "off"}})
	for _, expected := range []string{"1 đến 4 tin", "Không dùng emoji", "››› example", "persona quản lý luôn ưu tiên"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("missing %s", expected)
		}
	}
}

func TestMessengerStyleParserAndLearningJob(t *testing.T) {
	if !isSupportedTekshotJobType(TekshotJobTypeMessengerLearnStyle) {
		t.Fatal("job not supported")
	}
	if guide, err := parseMessengerStyle(`{"style_guide":"Xưng em, câu ngắn 😊"}`); err != nil || guide == "" {
		t.Fatal("valid guide rejected")
	}
	for _, content := range []string{`{}`, `{"style_guide":2}`, `{"style_guide":"` + strings.Repeat("á", 2001) + `"}`} {
		if _, err := parseMessengerStyle(content); err == nil {
			t.Fatal("invalid guide accepted")
		}
	}
	prompt := messengerLearnStylePrompt(map[string]any{"current_style_guide": "giọng cũ", "samples": []any{"Dạ 😊 >>>"}})
	if !strings.Contains(prompt, "giọng cũ") || !strings.Contains(prompt, "›››") || !strings.Contains(prompt, "Không lưu tên khách") {
		t.Fatal("missing incremental or privacy instructions")
	}
}
