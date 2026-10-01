package pool_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/pool"
	"github.com/dezren39/mcpx/internal/testsupport"
)

func resolved(t *testing.T, bin string, ex *config.Extras) *config.Resolved {
	t.Helper()
	cfg := &config.Config{MCPServers: map[string]*config.Server{
		"fake": {Name: "fake", Command: bin, Mcpx: ex},
	}}
	r, err := cfg.Resolve("fake")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return r
}

// key resolves a scope the way the daemon does, so these tests exercise the
// real path rather than inventing keys.
func key(t *testing.T, r *config.Resolved, cc config.CallContext) string {
	t.Helper()
	k, _ := r.Scope.Key(cc)
	return k
}

// call resolves the key for a call context and dispatches.
func call(t *testing.T, p *pool.Pool, r *config.Resolved, cc config.CallContext, tool string, args any) (json.RawMessage, error) {
	t.Helper()
	return p.Call(context.Background(), key(t, r, cc), tool, args)
}

// textOf pulls the single text block out of a CallToolResult.
func textOf(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var r struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatalf("decode result: %v (%s)", err, raw)
	}
	if len(r.Content) == 0 {
		t.Fatalf("no content in %s", raw)
	}
	return r.Content[0].Text
}

func TestSharedModeReusesOneProcess(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{Sharing: config.SharingShared, Scope: config.ScopeGlobal}))
	defer p.Close()

	r := resolved(t, bin, &config.Extras{Sharing: config.SharingShared, Scope: config.ScopeGlobal})
	pids := map[string]bool{}
	for i := 0; i < 5; i++ {
		// Different callers, different sessions, same global scope.
		cc := config.CallContext{SessionID: fmt.Sprintf("s%d", i), CallID: fmt.Sprintf("c%d", i)}
		res, err := call(t, p, r, cc, "state", map[string]any{})
		if err != nil {
			t.Fatalf("call: %v", err)
		}
		pids[textOf(t, res)] = true
	}
	if len(pids) != 1 {
		t.Fatalf("global scope should use one process, saw %d distinct states: %v", len(pids), pids)
	}
}

func TestSharedModeHandlesConcurrentCalls(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{Sharing: config.SharingShared, Scope: config.ScopeGlobal}))
	defer p.Close()

	// Ten calls on one process must all be in flight at once, proving
	// requests are multiplexed rather than serialised. Each blocks in the
	// fake until all ten have arrived; the bound is only how long to wait
	// before calling it a failure, so a loaded machine cannot fail it.
	const n = 10
	barrier(t, p, "global", n)
}

// barrier makes n concurrent calls on key that each block in the fake until
// all n are in flight at once, and fails unless every one saw the others.
func barrier(t *testing.T, p *pool.Pool, key string, n int) {
	t.Helper()
	var wg sync.WaitGroup
	res := make([]json.RawMessage, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res[i], errs[i] = p.Call(context.Background(), key, "slow",
				map[string]any{"ms": 10000, "barrier": n})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if got := textOf(t, res[i]); got != "barrier met" {
			t.Fatalf("call %d: %s -- the %d calls were never all in flight at once", i, got, n)
		}
	}
}

func TestSessionModeIsolatesState(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	r := resolved(t, bin, &config.Extras{Sharing: config.SharingExclusive, Scope: config.ScopeSession, Max: 3})
	p := pool.New(r)
	defer p.Close()

	const n = 3
	var wg sync.WaitGroup
	states := make([]string, n)
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cc := config.CallContext{SessionID: fmt.Sprintf("run-%d", i), CallID: fmt.Sprintf("c%d", i)}
			value := fmt.Sprintf("value-%d", i)
			if _, err := call(t, p, r, cc, "open", map[string]any{"value": value}); err != nil {
				errs[i] = err
				return
			}
			res, err := call(t, p, r, cc, "state", map[string]any{})
			if err != nil {
				errs[i] = err
				return
			}
			states[i] = textOf(t, res)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	pids := map[string]bool{}
	for i, s := range states {
		var got struct {
			PID  int      `json:"pid"`
			Seen []string `json:"seen"`
		}
		if err := json.Unmarshal([]byte(s), &got); err != nil {
			t.Fatalf("run %d: decode state %q: %v", i, s, err)
		}
		want := fmt.Sprintf("value-%d", i)
		if len(got.Seen) != 1 || got.Seen[0] != want {
			t.Fatalf("run %d leaked state: saw %v, want exactly [%s]", i, got.Seen, want)
		}
		pids[fmt.Sprint(got.PID)] = true
	}
	if len(pids) != n {
		t.Fatalf("expected %d distinct processes, got %d: %v", n, len(pids), pids)
	}
}

func TestSessionModeReusesTheSameInstanceWithinASession(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{Sharing: config.SharingExclusive, Scope: config.ScopeSession, Max: 4}))
	defer p.Close()

	ctx := context.Background()
	for i := 0; i < 4; i++ {
		if _, err := p.Call(ctx, "sticky", "open", map[string]any{"value": fmt.Sprint(i)}); err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
	}
	res, err := p.Call(ctx, "sticky", "state", map[string]any{})
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	var got struct {
		Seen []string `json:"seen"`
	}
	if err := json.Unmarshal([]byte(textOf(t, res)), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Seen) != 4 {
		t.Fatalf("a session should keep one instance; saw %v", got.Seen)
	}
	if st := p.Status(); st.Live != 1 {
		t.Fatalf("expected 1 live instance, got %d", st.Live)
	}
}

func TestSessionModeRespectsMaxAndQueues(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{Sharing: config.SharingExclusive, Scope: config.ScopeSession, Max: 2}))
	defer p.Close()

	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			session := fmt.Sprintf("s%d", i)
			_, errs[i] = p.Call(ctx, session, "slow", map[string]any{"ms": 100})
			p.ReleaseKey(session)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if st := p.Status(); st.Live > 2 {
		t.Fatalf("pool exceeded max: %d live", st.Live)
	}
}

func TestReleaseSessionStopsTheInstance(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{Sharing: config.SharingExclusive, Scope: config.ScopeSession, Max: 2}))
	defer p.Close()

	ctx := context.Background()
	if _, err := p.Call(ctx, "s1", "echo", map[string]any{"message": "hi"}); err != nil {
		t.Fatal(err)
	}
	if st := p.Status(); st.Live != 1 {
		t.Fatalf("want 1 live, got %d", st.Live)
	}
	if n := p.ReleaseKey("s1"); n != 1 {
		t.Fatalf("want 1 released, got %d", n)
	}
	if st := p.Status(); st.Live != 0 {
		t.Fatalf("instance should be gone, %d still live", st.Live)
	}
}

func TestPooledModeRecyclesInstances(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{Sharing: config.SharingExclusive, Scope: config.ScopeCall, Max: 2}))
	defer p.Close()

	// Six distinct call keys against a max of two: the pool must evict idle
	// instances to make room rather than deadlocking or exceeding the cap.
	ctx := context.Background()
	for i := 0; i < 6; i++ {
		k := fmt.Sprintf("call:c%d", i)
		if _, err := p.Call(ctx, k, "echo", map[string]any{"message": "x"}); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if st := p.Status(); st.Live > 2 {
			t.Fatalf("exceeded max after call %d: %d live", i, st.Live)
		}
	}
}

func TestStartFailureIsReportedWithStderr(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	r := resolved(t, bin, &config.Extras{Sharing: config.SharingShared, Scope: config.ScopeGlobal})
	r.Env = map[string]string{"FAKEMCP_FAIL_START": "1"}
	p := pool.New(r)
	defer p.Close()

	_, err := p.Call(context.Background(), "", "echo", map[string]any{"message": "x"})
	if err == nil {
		t.Fatal("expected an error when the server refuses to start")
	}
	if !strings.Contains(err.Error(), "FAKEMCP_FAIL_START") {
		t.Fatalf("error should carry the server's stderr, got: %v", err)
	}
}

func TestStartFailureEntersCooldown(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	r := resolved(t, bin, &config.Extras{Sharing: config.SharingShared, Scope: config.ScopeGlobal})
	r.Env = map[string]string{"FAKEMCP_FAIL_START": "1"}
	p := pool.New(r)
	defer p.Close()

	ctx := context.Background()
	if _, err := p.Call(ctx, "", "echo", map[string]any{}); err == nil {
		t.Fatal("first call should fail")
	}
	_, err := p.Call(ctx, "", "echo", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "cooldown") {
		t.Fatalf("second call should be short-circuited by cooldown, got: %v", err)
	}
}

func TestToolErrorsPropagate(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{Sharing: config.SharingShared, Scope: config.ScopeGlobal}))
	defer p.Close()

	res, err := p.Call(context.Background(), "", "boom", map[string]any{})
	if err != nil {
		t.Fatalf("a tool-level error is a valid result, not a transport error: %v", err)
	}
	var got struct {
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(res, &got); err != nil {
		t.Fatal(err)
	}
	if !got.IsError {
		t.Fatal("expected isError to survive the round trip")
	}
}

func TestSchemasAreCachedAfterFirstFetch(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{Sharing: config.SharingShared, Scope: config.ScopeGlobal}))
	defer p.Close()

	ctx := context.Background()
	tools, _, err := p.Schemas(ctx)
	if err != nil {
		t.Fatalf("schemas: %v", err)
	}
	if len(tools) == 0 {
		t.Fatal("no tools returned")
	}
	start := time.Now()
	for i := 0; i < 1000; i++ {
		if _, _, err := p.Schemas(ctx); err != nil {
			t.Fatal(err)
		}
	}
	// 1000 cache reads must be far below one round trip.
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("cached schema reads took %s for 1000 calls; cache is not working", d)
	}
}

func TestPoolCachesEveryToolRegardlessOfViewFilters(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	// Allow and deny lists are a property of the *view*, not the process, so
	// that two aliases can expose different subsets of one shared child. The
	// pool must therefore cache the server's full catalogue.
	p := pool.New(resolved(t, bin, &config.Extras{
		Sharing:      config.SharingShared,
		Scope:        config.ScopeGlobal,
		Tools:        []string{"echo"},
		ExcludeTools: []string{"boom"},
	}))
	defer p.Close()

	tools, _, err := p.Schemas(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.Name] = true
	}
	if !names["echo"] || !names["boom"] || !names["state"] {
		t.Fatalf("the pool should cache every tool; filtering happens per view: %v", names)
	}
}

func TestRestartStopsEverything(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{Sharing: config.SharingExclusive, Scope: config.ScopeSession, Max: 2}))
	defer p.Close()

	ctx := context.Background()
	if _, err := p.Call(ctx, "a", "open", map[string]any{"value": "1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Call(ctx, "b", "open", map[string]any{"value": "2"}); err != nil {
		t.Fatal(err)
	}
	if n := p.Restart(ctx, true).Stopped; n != 2 {
		t.Fatalf("restart should stop 2 instances, stopped %d", n)
	}
	if st := p.Status(); st.Live != 0 {
		t.Fatalf("%d instances survived restart", st.Live)
	}
	// A fresh instance must have no memory of the old state.
	res, err := p.Call(ctx, "a", "state", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(textOf(t, res), `"1"`) {
		t.Fatal("state survived a restart")
	}
}

// An eager restart replaces each instance under the key it served, before it
// returns, and the replacement has none of the old state.
func TestRestartReplacesEachInstanceUnderItsKey(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{Sharing: config.SharingExclusive, Scope: config.ScopeSession, Max: 2}))
	defer p.Close()

	ctx := context.Background()
	for _, k := range []string{"session:a", "session:b"} {
		if _, err := p.Call(ctx, k, "open", map[string]any{"value": "1"}); err != nil {
			t.Fatal(err)
		}
	}
	before := map[string]string{}
	for _, in := range p.Status().Instances {
		before[in.Key] = in.ID
	}
	res := p.Restart(ctx, false)
	if res.Stopped != 2 || len(res.Started) != 2 || len(res.Failed) != 0 {
		t.Fatalf("restart = %+v", res)
	}
	st := p.Status()
	if st.Live != 2 {
		t.Fatalf("%d instances live after restart, want 2", st.Live)
	}
	for _, in := range st.Instances {
		old, ok := before[in.Key]
		if !ok || old == in.ID || in.PID == 0 {
			t.Fatalf("instance %+v is not a fresh replacement for a previous key (before %v)", in, before)
		}
	}
	got, err := p.Call(ctx, "session:a", "state", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(textOf(t, got), `"1"`) {
		t.Fatal("state survived a restart")
	}

	// With nothing running, a scoped pool has no caller to start one for.
	p.Restart(ctx, true)
	if res := p.Restart(ctx, false); len(res.Started) != 0 || res.Note == "" {
		t.Fatalf("scoped pool with nothing running: %+v", res)
	}
}

// A global pool with nothing running starts one, so restart verifies the
// server comes up; a replacement that does not come up is reported with the
// server's stderr, not swallowed until the next call.
func TestRestartStartsGlobalAndSurfacesStderr(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken")
	wrapper := filepath.Join(dir, "wrap.sh")
	script := "#!/bin/sh\nif [ -e " + broken + " ]; then echo 'fatal: missing API_TOKEN' >&2; exit 3; fi\nexec " + bin + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	p := pool.New(resolved(t, wrapper, &config.Extras{Sharing: config.SharingShared, Scope: config.ScopeGlobal}))
	defer p.Close()
	ctx := context.Background()

	res := p.Restart(ctx, false)
	if res.Stopped != 0 || len(res.Started) != 1 || p.Status().Live != 1 {
		t.Fatalf("restart of an idle global pool = %+v, live %d", res, p.Status().Live)
	}
	res = p.Restart(ctx, false)
	if res.Stopped != 1 || len(res.Started) != 1 {
		t.Fatalf("second restart = %+v", res)
	}

	if err := os.WriteFile(broken, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	res = p.Restart(ctx, false)
	if res.Stopped != 1 || len(res.Started) != 0 || len(res.Failed) != 1 {
		t.Fatalf("restart into a broken server = %+v", res)
	}
	if !strings.Contains(res.Failed[0].Error, "missing API_TOKEN") {
		t.Fatalf("failure does not carry the server's stderr: %q", res.Failed[0].Error)
	}
}

func TestCallTimeoutIsEnforced(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{
		Sharing: config.SharingShared, Scope: config.ScopeGlobal, CallTimeout: "150ms",
	}))
	defer p.Close()

	_, err := p.Call(context.Background(), "", "slow", map[string]any{"ms": 3000})
	if err == nil {
		t.Fatal("expected a timeout")
	}
	if !strings.Contains(err.Error(), "deadline") && !strings.Contains(err.Error(), "context") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIdleReaperStopsUnusedInstances(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{
		Sharing: config.SharingExclusive, Scope: config.ScopeSession, Max: 2, IdleTimeout: "10ms",
	}))
	defer p.Close()

	if _, err := p.Call(context.Background(), "s", "echo", map[string]any{"message": "x"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if n := p.ReapIdle(time.Now()); n != 1 {
		t.Fatalf("reaper should have stopped 1 instance, stopped %d", n)
	}
}

func TestSharedSharingAdmitsConcurrentHoldersOnOneKey(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{
		Sharing: config.SharingShared, Scope: config.ScopeSession, Max: 4,
	}))
	defer p.Close()

	// One key, four concurrent callers. Shared sharing must let them overlap
	// on a single process rather than serialising or forking more: all four
	// must be in that one process at once.
	barrier(t, p, "session:one", 4)
	if st := p.Status(); st.Live != 1 {
		t.Fatalf("one key must mean one process, got %d", st.Live)
	}
}

func TestExclusiveSharingSerialisesOneKey(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{
		Sharing: config.SharingExclusive, Scope: config.ScopeSession, Max: 4,
	}))
	defer p.Close()

	const n = 3
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = p.Call(context.Background(), "session:one", "slow", map[string]any{"ms": 150})
		}()
	}
	wg.Wait()
	// Three 150ms calls queued behind one another cannot finish in under
	// 300ms; if they did, they overlapped and exclusivity is broken.
	if d := time.Since(start); d < 300*time.Millisecond {
		t.Fatalf("exclusive sharing overlapped: %s for %d serialised 150ms calls", d, n)
	}
	if st := p.Status(); st.Live != 1 {
		t.Fatalf("exclusivity must queue, not fork: %d live", st.Live)
	}
}

func TestDistinctKeysGetDistinctProcesses(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{
		Sharing: config.SharingExclusive, Scope: config.ScopeSession, Max: 3,
	}))
	defer p.Close()

	ctx := context.Background()
	pids := map[string]bool{}
	for _, k := range []string{"session:a", "session:b", "session:c"} {
		if _, err := p.Call(ctx, k, "open", map[string]any{"value": k}); err != nil {
			t.Fatal(err)
		}
		res, err := p.Call(ctx, k, "state", map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			PID  int      `json:"pid"`
			Seen []string `json:"seen"`
		}
		if err := json.Unmarshal([]byte(textOf(t, res)), &got); err != nil {
			t.Fatal(err)
		}
		if len(got.Seen) != 1 || got.Seen[0] != k {
			t.Fatalf("key %q saw %v; state leaked across keys", k, got.Seen)
		}
		pids[fmt.Sprint(got.PID)] = true
	}
	if len(pids) != 3 {
		t.Fatalf("three keys must mean three processes, got %d", len(pids))
	}
}

func TestPidScopedInstanceIsReapedWhenThePidExits(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{
		Sharing: config.SharingExclusive, Scope: config.ScopePid, Max: 2,
		IdleTimeout: "1h", // prove the pid, not the timer, did the work
	}))
	defer p.Close()

	// A process that is already gone: its instance has no possible caller.
	dead := exec.Command("true")
	if err := dead.Run(); err != nil {
		t.Fatal(err)
	}
	deadPID := dead.Process.Pid

	if _, err := p.Call(context.Background(), fmt.Sprintf("pid:%d", deadPID), "echo",
		map[string]any{"message": "x"}); err != nil {
		t.Fatal(err)
	}
	if st := p.Status(); st.Live != 1 {
		t.Fatalf("want 1 live, got %d", st.Live)
	}
	if n := p.ReapIdle(time.Now()); n != 1 {
		t.Fatalf("an instance whose pid exited must be reaped, reaped %d", n)
	}
}

func TestPidScopedInstanceSurvivesWhileThePidLives(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{
		Sharing: config.SharingExclusive, Scope: config.ScopePid, Max: 2,
		IdleTimeout: "1h",
	}))
	defer p.Close()

	if _, err := p.Call(context.Background(), fmt.Sprintf("pid:%d", os.Getpid()), "echo",
		map[string]any{"message": "x"}); err != nil {
		t.Fatal(err)
	}
	if n := p.ReapIdle(time.Now()); n != 0 {
		t.Fatalf("a live pid must keep its instance, reaped %d", n)
	}
}

func TestReleaseKeyStopsOnlyThatKey(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{
		Sharing: config.SharingExclusive, Scope: config.ScopeSession, Max: 3,
	}))
	defer p.Close()

	ctx := context.Background()
	for _, k := range []string{"session:a", "session:b"} {
		if _, err := p.Call(ctx, k, "echo", map[string]any{"message": "x"}); err != nil {
			t.Fatal(err)
		}
	}
	if n := p.ReleaseKey("session:a"); n != 1 {
		t.Fatalf("want 1 released, got %d", n)
	}
	if st := p.Status(); st.Live != 1 {
		t.Fatalf("releasing one key must not touch the other: %d live", st.Live)
	}
}
