package pipeline

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func TestIsolatedContextUsesOnlyExplicitInput(t *testing.T) {
	deps := PipelineDeps{
		AutoInject: func(context.Context, string, string, string) (string, error) {
			t.Fatal("memory loaded")
			return "", nil
		},
		LoadSessionHistory: func(context.Context, string) ([]providers.Message, string) { t.Fatal("history loaded"); return nil, "" },
		InjectReminders: func(context.Context, *RunInput, []providers.Message) []providers.Message {
			t.Fatal("reminder loaded")
			return nil
		},
		DrainInjectCh:  func() []providers.Message { t.Fatal("external injection"); return nil },
		FlushMessages:  func(context.Context, string, []providers.Message) error { t.Fatal("session persisted"); return nil },
		MaybeSummarize: func(context.Context, string) { t.Fatal("memory updated") },
	}
	deps.IsolateContext()
	input := &RunInput{Message: "Public comment", ExtraSystemPrompt: "Explicit rules", SessionKey: "private-session"}
	state := NewRunState(input, nil, "test", nil)
	if err := NewContextStage(&deps).Execute(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	msgs := state.Messages.All()
	if len(msgs) != 2 || msgs[0].Content != input.ExtraSystemPrompt || msgs[1].Content != input.Message {
		t.Fatalf("unexpected context: %+v", msgs)
	}
	if deps.AutoInject != nil || deps.LoadContextFiles != nil || deps.LoadSessionHistory != nil || deps.InjectReminders != nil || deps.DrainInjectCh != nil || deps.FlushMessages != nil || deps.MaybeSummarize != nil || deps.RunMemoryFlush != nil || deps.EmitSessionCompleted != nil || deps.Hooks != nil {
		t.Fatal("isolated context retained implicit context or memory writers")
	}
}
