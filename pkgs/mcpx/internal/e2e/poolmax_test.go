package e2e_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// pool.max is the daemon-wide ceiling of every pool. The unit tests in
// internal/config prove the value reaches a resolved server; these read it
// back from a running daemon, which is where a value that reached the
// configuration but not the pool would show.
//
// Each server states session scope. Under global scope the single-key rule
// in config.Resolve holds every pool at one, so the number under test would
// never be visible. "other" is exclusive so that it is a pool of its own:
// two servers with identical definitions are one pool by design, and then
// status has one row to read instead of two.

const twoSessionPools = `{
  "mcpServers": {
    "demo":  { "command": "FAKE", "mcpx": { "scope": "session" } },
    "other": { "command": "FAKE", "mcpx": { "sharing": "exclusive", "scope": "session" } }
  }
}`

// poolMaxes returns each configured server's max as the running daemon
// reports it, keyed by namespace. The call first brings the daemon up under
// this environment, so the variables under test are the ones it was started
// with.
func (e *env) poolMaxes(t *testing.T) map[string]int {
	t.Helper()
	e.run("call", "demo.echo", `{"message":"warm"}`)
	var st struct {
		Servers []struct {
			Namespace string `json:"namespace"`
			Max       int    `json:"max"`
		} `json:"servers"`
	}
	if err := json.Unmarshal([]byte(jsonOf(t, e.run("--json", "status"))), &st); err != nil {
		t.Fatalf("status --json should be a document: %v", err)
	}
	out := map[string]int{}
	for _, s := range st.Servers {
		out[s.Namespace] = s.Max
	}
	return out
}

func TestWithNothingSetEveryLivePoolIsFour(t *testing.T) {
	got := newEnv(t, twoSessionPools).poolMaxes(t)
	for _, ns := range []string{"demo", "other"} {
		if got[ns] != 4 {
			t.Errorf("%s: live max=%d, want the built-in 4 (all: %v)", ns, got[ns], got)
		}
	}
}

func TestTheDaemonPoolMaxIsTheCeilingOfEveryLivePool(t *testing.T) {
	got := newEnv(t, twoSessionPools).withEnv("MCPX_POOL_MAX=3").poolMaxes(t)
	for _, ns := range []string{"demo", "other"} {
		if got[ns] != 3 {
			t.Errorf("%s: live max=%d, want the daemon's 3 (all: %v)", ns, got[ns], got)
		}
	}
}

func TestAPerServerMaxBeatsTheDaemonPoolMaxInTheLiveDaemon(t *testing.T) {
	const cfg = `{
  "mcpServers": {
    "demo":  { "command": "FAKE", "mcpx": { "scope": "session", "max": 2 } },
    "other": { "command": "FAKE", "mcpx": { "scope": "session" } }
  }
}`
	got := newEnv(t, cfg).withEnv("MCPX_POOL_MAX=3").poolMaxes(t)
	if got["demo"] != 2 {
		t.Errorf("demo: live max=%d, want its own 2 (all: %v)", got["demo"], got)
	}
	if got["other"] != 3 {
		t.Errorf("other: live max=%d, want the daemon's 3 (all: %v)", got["other"], got)
	}
}

func TestAnInvalidDaemonPoolMaxIsRefusedNotSilentlyReplaced(t *testing.T) {
	// Zero used to resolve to four without a word, because a zero reads as
	// "unset" to the layer beneath. Refusing it is the only way the user
	// learns their value did nothing.
	out, err := newEnv(t, twoSessionPools).withEnv("MCPX_POOL_MAX=0").try("status")
	if err == nil || !strings.Contains(out, "pool.max") {
		t.Fatalf("MCPX_POOL_MAX=0 should be refused, naming pool.max: %v\n%s", err, out)
	}
}

func TestASettingsSetOnTheRunningDaemonReachesEveryLivePool(t *testing.T) {
	// pool.max is registered hot. A runtime change must reach the pools that
	// are already running, not only the next daemon that starts.
	e := newEnv(t, twoSessionPools)
	if got := e.poolMaxes(t); got["demo"] != 4 || got["other"] != 4 {
		t.Fatalf("before the change every pool should be 4: %v", got)
	}
	e.run("settings", "set", "pool.max", "2")
	got := e.poolMaxes(t)
	for _, ns := range []string{"demo", "other"} {
		if got[ns] != 2 {
			t.Errorf("%s: live max=%d after `settings set pool.max 2`, want 2 (all: %v)", ns, got[ns], got)
		}
	}

	// Removing the runtime value hands the choice back to the layers beneath
	// it, which here is the built-in four.
	e.run("settings", "unset", "pool.max")
	got = e.poolMaxes(t)
	for _, ns := range []string{"demo", "other"} {
		if got[ns] != 4 {
			t.Errorf("%s: live max=%d after `settings unset pool.max`, want 4 (all: %v)", ns, got[ns], got)
		}
	}
}
