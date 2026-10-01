package main

import (
	"encoding/json"
	"testing"

	"github.com/dezren39/mcpx/internal/mcpheaders"
)

// The official suite's http-custom-header-server-validation scenario looks
// for a listed tool with a string-typed x-mcp-header property and reports
// every SEP-2243 custom-header check as untestable when there is none. This
// fixture is the upstream conformance.sh puts behind mcpx, so it has to
// provide one, and the annotation has to be one mcpx accepts.
func TestListsAStringXMcpHeaderTool(t *testing.T) {
	s := &server{running: map[string]chan struct{}{}}
	id := int64(1)
	b, err := json.Marshal(s.handle(frame{ID: &id, Method: "tools/list"}))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Result struct {
			Tools []struct {
				Name        string          `json:"name"`
				InputSchema json.RawMessage `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Result.Tools) == 0 {
		t.Fatalf("tools/list returned no tools: %s", b)
	}
	for _, tl := range out.Result.Tools {
		params, err := mcpheaders.ToolParams(tl.InputSchema)
		if err != nil {
			t.Errorf("%s: invalid x-mcp-header annotation: %v", tl.Name, err)
			continue
		}
		var schema struct {
			Properties map[string]struct {
				Type string `json:"type"`
			} `json:"properties"`
		}
		_ = json.Unmarshal(tl.InputSchema, &schema)
		for _, p := range params {
			if len(p.Path) == 1 && schema.Properties[p.Path[0]].Type == "string" {
				return
			}
		}
	}
	t.Fatal("no tool has a top-level string property annotated with x-mcp-header")
}
