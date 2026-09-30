package e2e_test

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLogShowsTheDaemonStartingAndTheCallItServed(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("call", "demo.echo", `{"message":"hello there"}`)

	out := e.run("log", "--level", "debug", "--limit", "500")
	if !strings.Contains(out, "daemon starting") {
		t.Fatalf("the daemon's own start is missing from the log:\n%s", out)
	}
	if !strings.Contains(out, "mcp.call") {
		t.Fatalf("the call that was just made is missing from the log:\n%s", out)
	}
}

func TestLogFiltersByEventGlobAndByServer(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("call", "demo.echo", `{"message":"x"}`)

	out := e.run("log", "--level", "debug", "--event", "server.*", "--limit", "50")
	if !strings.Contains(out, "server.start") {
		t.Fatalf("server.* did not match the start event:\n%s", out)
	}
	if strings.Contains(out, "daemon.start") {
		t.Fatalf("server.* matched a daemon event:\n%s", out)
	}
	byServer := e.run("log", "--level", "debug", "--server", "demo", "--limit", "50")
	if !strings.Contains(byServer, "demo") {
		t.Fatalf("--server demo returned nothing about demo:\n%s", byServer)
	}
}

func TestStatsCallsCountsTheCallsThatWereMade(t *testing.T) {
	e := newEnv(t, oneServer)
	for i := 0; i < 3; i++ {
		e.run("call", "demo.echo", `{"message":"x"}`)
	}
	out := e.run("--json", "stats", "calls")
	var rows []struct {
		Server string  `json:"server"`
		Tool   string  `json:"tool"`
		Calls  int     `json:"calls"`
		P50    float64 `json:"p50Ms"`
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("stats --json is not valid JSON: %v\n%s", err, out)
	}
	if len(rows) != 1 {
		t.Fatalf("grouped into %d rows, want one server+tool pair:\n%s", len(rows), out)
	}
	if rows[0].Server != "demo" || rows[0].Tool != "echo" {
		t.Errorf("row names %s.%s, want demo.echo", rows[0].Server, rows[0].Tool)
	}
	if rows[0].Calls != 3 {
		t.Errorf("counted %d calls, want 3", rows[0].Calls)
	}
}

func TestChainWalksFromARealCallBackToTheDaemonThatStartedIt(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("call", "demo.echo", `{"message":"x"}`)

	slowest := e.run("stats", "slowest")
	var trace string
	for _, l := range strings.Split(slowest, "\n") {
		if !strings.Contains(l, "echo") {
			continue
		}
		f := strings.Fields(l)
		trace = f[len(f)-1]
	}
	if !strings.HasPrefix(trace, "cal-") {
		t.Fatalf("no call trace id in:\n%s", slowest)
	}

	// No --level. A chain walks a trace tree by id and every filter was
	// silently discarded, so this asked for debug records and got all of
	// them -- which read as though the filter had worked.
	out := e.run("log", "--chain", trace)
	for _, want := range []string{"dmn-", "srv-", "cal-", "daemon starting"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the chain does not reach %q:\n%s", want, out)
		}
	}

	refused, err := e.try("log", "--level", "debug", "--chain", trace)
	if err == nil {
		t.Fatalf("a filter beside --chain should be refused, not dropped:\n%s", refused)
	}
	if !strings.Contains(refused, "--level") {
		t.Fatalf("the refusal should name the flag:\n%s", refused)
	}
}

func TestLogSQLRunsAReadAndRefusesAWrite(t *testing.T) {
	e := newEnv(t, oneServer)
	e.run("call", "demo.echo", `{"message":"x"}`)

	out := e.run("log", "sql", "SELECT count(*) AS n FROM records")
	if !strings.Contains(out, "n") {
		t.Fatalf("sql did not print a table:\n%s", out)
	}
	if _, err := e.try("log", "sql", "DELETE FROM records"); err == nil {
		t.Fatal("a write was accepted")
	}
	schema := e.run("log", "sql", "--schema")
	if !strings.Contains(schema, "CREATE TABLE") {
		t.Fatalf("--schema printed no DDL:\n%s", schema)
	}
}
