// Command officialclient lets the official MCP conformance suite test mcpx as a client.
//
// The suite starts a scenario server and runs `<command> <url>` with MCP_CONFORMANCE_SCENARIO set. mcpx is
// not a library a script calls into; it is a daemon and a CLI. So this program is the "client" the suite
// runs: it writes an isolated mcpx config naming the scenario URL as an HTTP upstream, then does through the
// real `mcpx` binary what the scenario expects a client to do -- list tools, call them with the scenario's
// arguments, answer any elicitation the broker holds -- and stops the daemon it caused to start.
//
// Everything the scenario observes on the wire therefore comes from mcpx's own client, which is the point.
// Nothing here speaks MCP itself.
//
// Usage (the suite appends the URL):
//
//	conformance client --command "officialclient -mcpx /path/to/mcpx" --scenario tools_call
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Knobs are flags rather than constants: this is test support run by a script, and each has a reason to be
// turned when a scenario is slow.
var (
	mcpxBin   = flag.String("mcpx", "mcpx", "the mcpx binary under test")
	idleExit  = flag.String("idle-exit", "60s", "backstop: an orphaned daemon exits after this long idle")
	pollEvery = flag.String("poll", "200ms", "how often pending elicitations are checked")
	deadline  = flag.String("deadline", "60s", "give up on the whole scenario after this long")
	namespace = flag.String("ns", "conf", "namespace the scenario server is configured under")
	keep      = flag.Bool("keep", false, "keep the scratch directory (its daemon log) for inspection")
)

// Env is what the suite passes. The names are the suite's, documented in its README.
const (
	envScenario = "MCP_CONFORMANCE_SCENARIO"
	envContext  = "MCP_CONFORMANCE_CONTEXT"
	envVersion  = "MCP_CONFORMANCE_PROTOCOL_VERSION"
	// modernFloor is the first revision without the initialize handshake; the suite says a client must
	// derive the lifecycle from the version it is asked to run.
	modernFloor = "2026-07-28"
)

type toolCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// scenarioCalls is what each scenario expects called when its context does not say. Arguments come from
// the scenario sources in the conformance repo (src/scenarios/client/*.ts); a scenario absent here gets
// every advertised tool called with no arguments, which is what the suite's own everything-client does
// for the scenarios that only watch headers or metadata.
var scenarioCalls = map[string][]toolCall{
	"initialize":               {{Name: "add_numbers", Arguments: map[string]any{"a": 2, "b": 3}}},
	"tools_call":               {{Name: "add_numbers", Arguments: map[string]any{"a": 2, "b": 3}}},
	"json-schema-ref-no-deref": {}, // list only: the scenario's mock serves nothing else
}

func main() {
	flag.Parse()
	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: officialclient [flags] <server-url>")
		os.Exit(2)
	}
	url := flag.Arg(flag.NArg() - 1)
	scenario := os.Getenv(envScenario)
	if err := run(url, scenario); err != nil {
		fmt.Fprintf(os.Stderr, "officialclient %s: %v\n", scenario, err)
		os.Exit(1)
	}
}

func run(url, scenario string) error {
	limit, err := time.ParseDuration(*deadline)
	if err != nil {
		return fmt.Errorf("-deadline: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()

	dir, err := os.MkdirTemp("", "mcpx-conformance-")
	if err != nil {
		return err
	}
	if *keep {
		fmt.Fprintf(os.Stderr, "scratch: %s\n", dir)
	} else {
		defer os.RemoveAll(dir)
	}

	version := os.Getenv(envVersion)
	protocol := "force-legacy"
	if version >= modernFloor {
		protocol = "force-modern"
	}
	cfg := map[string]any{"mcpServers": map[string]any{*namespace: map[string]any{
		"url": url, "transport": "http", "protocol": protocol,
		"mcpx": map[string]any{"sharing": "shared", "scope": "global"},
	}}}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	cfgPath := filepath.Join(dir, ".mcpx.json")
	if err := os.WriteFile(cfgPath, b, 0o600); err != nil {
		return err
	}
	m := &mcpx{dir: dir, env: append(os.Environ(),
		"MCPX_CONFIG="+cfgPath,
		"MCPX_STATE_DIR="+filepath.Join(dir, "state"),
		"MCPX_CACHE_DIR="+filepath.Join(dir, "cache"),
		"MCPX_AUTOSTART_IDLE_EXIT="+*idleExit,
		// Never the real registry, for the same reason the e2e harness says so.
		"MCPX_REGISTRY_URL=http://127.0.0.1:1/",
	)}
	// Every daemon this run caused is stopped, whatever happened; `--all` because the socket path can move
	// (see the e2e harness's cleanup for why plain `stop` misses).
	defer func() { _, _ = m.run(context.Background(), "stop", "--all") }()

	stopAnswering := m.answerElicitations(ctx)
	defer stopAnswering()

	// Listing forces the daemon up, the upstream connected, and tools/list sent.
	if out, err := m.run(ctx, "--json", "ls"); err != nil {
		return fmt.Errorf("ls: %v\n%s", err, out)
	}
	calls, err := callsFor(ctx, m, scenario)
	if err != nil {
		return err
	}
	var failed []string
	for _, c := range calls {
		args, _ := json.Marshal(c.Arguments)
		if c.Arguments == nil {
			args = []byte("{}")
		}
		out, err := m.run(ctx, "--json", "call", *namespace+"."+c.Name, string(args))
		fmt.Fprintf(os.Stderr, "call %s %s -> err=%v\n%s\n", c.Name, args, err, out)
		if err != nil {
			failed = append(failed, c.Name)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("calls failed: %s", strings.Join(failed, ", "))
	}
	return nil
}

// callsFor picks the tool calls: the scenario's context when it supplies toolCalls, else the table, else
// every tool the upstream advertised.
func callsFor(ctx context.Context, m *mcpx, scenario string) ([]toolCall, error) {
	if raw := os.Getenv(envContext); raw != "" {
		var c struct {
			ToolCalls []toolCall `json:"toolCalls"`
		}
		if json.Unmarshal([]byte(raw), &c) == nil && len(c.ToolCalls) > 0 {
			return c.ToolCalls, nil
		}
	}
	if calls, ok := scenarioCalls[scenario]; ok {
		return calls, nil
	}
	out, err := m.run(ctx, "--json", "search", "")
	if err != nil {
		return nil, fmt.Errorf("search: %v\n%s", err, out)
	}
	var hits []struct {
		Namespace string `json:"namespace"`
		Tool      string `json:"tool"`
	}
	if err := json.Unmarshal(out, &hits); err != nil {
		return nil, fmt.Errorf("search output: %v\n%s", err, out)
	}
	var calls []toolCall
	for _, h := range hits {
		if h.Namespace == *namespace {
			calls = append(calls, toolCall{Name: h.Tool})
		}
	}
	return calls, nil
}

type mcpx struct {
	dir string
	env []string
}

func (m *mcpx) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, *mcpxBin, args...)
	cmd.Env = m.env
	cmd.Dir = m.dir
	return cmd.CombinedOutput()
}

// answerElicitations accepts every question the broker holds with an empty answer. Empty on purpose: a
// scenario like elicitation-sep1034-client-defaults checks that the client fills in the schema's defaults,
// and an adapter that typed them in itself would be testing the adapter.
func (m *mcpx) answerElicitations(ctx context.Context) func() {
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		answered := map[string]bool{}
		every, err := time.ParseDuration(*pollEvery)
		if err != nil {
			fmt.Fprintf(os.Stderr, "-poll: %v\n", err)
			return
		}
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			out, err := m.run(ctx, "--json", "elicit", "list")
			if err != nil {
				continue
			}
			var pending []struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(out, &pending) != nil {
				continue
			}
			for _, p := range pending {
				if p.ID == "" || answered[p.ID] {
					continue
				}
				answered[p.ID] = true
				res, err := m.run(ctx, "elicit", "answer", p.ID, "{}")
				fmt.Fprintf(os.Stderr, "elicit answer %s -> err=%v %s\n", p.ID, err, res)
			}
		}
	}()
	return func() { cancel(); wg.Wait() }
}
