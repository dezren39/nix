package e2e_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Every tool an MCP client can see should be reachable from a script (#102).
//
// docs/story.md says of an adapted binary: "It appears in `tools/list`, it
// is callable from a script". For a long time only the first half was true:
// adapter tools were attached as mcpserver.Extra, which reaches tools/list
// over /mcp and stdio and nothing else. Now each adapter is also a server of
// its own (`mcpx adapter serve`, added by augmentAdapters), so it goes
// through the pool, the schema cache and the codegen like any other. This
// test pins both halves, and then calls the tool from a script.
func TestEveryToolInToolsListIsReachableFromAScript(t *testing.T) {
	e := newEnv(t, oneServer)

	// An adapted program: the case docs/story.md makes its claim about. One
	// typed parameter, so the signature in the client is checked too.
	spec := filepath.Join(e.dir, "adapters.json")
	if err := os.WriteFile(spec, []byte(`{"adapters":[{"name":"greet","command":"echo",
      "description":"say hello",
      "tools":[{"name":"once","description":"say it once","args":["hello"],
        "params":[{"name":"who","type":"string","required":true,"description":"whom to greet"}]}]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	e.envVars = append(e.envVars, "MCPX_PATHS_ADAPTERS="+spec)

	listed := toolsList(t, e)
	if len(listed) == 0 {
		t.Fatal("tools/list returned nothing; the harness is wrong and this test checks nothing")
	}
	var adapted []string
	for _, name := range listed {
		if strings.HasPrefix(name, "greet") {
			adapted = append(adapted, name)
		}
	}
	// The premise. Without this the comparison below is vacuous, which is
	// exactly how this test first passed while proving nothing.
	if len(adapted) == 0 {
		t.Fatalf("the adapted program is not in tools/list at all, so there is "+
			"nothing to compare; got %d tools", len(listed))
	}

	client := e.run("client")
	if strings.TrimSpace(client) == "" {
		t.Fatal("the generated client is empty; this test checks nothing")
	}

	// Reachable from a script means the generated client names the tool.
	// tools/list joins an adapter's namespace and tool with "_"; the client
	// reaches a tool as ns.tool, which compiles to __call("ns", "tool", ...)
	// -- so either spelling counts.
	reachable := func(listed string) bool {
		ns, tool, _ := strings.Cut(listed, "_")
		return strings.Contains(client, listed) ||
			strings.Contains(client, fmt.Sprintf("%q, %q", ns, tool))
	}

	// A control for the predicate itself: demo.echo is configured by
	// oneServer and is in the client, so a predicate that finds nothing
	// fails here rather than blaming the adapter.
	if !reachable("demo_echo") {
		t.Fatal("the reachability check cannot find demo.echo, which is in the " +
			"generated client; the check is wrong and this test proves nothing")
	}

	var missing []string
	for _, name := range adapted {
		if !reachable(name) {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%v reached tools/list and not the generated client, so "+
			"docs/story.md's \"it is callable from a script\" is false (#102)", missing)
	}

	// Typed from the declaration, not `any`.
	if types := e.run("types", "greet"); !strings.Contains(types, "who: string") {
		t.Errorf("mcpx types greet should carry the declared parameter as who: string:\n%s", types)
	} else if strings.Contains(types, "mcpx_") || strings.Contains(types, "namespaces") {
		// The adapter's server offers its own tools and nothing else, not a
		// recursive copy of mcpx's meta-tools.
		t.Errorf("the adapter namespace carries mcpx's own tools:\n%s", types)
	}
	// Everywhere a configured server's tools appear.
	for _, args := range [][]string{{"ls"}, {"search", "greet"}, {"catalog"}} {
		if out := e.run(args...); !strings.Contains(out, "greet") {
			t.Errorf("mcpx %s does not mention the adapter:\n%s", strings.Join(args, " "), out)
		}
	}
	// And it runs: from a script, from `mcpx call` (POST /v1/call).
	if out := e.run("exec", `console.log(String(await greet.once({ who: "world" })));`); !strings.Contains(out, "hello world") {
		t.Errorf("calling the adapter from a script: %s", out)
	}
	if out := e.run("call", "greet.once", `{"who":"call"}`); !strings.Contains(out, "hello call") {
		t.Errorf("mcpx call greet.once: %s", out)
	}
}

// toolsList asks the real binary's MCP server what tools it offers.
func toolsList(t *testing.T, e *env) []string {
	t.Helper()
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"parity","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	}, "\n") + "\n"

	cmd := exec.Command(e.mcpx, "serve")
	cmd.Dir = e.dir
	cmd.Env = e.envVars
	cmd.Stdin = strings.NewReader(in)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("mcpx serve: %v", err)
	}

	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "{") {
			continue
		}
		var msg struct {
			ID     int `json:"id"`
			Result struct {
				Tools []struct {
					Name string `json:"name"`
				} `json:"tools"`
			} `json:"result"`
		}
		if json.Unmarshal([]byte(line), &msg) != nil || msg.ID != 2 {
			continue
		}
		for _, tool := range msg.Result.Tools {
			names = append(names, tool.Name)
		}
	}
	return names
}

// An adapter is a namespace, so it cannot share a name with a configured
// server: one of them would silently shadow the other (#181 collision 4).
func TestAnAdapterNamedLikeAServerIsRefused(t *testing.T) {
	e := newEnv(t, oneServer)
	spec := filepath.Join(e.dir, "adapters.json")
	if err := os.WriteFile(spec, []byte(`{"adapters":[{"name":"demo","command":"echo","tools":[{"name":"x"}]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	e.envVars = append(e.envVars, "MCPX_PATHS_ADAPTERS="+spec)
	out, err := e.try("ls")
	if err == nil || !strings.Contains(out, "same name as a configured server") {
		t.Fatalf("expected the collision to be refused, got err=%v:\n%s", err, out)
	}
}
