package providers

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

var _ StudioImageProvider = (*ChatGPTOAuthRouter)(nil)

// StudioImage mirrors GenerateImage's failover: retryable errors move to the
// next pool member, anything else is returned as is.
func (p *ChatGPTOAuthRouter) StudioImage(ctx context.Context, req StudioImageRequest) (*StudioImageResult, error) {
	ordered, err := p.orderedProviders(ctx, chatGPTOAuthModalityImage, true)
	if err != nil {
		return nil, err
	}
	attempted := make([]string, 0, len(ordered))
	var lastErr error
	for i, provider := range ordered {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		attempted = append(attempted, provider.Name())
		sp, ok := provider.(StudioImageProvider)
		if !ok {
			lastErr = fmt.Errorf("member %s has no studio image support", provider.Name())
			continue
		}
		res, callErr := sp.StudioImage(ctx, req)
		if callErr == nil {
			return res, nil
		}
		lastErr = callErr
		if !IsRetryableError(callErr) {
			return nil, callErr
		}
		if i < len(ordered)-1 {
			slog.Warn("chatgpt_oauth router studio image failover",
				"from", provider.Name(), "to", ordered[i+1].Name(), "error", callErr)
		}
	}
	return nil, fmt.Errorf("all pool members failed studio image (%s): %w", strings.Join(attempted, ", "), lastErr)
}
