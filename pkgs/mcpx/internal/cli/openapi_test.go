package cli_test

import (
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/api"
	"github.com/dezren39/mcpx/internal/cli"
	"github.com/dezren39/mcpx/internal/defaults"
)

// `mcpx openapi` and the daemon's /v1/openapi.json are one document.
//
// They were two. The command printed a hand-written map that had not been
// touched since #61 removed `mcpx serve --transport http`: it still named
// that command as the API's only server, declared /health and /openapi.json
// (both 404 today), and listed none of the /v1 operations. A person reading
// the specification from the command line got the stale half.
func TestTheCommandsDocumentContainsTheDaemonsDocument(t *testing.T) {
	doc := cli.OpenAPI("test")
	paths, _ := doc["paths"].(map[string]any)
	if len(paths) == 0 {
		t.Fatal("no paths; the document changed shape and this test checks nothing")
	}

	ops := 0
	for _, op := range api.Ops() {
		ops++
		item, ok := paths[op.Path].(map[string]any)
		if !ok {
			t.Errorf("%s: the daemon serves %s %s and the document does not mention it",
				op.Name, op.Method, op.Path)
			continue
		}
		if _, ok := item[strings.ToLower(op.Method)]; !ok {
			t.Errorf("%s: %s is described but not for %s", op.Name, op.Path, op.Method)
		}
	}
	if ops == 0 {
		t.Fatal("no operations; the table changed shape and this test checks nothing")
	}
}

// Every path in the document is one something answers.
//
// The daemon mounts /v1 from the operation table and the MCP endpoint at
// defaults.ProtoMCPPath, and serves nothing at the root. A path outside those
// is a promise the binary does not keep -- which is how /health and
// /openapi.json came to be advertised while returning 404.
func TestNoPathIsAdvertisedThatNothingServes(t *testing.T) {
	declared := map[string]bool{}
	for _, op := range api.Ops() {
		declared[op.Path] = true
	}
	paths, _ := cli.OpenAPI("test")["paths"].(map[string]any)
	for p := range paths {
		switch {
		case declared[p], p == defaults.ProtoMCPPath:
			// From the table, or the MCP endpoint the daemon mounts.
		case strings.HasPrefix(p, "/v1/tools/"):
			// One REST path per mcpx tool, served by the same handler as
			// /v1/tools/{tool}.
		default:
			t.Errorf("the document advertises %q and nothing serves it", p)
		}
	}
}

// The document is the same on every machine.
//
// It is what a person publishes, so it must not depend on which servers
// happen to be configured or which daemon happens to be running. Every path
// comes from a declaration compiled into the binary; upstream tools are
// reached through the /v1/tools/{tool} template rather than enumerated.
func TestTheDocumentIsTheSameOnEveryMachine(t *testing.T) {
	a, b := cli.OpenAPI("test"), cli.OpenAPI("test")
	pa, _ := a["paths"].(map[string]any)
	pb, _ := b["paths"].(map[string]any)
	if len(pa) != len(pb) {
		t.Fatalf("two calls disagreed: %d paths then %d", len(pa), len(pb))
	}
	for p := range pa {
		if _, ok := pb[p]; !ok {
			t.Errorf("%q appeared in one call and not the other", p)
		}
	}
	if _, ok := pa["/v1/tools/{tool}"]; !ok {
		t.Error("the upstream-tool template is missing; without it the only way to " +
			"describe a configured server's tools is to enumerate them, and the " +
			"document stops being publishable")
	}
}
