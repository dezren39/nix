package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleSearchSemantic(t *testing.T) {
	reg := &Registry{
		testTools: []ToolInfo{
			{Namespace: "chrome_devtools", Tool: "click", Function: "chrome_devtools.click", Description: "Click an element on the active page"},
			{Namespace: "chrome_devtools", Tool: "take_snapshot", Function: "chrome_devtools.take_snapshot", Description: "Inspect web page contents and DOM elements"},
			{Namespace: "git", Tool: "commit", Function: "git.commit", Description: "Record changes to the repository"},
		},
	}
	s := &Server{reg: reg}

	req := httptest.NewRequest(http.MethodGet, "/v1/search?q=inspect+web+page+contents&semantic=true", nil)
	w := httptest.NewRecorder()

	s.handleSearch(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var results []ToolInfo
	if err := json.Unmarshal(w.Body.Bytes(), &results); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(results) == 0 {
		t.Fatalf("expected results, got none")
	}
	if results[0].Tool != "take_snapshot" {
		t.Fatalf("expected top result to be take_snapshot, got %s", results[0].Tool)
	}
}
