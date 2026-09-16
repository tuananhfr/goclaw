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
