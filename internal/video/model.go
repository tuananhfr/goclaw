package video

import "encoding/json"

const ContractVersion = 1

type ModelStatus string

const (
	ModelAvailable  ModelStatus = "available"
	ModelDegraded   ModelStatus = "degraded"
	ModelDisabled   ModelStatus = "disabled"
	ModelDeprecated ModelStatus = "deprecated"
)

type Model struct {
	SchemaVersion      int            `json:"schema_version"`
	CatalogVersion     string         `json:"catalog_version"`
	ID                 string         `json:"id"`
	Provider           string         `json:"provider"`
	ProviderModelID    *string        `json:"provider_model_id"`
	Label              string         `json:"label"`
	Description        *string        `json:"description"`
	Status             ModelStatus    `json:"status"`
	StatusMessage      *string        `json:"status_message"`
	DeprecatedAt       *string        `json:"deprecated_at"`
	ReplacementModelID *string        `json:"replacement_model_id"`
	Operations         []string       `json:"operations"`
	Capabilities       Capabilities   `json:"capabilities"`
	Defaults           Defaults       `json:"defaults"`
	ParametersSchema   map[string]any `json:"parameters_schema"`
	UISchema           map[string]any `json:"ui_schema,omitempty"`
	Pricing            *Pricing       `json:"pricing"`
	Limits             Limits         `json:"limits"`
}

type Capabilities struct {
	Prompt         PromptCapability `json:"prompt"`
	Inputs         InputCapability  `json:"inputs"`
	Duration       Duration         `json:"duration"`
	AspectRatios   []string         `json:"aspect_ratios"`
	Resolutions    []string         `json:"resolutions"`
	OutputCount    OutputCount      `json:"output_count"`
	NativeAudio    bool             `json:"native_audio"`
	Seed           bool             `json:"seed"`
	CameraControl  bool             `json:"camera_control"`
	MotionStrength bool             `json:"motion_strength"`
	Upscale        bool             `json:"upscale"`
	Cancel         bool             `json:"cancel"`
	Progress       string           `json:"progress"`
}

type PromptCapability struct {
	Required       bool     `json:"required"`
	MinLength      int      `json:"min_length"`
	MaxLength      int      `json:"max_length"`
	Languages      []string `json:"languages"`
	NegativePrompt bool     `json:"negative_prompt"`
}

type InputCapability struct {
	Image           bool                 `json:"image"`
	FirstFrame      bool                 `json:"first_frame"`
	LastFrame       bool                 `json:"last_frame"`
	Video           bool                 `json:"video"`
	ReferenceImages ReferenceImageLimits `json:"reference_images"`
	Audio           bool                 `json:"audio"`
}

type ReferenceImageLimits struct {
	Supported bool `json:"supported"`
	Min       int  `json:"min"`
	Max       int  `json:"max"`
}

type Duration struct {
	Mode     string `json:"mode"`
	ValuesMS []int  `json:"values_ms,omitempty"`
	MinMS    int    `json:"min_ms,omitempty"`
	MaxMS    int    `json:"max_ms,omitempty"`
	StepMS   int    `json:"step_ms,omitempty"`
}

type OutputCount struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

type Defaults struct {
	Operation   string `json:"operation"`
	DurationMS  int    `json:"duration_ms"`
	AspectRatio string `json:"aspect_ratio"`
	Resolution  string `json:"resolution"`
	OutputCount int    `json:"output_count"`
}

// PricingVariant overrides the base amount when every key in When matches the
// job's parameters (resolution, generate_audio, …). Most-specific match wins.
type PricingVariant struct {
	When         map[string]any `json:"when"`
	AmountMicros int64          `json:"amount_micros"`
}

// Pricing is in micro units of Currency per Unit: 115000 = 0.115 USD per second.
type Pricing struct {
	Currency     string           `json:"currency"`
	Unit         string           `json:"unit"`
	AmountMicros int64            `json:"amount_micros"`
	Variants     []PricingVariant `json:"variants,omitempty"`
	EffectiveAt  *string          `json:"effective_at"`
}

type Limits struct {
	MaxConcurrentJobs    int  `json:"max_concurrent_jobs"`
	MaxRequestsPerMinute *int `json:"max_requests_per_minute"`
	MaxJobsPerDay        *int `json:"max_jobs_per_day"`
}

func cloneModel(model Model) Model {
	data, err := json.Marshal(model)
	if err != nil {
		panic(err)
	}
	var cloned Model
	if err := json.Unmarshal(data, &cloned); err != nil {
		panic(err)
	}
	return cloned
}
