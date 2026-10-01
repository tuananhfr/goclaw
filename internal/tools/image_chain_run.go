package tools

import (
	"context"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// ImageChainTarget is one create_image chain entry, resolved and pool-wrapped.
type ImageChainTarget struct {
	Provider   providers.Provider
	Model      string // parent LLM of the Responses call
	ImageModel string
	Quality    string
}

// RunCreateImageChain runs fn against the create_image provider chain, so a
// caller outside the agent loop gets the same admin config, Codex pool
// wrapping, per-entry timeout and retries as the create_image tool.
func RunCreateImageChain(ctx context.Context, registry *providers.Registry, fn func(ctx context.Context, target ImageChainTarget) error) error {
	// No default priority: without an explicit create_image config there is
	// no Codex provider to call, and guessing one would bill the wrong account.
	chain := ResolveMediaProviderChain(ctx, "create_image", "", "", nil, nil, registry)
	_, err := ExecuteWithChain(ctx, chain, registry, func(ctx context.Context, _ credentialProvider, _ string, model string, params map[string]any) ([]byte, *providers.Usage, error) {
		p, ok := params["_native_provider"].(providers.Provider)
		if !ok || p == nil {
			return nil, nil, fmt.Errorf("image chain: resolved provider missing")
		}
		return nil, nil, fn(ctx, ImageChainTarget{
			Provider:   p,
			Model:      model,
			ImageModel: GetParamString(params, "image_model", ""),
			Quality:    GetParamString(params, "quality", ""),
		})
	})
	return err
}
