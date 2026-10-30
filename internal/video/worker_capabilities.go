package video

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	// Drupal hỏi catalog với timeout 8 s, nên hỏi worker không được quá 2 s.
	workerProbeTimeout = 2 * time.Second
	// Đổi image worker thì catalog đúng lại sau tối đa 60 s.
	workerProbeTTL = 60 * time.Second
	// Lỗi chỉ nhớ ngắn: worker đang khởi động lại không nên kẹt [1] cả phút.
	workerProbeFailureTTL = 15 * time.Second
)

// baselineManifestVersions là thứ worker nào cũng dựng được. Worker cũ không
// báo gì, không gọi được hay trả lời lạ đều rơi về đây, để Drupal không gửi
// manifest v2 tới một worker sẽ từ chối nó.
var baselineManifestVersions = []int{1}

// WorkerCapabilities hỏi /health của media worker xem nó dựng được những
// manifest contract_version nào, có nhớ đệm.
type WorkerCapabilities struct {
	url    string
	client *http.Client
	now    func() time.Time

	mu       sync.Mutex
	cached   []int
	expires  time.Time
	hasValue bool
}

// NewWorkerCapabilities dùng cùng biến môi trường với JobService, để catalog
// nói đúng về chính worker mà job sẽ được gửi tới.
func NewWorkerCapabilities() *WorkerCapabilities {
	return NewWorkerCapabilitiesFor(os.Getenv("GOCLAW_MEDIA_WORKER_URL"), nil)
}

func NewWorkerCapabilitiesFor(workerURL string, client *http.Client) *WorkerCapabilities {
	if client == nil {
		client = &http.Client{Timeout: workerProbeTimeout}
	}
	return &WorkerCapabilities{url: strings.TrimRight(strings.TrimSpace(workerURL), "/"), client: client, now: time.Now}
}

// ManifestVersions trả danh sách tăng dần, luôn có 1.
func (c *WorkerCapabilities) ManifestVersions(ctx context.Context) []int {
	if c == nil || c.url == "" {
		return slices.Clone(baselineManifestVersions)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.hasValue && c.now().Before(c.expires) {
		return slices.Clone(c.cached)
	}
	versions, ok := c.fetch(ctx)
	ttl := workerProbeTTL
	if !ok {
		versions, ttl = slices.Clone(baselineManifestVersions), workerProbeFailureTTL
	}
	c.cached, c.expires, c.hasValue = versions, c.now().Add(ttl), true
	return slices.Clone(versions)
}

func (c *WorkerCapabilities) fetch(ctx context.Context) ([]int, bool) {
	ctx, cancel := context.WithTimeout(ctx, workerProbeTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url+"/health", nil)
	if err != nil {
		return nil, false
	}
	response, err := c.client.Do(request)
	if err != nil {
		return nil, false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, false
	}
	var health struct {
		OK                       bool  `json:"ok"`
		AssembleManifestVersions []int `json:"assemble_manifest_versions"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&health); err != nil || !health.OK {
		return nil, false
	}
	// Worker cũ chỉ có contract_version: 1, không có danh sách => chỉ v1.
	versions := []int{1}
	for _, version := range health.AssembleManifestVersions {
		if version == 2 && !slices.Contains(versions, version) {
			versions = append(versions, version)
		}
	}
	return versions, true
}
