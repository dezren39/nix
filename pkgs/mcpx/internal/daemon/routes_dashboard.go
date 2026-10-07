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

func (s *Server) handleMetricsTools(w http.ResponseWriter, r *http.Request) {
	if s.set == nil {
		writeJSON(w, 200, map[string]any{
			"calls":        []any{},
			"errors":       []any{},
			"correlations": []any{},
			"timeline":     []any{},
		})
		return
	}
	st, err := s.openStore()
	if err != nil {
		writeJSON(w, 200, map[string]any{
			"calls":        []any{},
			"errors":       []any{},
			"correlations": []any{},
			"timeline":     []any{},
		})
		return
	}
	defer st.Close()

	calls, _ := st.Calls(logstore.Query{})
	errors, _ := st.Errors(logstore.Query{})
	correlations, _ := st.Correlations(logstore.Query{}, 20)
	timeline, _ := st.Timeline(logstore.Query{}, 24)

	if calls == nil {
		calls = []logstore.CallStat{}
	}
	if errors == nil {
		errors = []logstore.ErrorStat{}
	}
	if correlations == nil {
		correlations = []logstore.ToolCorrelation{}
	}
	if timeline == nil {
		timeline = []logstore.ToolTimelinePoint{}
	}

	writeJSON(w, 200, map[string]any{
		"calls":        calls,
		"errors":       errors,
		"correlations": correlations,
		"timeline":     timeline,
	})
}
