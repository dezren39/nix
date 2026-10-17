package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dezren39/mcpx/internal/logstore"
)

func TestHandleChatCompletionsSemanticToolInjection(t *testing.T) {
	tmpDir := t.TempDir()
	ts, err := logstore.NewTokenStore(filepath.Join(tmpDir, "tokens.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ts.Close()

	reg := &Registry{
		testTools: []ToolInfo{
			{Namespace: "browser", Tool: "click", Function: "browser.click", Description: "Click button or link on web page"},
			{Namespace: "browser", Tool: "navigate", Function: "browser.navigate", Description: "Navigate to a URL in the browser"},
			{Namespace: "git", Tool: "status", Function: "git.status", Description: "Show working tree status"},
			{Namespace: "git", Tool: "commit", Function: "git.commit", Description: "Commit changes to git repository"},
		},
	}
	s := &Server{reg: reg, tokenStore: ts}

	reqBody := []byte(`{
		"model": "gpt-4o",
		"messages": [
			{"role": "user", "content": "Please click the submit button in the web browser"}
		]
	}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(reqBody))
	w := httptest.NewRecorder()

	s.handleChatCompletions(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Choices []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Mcpx struct {
			ToolsInjected  int      `json:"tools_injected"`
			BaselineTokens int      `json:"baseline_tokens"`
			TokensSaved    int      `json:"tokens_saved"`
			ActiveTools    []string `json:"active_tools"`
		} `json:"mcpx"`
	}

	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if resp.Mcpx.ToolsInjected == 0 {
		t.Errorf("expected tools to be injected, got 0")
	}
	if len(resp.Mcpx.ActiveTools) == 0 {
		t.Errorf("expected active tools in mcpx telemetry")
	}

	// Verify token store recorded turn
	sum, err := ts.Summary()
	if err != nil {
		t.Fatal(err)
	}
	if sum.TotalTurns != 1 {
		t.Errorf("expected 1 turn in token store, got %d", sum.TotalTurns)
	}
}

func TestHandleChatCompletionsUpstreamProxy(t *testing.T) {
	// Create mock upstream LLM server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected upstream path: %s", r.URL.Path)
		}
		var receivedReq chatCompletionReq
		_ = json.NewDecoder(r.Body).Decode(&receivedReq)

		// Upstream should have received injected tools
		foundBrowserClick := false
		for _, tool := range receivedReq.Tools {
			if tool.Function.Name == "browser__click" {
				foundBrowserClick = true
				break
			}
		}
		if !foundBrowserClick {
			t.Errorf("upstream did not receive injected browser__click tool")
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-mock",
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "mock upstream answer"}},
			},
		})
	}))
	defer upstream.Close()

	reg := &Registry{
		testTools: []ToolInfo{
			{Namespace: "browser", Tool: "click", Function: "browser.click", Description: "Click button or link on web page"},
		},
	}
	s := &Server{reg: reg}

	reqBody := []byte(`{
		"model": "gpt-4o",
		"messages": [
			{"role": "user", "content": "click the button"}
		]
	}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(reqBody))
	req.Header.Set("X-Upstream-Url", upstream.URL)
	w := httptest.NewRecorder()

	s.handleChatCompletions(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["id"] != "chatcmpl-mock" {
		t.Errorf("expected chatcmpl-mock, got %v", resp["id"])
	}
}
