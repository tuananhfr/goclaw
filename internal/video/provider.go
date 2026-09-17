package video

import (
	"context"
	"errors"
)

// RenderProvider turns one scene.render job into a single video file on disk.
type RenderProvider interface {
	Render(ctx context.Context, request RenderRequest, update func(RenderUpdate)) (RenderOutput, error)
}

type RenderRequest struct {
	Job   Job
	Model Model
	// ProviderState is what the previous process persisted; a non-empty state
	// means the provider already accepted (and billed) the job, so resume it.
	ProviderState map[string]string
	OutputPath    string
}

type RenderUpdate struct {
	ProviderJobID string
	ProviderState map[string]string
	Progress      int
	Message       string
}

type RenderOutput struct {
	MIMEType       string
	FileSize       int
	ChecksumSHA256 string
}

// ProviderError carries a contract error code back to the job ledger.
type ProviderError struct {
	Code      string
	Message   string
	Retryable bool
}

func (e *ProviderError) Error() string { return e.Code + ": " + e.Message }

func asProviderError(err error) *ProviderError {
	var providerErr *ProviderError
	if errors.As(err, &providerErr) {
		return providerErr
	}
	return &ProviderError{Code: "PROVIDER_ERROR", Message: "Video provider request failed.", Retryable: true}
}

type JobServiceOption func(*JobService)

// WithRenderProviders routes models whose Provider matches a key to a real
// provider; everything else stays on the mock path.
func WithRenderProviders(outputDir string, providers map[string]RenderProvider) JobServiceOption {
	return func(s *JobService) {
		s.outputDir = outputDir
		s.providers = providers
	}
}
