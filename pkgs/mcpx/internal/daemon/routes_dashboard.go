package daemon

import (
	"net/http"

	"github.com/dezren39/mcpx/internal/dashboard"
	"github.com/dezren39/mcpx/internal/logstore"
)

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(dashboard.HTML(s.version)))
}

func (s *Server) handleMetricsTokens(w http.ResponseWriter, r *http.Request) {
	if s.tokenStore == nil {
		writeJSON(w, 200, logstore.TokenSummary{})
		return
	}
	sum, err := s.tokenStore.Summary()
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, sum)
}
