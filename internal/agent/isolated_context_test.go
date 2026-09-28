package agent

import "testing"

func TestIsolatedContextIsOptInAtAgentBoundary(t *testing.T) {
	loop := &Loop{maxIterations: 6, contextWindow: 8192}
	ordinary := loop.buildPipelineDeps(&RunRequest{}, &runState{})
	if ordinary.LoadSessionHistory == nil || ordinary.LoadContextFiles == nil || ordinary.FlushMessages == nil {
		t.Fatal("ordinary Messenger and agent runs must retain their existing context")
	}
	isolated := loop.buildPipelineDeps(&RunRequest{IsolatedContext: true}, &runState{})
	if isolated.LoadSessionHistory != nil || isolated.LoadContextFiles != nil || isolated.FlushMessages != nil || isolated.InjectReminders != nil || isolated.MaybeSummarize != nil {
		t.Fatal("agent did not enforce isolated context")
	}
	if isolated.InjectContext == nil || isolated.ExecuteToolCall == nil {
		t.Fatal("identity scoping, input guard, and scoped tool execution must remain active")
	}
}
