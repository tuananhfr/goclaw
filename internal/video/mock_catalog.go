package video

const MockCatalogVersion = "2026-09-14T10:00:00Z"

func NewMockRegistry() (*StaticRegistry, error) {
	return NewStaticRegistry(MockCatalogVersion, mockModels())
}

func MustMockRegistry() *StaticRegistry {
	registry, err := NewMockRegistry()
	if err != nil {
		panic(err)
	}
	return registry
}

func mockModels() []Model {
	deprecatedAt := MockCatalogVersion
	replacement := "mock/cinematic-v1"
	return []Model{
		{
			SchemaVersion: 1,
			ID:            "mock/cinematic-v1",
			Provider:      "mock",
			Label:         "Mock Cinematic v1",
			Description:   stringPtr("Model mô phỏng chất lượng cao cho luồng phát triển."),
			Status:        ModelAvailable,
			Operations:    []string{"text-to-video", "image-to-video"},
			Capabilities: Capabilities{
				Prompt:         PromptCapability{Required: true, MinLength: 1, MaxLength: 4000, Languages: []string{"vi", "en"}, NegativePrompt: true},
				Inputs:         InputCapability{Image: true, ReferenceImages: ReferenceImageLimits{}},
				Duration:       Duration{Mode: "allowed-values", ValuesMS: []int{4000, 5000, 6000, 8000}},
				AspectRatios:   []string{"16:9", "9:16"},
				Resolutions:    []string{"720p", "1080p"},
				OutputCount:    OutputCount{Min: 1, Max: 2},
				Seed:           true,
				CameraControl:  true,
				MotionStrength: true,
				Cancel:         true,
				Progress:       "provider",
			},
			Defaults: Defaults{Operation: "text-to-video", DurationMS: 5000, AspectRatio: "16:9", Resolution: "720p", OutputCount: 1},
			ParametersSchema: objectSchema(map[string]any{
				"seed":            map[string]any{"type": "integer", "minimum": 0, "title": "Seed"},
				"camera_motion":   map[string]any{"type": "string", "enum": []string{"static", "pan-left", "pan-right", "zoom-in", "zoom-out"}, "default": "static", "title": "Chuyển động máy quay"},
				"motion_strength": map[string]any{"type": "number", "minimum": 0, "maximum": 1, "default": 0.5, "title": "Mức chuyển động"},
			}),
			UISchema: map[string]any{"order": []string{"camera_motion", "motion_strength", "seed"}},
			Limits:   Limits{MaxConcurrentJobs: 4, MaxRequestsPerMinute: intPtr(60)},
		},
		{
			SchemaVersion: 1,
			ID:            "mock/fast-v1",
			Provider:      "mock",
			Label:         "Mock Fast v1",
			Description:   stringPtr("Model mô phỏng nhanh, chỉ hỗ trợ text-to-video 720p."),
			Status:        ModelAvailable,
			Operations:    []string{"text-to-video"},
			Capabilities: Capabilities{
				Prompt:       PromptCapability{Required: true, MinLength: 1, MaxLength: 1000, Languages: []string{"vi", "en"}},
				Inputs:       InputCapability{ReferenceImages: ReferenceImageLimits{}},
				Duration:     Duration{Mode: "allowed-values", ValuesMS: []int{3000, 5000}},
				AspectRatios: []string{"16:9", "9:16", "1:1"},
				Resolutions:  []string{"720p"},
				OutputCount:  OutputCount{Min: 1, Max: 1},
				Cancel:       true,
				Progress:     "estimated",
			},
			Defaults:         Defaults{Operation: "text-to-video", DurationMS: 3000, AspectRatio: "9:16", Resolution: "720p", OutputCount: 1},
			ParametersSchema: objectSchema(map[string]any{}),
			Limits:           Limits{MaxConcurrentJobs: 8, MaxRequestsPerMinute: intPtr(120)},
		},
		{
			SchemaVersion: 1,
			ID:            "mock/reference-v1",
			Provider:      "mock",
			Label:         "Mock Reference v1",
			Description:   stringPtr("Model mô phỏng điều khiển bằng nhiều ảnh tham chiếu."),
			Status:        ModelDegraded,
			StatusMessage: stringPtr("Hàng đợi mô phỏng đang chậm."),
			Operations:    []string{"image-to-video", "reference-to-video"},
			Capabilities: Capabilities{
				Prompt:       PromptCapability{Required: true, MinLength: 10, MaxLength: 2000, Languages: []string{"vi", "en"}, NegativePrompt: true},
				Inputs:       InputCapability{Image: true, ReferenceImages: ReferenceImageLimits{Supported: true, Min: 1, Max: 4}},
				Duration:     Duration{Mode: "range", MinMS: 4000, MaxMS: 12000, StepMS: 1000},
				AspectRatios: []string{"16:9", "9:16"},
				Resolutions:  []string{"1080p"},
				OutputCount:  OutputCount{Min: 1, Max: 4},
				Seed:         true,
				Cancel:       false,
				Progress:     "none",
			},
			Defaults: Defaults{Operation: "reference-to-video", DurationMS: 6000, AspectRatio: "16:9", Resolution: "1080p", OutputCount: 1},
			ParametersSchema: objectSchema(map[string]any{
				"reference_strength": map[string]any{"type": "number", "minimum": 0, "maximum": 1, "default": 0.7, "title": "Độ bám ảnh tham chiếu"},
			}),
			Limits: Limits{MaxConcurrentJobs: 2, MaxRequestsPerMinute: intPtr(20)},
		},
		{
			SchemaVersion: 1,
			ID:            "mock/disabled-v1",
			Provider:      "mock",
			Label:         "Mock Disabled v1",
			Description:   stringPtr("Model tắt để kiểm tra policy không cho submit."),
			Status:        ModelDisabled,
			StatusMessage: stringPtr("Model đã bị quản trị viên tắt."),
			Operations:    []string{"text-to-video"},
			Capabilities:  basicCapabilities(),
			Defaults:      basicDefaults(),
			ParametersSchema: objectSchema(map[string]any{
				"quality": map[string]any{"type": "string", "enum": []string{"draft"}, "default": "draft", "title": "Chất lượng"},
			}),
			Limits: Limits{MaxConcurrentJobs: 1},
		},
		{
			SchemaVersion:      1,
			ID:                 "mock/deprecated-v1",
			Provider:           "mock",
			Label:              "Mock Deprecated v1",
			Description:        stringPtr("Model cũ chỉ để mở lại scene đã lưu."),
			Status:             ModelDeprecated,
			StatusMessage:      stringPtr("Hãy chuyển sang Mock Cinematic v1."),
			DeprecatedAt:       &deprecatedAt,
			ReplacementModelID: &replacement,
			Operations:         []string{"text-to-video"},
			Capabilities:       basicCapabilities(),
			Defaults:           basicDefaults(),
			ParametersSchema:   objectSchema(map[string]any{}),
			Limits:             Limits{MaxConcurrentJobs: 1},
		},
	}
}

func basicCapabilities() Capabilities {
	return Capabilities{
		Prompt:       PromptCapability{Required: true, MinLength: 1, MaxLength: 500, Languages: []string{"vi", "en"}},
		Inputs:       InputCapability{ReferenceImages: ReferenceImageLimits{}},
		Duration:     Duration{Mode: "allowed-values", ValuesMS: []int{4000}},
		AspectRatios: []string{"16:9"},
		Resolutions:  []string{"720p"},
		OutputCount:  OutputCount{Min: 1, Max: 1},
		Progress:     "none",
	}
}

func basicDefaults() Defaults {
	return Defaults{Operation: "text-to-video", DurationMS: 4000, AspectRatio: "16:9", Resolution: "720p", OutputCount: 1}
}

func objectSchema(properties map[string]any) map[string]any {
	return map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
	}
}

func stringPtr(value string) *string { return &value }
func intPtr(value int) *int          { return &value }
