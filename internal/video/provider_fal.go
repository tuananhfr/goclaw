package video

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/security"
)

const (
	falDefaultQueueURL  = "https://queue.fal.run"
	falMaxImageBytes    = 10 << 20
	falMaxVideoBytes    = 512 << 20
	falMaxPollFailures  = 10
	falWanFramesPerSec  = 16
	falWanMinFrames     = 17
	falWanMaxFrames     = 161
	falStatusInQueue    = "IN_QUEUE"
	falStatusInProgress = "IN_PROGRESS"
	falStatusCompleted  = "COMPLETED"
	// fal rejects an empty prompt; the image already fixes the content, so only motion is described.
	falDefaultMotionPrompt = "Subtle natural motion, gentle slow camera push-in, keep the subject and composition unchanged"
)

type FalProvider struct {
	apiKey       string
	queueURL     string
	apiClient    *http.Client
	mediaClient  *http.Client
	pollInterval time.Duration
	// Tests point media downloads at httptest servers, which the SSRF client refuses.
	skipSSRFCheck bool
}

func NewFalProvider(apiKey string) *FalProvider {
	return &FalProvider{
		apiKey:       strings.TrimSpace(apiKey),
		queueURL:     falDefaultQueueURL,
		apiClient:    &http.Client{Timeout: 60 * time.Second},
		mediaClient:  security.NewSafeClient(5 * time.Minute),
		pollInterval: 5 * time.Second,
	}
}

type falQueueResponse struct {
	RequestID     string `json:"request_id"`
	Status        string `json:"status"`
	StatusURL     string `json:"status_url"`
	ResponseURL   string `json:"response_url"`
	CancelURL     string `json:"cancel_url"`
	QueuePosition *int   `json:"queue_position"`
}

type falVideoResult struct {
	Video struct {
		URL         string `json:"url"`
		ContentType string `json:"content_type"`
	} `json:"video"`
}

func (p *FalProvider) Render(ctx context.Context, request RenderRequest, update func(RenderUpdate)) (RenderOutput, error) {
	if p.apiKey == "" {
		return RenderOutput{}, &ProviderError{Code: "MODEL_UNAVAILABLE", Message: "fal API key is not configured.", Retryable: false}
	}
	state := request.ProviderState
	if state["request_id"] == "" {
		submitted, err := p.submit(ctx, request)
		if err != nil {
			return RenderOutput{}, err
		}
		state = submitted
		update(RenderUpdate{ProviderJobID: state["request_id"], ProviderState: state, Progress: 10, Message: "fal đã nhận job, đang chờ hàng đợi."})
	}
	if err := p.waitCompleted(ctx, state, update); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			p.cancel(state["cancel_url"])
		}
		return RenderOutput{}, err
	}
	update(RenderUpdate{Progress: 90, Message: "fal đã render xong, đang tải video về."})
	videoURL, err := p.result(ctx, state["response_url"])
	if err != nil {
		return RenderOutput{}, err
	}
	return p.download(ctx, videoURL, request.OutputPath)
}

func (p *FalProvider) submit(ctx context.Context, request RenderRequest) (map[string]string, error) {
	if request.Model.ProviderModelID == nil || *request.Model.ProviderModelID == "" {
		return nil, &ProviderError{Code: "MODEL_UNAVAILABLE", Message: "Model has no fal endpoint.", Retryable: false}
	}
	imageURL := stringValue(request.Job.Request.Inputs.ImageURL)
	if imageURL == "" {
		return nil, &ProviderError{Code: "INVALID_REQUEST", Message: "Cảnh chưa có ảnh đầu vào cho image-to-video.", Retryable: false}
	}
	// Send bytes, not the URL: Drupal's signed URL may expire before fal fetches it.
	image, err := p.fetchImageDataURI(ctx, imageURL)
	if err != nil {
		return nil, err
	}
	input := falWanImageToVideoInput(request.Job.Request, image)
	body, err := json.Marshal(input)
	if err != nil {
		return nil, &ProviderError{Code: "INVALID_REQUEST", Message: "Could not encode the fal request.", Retryable: false}
	}
	endpoint := strings.TrimRight(p.queueURL, "/") + "/" + strings.Trim(*request.Model.ProviderModelID, "/")
	var queued falQueueResponse
	if err := p.callJSON(ctx, http.MethodPost, endpoint, body, &queued); err != nil {
		return nil, err
	}
	if queued.RequestID == "" || !p.trustedQueueURL(queued.StatusURL) || !p.trustedQueueURL(queued.ResponseURL) {
		return nil, &ProviderError{Code: "PROVIDER_ERROR", Message: "fal returned an invalid queue response.", Retryable: false}
	}
	state := map[string]string{
		"request_id":   queued.RequestID,
		"status_url":   queued.StatusURL,
		"response_url": queued.ResponseURL,
	}
	if p.trustedQueueURL(queued.CancelURL) {
		state["cancel_url"] = queued.CancelURL
	}
	return state, nil
}

func falWanImageToVideoInput(request JobRequest, imageDataURI string) map[string]any {
	frames := request.Output.DurationMS*falWanFramesPerSec/1000 + 1
	frames = min(max(frames, falWanMinFrames), falWanMaxFrames)
	prompt := strings.TrimSpace(stringValue(request.Inputs.Prompt))
	if prompt == "" {
		prompt = falDefaultMotionPrompt
	}
	input := map[string]any{
		"prompt":       prompt,
		"image_url":    imageDataURI,
		"resolution":   request.Output.Resolution,
		"aspect_ratio": request.Output.AspectRatio,
		"num_frames":   frames,
	}
	if negative := strings.TrimSpace(stringValue(request.Inputs.NegativePrompt)); negative != "" {
		input["negative_prompt"] = negative
	}
	// JSON numbers decode as float64 from Drupal's provider_options.
	if seed, ok := request.ProviderOptions["seed"].(float64); ok && seed >= 0 {
		input["seed"] = int64(seed)
	}
	if expand, ok := request.ProviderOptions["enable_prompt_expansion"].(bool); ok {
		input["enable_prompt_expansion"] = expand
	}
	return input
}

func (p *FalProvider) waitCompleted(ctx context.Context, state map[string]string, update func(RenderUpdate)) error {
	failures := 0
	lastStatus := ""
	for {
		var status falQueueResponse
		err := p.callJSON(ctx, http.MethodGet, state["status_url"], nil, &status)
		switch {
		case ctx.Err() != nil:
			return ctx.Err()
		case err != nil:
			if providerErr := asProviderError(err); !providerErr.Retryable {
				return err
			}
			failures++
			if failures >= falMaxPollFailures {
				return err
			}
		default:
			failures = 0
			if status.Status == falStatusCompleted {
				return nil
			}
			if status.Status != lastStatus {
				lastStatus = status.Status
				switch status.Status {
				case falStatusInProgress:
					update(RenderUpdate{Progress: 40, Message: "fal đang render video."})
				case falStatusInQueue:
					update(RenderUpdate{Progress: 15, Message: "Đang chờ trong hàng đợi fal."})
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(p.pollInterval):
		}
	}
}

func (p *FalProvider) result(ctx context.Context, responseURL string) (string, error) {
	var result falVideoResult
	if err := p.callJSON(ctx, http.MethodGet, responseURL, nil, &result); err != nil {
		return "", err
	}
	if result.Video.URL == "" {
		return "", &ProviderError{Code: "MEDIA_VALIDATION_FAILED", Message: "fal returned no video.", Retryable: false}
	}
	return result.Video.URL, nil
}

func (p *FalProvider) cancel(cancelURL string) {
	if cancelURL == "" {
		return
	}
	// The job context is already done, so cancellation needs its own deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = p.callJSON(ctx, http.MethodPut, cancelURL, nil, nil)
}

func (p *FalProvider) callJSON(ctx context.Context, method, endpoint string, body []byte, target any) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return &ProviderError{Code: "PROVIDER_ERROR", Message: "Could not build the fal request.", Retryable: false}
	}
	request.Header.Set("Authorization", "Key "+p.apiKey)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := p.apiClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &ProviderError{Code: "MODEL_UNAVAILABLE", Message: "fal is unreachable.", Retryable: true}
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return &ProviderError{Code: "MODEL_UNAVAILABLE", Message: "Could not read the fal response.", Retryable: true}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return falHTTPError(response.StatusCode, payload)
	}
	if target == nil {
		return nil
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return &ProviderError{Code: "PROVIDER_ERROR", Message: "fal returned malformed JSON.", Retryable: false}
	}
	return nil
}

func falHTTPError(status int, payload []byte) error {
	message := falErrorDetail(payload)
	if message == "" {
		message = fmt.Sprintf("fal returned HTTP %d.", status)
	}
	lowered := strings.ToLower(string(payload))
	switch {
	// fal answers 403 for an exhausted balance, so it must not read as a key problem.
	case status == http.StatusPaymentRequired || strings.Contains(lowered, "exhausted balance"):
		return &ProviderError{Code: "INSUFFICIENT_PROVIDER_CREDIT", Message: message, Retryable: false}
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return &ProviderError{Code: "PROVIDER_AUTH_FAILED", Message: message, Retryable: false}
	case status == http.StatusTooManyRequests:
		return &ProviderError{Code: "RATE_LIMITED", Message: message, Retryable: true}
	case status >= 500:
		return &ProviderError{Code: "MODEL_UNAVAILABLE", Message: message, Retryable: true}
	case status == http.StatusNotFound:
		return &ProviderError{Code: "JOB_NOT_FOUND", Message: message, Retryable: false}
	case strings.Contains(lowered, "content_policy"):
		return &ProviderError{Code: "CONTENT_REJECTED", Message: message, Retryable: false}
	default:
		return &ProviderError{Code: "VALIDATION_FAILED", Message: message, Retryable: false}
	}
}

func falErrorDetail(payload []byte) string {
	var decoded struct {
		Detail any `json:"detail"`
	}
	if json.Unmarshal(payload, &decoded) != nil || decoded.Detail == nil {
		return ""
	}
	var text string
	switch detail := decoded.Detail.(type) {
	case string:
		text = detail
	default:
		encoded, _ := json.Marshal(detail)
		text = string(encoded)
	}
	if len(text) > 500 {
		text = text[:500]
	}
	return "fal: " + text
}

// The API key is attached to every queue call, so only fal's own queue host may receive it.
func (p *FalProvider) trustedQueueURL(raw string) bool {
	candidate, err := url.Parse(raw)
	if err != nil {
		return false
	}
	base, err := url.Parse(p.queueURL)
	if err != nil {
		return false
	}
	return candidate.Scheme == base.Scheme && candidate.Host == base.Host
}

func (p *FalProvider) openMedia(ctx context.Context, rawURL, accept, failureCode string) (*http.Response, error) {
	if !p.skipSSRFCheck {
		_, pinnedIP, err := security.Validate(rawURL)
		if err != nil {
			return nil, &ProviderError{Code: "INVALID_REQUEST", Message: "Media URL is not allowed.", Retryable: false}
		}
		ctx = security.WithPinnedIP(ctx, pinnedIP)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, &ProviderError{Code: "INVALID_REQUEST", Message: "Media URL is invalid.", Retryable: false}
	}
	request.Header.Set("Accept", accept)
	response, err := p.mediaClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &ProviderError{Code: failureCode, Message: "Could not download media.", Retryable: true}
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, &ProviderError{Code: failureCode, Message: fmt.Sprintf("Media download returned HTTP %d.", response.StatusCode), Retryable: response.StatusCode >= 500}
	}
	return response, nil
}

func (p *FalProvider) fetchImageDataURI(ctx context.Context, imageURL string) (string, error) {
	response, err := p.openMedia(ctx, imageURL, "image/*", "INPUT_DOWNLOAD_FAILED")
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	mediaType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if mediaType != "image/jpeg" && mediaType != "image/png" && mediaType != "image/webp" {
		return "", &ProviderError{Code: "INVALID_REQUEST", Message: "Ảnh đầu vào phải là JPEG, PNG hoặc WebP.", Retryable: false}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, falMaxImageBytes+1))
	if err != nil {
		return "", &ProviderError{Code: "INPUT_DOWNLOAD_FAILED", Message: "Could not read the input image.", Retryable: true}
	}
	if len(data) == 0 || len(data) > falMaxImageBytes {
		return "", &ProviderError{Code: "INVALID_REQUEST", Message: "Ảnh đầu vào rỗng hoặc lớn hơn 10MB.", Retryable: false}
	}
	return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func (p *FalProvider) download(ctx context.Context, videoURL, outputPath string) (RenderOutput, error) {
	response, err := p.openMedia(ctx, videoURL, "video/mp4", "OUTPUT_DOWNLOAD_FAILED")
	if err != nil {
		return RenderOutput{}, err
	}
	defer response.Body.Close()
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o700); err != nil {
		return RenderOutput{}, fmt.Errorf("create video output directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(outputPath), ".output-*.tmp")
	if err != nil {
		return RenderOutput{}, fmt.Errorf("create video output: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(response.Body, falMaxVideoBytes+1))
	closeErr := temporary.Close()
	if copyErr != nil || closeErr != nil {
		return RenderOutput{}, &ProviderError{Code: "OUTPUT_DOWNLOAD_FAILED", Message: "Could not download the rendered video.", Retryable: true}
	}
	if written == 0 || written > falMaxVideoBytes {
		return RenderOutput{}, &ProviderError{Code: "MEDIA_VALIDATION_FAILED", Message: "Rendered video is empty or too large.", Retryable: false}
	}
	if !hasMP4Signature(temporaryName) {
		return RenderOutput{}, &ProviderError{Code: "MEDIA_VALIDATION_FAILED", Message: "fal returned a file that is not an MP4.", Retryable: false}
	}
	if err := os.Rename(temporaryName, outputPath); err != nil {
		return RenderOutput{}, fmt.Errorf("commit video output: %w", err)
	}
	return RenderOutput{MIMEType: "video/mp4", FileSize: int(written), ChecksumSHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func hasMP4Signature(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	header := make([]byte, 12)
	if _, err := io.ReadFull(file, header); err != nil {
		return false
	}
	return string(header[4:8]) == "ftyp"
}
