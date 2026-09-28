package tekshot

import (
	"context"
	"strings"
	"testing"
)

func TestCommentComposeUsesIsolatedContextForComposeAndVerify(t *testing.T) {
	fake := &fakeComposeAgent{submit: composeSubmit(map[string]any{"text": "Dạ em kiểm tra mẫu này nhé.", "row_ids": []any{}, "hold_text": "", "grounding": "social"}), verdicts: []string{`{"verdict":"PASS"}`}}
	req := composeRequest()
	req["post"] = map[string]any{"title": "Public post", "content": "Public content"}
	req["max_chars"] = float64(500)
	result := (&JobService{}).commentComposeWith(context.Background(), fake, composeJob(), req)
	if result["action"] != "reply" || len(fake.requests) != 2 {
		t.Fatalf("unexpected result: %+v / %d calls", result, len(fake.requests))
	}
	for _, call := range fake.requests {
		if !call.IsolatedContext || !strings.Contains(call.ExtraSystemPrompt, "PUBLIC Facebook") || len(call.Media) != 0 {
			t.Fatalf("unisolated comment pass: %+v", call)
		}
	}
}

func TestCommentVerbatimMismatchAndMultipleRepliesHold(t *testing.T) {
	for _, change := range []map[string]any{
		{"script_index": float64(1), "text": "Invented exact reply"},
		{"messages": []any{"First", "Second"}, "text": "First\n\nSecond"},
	} {
		fake := &fakeComposeAgent{submit: composeSubmit(change), verdicts: []string{`{"verdict":"PASS"}`}}
		result := (&JobService{}).commentComposeWith(context.Background(), fake, composeJob(), composeRequest())
		if result["action"] != "hold" {
			t.Fatalf("unsafe reply accepted: %+v", result)
		}
	}
}

func TestCommentDoesNotForceSubmitWithoutOriginalContext(t *testing.T) {
	fake := &fakeComposeAgent{}
	result := (&JobService{}).commentComposeWith(context.Background(), fake, composeJob(), composeRequest())
	if result["action"] != "hold" || len(fake.requests) != 1 {
		t.Fatalf("isolated compose retried without its tool evidence: %+v / %d calls", result, len(fake.requests))
	}
}

func TestCommentScriptsModeKeepsSilenceButHoldsUnscriptedReply(t *testing.T) {
	req := composeRequest()
	req["mode"] = "scripts"
	silent := &fakeComposeAgent{submit: composeSubmit(map[string]any{"action": "silence", "text": "", "row_ids": []any{}})}
	if result := (&JobService{}).commentComposeWith(context.Background(), silent, composeJob(), req); result["action"] != "silence" {
		t.Fatalf("an acknowledgement must stay silent in scripts mode: %+v", result)
	}
	free := &fakeComposeAgent{submit: composeSubmit(map[string]any{"text": "Dạ em chào anh ạ", "row_ids": []any{}, "grounding": "social"}), verdicts: []string{`{"verdict":"PASS"}`, `{"verdict":"PASS"}`}}
	if result := (&JobService{}).commentComposeWith(context.Background(), free, composeJob(), req); result["action"] != "hold" || result["reason"] != "no_script" {
		t.Fatalf("scripts mode must not send a free-written reply: %+v", result)
	}
}

func TestCommentRejectsFabricatedVaultQuote(t *testing.T) {
	fake := &fakeComposeAgent{submit: composeSubmit(map[string]any{"quotes": []any{"An invented business promise"}, "hold_text": ""})}
	result := (&JobService{}).commentComposeWith(context.Background(), fake, composeJob(), composeRequest())
	if result["action"] != "hold" || result["reason"] != "unverified_vault_quote" {
		t.Fatalf("unread Vault quote accepted: %+v", result)
	}
}
