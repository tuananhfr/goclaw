package video

import (
	"fmt"
	"slices"
	"sort"
	"time"
)

type ModelFilter struct {
	Operation string
	Status    ModelStatus
}

type ModelRegistry interface {
	CatalogVersion() string
	List(ModelFilter) []Model
	Get(string) (Model, bool)
}

type StaticRegistry struct {
	catalogVersion string
	models         map[string]Model
}

func NewStaticRegistry(catalogVersion string, models []Model) (*StaticRegistry, error) {
	if _, err := time.Parse(time.RFC3339, catalogVersion); err != nil {
		return nil, fmt.Errorf("invalid catalog version: %w", err)
	}

	registry := &StaticRegistry{
		catalogVersion: catalogVersion,
		models:         make(map[string]Model, len(models)),
	}
	for index := range models {
		model := cloneModel(models[index])
		model.CatalogVersion = catalogVersion
		if err := ValidateModel(model); err != nil {
			return nil, fmt.Errorf("model %q: %w", model.ID, err)
		}
		if _, exists := registry.models[model.ID]; exists {
			return nil, fmt.Errorf("duplicate model id %q", model.ID)
		}
		registry.models[model.ID] = model
	}
	for _, model := range registry.models {
		if model.ReplacementModelID != nil {
			if _, exists := registry.models[*model.ReplacementModelID]; !exists {
				return nil, fmt.Errorf("model %q references unknown replacement %q", model.ID, *model.ReplacementModelID)
			}
		}
	}
	return registry, nil
}

func (r *StaticRegistry) CatalogVersion() string { return r.catalogVersion }

func (r *StaticRegistry) List(filter ModelFilter) []Model {
	models := make([]Model, 0, len(r.models))
	for _, model := range r.models {
		if filter.Status != "" && model.Status != filter.Status {
			continue
		}
		if filter.Operation != "" && !slices.Contains(model.Operations, filter.Operation) {
			continue
		}
		models = append(models, cloneModel(model))
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models
}

func (r *StaticRegistry) Get(id string) (Model, bool) {
	model, ok := r.models[id]
	if !ok {
		return Model{}, false
	}
	return cloneModel(model), true
}
