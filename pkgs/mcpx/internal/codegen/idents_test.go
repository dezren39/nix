package codegen_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/codegen"
	"github.com/dezren39/mcpx/internal/config"
)

var emptyObject = json.RawMessage(`{"type":"object","properties":{}}`)

// A tool named after a reserved word was emitted as `function delete(...)`,
// which no TypeScript parser accepts (#215, CODE-40).
func TestReservedToolNamesAreSuffixed(t *testing.T) {
	if got := codegen.ToolFuncName("delete"); got != "delete_" {
		t.Errorf("ToolFuncName(delete) = %q", got)
	}
	if got := codegen.ToolFuncName("deleted"); got != "deleted" {
		t.Errorf("an ordinary name should be untouched: %q", got)
	}
	decl := codegen.Declarations([]codegen.Namespace{{Name: "db", Server: "db",
		Tools: []codegen.Tool{{Name: "delete", InputSchema: emptyObject}, {Name: "class", InputSchema: emptyObject}}}})
	for _, bad := range []string{"function delete(", "function class("} {
		if strings.Contains(decl, bad) {
			t.Errorf("declarations contain %q:\n%s", bad, decl)
		}
	}
	if !strings.Contains(decl, "function delete_(") || !strings.Contains(decl, "function class_(") {
		t.Errorf("reserved names should be suffixed:\n%s", decl)
	}
	// The real name still goes on the wire.
	mod := codegen.Module([]codegen.Namespace{{Name: "db", Server: "db",
		Tools: []codegen.Tool{{Name: "delete", InputSchema: emptyObject}}}}, "", "")
	if !strings.Contains(mod, `__call("db", "delete", args)`) {
		t.Errorf("the call should carry the tool's own name:\n%s", mod)
	}
}

// A server named `log` became `export const log = {...}` beside the runtime's
// own `log`, a redeclaration that broke every script (#215, CODE-41).
func TestNamespacesAvoidPreludeAndReservedNames(t *testing.T) {
	prelude := codegen.PreludeNames()
	for _, n := range []string{"log", "tools", "call", "search"} {
		if !prelude[n] {
			t.Fatalf("premise: the runtime should declare %q; PreludeNames found %d names", n, len(prelude))
		}
	}
	for _, n := range []string{"log", "tools", "default", "delete", "console", "fetch"} {
		got := config.SanitizeNamespace(n)
		if got == n || codegen.ReservedNamespace(got) {
			t.Errorf("SanitizeNamespace(%q) = %q, still clashes", n, got)
		}
	}
	if got := config.SanitizeNamespace("github"); got != "github" {
		t.Errorf("an ordinary name should be untouched: %q", got)
	}

	cfg := &config.Config{MCPServers: map[string]*config.Server{
		"x": {Command: "true", Mcpx: &config.Extras{Namespace: "log"}}}}
	if _, err := cfg.Resolve("x"); err == nil || !strings.Contains(err.Error(), `"log"`) {
		t.Errorf("an explicit namespace that clashes should be refused, got %v", err)
	}
}

// Two tools that normalise to one identifier were two keys of one object
// literal; the last won, so a script calling one called the other (#215,
// CODE-42).
func TestCollidingToolNamesAreNotSilentlyMerged(t *testing.T) {
	ns := []codegen.Namespace{{Name: "kv", Server: "kv", Tools: []codegen.Tool{
		{Name: "get-item", InputSchema: emptyObject},
		{Name: "get_item", InputSchema: emptyObject},
		{Name: "put", InputSchema: emptyObject},
	}}}
	mod := codegen.Module(ns, "", "")
	if strings.Contains(mod, `__call("kv", "get-item"`) || strings.Contains(mod, `__call("kv", "get_item"`) {
		t.Errorf("neither colliding tool should be bound to the shared identifier:\n%s", mod)
	}
	if strings.Contains(mod, "\n  get_item(") || !strings.Contains(mod, "get get_item(): never") {
		t.Errorf("the identifier should be one member that throws:\n%s", mod)
	}
	if !strings.Contains(mod, `\"get-item\"`) || !strings.Contains(mod, `\"get_item\"`) {
		t.Errorf("the error should name both tools:\n%s", mod)
	}
	if !strings.Contains(mod, `__call("kv", "put", args)`) {
		t.Errorf("a tool that does not collide is unaffected:\n%s", mod)
	}
	decl := codegen.Declarations(ns)
	if strings.Contains(decl, "function get_item(") || !strings.Contains(decl, `"get-item" and "get_item"`) {
		t.Errorf("declarations should omit the shared identifier and say why:\n%s", decl)
	}
}

// The three cases above, through a real type checker: a generated client
// with a namespace named `log` and tools named `delete`, `get-item` and
// `get_item` must compile.
func TestGeneratedClientWithAwkwardNamesTypeChecks(t *testing.T) {
	deno, err := exec.LookPath("deno")
	if err != nil {
		t.Skip("deno not on PATH")
	}
	nss := []codegen.Namespace{{Name: config.SanitizeNamespace("log"), Server: "log", Tools: []codegen.Tool{
		{Name: "delete", InputSchema: emptyObject},
		{Name: "get-item", InputSchema: emptyObject},
		{Name: "get_item", InputSchema: emptyObject},
	}}}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "client.ts"), []byte(codegen.Module(nss, "http://127.0.0.1:1", "")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "types.d.ts"), []byte("type ToolResult = unknown;\n"+codegen.Declarations(nss)), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(deno, "check", "--quiet", "client.ts", "types.d.ts")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "DENO_NO_UPDATE_CHECK=1", "NO_COLOR=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("deno check failed: %v\n%s", err, out)
	}
}
