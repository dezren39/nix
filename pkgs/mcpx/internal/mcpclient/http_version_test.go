package mcpclient

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/mcpheaders"
)

func TestTheVersionHeaderFollowsTheFrame(t *testing.T) {
	modern := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}}`
	legacy := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	notif := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	header := func(tr *HTTPTransport, frame string) http.Header {
		req, _ := http.NewRequest(http.MethodPost, "http://x", nil)
		tr.setHeadersFor(req, []byte(frame))
		return req.Header
	}

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#protocol-version-header
	t.Run("2026-07-28/transport/protocol-version-header-matches-meta", func(t *testing.T) {
		tr := &HTTPTransport{negotiated: "2025-06-18"}
		if got := header(tr, modern).Get("MCP-Protocol-Version"); got != "2026-07-28" {
			t.Fatalf("header %q, want 2026-07-28", got)
		}
	})

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#protocol-version-header
	t.Run("2025-11-25/transport/protocol-version-header-is-negotiated", func(t *testing.T) {
		for _, v := range []string{"2025-06-18", "2025-11-25"} {
			tr := &HTTPTransport{}
			tr.setNegotiated(v)
			for _, f := range []string{legacy, notif, ""} {
				if got := header(tr, f).Get("MCP-Protocol-Version"); got != v {
					t.Errorf("negotiated %s, frame %q: header %q", v, f, got)
				}
			}
		}
	})

	// The header was defined in 2025-06-18. Before initialize answers
	// nothing is negotiated, and a 2025-03-26 server never defined it.
	t.Run("2025-06-18/transport/no-protocol-version-header-before-negotiation-or-before-2025-06-18", func(t *testing.T) {
		for _, v := range []string{"", "2024-11-05", "2025-03-26"} {
			tr := &HTTPTransport{negotiated: v}
			if got := header(tr, legacy).Get("MCP-Protocol-Version"); got != "" {
				t.Errorf("negotiated %q: header %q, want none", v, got)
			}
		}
	})

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#standard-request-headers
	t.Run("2026-07-28/transport/standard-headers-mcp-method-and-mcp-name", func(t *testing.T) {
		meta := `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}`
		for frame, want := range map[string][2]string{
			`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{` + meta + `}}`:                         {"tools/list", ""},
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_weather",` + meta + `}}`:    {"tools/call", "get_weather"},
			`{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"p",` + meta + `}}`:             {"prompts/get", "p"},
			`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"file:///a b",` + meta + `}}`: {"resources/read", "file:///a b"},
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"天気",` + meta + `}}`:             {"tools/call", "=?base64?5aSp5rCX?="},
		} {
			h := header(&HTTPTransport{}, frame)
			if got := h.Get("Mcp-Method"); got != want[0] {
				t.Errorf("%s: Mcp-Method %q, want %q", frame, got, want[0])
			}
			if got := h.Get("Mcp-Name"); got != want[1] {
				t.Errorf("%s: Mcp-Name %q, want %q", frame, got, want[1])
			}
		}
		if h := header(&HTTPTransport{}, legacy); h.Get("Mcp-Method") != "" {
			t.Errorf("a legacy frame got Mcp-Method %q", h.Get("Mcp-Method"))
		}
	})

	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#earlier-streamable-http-revisions
	t.Run("2026-07-28/transport/no-session-id-on-modern-requests", func(t *testing.T) {
		tr := &HTTPTransport{sessionID: "s1"}
		if got := header(tr, modern).Get("Mcp-Session-Id"); got != "" {
			t.Errorf("modern frame carried Mcp-Session-Id %q", got)
		}
		if got := header(tr, legacy).Get("Mcp-Session-Id"); got != "s1" {
			t.Errorf("legacy frame Mcp-Session-Id %q, want s1", got)
		}
	})
}

// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#value-encoding
func TestHeaderValueEncoding(t *testing.T) {
	t.Run("2026-07-28/transport/value-encoding-examples", func(t *testing.T) {
		for in, want := range map[string]string{
			"us-west1":           "us-west1",
			"Hello, 世界":          "=?base64?SGVsbG8sIOS4lueVjA==?=",
			" padded ":           "=?base64?IHBhZGRlZCA=?=",
			"line1\nline2":       "=?base64?bGluZTEKbGluZTI=?=",
			"=?base64?literal?=": "=?base64?PT9iYXNlNjQ/bGl0ZXJhbD89?=",
			"us west 1":          "us west 1",
			"\tindented":         "=?base64?CWluZGVudGVk?=",
			"":                   "",
		} {
			if got := mcpheaders.Encode(in); got != want {
				t.Errorf("encode(%q) = %q, want %q", in, got, want)
			}
		}
	})
}

// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#schema-extension
func TestToolHeaderAnnotations(t *testing.T) {
	t.Run("2026-07-28/transport/client-rejects-invalid-x-mcp-header", func(t *testing.T) {
		for name, schema := range map[string]string{
			"empty":            `{"type":"object","properties":{"v":{"type":"string","x-mcp-header":""}}}`,
			"object":           `{"type":"object","properties":{"v":{"type":"object","x-mcp-header":"Data"}}}`,
			"array":            `{"type":"object","properties":{"v":{"type":"array","items":{"type":"string"},"x-mcp-header":"Items"}}}`,
			"null":             `{"type":"object","properties":{"v":{"type":"null","x-mcp-header":"Nil"}}}`,
			"number":           `{"type":"object","properties":{"v":{"type":"number","x-mcp-header":"N"}}}`,
			"no type":          `{"type":"object","properties":{"v":{"x-mcp-header":"N"}}}`,
			"dup same case":    `{"type":"object","properties":{"a":{"type":"string","x-mcp-header":"R"},"b":{"type":"string","x-mcp-header":"R"}}}`,
			"dup diff case":    `{"type":"object","properties":{"a":{"type":"string","x-mcp-header":"R"},"b":{"type":"string","x-mcp-header":"r"}}}`,
			"space":            `{"type":"object","properties":{"v":{"type":"string","x-mcp-header":"A B"}}}`,
			"colon":            `{"type":"object","properties":{"v":{"type":"string","x-mcp-header":"A:B"}}}`,
			"non-ascii":        `{"type":"object","properties":{"v":{"type":"string","x-mcp-header":"Régión"}}}`,
			"control":          `{"type":"object","properties":{"v":{"type":"string","x-mcp-header":"A\nB"}}}`,
			"under items":      `{"type":"object","properties":{"l":{"type":"array","items":{"type":"object","properties":{"v":{"type":"string","x-mcp-header":"V"}}}}}}`,
			"under anyOf":      `{"type":"object","anyOf":[{"properties":{"v":{"type":"string","x-mcp-header":"V"}}}]}`,
			"under $defs":      `{"type":"object","$defs":{"x":{"type":"string","x-mcp-header":"V"}}}`,
			"at the root":      `{"type":"string","x-mcp-header":"V"}`,
			"not a string":     `{"type":"object","properties":{"v":{"type":"string","x-mcp-header":7}}}`,
			"under properties": `{"type":"object","properties":{"o":{"type":"object","properties":{"v":{"type":"string"}},"not":{"properties":{"v":{"x-mcp-header":"V","type":"string"}}}}}}`,
		} {
			if _, err := mcpheaders.ToolParams(json.RawMessage(schema)); err == nil {
				t.Errorf("%s: accepted %s", name, schema)
			}
		}
	})
	t.Run("2026-07-28/transport/x-mcp-header-statically-reachable-accepted", func(t *testing.T) {
		hp, err := mcpheaders.ToolParams(json.RawMessage(`{"type":"object","properties":{
			"region":{"type":"string","x-mcp-header":"Region"},
			"o":{"type":"object","properties":{"n":{"type":"integer","x-mcp-header":"Nested"}}},
			"flag":{"type":"boolean","x-mcp-header":"Flag"},
			"q":{"type":"string"}}}`))
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, p := range hp {
			got[p.Name] = strings.Join(p.Path, ".")
		}
		want := map[string]string{"Region": "region", "Nested": "o.n", "Flag": "flag"}
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("%s: path %q, want %q", k, got[k], v)
			}
		}
	})
}

// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#client-behavior
func TestParamHeaders(t *testing.T) {
	params := []mcpheaders.Param{
		{Name: "Region", Path: []string{"region"}}, {Name: "Priority", Path: []string{"priority"}},
		{Name: "Verbose", Path: []string{"verbose"}}, {Name: "Nested", Path: []string{"o", "n"}},
		{Name: "Missing", Path: []string{"missing"}}, {Name: "Empty", Path: []string{"empty"}},
	}
	t.Run("2026-07-28/transport/param-headers-mirror-and-omit-null", func(t *testing.T) {
		h, err := mcpheaders.Values(params, map[string]any{
			"region": "Hello, 世界", "priority": 42, "verbose": nil, "o": map[string]any{"n": -7}, "empty": "",
		})
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{
			"Mcp-Param-Region": "=?base64?SGVsbG8sIOS4lueVjA==?=", "Mcp-Param-Priority": "42",
			"Mcp-Param-Nested": "-7", "Mcp-Param-Empty": "",
		}
		if len(h) != len(want) {
			t.Fatalf("got %v, want %v", h, want)
		}
		for k, v := range want {
			if got, ok := h[k]; !ok || got != v {
				t.Errorf("%s = %q (present %v), want %q", k, got, ok, v)
			}
		}
		h, _ = mcpheaders.Values(params, map[string]any{"verbose": false})
		if h["Mcp-Param-Verbose"] != "false" {
			t.Errorf("boolean false: %q", h["Mcp-Param-Verbose"])
		}
	})
	t.Run("2026-07-28/transport/param-integer-within-safe-range", func(t *testing.T) {
		if _, err := mcpheaders.Values(params, map[string]any{"priority": int64(1) << 53}); err == nil {
			t.Error("2^53 accepted")
		}
		if _, err := mcpheaders.Values(params, map[string]any{"priority": 1.5}); err == nil {
			t.Error("1.5 accepted for an integer header")
		}
	})
}

func TestExtraHeadersReachTheRequest(t *testing.T) {
	ctx := withExtraHeaders(context.Background(), map[string]string{"Mcp-Param-X": "1"})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://x", nil)
	(&HTTPTransport{}).setHeadersFor(req, nil)
	if req.Header.Get("Mcp-Param-X") != "1" {
		t.Fatal("per-request header lost")
	}
}
