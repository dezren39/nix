package pool_test

import (
	"context"
	"encoding/json"
	"fmt"
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
	p := pool.New(resolved(t, bin, &config.Extras{Mode: config.ModeShared}))
	defer p.Close()

	ctx := context.Background()
	pids := map[string]bool{}
	for i := 0; i < 5; i++ {
		res, err := p.Call(ctx, fmt.Sprintf("session-%d", i), "state", map[string]any{})
		if err != nil {
			t.Fatalf("call: %v", err)
		}
		pids[textOf(t, res)] = true
	}
	if len(pids) != 1 {
		t.Fatalf("shared mode should use one process, saw %d distinct states: %v", len(pids), pids)
	}
}

func TestSharedModeHandlesConcurrentCalls(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{Mode: config.ModeShared}))
	defer p.Close()

	// Ten 200ms calls on one process must overlap, proving requests are
	// multiplexed rather than serialised.
	const n = 10
	start := time.Now()
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = p.Call(context.Background(), "", "slow", map[string]any{"ms": 200})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
		t.Fatalf("10 concurrent 200ms calls took %s; they appear serialised", elapsed)
	}
}

func TestSessionModeIsolatesState(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{Mode: config.ModeSession, Max: 3}))
	defer p.Close()

	ctx := context.Background()
	const n = 3
	var wg sync.WaitGroup
	states := make([]string, n)
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			session := fmt.Sprintf("run-%d", i)
			value := fmt.Sprintf("value-%d", i)
			if _, err := p.Call(ctx, session, "open", map[string]any{"value": value}); err != nil {
				errs[i] = err
				return
			}
			res, err := p.Call(ctx, session, "state", map[string]any{})
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
	p := pool.New(resolved(t, bin, &config.Extras{Mode: config.ModeSession, Max: 4}))
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
	p := pool.New(resolved(t, bin, &config.Extras{Mode: config.ModeSession, Max: 2}))
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
			p.ReleaseSession(session)
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
	p := pool.New(resolved(t, bin, &config.Extras{Mode: config.ModeSession, Max: 2}))
	defer p.Close()

	ctx := context.Background()
	if _, err := p.Call(ctx, "s1", "echo", map[string]any{"message": "hi"}); err != nil {
		t.Fatal(err)
	}
	if st := p.Status(); st.Live != 1 {
		t.Fatalf("want 1 live, got %d", st.Live)
	}
	if n := p.ReleaseSession("s1"); n != 1 {
		t.Fatalf("want 1 released, got %d", n)
	}
	if st := p.Status(); st.Live != 0 {
		t.Fatalf("instance should be gone, %d still live", st.Live)
	}
}

func TestPooledModeRecyclesInstances(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{Mode: config.ModePooled, Max: 2}))
	defer p.Close()

	ctx := context.Background()
	for i := 0; i < 6; i++ {
		if _, err := p.Call(ctx, "", "echo", map[string]any{"message": "x"}); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if st := p.Status(); st.Live > 2 {
		t.Fatalf("pooled mode exceeded max: %d", st.Live)
	}
}

func TestStartFailureIsReportedWithStderr(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	r := resolved(t, bin, &config.Extras{Mode: config.ModeShared})
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
	r := resolved(t, bin, &config.Extras{Mode: config.ModeShared})
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
	p := pool.New(resolved(t, bin, &config.Extras{Mode: config.ModeShared}))
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
	p := pool.New(resolved(t, bin, &config.Extras{Mode: config.ModeShared}))
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

func TestToolAllowlistAndDenylist(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{
		Mode:         config.ModeShared,
		Tools:        []string{"echo", "state", "boom"},
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
	if !names["echo"] || !names["state"] {
		t.Fatalf("allowlisted tools missing: %v", names)
	}
	if names["boom"] {
		t.Fatal("excludeTools should win over tools")
	}
	if len(names) != 2 {
		t.Fatalf("expected exactly 2 tools, got %v", names)
	}
}

func TestRestartStopsEverything(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{Mode: config.ModeSession, Max: 2}))
	defer p.Close()

	ctx := context.Background()
	if _, err := p.Call(ctx, "a", "open", map[string]any{"value": "1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Call(ctx, "b", "open", map[string]any{"value": "2"}); err != nil {
		t.Fatal(err)
	}
	if n := p.Restart(); n != 2 {
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

func TestCallTimeoutIsEnforced(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{
		Mode: config.ModeShared, CallTimeout: "150ms",
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
		Mode: config.ModeSession, Max: 2, IdleTimeout: "10ms",
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
