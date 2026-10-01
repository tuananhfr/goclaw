package tools

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/providers/providertest"
)

func TestRunCreateImageChain_UsesCreateImageSettings(t *testing.T) {
	registry := providers.NewRegistry(nil)
	registry.Register(providertest.NewCodexProviderFast("openai-codex", "http://unused"))
	ctx := WithBuiltinToolSettings(context.Background(), BuiltinToolSettings{
		"create_image": []byte(`{"providers":[{"provider":"openai-codex","model":"gpt-5.6-luna","enabled":true,"params":{"image_model":"gpt-image-2.5-flare","quality":"high"}}]}`),
	})

	var got ImageChainTarget
	err := RunCreateImageChain(ctx, registry, func(_ context.Context, target ImageChainTarget) error {
		got = target
		return nil
	})
	if err != nil {
		t.Fatalf("RunCreateImageChain: %v", err)
	}
	if got.Provider == nil || got.Provider.Name() != "openai-codex" {
		t.Fatalf("provider = %v", got.Provider)
	}
	if got.Model != "gpt-5.6-luna" || got.ImageModel != "gpt-image-2.5-flare" || got.Quality != "high" {
		t.Fatalf("target = %+v", got)
	}
}

func TestRunCreateImageChain_NoSettingsFails(t *testing.T) {
	registry := providers.NewRegistry(nil)
	err := RunCreateImageChain(context.Background(), registry, func(context.Context, ImageChainTarget) error { return nil })
	if err == nil {
		t.Fatal("want error when create_image has no provider chain")
	}
}
