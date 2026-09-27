package tools

import (
	"slices"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func TestRequestToolLimitCannotBeExpandedByAlsoAllow(t *testing.T) {
	all := []string{"read_file", "write_file", "vault_search"}
	for _, scope := range []string{"global", "agent"} {
		t.Run(scope, func(t *testing.T) {
			pe := NewPolicyEngine(&config.ToolsConfig{})
			agentPolicy := &config.ToolPolicySpec{}
			if scope == "global" {
				pe.globalPolicy.AlsoAllow = []string{"read_file", "write_file"}
			} else {
				agentPolicy.AlsoAllow = []string{"read_file", "write_file"}
			}
			for _, sentinel := range []string{"messenger-style/no-tools", "messenger-reply-verify/no-tools"} {
				if got := pe.evaluate(all, "test", agentPolicy, []string{sentinel}); len(got) != 0 {
					t.Fatalf("%s allowed tools: %v", sentinel, got)
				}
			}
			if got := pe.evaluate(all, "test", agentPolicy, []string{"vault_search"}); !slices.Equal(got, []string{"vault_search"}) {
				t.Fatalf("request allowlist expanded: %v", got)
			}
			if got := pe.evaluate(all, "test", agentPolicy, nil); len(got) != len(all) {
				t.Fatalf("unrestricted request changed: %v", got)
			}
		})
	}
}
