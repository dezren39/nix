package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleBatchEmpty(t *testing.T) {
	s := &Server{}
	body := []byte(`{"calls":[]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/batch", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleBatch(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var res map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to parse json response: %v", err)
	}
	results, ok := res["results"].([]any)
	if !ok || len(results) != 0 {
		t.Fatalf("expected empty results slice, got: %v", res)
	}
}

func TestHandleBatchValidation(t *testing.T) {
	s := &Server{}
	// Missing server and tool
	body := []byte(`{"calls":[{"args":{}}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/batch", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleBatch(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var res struct {
		Results []struct {
			Index   int    `json:"index"`
			Status  string `json:"status"`
			Error   string `json:"error"`
			IsError bool   `json:"isError"`
		} `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to parse json response: %v", err)
	}
	if len(res.Results) != 1 || !res.Results[0].IsError {
		t.Fatalf("expected error result item, got: %+v", res)
	}
}
