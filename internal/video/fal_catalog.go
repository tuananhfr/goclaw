package video

// Bump when a fal model's capabilities change so Drupal refreshes its cache.
const FalCatalogVersion = "2026-09-17T12:00:00Z"

const FalProviderName = "fal"

// NewRegistry always carries the mock models; fal models join only when the
// gateway has a key, so a keyless deploy never lists a model it cannot run.
func NewRegistry(includeFal bool) (*StaticRegistry, error) {
	if !includeFal {
		return NewMockRegistry()
	}
	return NewStaticRegistry(FalCatalogVersion, append(mockModels(), falModels()...))
}

func falModels() []Model {
	return []Model{
		{
			SchemaVersion:   1,
			ID:              "fal/wan-2.2-a14b-i2v",
			Provider:        FalProviderName,
			ProviderModelID: stringPtr("fal-ai/wan/v2.2-a14b/image-to-video"),
			Label:           "Wan 2.2 (fal) — ảnh thành video",
			Description:     stringPtr("Biến ảnh của cảnh thành clip ngắn, không có tiếng. Giá khoảng 0,04 USD/giây ở 480p, 0,08 USD/giây ở 720p."),
			Status:          ModelAvailable,
			Operations:      []string{"image-to-video"},
			Capabilities: Capabilities{
				// Storyboard scenes may carry no visual prompt; the adapter supplies a motion-only default.
				Prompt: PromptCapability{Required: false, MinLength: 1, MaxLength: 2000, Languages: []string{"en", "vi"}, NegativePrompt: true},
				Inputs: InputCapability{Image: true, ReferenceImages: ReferenceImageLimits{}},
				// fal counts frames at 16 fps and caps a clip at 161 frames (~10s).
				Duration:     Duration{Mode: "range", MinMS: 2000, MaxMS: 10000, StepMS: 1000},
				AspectRatios: []string{"16:9", "9:16", "1:1"},
				Resolutions:  []string{"480p", "580p", "720p"},
				OutputCount:  OutputCount{Min: 1, Max: 1},
				Seed:         true,
				Cancel:       true,
				Progress:     "estimated",
			},
			Defaults: Defaults{Operation: "image-to-video", DurationMS: 5000, AspectRatio: "9:16", Resolution: "480p", OutputCount: 1},
			ParametersSchema: objectSchema(map[string]any{
				"seed":                    map[string]any{"type": "integer", "minimum": 0, "title": "Seed"},
				"enable_prompt_expansion": map[string]any{"type": "boolean", "default": false, "title": "Để AI mở rộng prompt"},
			}),
			UISchema: map[string]any{"order": []string{"enable_prompt_expansion", "seed"}},
			Pricing:  &Pricing{Currency: "USD", Unit: "second", AmountMinor: 4},
			Limits:   Limits{MaxConcurrentJobs: 2, MaxRequestsPerMinute: intPtr(10)},
		},
	}
}
