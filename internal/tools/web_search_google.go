package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// --- Google Programmable Search Engine (CSE) Provider ---
//
// Why this exists next to the other providers: the keyless DuckDuckGo HTML
// endpoint is a scraping surface, not an API — it answers one query then
// serves an anomaly challenge to the same IP for a long while (measured: 1 ok,
// then 20 blocked probes over 10 minutes). Any agent that searches in bursts
// is unusable on it. CSE is a first-party Google API: 100 queries/day free,
// $5/1000 after, and Google's index — the widest coverage for long-tail
// Vietnamese product pages, which is what price lookups actually need.
//
// CSE needs TWO values (API key + search engine id "cx") while the chain only
// carries one secret per provider. Rather than reshape the config for every
// provider, the key accepts "APIKEY:CX"; a plain key still works when cx comes
// from settings or GOCLAW_GOOGLE_CSE_CX. Split on the FIRST colon only — the
// legacy cx format itself contains one ("0123...789:abcdef").
type googleCSESearchProvider struct {
	apiKey     string
	cx         string
	maxResults int
	client     *http.Client
}

func newGoogleCSESearchProvider(apiKey string, maxResults int) *googleCSESearchProvider {
	key, cx := splitGoogleCSEKey(apiKey)
	if cx == "" {
		cx = strings.TrimSpace(os.Getenv("GOCLAW_GOOGLE_CSE_CX"))
	}
	return &googleCSESearchProvider{
		apiKey:     key,
		cx:         cx,
		maxResults: normalizeProviderMaxResults(maxResults),
		client:     &http.Client{Timeout: time.Duration(searchTimeoutSeconds) * time.Second},
	}
}

// splitGoogleCSEKey accepts "APIKEY:CX" or a bare "APIKEY".
func splitGoogleCSEKey(raw string) (string, string) {
	key, cx, found := strings.Cut(strings.TrimSpace(raw), ":")
	if !found {
		return strings.TrimSpace(key), ""
	}
	return strings.TrimSpace(key), strings.TrimSpace(cx)
}

func (p *googleCSESearchProvider) Name() string { return searchProviderGoogleCSE }

func (p *googleCSESearchProvider) Search(ctx context.Context, params searchParams) ([]searchResult, error) {
	if p.cx == "" {
		return nil, fmt.Errorf("google cse: missing search engine id (set the key as \"APIKEY:CX\" or GOCLAW_GOOGLE_CSE_CX)")
	}

	query := url.Values{}
	query.Set("key", p.apiKey)
	query.Set("cx", p.cx)
	query.Set("q", params.Query)
	// The API caps num at 10 per call and rejects anything higher outright.
	query.Set("num", fmt.Sprintf("%d", min(clampProviderResultCount(params.Count, p.maxResults), 10)))
	// Vietnamese suppliers are the target: without these the same query drifts
	// to English pages and international distributors that quote in USD.
	query.Set("hl", "vi")
	query.Set("gl", "vn")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, googleCSEEndpoint+"?"+query.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", webSearchUserAgent)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// 429 here means the daily free quota is spent, not a transient block —
		// surface it verbatim so the caller can tell "out of quota" from "down".
		return nil, fmt.Errorf("google cse API returned %d: %s", resp.StatusCode, truncateStr(string(body), 200))
	}

	var cseResp struct {
		Items []struct {
			Title   string `json:"title"`
			Link    string `json:"link"`
			Snippet string `json:"snippet"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &cseResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	// No items is a legitimate empty result, not an error: the chain must not
	// fall through to a weaker provider just because a query found nothing.
	results := make([]searchResult, 0, len(cseResp.Items))
	for _, item := range cseResp.Items {
		results = append(results, searchResult{
			Title:       coalesceSearchText(item.Title, item.Link, "Untitled"),
			URL:         item.Link,
			Description: truncateStr(item.Snippet, 240),
		})
	}
	return results, nil
}
