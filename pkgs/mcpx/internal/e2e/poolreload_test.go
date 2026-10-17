package e2e_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A runtime change to a pool setting reloads the configuration. Every server
// whose pool identity includes the changed knob is replaced, and the one it
// replaces may be serving a call. That call must finish on the old child and
// return its real result: a runtime tuning change is not a reason to fail a
// caller's work.
//
// pool.max only changes the identity of a server that is not global-scoped:
// a global server is held at one whatever the ceiling says (config.Resolve),
// so these use twoSessionPools, whose demo server is session-scoped.

type callResult struct {
	out string
	err error
}

// callInBackground starts an mcpx command and returns the channel its result
// arrives on, so the test can change settings while the command is running.
func (e *env) callInBackground(args ...string) <-chan callResult {
	ch := make(chan callResult, 1)
	go func() {
		cmd := exec.Command(e.mcpx, args...)
		cmd.Dir = e.dir
		cmd.Env = e.envVars
		out, err := cmd.CombinedOutput()
		ch <- callResult{out: string(out), err: err}
	}()
	return ch
}

// awaitInFlight returns once the daemon reports a caller holding an instance,
// so the change is made while the call is really in progress and not before it
// has reached the server.
func (e *env) awaitInFlight(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		var st struct {
			Servers []struct {
				Instances []struct {
					Holders int `json:"holders"`
				} `json:"instances"`
			} `json:"servers"`
		}
		if err := json.Unmarshal([]byte(jsonOf(t, e.run("--json", "status"))), &st); err != nil {
			t.Fatal(err)
		}
		for _, s := range st.Servers {
			for _, in := range s.Instances {
				if in.Holders > 0 {
					return
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("the slow call never reached the server")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func awaitResult(t *testing.T, ch <-chan callResult) callResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(60 * time.Second):
		t.Fatal("the call in flight did not finish")
		return callResult{}
	}
}

func TestAPoolSettingChangeDoesNotCutACallInFlight(t *testing.T) {
	cases := []struct {
		name, cfg, path, value string
		ms                     int
	}{
		{"max during a short call", twoSessionPools, "pool.max", "2", 3000},
		{"max during a long call", twoSessionPools, "pool.max", "2", 7000},
		{"idle timeout during a short call", oneServer, "pool.idleTimeout", "1m", 3000},
		// Longer than the grace a replaced pool gives its callers before it
		// closes their connection.
		{"idle timeout during a long call", oneServer, "pool.idleTimeout", "1m", 7000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, tc.cfg)
			e.run("call", "demo.echo", `{"message":"warm"}`)
			slow := e.callInBackground("call", "demo.slow", fmt.Sprintf(`{"ms":%d}`, tc.ms))
			e.awaitInFlight(t)

			e.run("settings", "set", tc.path, tc.value)
			r := awaitResult(t, slow)
			want := fmt.Sprintf("slept %dms", tc.ms)
			if r.err != nil || !strings.Contains(r.out, want) {
				t.Fatalf("the call in flight was cut by `settings set %s %s`: err=%v\n%s",
					tc.path, tc.value, r.err, r.out)
			}

			// The change still applies: new calls run on the new configuration.
			e.run("call", "demo.echo", `{"message":"after"}`)
			if tc.path == "pool.max" {
				if got := e.poolMaxes(t); got["demo"] != 2 {
					t.Fatalf("a call after the change ran on max=%d, want 2 (all: %v)", got["demo"], got)
				}
			}
		})
	}
}

// A runtime change that cannot be reloaded is refused, and the value it set
// is taken back. Otherwise the next unrelated reload -- a config edit, a
// `servers add` -- would apply a change the caller was told had failed.
func TestAFailedReloadRollsBackARuntimePoolChange(t *testing.T) {
	e := newEnv(t, twoSessionPools)
	e.run("call", "demo.echo", `{"message":"warm"}`)
	e.run("settings", "set", "pool.max", "3")
	if got := e.poolMaxes(t); got["demo"] != 3 {
		t.Fatalf("before the failure the live max should be 3: %v", got)
	}

	// The CLI reads the file itself, so a broken file would stop it before it
	// reached the daemon. The runtime API is the path that reloads, so the
	// change goes there. The daemon's file is broken, so the reload fails.
	c := e.socketClient(t)
	good, err := os.ReadFile(filepath.Join(e.dir, ".mcpx.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.dir, ".mcpx.json"), []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	put := func(path, body string) (int, string) {
		req, _ := http.NewRequest(http.MethodPut, "http://mcpx/v1/settings/"+path, strings.NewReader(body))
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	del := func(path string) (int, string) {
		req, _ := http.NewRequest(http.MethodDelete, "http://mcpx/v1/settings/"+path, nil)
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	value := func() string {
		resp, err := c.Get("http://mcpx/v1/settings/pool.max")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var rec struct {
			Value string `json:"value"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&rec); err != nil {
			t.Fatal(err)
		}
		return rec.Value
	}

	code, body := put("pool.max", `{"value":"2"}`)
	if code != http.StatusInternalServerError || !strings.Contains(body, "rolled back") {
		t.Fatalf("a change that cannot be reloaded: status %d, want 500 saying it was rolled back:\n%s", code, body)
	}
	if got := value(); got != "3" {
		t.Errorf("after the refused change pool.max should still be the earlier runtime 3, got %q", got)
	}

	// Removing the override fails the same way and must put it back.
	code, body = del("pool.max")
	if code != http.StatusInternalServerError || !strings.Contains(body, "rolled back") {
		t.Fatalf("a removal that cannot be reloaded: status %d, want 500 saying it was rolled back:\n%s", code, body)
	}
	if got := value(); got != "3" {
		t.Errorf("after the refused removal pool.max should still be 3, got %q", got)
	}

	// With the file mended the next reload succeeds, and the live pool is the
	// value the override held all along.
	if err := os.WriteFile(filepath.Join(e.dir, ".mcpx.json"), good, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := e.poolMaxes(t); got["demo"] != 3 {
		t.Errorf("the live pool should be at 3 once the file is mended: %v", got)
	}
}
