package providers

import "context"

type studioImageObservationKey struct{}

// StudioImageObservation observes native image calls without coupling providers to tracing.
type StudioImageObservation struct {
	Start func(context.Context, string, StudioImageRequest) (context.Context, func(*StudioImageResult, error))
	Retry func(context.Context, string, int, int, error)
}

func WithStudioImageObservation(ctx context.Context, observation *StudioImageObservation) context.Context {
	return context.WithValue(ctx, studioImageObservationKey{}, observation)
}

func studioImageObservationFromContext(ctx context.Context) *StudioImageObservation {
	observation, _ := ctx.Value(studioImageObservationKey{}).(*StudioImageObservation)
	return observation
}

func observeStudioImage(ctx context.Context, provider string, req StudioImageRequest) (context.Context, func(*StudioImageResult, error)) {
	observation := studioImageObservationFromContext(ctx)
	if observation == nil || observation.Start == nil {
		return ctx, func(*StudioImageResult, error) {}
	}
	return observation.Start(ctx, provider, req)
}

func withStudioImageRetryObservation(ctx context.Context, provider string) context.Context {
	observation := studioImageObservationFromContext(ctx)
	if observation == nil || observation.Retry == nil {
		return ctx
	}
	previous := retryHookFromContext(ctx)
	return WithRetryHook(ctx, func(attempt, maxAttempts int, err error) {
		observation.Retry(ctx, provider, attempt, maxAttempts, err)
		if previous != nil {
			previous(attempt, maxAttempts, err)
		}
	})
}
