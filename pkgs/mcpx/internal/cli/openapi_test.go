package cli_test

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/api"
	"github.com/dezren39/mcpx/internal/cli"
	"github.com/dezren39/mcpx/internal/defaults"
	"github.com/dezren39/mcpx/internal/settings"
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

// The templates that stand in for what a machine happens to be running.
//
// Without them the only way to describe a configured server's tools is to
// enumerate them, and the document stops being publishable -- which is the
// property TestTheOpenAPIDocumentIsTheSameWhateverIsConfigured checks by
// driving the binary under two configurations. This is the cheap half: these
// are the mechanism that property rests on, so losing one fails in this
// package rather than in an end-to-end run.
//
// /v1/call/{server}/{tool} is the one that matters for upstream tools.
// /v1/tools/{tool} is mcpx's own MCP surface and does not reach them at all,
// which this file's doc comment used to claim it did -- against a live
// daemon, POST /v1/tools/echo, /v1/tools/demo_echo and /v1/tools/demo.echo
// all answer 400 "no tool named ...", while POST /v1/call/demo/echo answers
// 200 with the result.
//
// It replaces a test that called cli.OpenAPI twice in one process and
// asserted the two results agreed. cli.OpenAPI reads the operation table, a
// nil-backed mcpserver and the settings registry -- all compiled in -- so
// that comparison was true by construction and could not observe the thing
// its name promised.
func TestTheTemplatesThatKeepTheDocumentPublishableArePresent(t *testing.T) {
	paths, _ := cli.OpenAPI("test")["paths"].(map[string]any)
	if len(paths) == 0 {
		t.Fatal("no paths; the document changed shape and this test checks nothing")
	}
	for _, tmpl := range []string{"/v1/call/{server}/{tool}", "/v1/tools/{tool}"} {
		if _, ok := paths[tmpl]; !ok {
			t.Errorf("%s is missing; without it the only way to describe a configured "+
				"server's tools is to enumerate them, and the document stops being "+
				"publishable", tmpl)
		}
	}
}

// No `servers` entry is an address nothing can listen on.
//
// The document's first version named `mcpx serve --transport http`, a command
// #61 had removed. Its replacement named `http://127.0.0.1:0` -- daemon.port
// defaults to 0, which means "choose a free one at start-up", so that URL is
// a port nothing ever binds. Both are the same defect the paths above are
// guarded against, one level up: a destination handed to a client that cannot
// receive a request. A port decided at run time has to be a server variable,
// because any number written here is wrong on every machine.
func TestNoServerIsAdvertisedThatNothingListensOn(t *testing.T) {
	servers, _ := cli.OpenAPI("test")["servers"].([]any)
	if len(servers) == 0 {
		t.Fatal("no servers; the document changed shape and this test checks nothing")
	}
	placeholder := regexp.MustCompile(`\{([^}]+)\}`)
	for _, entry := range servers {
		m, _ := entry.(map[string]any)
		url, _ := m["url"].(string)
		if url == "" {
			t.Errorf("a servers entry has no url: %v", m)
			continue
		}
		if port := url[strings.LastIndex(url, ":")+1:]; port != "" {
			if n, err := strconv.Atoi(port); err == nil && n == 0 {
				t.Errorf("%q names port %d, which nothing listens on", url, n)
			}
		}
		vars, _ := m["variables"].(map[string]any)
		for _, match := range placeholder.FindAllStringSubmatch(url, -1) {
			spec, ok := vars[match[1]].(map[string]any)
			if !ok {
				t.Errorf("%q uses {%s} and declares no such variable", url, match[1])
				continue
			}
			if _, ok := spec["default"]; !ok {
				t.Errorf("%q: variable %s has no default, which OpenAPI requires", url, match[1])
			}
		}
	}
}

// The port the document offers is the port the setting declares. Two copies
// of a default are one rename from disagreeing, and `mcpx settings` is what a
// reader would check it against.
func TestTheLoopbackPortIsTheDeclaredDefault(t *testing.T) {
	var want string
	found := false
	for _, s := range settings.Registry() {
		if s.Path == "daemon.port" {
			want, found = s.Default, true
		}
	}
	if !found {
		t.Fatal("daemon.port is not in the registry; the document's fallback would be a second copy of its default")
	}
	servers, _ := cli.OpenAPI("test")["servers"].([]any)
	for _, entry := range servers {
		m, _ := entry.(map[string]any)
		vars, _ := m["variables"].(map[string]any)
		spec, ok := vars["port"].(map[string]any)
		if !ok {
			continue
		}
		if got, _ := spec["default"].(string); got != want {
			t.Errorf("the document offers port %q and daemon.port declares %q", got, want)
		}
		return
	}
	t.Error("no servers entry has a port variable; the loopback endpoint is either absent or a literal again")
}
