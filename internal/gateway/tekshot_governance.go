package gateway

import (
	"net/http"

	tekshottools "github.com/nextlevelbuilder/goclaw/internal/tekshot"
)

// handleTekshotGovernance trả bộ luật Kim chỉ nam mà các prompt tekshot đang dùng.
func (s *Server) handleTekshotGovernance(w http.ResponseWriter, r *http.Request) {
	if !s.hasGatewayBearer(r) {
		writeGatewayJSON(w, http.StatusUnauthorized, map[string]any{
			"ok":      false,
			"message": "valid gateway token required",
		})
		return
	}
	if r.Method != http.MethodGet {
		writeGatewayJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"ok":      false,
			"message": "method not allowed",
		})
		return
	}
	writeGatewayJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"governance": tekshottools.BuildGovernanceCatalog(),
	})
}
