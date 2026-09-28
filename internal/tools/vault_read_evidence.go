package tools

import (
	"context"
	"strings"
	"sync"
)

type vaultEvidenceKey struct{}

// VaultReadEvidence records only content actually returned by successful scoped reads.
type VaultReadEvidence struct {
	mu      sync.Mutex
	content map[string][]string
}

func WithVaultReadEvidence(ctx context.Context) (context.Context, *VaultReadEvidence) {
	e := &VaultReadEvidence{content: make(map[string][]string)}
	return context.WithValue(ctx, vaultEvidenceKey{}, e), e
}

func recordVaultRead(ctx context.Context, id, content string) {
	e, ok := ctx.Value(vaultEvidenceKey{}).(*VaultReadEvidence)
	if !ok {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.content[id] = append(e.content[id], content)
}

func (e *VaultReadEvidence) Contains(quote string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	quote = strings.TrimSpace(quote)
	if quote == "" {
		return false
	}
	for _, chunks := range e.content {
		for _, content := range chunks {
			if strings.Contains(content, quote) {
				return true
			}
		}
	}
	return false
}
