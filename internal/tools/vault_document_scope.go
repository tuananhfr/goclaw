package tools

import (
	"context"

	"github.com/nextlevelbuilder/goclaw/internal/vault"
)

type vaultDocumentScopeKey struct{}

// vaultScopedSearchPool is how many ranked results a scoped search draws before the allowlist cut.
const vaultScopedSearchPool = 200

func vaultScopeRestricted(ctx context.Context) bool {
	_, restricted := ctx.Value(vaultDocumentScopeKey{}).(map[string]struct{})
	return restricted
}

// WithVaultDocumentScope narrows existing access; an empty list denies every document.
func WithVaultDocumentScope(ctx context.Context, ids []string) context.Context {
	allowed := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id != "" {
			allowed[id] = struct{}{}
		}
	}
	return context.WithValue(ctx, vaultDocumentScopeKey{}, allowed)
}

func vaultDocumentAllowed(ctx context.Context, id string) bool {
	allowed, restricted := ctx.Value(vaultDocumentScopeKey{}).(map[string]struct{})
	if !restricted {
		return true
	}
	_, ok := allowed[id]
	return ok
}

func restrictVaultResults(ctx context.Context, results []vault.UnifiedSearchResult) []vault.UnifiedSearchResult {
	if _, restricted := ctx.Value(vaultDocumentScopeKey{}).(map[string]struct{}); !restricted {
		return results
	}
	out := make([]vault.UnifiedSearchResult, 0, len(results))
	for _, result := range results {
		if result.Source == "vault" && vaultDocumentAllowed(ctx, result.ID) {
			out = append(out, result)
		}
	}
	return out
}
