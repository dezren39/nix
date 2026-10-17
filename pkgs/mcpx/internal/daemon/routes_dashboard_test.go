package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/logstore"
)

func TestHandleDashboard(t *testing.T) {
	s := &Server{version: "1.2.3"}
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	w := httptest.NewRecorder()

	s.handleDashboard(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "mcpx Dashboard") {
		t.Errorf("expected dashboard title in body")
	}
	if !strings.Contains(body, "1.2.3") {
		t.Errorf("expected version 1.2.3 in dashboard badge")
	}
}

func TestHandleMetricsTokens(t *testing.T) {
	tmpDir := t.TempDir()
	ts, err := logstore.NewTokenStore(filepath.Join(tmpDir, "tokens.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ts.Close()

	_ = ts.RecordTurn(logstore.TokenRecord{
		ID:             "turn-1",
		BaselineTokens: 1000,
		InjectedTokens: 300,
	})

	s := &Server{tokenStore: ts}
	req := httptest.NewRequest(http.MethodGet, "/v1/metrics/tokens", nil)
	w := httptest.NewRecorder()

	s.handleMetricsTokens(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var sum logstore.TokenSummary
	if err := json.Unmarshal(w.Body.Bytes(), &sum); err != nil {
		t.Fatal(err)
	}
	if sum.TotalTurns != 1 {
		t.Errorf("expected 1 turn, got %d", sum.TotalTurns)
	}
	if sum.TotalTokensSaved != 700 {
		t.Errorf("expected 700 tokens saved, got %d", sum.TotalTokensSaved)
	}
}

func TestHandleMetricsTools(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/v1/metrics/tools", nil)
	w := httptest.NewRecorder()

	s.handleMetricsTools(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Calls        []logstore.CallStat          `json:"calls"`
		Errors       []logstore.ErrorStat         `json:"errors"`
		Correlations []logstore.ToolCorrelation   `json:"correlations"`
		Timeline     []logstore.ToolTimelinePoint `json:"timeline"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Calls == nil || resp.Errors == nil || resp.Correlations == nil || resp.Timeline == nil {
		t.Errorf("expected non-nil slices in response")
	}
}
