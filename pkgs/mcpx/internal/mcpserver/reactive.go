package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/dezren39/mcpx/internal/catalog"
)

// reactiveSurface builds the dynamic tool surface for Mode 2 reactive discovery.
func (s *Server) reactiveSurface(ctx context.Context) ([]Tool, error) {
	var out []Tool
	// 1. Core meta tools
	out = append(out, Tool{
		Name: "request_tools",
		Description: "Search for and activate additional tools into the active tool set matching a natural language query.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"query": {"type": "string", "description": "Natural language query describing tools needed"},
				"limit": {"type": "integer", "description": "Maximum number of tools to activate"}
			},
			"required": ["query"]
		}`),
	})
	out = append(out, Tool{
		Name: "batch_call",
		Description: "Execute multiple tool calls in a single turn.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"calls": {
					"type": "array",
					"items": {
						"type": "object",
						"properties": {
							"tool": {"type": "string"},
							"args": {"type": "object"}
						},
						"required": ["tool"]
					}
				},
				"stopOnError": {"type": "boolean"}
			},
			"required": ["calls"]
		}`),
	})

	all, _ := s.allAvailableTools(ctx)
	allMap := make(map[string]Tool, len(all))
	for _, t := range all {
		allMap[t.Name] = t
	}

	// 2. Pinned tools
	for _, p := range s.PinnedTools {
		if t, ok := allMap[p]; ok {
			out = append(out, t)
		}
	}

	// 3. Retained working-set tools
	if s.ReactiveWorkingSet != nil {
		for _, r := range s.ReactiveWorkingSet.List() {
			if t, ok := allMap[r.Tool]; ok {
				out = append(out, t)
			}
		}
	}
	return out, nil
}

// executeRequestTools handles request_tools invocations.
func (s *Server) executeRequestTools(ctx context.Context, raw json.RawMessage) (string, error) {
	var p struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &p)
	}
	if p.Query == "" {
		return "", errors.New("request_tools requires a query parameter")
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 5
	}

	var found []Tool
	if s.SearchTools != nil {
		var err error
		found, err = s.SearchTools(ctx, p.Query)
		if err != nil {
			return "", err
		}
	} else {
		all, err := s.allAvailableTools(ctx)
		if err != nil {
			return "", err
		}
		queryVec := catalog.Embed(p.Query)
		type scoredTool struct {
			tool Tool
			sim  float32
		}
		var scored []scoredTool
		for _, t := range all {
			if t.Name == "request_tools" || t.Name == "batch_call" {
				continue
			}
			tVec := catalog.Embed(t.Name + " " + t.Description)
			sim := queryVec.CosineSimilarity(tVec)
			scored = append(scored, scoredTool{tool: t, sim: sim})
		}
		sort.SliceStable(scored, func(i, j int) bool {
			return scored[i].sim > scored[j].sim
		})
		for i := 0; i < len(scored) && i < limit; i++ {
			found = append(found, scored[i].tool)
		}
	}

	if s.ReactiveWorkingSet == nil {
		s.ReactiveWorkingSet = catalog.NewWorkingSet(24, 2*time.Hour, s.PinnedTools)
	}

	var activated []string
	for _, t := range found {
		s.ReactiveWorkingSet.Add("", t.Name, catalog.OriginSticky)
		activated = append(activated, t.Name)
	}

	// Push notifications/tools/list_changed
	s.NotifyToolsListChanged()

	type toolDesc struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"inputSchema,omitempty"`
	}
	var descList []toolDesc
	for _, t := range found {
		descList = append(descList, toolDesc{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}

	res, _ := json.Marshal(map[string]any{
		"message":        fmt.Sprintf("Activated %d tool(s) into working set", len(activated)),
		"activatedTools": activated,
		"tools":          descList,
	})
	return string(res), nil
}

// NotifyToolsListChanged broadcasts a notifications/tools/list_changed frame.
func (s *Server) NotifyToolsListChanged() {
	s.mu.Lock()
	conn := s.def
	s.mu.Unlock()
	if conn != nil {
		conn.push("notifications/tools/list_changed", map[string]any{})
	}

	s.sessMu.Lock()
	for _, sc := range s.sessions {
		if sc != nil {
			sc.push("notifications/tools/list_changed", map[string]any{})
		}
	}
	s.sessMu.Unlock()
}
