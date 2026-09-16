package video

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

var (
	modelIDPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]*$`)
	providerPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	aspectRatioPattern = regexp.MustCompile(`^[1-9][0-9]*:[1-9][0-9]*$`)
)

var supportedOperations = map[string]bool{
	"text-to-video":             true,
	"image-to-video":            true,
	"first-last-frame-to-video": true,
	"reference-to-video":        true,
	"video-to-video":            true,
	"video-extension":           true,
	"video-upscale":             true,
}

func IsSupportedOperation(operation string) bool { return supportedOperations[operation] }

func ValidateModel(model Model) error {
	if model.SchemaVersion != ContractVersion {
		return fmt.Errorf("schema_version must be %d", ContractVersion)
	}
	if _, err := time.Parse(time.RFC3339, model.CatalogVersion); err != nil {
		return fmt.Errorf("catalog_version must be RFC3339: %w", err)
	}
	if !modelIDPattern.MatchString(model.ID) || len(model.ID) > 128 {
		return fmt.Errorf("id has invalid format")
	}
	if !providerPattern.MatchString(model.Provider) || len(model.Provider) > 64 {
		return fmt.Errorf("provider has invalid format")
	}
	if strings.Count(model.ID, "/") != 1 || !strings.HasPrefix(model.ID, model.Provider+"/") {
		return fmt.Errorf("id must use provider/model-key format")
	}
	if strings.TrimSpace(model.Label) == "" || len(model.Label) > 255 {
		return fmt.Errorf("label is required and must not exceed 255 bytes")
	}
	if !slices.Contains([]ModelStatus{ModelAvailable, ModelDegraded, ModelDisabled, ModelDeprecated}, model.Status) {
		return fmt.Errorf("invalid status %q", model.Status)
	}
	if model.Status == ModelDeprecated && model.DeprecatedAt == nil {
		return fmt.Errorf("deprecated model requires deprecated_at")
	}
	if model.DeprecatedAt != nil {
		if _, err := time.Parse(time.RFC3339, *model.DeprecatedAt); err != nil {
			return fmt.Errorf("deprecated_at must be RFC3339: %w", err)
		}
	}
	if err := validateUniqueStrings("operations", model.Operations, func(value string) bool { return supportedOperations[value] }); err != nil {
		return err
	}
	if !slices.Contains(model.Operations, model.Defaults.Operation) {
		return fmt.Errorf("default operation is not supported")
	}
	if model.Capabilities.Prompt.MinLength < 0 || model.Capabilities.Prompt.MaxLength < 1 || model.Capabilities.Prompt.MinLength > model.Capabilities.Prompt.MaxLength {
		return fmt.Errorf("invalid prompt length range")
	}
	if err := validateUniqueStrings("prompt languages", model.Capabilities.Prompt.Languages, func(value string) bool {
		return len(value) >= 2 && len(value) <= 16
	}); err != nil {
		return err
	}
	refs := model.Capabilities.Inputs.ReferenceImages
	if refs.Min < 0 || refs.Max < refs.Min || (!refs.Supported && (refs.Min != 0 || refs.Max != 0)) {
		return fmt.Errorf("invalid reference image limits")
	}
	if err := validateDuration(model.Capabilities.Duration, model.Defaults.DurationMS); err != nil {
		return err
	}
	if err := validateUniqueStrings("aspect ratios", model.Capabilities.AspectRatios, aspectRatioPattern.MatchString); err != nil {
		return err
	}
	if !slices.Contains(model.Capabilities.AspectRatios, model.Defaults.AspectRatio) {
		return fmt.Errorf("default aspect ratio is not supported")
	}
	if err := validateUniqueStrings("resolutions", model.Capabilities.Resolutions, func(value string) bool {
		return strings.TrimSpace(value) != "" && len(value) <= 32
	}); err != nil {
		return err
	}
	if !slices.Contains(model.Capabilities.Resolutions, model.Defaults.Resolution) {
		return fmt.Errorf("default resolution is not supported")
	}
	outputs := model.Capabilities.OutputCount
	if outputs.Min < 1 || outputs.Max > 4 || outputs.Min > outputs.Max {
		return fmt.Errorf("invalid output count limits")
	}
	if model.Defaults.OutputCount < outputs.Min || model.Defaults.OutputCount > outputs.Max {
		return fmt.Errorf("default output count is outside limits")
	}
	if !slices.Contains([]string{"none", "estimated", "provider"}, model.Capabilities.Progress) {
		return fmt.Errorf("invalid progress mode %q", model.Capabilities.Progress)
	}
	if model.ParametersSchema == nil || model.ParametersSchema["type"] != "object" {
		return fmt.Errorf("parameters_schema must describe an object")
	}
	if model.Limits.MaxConcurrentJobs < 1 {
		return fmt.Errorf("max_concurrent_jobs must be positive")
	}
	return nil
}

func validateDuration(duration Duration, defaultMS int) error {
	switch duration.Mode {
	case "allowed-values":
		if len(duration.ValuesMS) == 0 {
			return fmt.Errorf("duration values are required")
		}
		seen := make(map[int]bool, len(duration.ValuesMS))
		for _, value := range duration.ValuesMS {
			if value < 1000 || value > 30000 || seen[value] {
				return fmt.Errorf("invalid or duplicate duration %d", value)
			}
			seen[value] = true
		}
		if !seen[defaultMS] {
			return fmt.Errorf("default duration is not supported")
		}
	case "range":
		if duration.MinMS < 1000 || duration.MaxMS > 30000 || duration.MinMS > duration.MaxMS || duration.StepMS < 1 {
			return fmt.Errorf("invalid duration range")
		}
		if defaultMS < duration.MinMS || defaultMS > duration.MaxMS || (defaultMS-duration.MinMS)%duration.StepMS != 0 {
			return fmt.Errorf("default duration is outside range")
		}
	default:
		return fmt.Errorf("invalid duration mode %q", duration.Mode)
	}
	return nil
}

func validateUniqueStrings(name string, values []string, valid func(string) bool) error {
	if len(values) == 0 {
		return fmt.Errorf("%s must not be empty", name)
	}
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if !valid(value) || seen[value] {
			return fmt.Errorf("%s contains invalid or duplicate value %q", name, value)
		}
		seen[value] = true
	}
	return nil
}
