package gateway

import (
	"net/http"
	"net/url"
	"os"
	"strings"
)

// Chuyển tiếp callback OAuth của Google về đúng site Tekshot Studio.
//
// Google chỉ cho khai một redirect URI cố định, mà site Tekshot có thể chạy ở
// nhiều nơi (localhost khi dev, domain thật khi chạy). Gateway nhận callback rồi
// đá tiếp về địa chỉ khai trong TEKSHOT_GOOGLE_OAUTH_CALLBACK_TARGET.
func (s *Server) handleTekshotGoogleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeGatewayJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"ok":      false,
			"message": "method not allowed",
		})
		return
	}

	targetRaw := strings.TrimSpace(os.Getenv("TEKSHOT_GOOGLE_OAUTH_CALLBACK_TARGET"))
	if targetRaw == "" {
		writeGatewayJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok":      false,
			"message": "TEKSHOT_GOOGLE_OAUTH_CALLBACK_TARGET is not configured",
		})
		return
	}

	target, err := url.Parse(targetRaw)
	if err != nil || target.Scheme == "" || target.Host == "" {
		writeGatewayJSON(w, http.StatusInternalServerError, map[string]any{
			"ok":      false,
			"message": "TEKSHOT_GOOGLE_OAUTH_CALLBACK_TARGET must be an absolute URL",
		})
		return
	}

	if !isAllowedTekshotOAuthCallbackTarget(target) {
		writeGatewayJSON(w, http.StatusForbidden, map[string]any{
			"ok":      false,
			"message": "TEKSHOT_GOOGLE_OAUTH_CALLBACK_TARGET is not allowed",
		})
		return
	}

	target.RawQuery = r.URL.RawQuery
	http.Redirect(w, r, target.String(), http.StatusFound)
}

// http:// chỉ được phép về máy phát triển; ngoài ra bắt buộc https.
func isAllowedTekshotOAuthCallbackTarget(target *url.URL) bool {
	host := strings.ToLower(target.Hostname())
	if target.Scheme == "https" {
		return true
	}
	if target.Scheme != "http" {
		return false
	}
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "tekshot.localhost"
}
