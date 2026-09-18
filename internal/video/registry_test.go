package video

import "testing"

func TestMockRegistryModelsAreValid(t *testing.T) {
	registry, err := NewMockRegistry()
	if err != nil {
		t.Fatalf("NewMockRegistry() error = %v", err)
	}
	models := registry.List(ModelFilter{})
	if len(models) != 5 {
		t.Fatalf("List() returned %d models, want 5", len(models))
	}
	for _, model := range models {
		if err := ValidateModel(model); err != nil {
			t.Errorf("ValidateModel(%q) error = %v", model.ID, err)
		}
	}
}

func TestRegistryFiltersAndReturnsClones(t *testing.T) {
	registry := MustMockRegistry()
	models := registry.List(ModelFilter{Operation: "reference-to-video", Status: ModelDegraded})
	if len(models) != 1 || models[0].ID != "mock/reference-v1" {
		t.Fatalf("filtered List() = %#v", models)
	}

	model, ok := registry.Get("mock/cinematic-v1")
	if !ok {
		t.Fatal("Get() did not find cinematic model")
	}
	model.Capabilities.Resolutions[0] = "mutated"
	fresh, _ := registry.Get("mock/cinematic-v1")
	if fresh.Capabilities.Resolutions[0] == "mutated" {
		t.Fatal("Get() leaked mutable registry state")
	}
}

func TestValidateModelRejectsUnsupportedDefault(t *testing.T) {
	model, _ := MustMockRegistry().Get("mock/fast-v1")
	model.Defaults.Resolution = "1080p"
	if err := ValidateModel(model); err == nil {
		t.Fatal("ValidateModel() accepted unsupported default resolution")
	}
}

func TestFalPricingCarriesResolutionAndAudioVariants(t *testing.T) {
	registry, err := NewRegistry(true)
	if err != nil {
		t.Fatalf("NewRegistry(true) error = %v", err)
	}
	wan, ok := registry.Get("fal/wan-2.2-a14b-i2v")
	if !ok || wan.Pricing == nil {
		t.Fatal("wan model or pricing missing")
	}
	if wan.Pricing.Unit != "output_second" || wan.Pricing.AmountMicros != 40000 {
		t.Fatalf("wan base pricing = %+v", wan.Pricing)
	}
	if got := variantAmount(t, wan.Pricing, "resolution", "720p"); got != 80000 {
		t.Fatalf("wan 720p = %d, want 80000", got)
	}
	kling, _ := registry.Get("fal/kling-2.6-pro-i2v")
	if got := variantAmount(t, kling.Pricing, "generate_audio", false); got != 70000 {
		t.Fatalf("kling muted = %d, want 70000", got)
	}
	avatar, _ := registry.Get("fal/kling-avatar-v2-pro")
	if avatar.Pricing.AmountMicros != 115000 {
		t.Fatalf("avatar = %d, want 115000", avatar.Pricing.AmountMicros)
	}
	for _, model := range registry.List(ModelFilter{}) {
		if err := ValidateModel(model); err != nil {
			t.Errorf("ValidateModel(%q) error = %v", model.ID, err)
		}
	}
}

func variantAmount(t *testing.T, pricing *Pricing, param string, value any) int64 {
	t.Helper()
	for _, variant := range pricing.Variants {
		if got, ok := variant.When[param]; ok && got == value {
			return variant.AmountMicros
		}
	}
	t.Fatalf("no variant for %s=%v", param, value)
	return 0
}

func TestValidateModelRejectsBadPricing(t *testing.T) {
	model, _ := MustMockRegistry().Get("mock/cinematic-v1")
	model.Pricing = &Pricing{Currency: "USD", Unit: "second", AmountMicros: 1}
	if err := ValidateModel(model); err == nil {
		t.Fatal("unit \"second\" must be rejected; the contract only allows output_second")
	}
	model.Pricing = &Pricing{Currency: "USD", Unit: "output_second", AmountMicros: 1, Variants: []PricingVariant{{When: map[string]any{}, AmountMicros: 2}}}
	if err := ValidateModel(model); err == nil {
		t.Fatal("a variant with an empty when must be rejected")
	}
}
