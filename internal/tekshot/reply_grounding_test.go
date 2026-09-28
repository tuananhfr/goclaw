package tekshot

import (
	"context"
	"strings"
	"testing"
)

func TestReplyGroundingRequiresARealSource(t *testing.T) {
	req := composeRequest()
	req["post"] = map[string]any{"title": "Sale", "content": "Giảm 20% tới chủ nhật"}
	scripts := messengerComposeScriptsFromRequest(req)
	cases := []struct {
		name, grounding string
		result          map[string]any
		want            string
	}{
		{"social needs nothing", "social", map[string]any{}, "reply"},
		{"sourced without citation", "sourced", map[string]any{}, "hold"},
		{"sourced with row", "sourced", map[string]any{"row_ids": []string{"r12"}}, "reply"},
		{"sourced with quote", "sourced", map[string]any{"quotes": []string{"Gói cơ bản 450.000đ"}}, "reply"},
		{"profile present", "profile", map[string]any{}, "reply"},
		{"post present", "post", map[string]any{}, "reply"},
		{"missing grounding", "", map[string]any{}, "hold"},
		{"unknown grounding", "web", map[string]any{}, "hold"},
		{"verbatim script is its own source", "", map[string]any{"script_index": 1}, "reply"},
	}
	for _, c := range cases {
		result := map[string]any{"action": "reply", "row_ids": []string{}, "quotes": []string{}, "script_index": 0}
		for k, v := range c.result {
			result[k] = v
		}
		applyReplyGrounding(result, map[string]any{"grounding": c.grounding}, req, scripts)
		if result["action"] != c.want {
			t.Fatalf("%s: got %+v", c.name, result)
		}
	}
	noProfile := composeRequest()
	noProfile["profile"] = ""
	result := map[string]any{"action": "reply", "row_ids": []string{}, "quotes": []string{}, "script_index": 0}
	applyReplyGrounding(result, map[string]any{"grounding": "profile"}, noProfile, scripts)
	if result["action"] != "hold" || result["reason"] != "ungrounded" {
		t.Fatalf("profile grounding without a profile must hold: %+v", result)
	}
}

func TestMessengerComposeHoldsReplyWithoutSource(t *testing.T) {
	fake := &fakeComposeAgent{submit: composeSubmit(map[string]any{"text": "Dạ bánh rán tốt cho sức khoẻ lắm ạ", "row_ids": []any{}}), verdicts: []string{`{"verdict":"PASS"}`, `{"verdict":"PASS"}`}}
	result := (&JobService{}).messengerComposeWith(context.Background(), fake, composeJob(), composeRequest())
	if result["action"] != "hold" || result["reason"] != "ungrounded" || result["grounding"] != "sourced" {
		t.Fatalf("invented knowledge accepted: %+v", result)
	}
}

func TestMessengerComposeRejectsUnreadOrTrivialVaultQuote(t *testing.T) {
	for _, quote := range []string{"Bánh rán bảo hành một năm", "7h"} {
		fake := &fakeComposeAgent{submit: composeSubmit(map[string]any{"text": "Dạ có ạ", "row_ids": []any{}, "quotes": []any{quote}}), verdicts: []string{`{"verdict":"PASS"}`, `{"verdict":"PASS"}`}}
		result := (&JobService{}).messengerComposeWith(context.Background(), fake, composeJob(), composeRequest())
		if result["action"] != "hold" || result["reason"] != "unverified_vault_quote" {
			t.Fatalf("quote %q accepted without a vault_read: %+v", quote, result)
		}
	}
}

func TestComposeAndVerifyPromptsForbidOutsideKnowledge(t *testing.T) {
	req := composeRequest()
	prompt := buildMessengerComposePrompt(req, messengerReplyLinesFromRequest(req), messengerComposeScriptsFromRequest(req), messengerComposeRowsFromRequest(req), false)
	for _, want := range []string{"KHÔNG dùng hiểu biết riêng", "KHÔNG lấy thông tin trên mạng", `grounding="social"`} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("compose prompt missing %q", want)
		}
	}
	required, _ := NewMessengerReplyTool().Parameters()["required"].([]string)
	if !strings.Contains(strings.Join(required, ","), "grounding") {
		t.Fatal("submit tool must require grounding")
	}
	fake := &fakeComposeAgent{submit: composeSubmit(map[string]any{"text": "Dạ em chào anh ạ", "row_ids": []any{}, "grounding": "social"}), verdicts: []string{`{"verdict":"PASS"}`, `{"verdict":"PASS"}`}}
	result := (&JobService{}).messengerComposeWith(context.Background(), fake, composeJob(), composeRequest())
	if result["action"] != "reply" || len(fake.requests) < 2 || !strings.Contains(fake.requests[1].Message, "kiến thức phổ thông") {
		t.Fatalf("social reply must pass through the strict verifier: %+v", result)
	}
}
