package daemon

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/dezren39/mcpx/internal/logstore"
)

type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

type openAIFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type chatCompletionReq struct {
	Model       string          `json:"model"`
	Messages    []chatMessage   `json:"messages"`
	Tools       []openAITool    `json:"tools,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	MaxTokens   *int            `json:"max_tokens,omitempty"`
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	defer r.Body.Close()

	var req chatCompletionReq
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid json request: %w", err))
		return
	}

	// 1. Extract natural language user intent from the latest user message
	query := extractUserQuery(req.Messages)

	// 2. Discover relevant tools semantically
	maxTools := 5
	if s.set != nil {
		if m := s.set.Int("proxy.maxTools"); m > 0 {
			maxTools = m
		}
	}

	var discovered []openAITool
	existingToolNames := make(map[string]bool)
	for _, t := range req.Tools {
		existingToolNames[t.Function.Name] = true
	}

	matchedTools := s.reg.SearchSemantic(query, maxTools, true)
	for _, mt := range matchedTools {
		cleanName := strings.ReplaceAll(mt.Function, ".", "__")
		if existingToolNames[cleanName] {
			continue
		}
		existingToolNames[cleanName] = true
		paramBytes := mt.InputSchema
		if len(paramBytes) == 0 {
			paramBytes = json.RawMessage(`{"type":"object"}`)
		}
		discovered = append(discovered, openAITool{
			Type: "function",
			Function: openAIFunction{
				Name:        cleanName,
				Description: mt.Description,
				Parameters:  paramBytes,
			},
		})
	}

	// 3. Calculate baseline vs injected tool tokens
	allTools := s.reg.Tools(nil)
	var baselineTokens int
	for _, t := range allTools {
		b, _ := json.Marshal(t)
		baselineTokens += max(1, len(b)/4)
	}

	req.Tools = append(req.Tools, discovered...)
	var injectedTokens int
	for _, t := range req.Tools {
		b, _ := json.Marshal(t)
		injectedTokens += max(1, len(b)/4)
	}
	if baselineTokens < injectedTokens {
		baselineTokens = injectedTokens
	}
	tokensSaved := baselineTokens - injectedTokens
	if tokensSaved < 0 {
		tokensSaved = 0
	}

	promptTokens := len(bodyBytes) / 4

	// Record token economics
	turnID := randomHex(12)
	if s.tokenStore != nil {
		_ = s.tokenStore.RecordTurn(logstore.TokenRecord{
			ID:               turnID,
			Timestamp:        time.Now(),
			PromptTokens:     promptTokens,
			CompletionTokens: 0,
			ToolsInjected:    len(discovered),
			BaselineTokens:   baselineTokens,
			InjectedTokens:   injectedTokens,
			TokensSaved:      tokensSaved,
		})
	}

	// 4. Determine upstream endpoint
	upstreamURL := ""
	if s.set != nil {
		upstreamURL = s.set.String("proxy.upstreamUrl")
	}
	if upstreamURL == "" {
		upstreamURL = os.Getenv("OPENAI_BASE_URL")
	}
	if h := r.Header.Get("X-Upstream-Url"); h != "" {
		upstreamURL = h
	}

	apiKey := ""
	if s.set != nil {
		apiKey = s.set.String("proxy.apiKey")
	}
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if authHdr := r.Header.Get("Authorization"); authHdr != "" {
		apiKey = strings.TrimPrefix(authHdr, "Bearer ")
	}

	toolNames := make([]string, 0, len(req.Tools))
	for _, t := range req.Tools {
		toolNames = append(toolNames, t.Function.Name)
	}

	// 5. Proxy to upstream if configured
	if upstreamURL != "" {
		s.proxyToUpstream(w, r, upstreamURL, apiKey, req, turnID, query, toolNames)
		return
	}

	// 6. If no upstream URL configured, return OpenAI-compatible intercept response

	resp := map[string]any{
		"id":      "chatcmpl-" + turnID,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   req.Model,
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": fmt.Sprintf("mcpx reverse proxy: semantically injected %d tools for query %q.", len(discovered), query),
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     promptTokens,
			"completion_tokens": 16,
			"total_tokens":      promptTokens + 16,
		},
		"mcpx": map[string]any{
			"turn_id":         turnID,
			"tools_injected":  len(discovered),
			"baseline_tokens": baselineTokens,
			"injected_tokens": injectedTokens,
			"tokens_saved":    tokensSaved,
			"active_tools":    toolNames,
		},
	}

	if fb := s.reg.FeedbackStore(); fb != nil {
		outStr := fmt.Sprintf("mcpx reverse proxy: semantically injected %d tools for query %q.", len(discovered), query)
		_ = fb.RecordInteraction(logstore.InteractionRecord{
			TraceID:   turnID,
			Timestamp: time.Now(),
			Source:    "proxy",
			Input:     query,
			Output:    outStr,
			ToolsUsed: toolNames,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) proxyToUpstream(w http.ResponseWriter, r *http.Request, upstreamURL, apiKey string, req chatCompletionReq, turnID, query string, toolNames []string) {
	targetURL := strings.TrimRight(upstreamURL, "/")
	if !strings.HasSuffix(targetURL, "/chat/completions") {
		targetURL += "/chat/completions"
	}

	payload, err := json.Marshal(req)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	upReq, err := http.NewRequestWithContext(r.Context(), "POST", targetURL, bytes.NewReader(payload))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	upReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		upReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(upReq)
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Errorf("upstream error: %w", err))
		return
	}
	defer resp.Body.Close()

	var buf bytes.Buffer
	rdr := io.TeeReader(resp.Body, &buf)

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("X-Mcpx-Trace-Id", turnID)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, rdr)

	if fb := s.reg.FeedbackStore(); fb != nil {
		_ = fb.RecordInteraction(logstore.InteractionRecord{
			TraceID:   turnID,
			Timestamp: time.Now(),
			Source:    "proxy",
			Input:     query,
			Output:    buf.String(),
			ToolsUsed: toolNames,
		})
	}
}

func extractUserQuery(msgs []chatMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Role == "user" {
			switch v := m.Content.(type) {
			case string:
				return v
			case []any:
				var sb strings.Builder
				for _, part := range v {
					if pMap, ok := part.(map[string]any); ok {
						if pMap["type"] == "text" {
							if txt, ok := pMap["text"].(string); ok {
								sb.WriteString(txt)
								sb.WriteString(" ")
							}
						}
					}
				}
				return strings.TrimSpace(sb.String())
			}
		}
	}
	return ""
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
