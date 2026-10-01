package e2e_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// Tests for the second batch of communication conflicts (WP11). Each case
// is named <rev>/<area>/<requirement> so the conformance matrix can cite it.

// mcpText posts one frame to /mcp and returns the first text block of its
// result, failing on a protocol error.
func mcpText(t *testing.T, ep, session string, frame map[string]any) string {
	t.Helper()
	resp := mcpPost(t, ep, session, frame)
	defer resp.Body.Close()
	body := decodeJSON(t, resp.Body)
	res, _ := body["result"].(map[string]any)
	if res == nil {
		t.Fatalf("no result:\n%s", dumpJSON(t, body))
	}
	content, _ := res["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("no content:\n%s", dumpJSON(t, body))
	}
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	return text
}

func mcpCall(id int, tool string, args map[string]any, meta map[string]any) map[string]any {
	params := map[string]any{"name": "mcpx_call",
		"arguments": map[string]any{"namespace": "demo", "tool": tool, "arguments": args}}
	if meta != nil {
		params["_meta"] = meta
	}
	return map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": params}
}

func legacySession(t *testing.T, ep string) string {
	t.Helper()
	resp := mcpPost(t, ep, "", map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{},
			"clientInfo": map[string]any{"name": "wp11", "version": "0"}},
	})
	resp.Body.Close()
	id := resp.Header.Get("Mcp-Session-Id")
	if id == "" {
		t.Fatal("initialize minted no session")
	}
	return id
}

// seenBy decodes fakemcp's `state` reply.
func seenBy(t *testing.T, text string) (int, []string) {
	t.Helper()
	var st struct {
		PID  int      `json:"pid"`
		Seen []string `json:"seen"`
	}
	if err := json.Unmarshal([]byte(text), &st); err != nil {
		t.Fatalf("state: %v\n%s", err, text)
	}
	return st.PID, st.Seen
}

func wp11Meta(extra map[string]any) map[string]any {
	m := map[string]any{
		"io.modelcontextprotocol/protocolVersion":    "2026-07-28",
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
		"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "wp11", "version": "0"},
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

// Conflict #2: every HTTP client of the daemon's /mcp used to resolve to the
// daemon's own pid, so a session-scoped upstream was one instance shared by
// all of them.
func TestHTTPMCPClientsHaveTheirOwnIdentity(t *testing.T) {
	e := newEnv(t, `{"mcpServers":{"demo":{"command":"FAKE",
		"mcpx":{"sharing":"exclusive","scope":"session","max":16}}}}`)
	e.run("refresh")
	ep := e.endpoint(t)

	// https://modelcontextprotocol.io/specification/2025-11-25/basic/transports#session-management
	t.Run("2025-11-25/transport/http-mcp-clients-do-not-share-scoped-instances", func(t *testing.T) {
		a, b := legacySession(t, ep), legacySession(t, ep)
		mcpText(t, ep, a, mcpCall(2, "open", map[string]any{"value": "from-A"}, nil))
		mcpText(t, ep, b, mcpCall(2, "open", map[string]any{"value": "from-B"}, nil))
		pidA, seenA := seenBy(t, mcpText(t, ep, a, mcpCall(3, "state", nil, nil)))
		pidB, seenB := seenBy(t, mcpText(t, ep, b, mcpCall(3, "state", nil, nil)))
		if strings.Join(seenA, ",") != "from-A" || strings.Join(seenB, ",") != "from-B" {
			t.Fatalf("two sessions shared one instance: A saw %v, B saw %v", seenA, seenB)
		}
		if pidA == pidB {
			t.Fatalf("both sessions reached upstream pid %d", pidA)
		}
	})

	// resources/read has to land in the same session's instance as a call.
	// The CLI client sent sessionId/callId, which /v1/resource never read,
	// so every read went to a fresh anonymous instance.
	t.Run("2025-11-25/transport/resource-read-uses-the-sessions-instance", func(t *testing.T) {
		a := legacySession(t, ep)
		resp := mcpPost(t, ep, a, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "resources/read",
			"params": map[string]any{"uri": "mcpx://demo/demo://greeting"}})
		body := dumpJSON(t, decodeJSON(t, resp.Body))
		resp.Body.Close()
		if !strings.Contains(body, "hello from a resource") {
			t.Fatalf("%s", body)
		}
		if st := e.run("--json", "status"); !strings.Contains(st, "session:mcp-"+a) {
			t.Fatalf("no instance keyed to session %s:\n%s", a, st)
		}
	})

	// 2026-07-28 has no session: a client that names itself under mcpx's
	// own _meta key is one caller across requests; one that does not is
	// scoped to the request.
	// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports
	t.Run("2026-07-28/transport/client-named-session-is-kept-across-requests", func(t *testing.T) {
		meta := wp11Meta(map[string]any{"dev.mcpx/session": "wp11-named"})
		mcpText(t, ep, "", mcpCall(1, "open", map[string]any{"value": "named"}, meta))
		_, seen := seenBy(t, mcpText(t, ep, "", mcpCall(2, "state", nil, meta)))
		if strings.Join(seen, ",") != "named" {
			t.Fatalf("the same named client should reach the same instance, saw %v", seen)
		}
	})
	t.Run("2026-07-28/transport/unnamed-requests-are-their-own-scope", func(t *testing.T) {
		meta := wp11Meta(nil)
		mcpText(t, ep, "", mcpCall(1, "open", map[string]any{"value": "anon"}, meta))
		_, seen := seenBy(t, mcpText(t, ep, "", mcpCall(2, "state", nil, meta)))
		for _, v := range seen {
			if v == "anon" || v == "named" || v == "from-A" || v == "from-B" {
				t.Fatalf("an unnamed request reached another caller's instance: %v", seen)
			}
		}
	})
}

// stdioReplies runs frames through `mcpx serve` and returns each reply by id.
func stdioReplies(t *testing.T, e *env, frames []map[string]any, args ...string) map[string]map[string]any {
	t.Helper()
	var in strings.Builder
	for _, f := range frames {
		b, _ := json.Marshal(f)
		in.Write(b)
		in.WriteByte('\n')
	}
	out := e.runStdin(in.String(), append(args, "serve")...)
	got := map[string]map[string]any{}
	for _, line := range strings.Split(out, "\n") {
		var f map[string]any
		if json.Unmarshal([]byte(line), &f) == nil && f["id"] != nil {
			got[fmt.Sprint(f["id"])] = f
		}
	}
	return got
}

func rpc(id int, method string, params any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
}

var stdioInit = rpc(1, "initialize", map[string]any{"protocolVersion": "2025-11-25",
	"capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "wp11", "version": "0"}})

func errCode(f map[string]any) float64 {
	e, _ := f["error"].(map[string]any)
	c, _ := e["code"].(float64)
	return c
}

// Conflict #9c, #12 and the binary-resource gap, through the real binary.
func TestMCPSurfaceAnswersLikeV1(t *testing.T) {
	// https://modelcontextprotocol.io/specification/2025-11-25/server/utilities/completion
	t.Run("2025-11-25/completion/forwarded-to-the-server-that-owns-the-prompt", func(t *testing.T) {
		e := askEnv(t)
		e.run("refresh")
		got := stdioReplies(t, e, []map[string]any{stdioInit,
			rpc(2, "completion/complete", map[string]any{
				"ref":      map[string]any{"type": "ref/prompt", "name": "ask_confirmed"},
				"argument": map[string]any{"name": "x", "value": ""}})})
		if s := dumpJSON(t, got["2"]); !strings.Contains(s, "from-upstream-a") {
			t.Fatalf("mcpx could not have guessed these values, so they prove who answered:\n%s", s)
		}
	})

	// context.arguments was dropped on both surfaces, so an upstream that
	// narrows by the arguments already chosen never saw them (#213, CMP-05).
	t.Run("2025-11-25/completion/context-arguments-reach-the-server", func(t *testing.T) {
		e := askEnv(t)
		e.run("refresh")
		got := stdioReplies(t, e, []map[string]any{stdioInit,
			rpc(2, "completion/complete", map[string]any{
				"ref":      map[string]any{"type": "ref/prompt", "name": "ask_confirmed"},
				"argument": map[string]any{"name": "x", "value": ""},
				"context":  map[string]any{"arguments": map[string]any{"owner": "me"}}})})
		s := dumpJSON(t, got["2"])
		if !strings.Contains(s, "from-upstream-a") {
			t.Fatalf("premise: upstream was not asked:\n%s", s)
		}
		if !strings.Contains(s, "ctx:owner=me") {
			t.Fatalf("context.arguments never reached the server:\n%s", s)
		}
	})

	e := newEnv(t, oneServer)
	e.run("refresh")
	// https://modelcontextprotocol.io/specification/2025-11-25/server/resources#binary-content
	t.Run("2025-11-25/resources/binary-read-returns-blob-with-mimeType", func(t *testing.T) {
		got := stdioReplies(t, e, []map[string]any{stdioInit,
			rpc(2, "resources/read", map[string]any{"uri": "mcpx://demo/demo://logo"})})
		res, _ := got["2"]["result"].(map[string]any)
		contents, _ := res["contents"].([]any)
		if len(contents) != 1 {
			t.Fatalf("%s", dumpJSON(t, got["2"]))
		}
		c, _ := contents[0].(map[string]any)
		if c["blob"] != "iVBORw0KGgo=" || c["mimeType"] != "image/png" || c["text"] != nil {
			t.Fatalf("not a blob: %s", dumpJSON(t, c))
		}
		// _meta on a read entry was dropped (#207, RES-06).
		if m, _ := c["_meta"].(map[string]any); m["example.com/k"] != "contents" {
			t.Errorf("the entry's _meta should survive: %s", dumpJSON(t, c))
		}
		if c["uri"] != "mcpx://demo/demo://logo" {
			t.Errorf("the entry's uri should be readable back through mcpx: %v", c["uri"])
		}
	})
	// https://modelcontextprotocol.io/specification/2025-11-25/server/prompts#error-handling
	t.Run("2025-11-25/prompts/unknown-prompt-is-invalid-params", func(t *testing.T) {
		got := stdioReplies(t, e, []map[string]any{stdioInit,
			rpc(2, "prompts/get", map[string]any{"name": "nosuch"})})
		if c := errCode(got["2"]); c != -32602 {
			t.Fatalf("code %v: %s", c, dumpJSON(t, got["2"]))
		}
	})

	// A client that can answer questions reads through the ask path, which
	// turned an upstream not-found into contents whose text was the error.
	// https://modelcontextprotocol.io/specification/2025-11-25/server/resources#error-handling
	t.Run("2025-11-25/resources/not-found-via-ask-path-is-32002", func(t *testing.T) {
		ep := e.endpoint(t)
		sess := func() string {
			r := mcpPost(t, ep, "", rpc(1, "initialize", map[string]any{"protocolVersion": "2025-11-25",
				"capabilities": map[string]any{"elicitation": map[string]any{}},
				"clientInfo":   map[string]any{"name": "wp11", "version": "0"}}))
			r.Body.Close()
			return r.Header.Get("Mcp-Session-Id")
		}()
		r := mcpPost(t, ep, sess, rpc(2, "resources/read", map[string]any{"uri": "mcpx://demo/demo://nope"}))
		defer r.Body.Close()
		raw, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(raw), `"code":-32002`) || !strings.Contains(string(raw), `"uri":"mcpx://demo/demo://nope"`) {
			t.Fatalf("%s", raw)
		}
	})

	// Conflict #12: one unknown server, one status, whichever route asked.
	t.Run("v1/errors/unknown-server-is-400-on-every-route", func(t *testing.T) {
		c := e.socketClient(t)
		for _, tc := range []struct {
			path string
			body any
		}{
			{"/v1/call", map[string]any{"server": "nosuch", "tool": "x"}},
			{"/v1/call/nosuch/x", map[string]any{}},
			{"/v1/tools/mcpx_call", map[string]any{"namespace": "nosuch", "tool": "x"}},
			{"/v1/resource", map[string]any{"server": "nosuch", "uri": "x"}},
			{"/v1/prompt", map[string]any{"server": "nosuch", "name": "x"}},
			{"/v1/complete", map[string]any{"server": "nosuch",
				"ref": map[string]any{"type": "ref/prompt", "name": "x"}}},
		} {
			if st, out := sockDo(t, c, http.MethodPost, tc.path, tc.body); st != http.StatusBadRequest {
				t.Errorf("%s: %d %v", tc.path, st, out)
			}
		}
	})
	// And an upstream that failed is 502 whichever route asked, including
	// /v1/tools, which relays a daemon call and used to call every failure
	// of it a bad request.
	t.Run("v1/errors/upstream-failure-is-502-on-every-route", func(t *testing.T) {
		c := e.socketClient(t)
		for _, tc := range []struct {
			path string
			body any
		}{
			{"/v1/call", map[string]any{"server": "demo", "tool": "no-such-tool"}},
			{"/v1/tools/mcpx_call", map[string]any{"namespace": "demo", "tool": "no-such-tool"}},
		} {
			if st, out := sockDo(t, c, http.MethodPost, tc.path, tc.body); st != http.StatusBadGateway {
				t.Errorf("%s: %d %v", tc.path, st, out)
			}
		}
	})
}

// Conflict #15: /v1/protocol reported the built-in default for nativeElicit.
func TestProtocolReportsTheSettingInForce(t *testing.T) {
	e := newEnv(t, oneServer)
	e.setenv("MCPX_PROTO_NATIVE=false")
	e.run("refresh")
	st, out := sockDo(t, e.socketClient(t), http.MethodGet, "/v1/protocol", nil)
	as, _ := out["asServer"].(map[string]any)
	if st != http.StatusOK || as["nativeElicit"] != false {
		t.Fatalf("proto.native is false here: %d %v", st, as["nativeElicit"])
	}
}

// Conflict #14: a profile hid a server from mcpx_namespaces but not from
// search, resources, prompts or calls.
func TestProfilesBoundTheMCPSurface(t *testing.T) {
	e := newEnv(t, profileServers)
	e.run("refresh")
	frames := []map[string]any{stdioInit,
		rpc(2, "tools/call", map[string]any{"name": "mcpx_call",
			"arguments": map[string]any{"namespace": "extra", "tool": "echo",
				"arguments": map[string]any{"message": "leaked"}}}),
		rpc(3, "prompts/list", map[string]any{}),
		rpc(4, "resources/list", map[string]any{}),
		rpc(5, "resources/read", map[string]any{"uri": "mcpx://extra/demo://greeting"}),
		rpc(6, "tools/call", map[string]any{"name": "mcpx_search",
			"arguments": map[string]any{"query": "echo"}}),
	}
	t.Run("profile/outside-the-profile-is-not-reachable-over-mcp", func(t *testing.T) {
		got := stdioReplies(t, e, frames)
		if r, _ := got["2"]["result"].(map[string]any); r["isError"] != true ||
			strings.Contains(dumpJSON(t, r), "leaked") {
			t.Errorf("a server outside the profile was callable: %s", dumpJSON(t, got["2"]))
		}
		for _, id := range []string{"3", "4", "6"} {
			if s := dumpJSON(t, got[id]); strings.Contains(s, "extra") {
				t.Errorf("reply %s mentions the hidden namespace: %s", id, s)
			}
		}
		if c := errCode(got["5"]); c != -32002 {
			t.Errorf("a hidden resource reads as not found: %s", dumpJSON(t, got["5"]))
		}
	})
	t.Run("profile/inside-the-profile-is-reachable-over-mcp", func(t *testing.T) {
		got := stdioReplies(t, e, frames, "--profile", "web")
		if s := dumpJSON(t, got["2"]); !strings.Contains(s, "leaked") {
			t.Errorf("the profile's own server should be callable: %s", s)
		}
		if s := dumpJSON(t, got["3"]); !strings.Contains(s, "extra_summarise") {
			t.Errorf("the profile's prompts should be listed: %s", s)
		}
	})
}

// A task's pollInterval was a literal 1000 in the task store; it follows
// protoTasks.pollInterval now, on /v1 as on MCP.
// https://modelcontextprotocol.io/specification/2025-11-25/basic/utilities/tasks#task-creation
func TestTaskPollIntervalFollowsTheSetting(t *testing.T) {
	e := newEnv(t, oneServer)
	e.setenv("MCPX_PROTO_TASKS_POLL_INTERVAL=250ms")
	e.run("refresh")
	t.Run("2025-11-25/tasks/v1-task-pollInterval-comes-from-the-setting", func(t *testing.T) {
		st, out := sockDo(t, e.socketClient(t), http.MethodPost, "/v1/call",
			map[string]any{"server": "demo", "tool": "echo", "args": map[string]any{"message": "x"},
				"task": map[string]any{"ttl": 60000}})
		task, _ := out["task"].(map[string]any)
		if st != http.StatusAccepted || task["pollInterval"] != float64(250) {
			t.Fatalf("%d %v", st, out)
		}
	})
}
