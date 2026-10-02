package pool_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/mcpclient"
	"github.com/dezren39/mcpx/internal/pool"
	"github.com/dezren39/mcpx/internal/testsupport"
)

// eraServer is one eramcp configuration with its own frame log.
type eraServer struct {
	cfg *config.Resolved
	log string
}

func newEraServer(t *testing.T, mode string, extra map[string]string) *eraServer {
	t.Helper()
	bin := testsupport.EraMCPBinary(t)
	log := filepath.Join(t.TempDir(), "frames")
	env := map[string]string{"ERAMCP_MODE": mode, "ERAMCP_LOG": log}
	for k, v := range extra {
		env[k] = v
	}
	cfg := &config.Config{MCPServers: map[string]*config.Server{
		"era": {Name: "era", Command: bin, Env: env},
	}}
	r, err := cfg.Resolve("era")
	if err != nil {
		t.Fatal(err)
	}
	return &eraServer{cfg: r, log: log}
}

// frames returns the methods the server process(es) received, and clears the
// log so the next start can be asserted on alone.
func (s *eraServer) frames(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(s.log)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	_ = os.Remove(s.log)
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		// Notifications are dropped: a process stopped straight after the
		// handshake may be killed before it logs the one that ended it.
		if l != "" && !strings.HasPrefix(l, "notifications/") {
			out = append(out, l)
		}
	}
	return out
}

func startOnce(t *testing.T, p *pool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	lease, err := p.Acquire(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()
	p.Restart(context.Background(), true)
}

func newEraPool(s *eraServer, eras pool.EraStore) *pool.Pool {
	p := pool.New(s.cfg)
	p.Hooks = &pool.Hooks{Eras: eras, ProbeTimeout: 150 * time.Millisecond}
	return p
}

// lifecycleEvents captures pool lifecycle events of one kind. Lifecycle is a
// package-level hook, so tests using it do not run in parallel.
func lifecycleEvents(t *testing.T, kind string) func() []map[string]any {
	t.Helper()
	var mu sync.Mutex
	var got []map[string]any
	prev := pool.Lifecycle
	pool.Lifecycle = func(event string, attrs map[string]any) {
		if event == kind {
			mu.Lock()
			got = append(got, attrs)
			mu.Unlock()
		}
	}
	t.Cleanup(func() { pool.Lifecycle = prev })
	return func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]any(nil), got...)
	}
}

func readEraFile(t *testing.T, path string) map[string]pool.EraRecord {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Version int                       `json:"version"`
		Servers map[string]pool.EraRecord `json:"servers"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("era file is not valid JSON: %v\n%s", err, b)
	}
	return doc.Servers
}

// https://modelcontextprotocol.io/specification/2026-07-28/basic/versioning#backward-compatibility-with-initialization-based-versions
func TestEraCacheAcrossStarts(t *testing.T) {
	t.Run("2026-07-28/era-cache/cached-legacy-server-gets-no-discover", func(t *testing.T) {
		s := newEraServer(t, "legacy-32601", nil)
		file := filepath.Join(t.TempDir(), "eras.json")
		p := newEraPool(s, pool.OpenEraFile(file))
		defer p.Close()

		startOnce(t, p)
		if got := strings.Join(s.frames(t), ","); got != "server/discover,initialize" {
			t.Fatalf("cold start frames = %s", got)
		}
		startOnce(t, p)
		if got := strings.Join(s.frames(t), ","); got != "initialize" {
			t.Errorf("warm start must not probe: %s", got)
		}
		rec := readEraFile(t, file)[pool.Identity(s.cfg)]
		if rec.Era != mcpclient.EraLegacy || rec.Version != "2025-06-18" || rec.Source != mcpclient.SourceProbe {
			t.Errorf("record = %+v", rec)
		}
	})

	t.Run("2026-07-28/era-cache/cache-survives-pool-recreation", func(t *testing.T) {
		s := newEraServer(t, "legacy-silent", nil)
		file := filepath.Join(t.TempDir(), "eras.json")
		p := newEraPool(s, pool.OpenEraFile(file))
		startOnce(t, p)
		p.Close()
		_ = s.frames(t)

		// A new pool and a fresh read of the file: a daemon restart.
		p2 := newEraPool(s, pool.OpenEraFile(file))
		defer p2.Close()
		startOnce(t, p2)
		if got := strings.Join(s.frames(t), ","); got != "initialize" {
			t.Errorf("frames after restart = %s", got)
		}
		// No discover frame is the proof the cache was used: a silent legacy
		// server only costs the probe timeout when discover is sent. A
		// wall-clock bound said the same thing and failed under load.
	})

	t.Run("2026-07-28/era-cache/stale-cache-reprobes-and-rewrites", func(t *testing.T) {
		stale := lifecycleEvents(t, "server.era.stale")
		s := newEraServer(t, "legacy-32601", nil)
		file := filepath.Join(t.TempDir(), "eras.json")
		store := pool.OpenEraFile(file)
		if err := store.Put(pool.Identity(s.cfg), pool.EraRecord{Era: mcpclient.EraModern, Version: "2026-07-28", Source: "probe"}); err != nil {
			t.Fatal(err)
		}
		p := newEraPool(s, store)
		defer p.Close()
		startOnce(t, p)
		if rec := readEraFile(t, file)[pool.Identity(s.cfg)]; rec.Era != mcpclient.EraLegacy {
			t.Errorf("cache not rewritten: %+v", rec)
		}
		ev := stale()
		if len(ev) != 1 || ev[0]["cached"] != "modern" || ev[0]["era"] != "legacy" {
			t.Errorf("stale events = %v", ev)
		}
	})

	t.Run("2026-07-28/era-cache/corrupt-file-is-ignored-and-rewritten", func(t *testing.T) {
		s := newEraServer(t, "legacy-32601", nil)
		file := filepath.Join(t.TempDir(), "eras.json")
		// The wrong-version case names this very server as legacy: accepted,
		// it would skip the probe, so the assertion below tells them apart.
		other, _ := json.Marshal(map[string]any{"version": 999, "servers": map[string]any{
			pool.Identity(s.cfg): map[string]any{"era": "legacy", "source": "probe"},
		}})
		for _, junk := range []string{`{"version":1,"servers":{"x":`, `not json`, string(other)} {
			if err := os.WriteFile(file, []byte(junk), 0o600); err != nil {
				t.Fatal(err)
			}
			p := newEraPool(s, pool.OpenEraFile(file))
			startOnce(t, p)
			p.Close()
			// A probe happened: discover was sent. Not that it came first --
			// after ProbeTimeout (150ms here) initialize goes out alongside
			// it, and on a loaded machine (the nix build runs every package's
			// tests at once) the two can reach the server's log in either
			// order. Trusting the file instead would send initialize alone.
			if got := s.frames(t); !slices.Contains(got, "server/discover") {
				t.Errorf("%q: a bad file should mean a probe, got %v", junk, got)
			}
			if rec := readEraFile(t, file)[pool.Identity(s.cfg)]; rec.Era != mcpclient.EraLegacy {
				t.Errorf("%q: not rewritten: %+v", junk, rec)
			}
			fi, err := os.Stat(file)
			if err != nil {
				t.Fatal(err)
			}
			if fi.Mode().Perm() != 0o600 {
				t.Errorf("era file mode = %v, want private", fi.Mode().Perm())
			}
		}
	})

	t.Run("2026-07-28/era-cache/force-never-reads-the-cache", func(t *testing.T) {
		s := newEraServer(t, "dual", nil)
		s.cfg.Protocol = string(mcpclient.ForceModern)
		store := pool.OpenEraFile(filepath.Join(t.TempDir(), "eras.json"))
		_ = store.Put(pool.Identity(s.cfg), pool.EraRecord{Era: mcpclient.EraLegacy, Source: "probe"})
		p := newEraPool(s, store)
		defer p.Close()
		startOnce(t, p)
		if got := s.frames(t); len(got) != 1 || got[0] != "server/discover" {
			t.Errorf("frames = %v", got)
		}
	})
}

// https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/stdio#backward-compatibility
func TestPoolProbe(t *testing.T) {
	t.Run("2026-07-28/stdio-compat/legacy-that-exits-on-discover-is-respawned-legacy", func(t *testing.T) {
		s := newEraServer(t, "legacy-exit", nil)
		file := filepath.Join(t.TempDir(), "eras.json")
		p := newEraPool(s, pool.OpenEraFile(file))
		defer p.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		res, err := p.Call(ctx, "", "hello", nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(res), "legacy-exit") {
			t.Errorf("result = %s", res)
		}
		if got := strings.Join(s.frames(t), ","); !strings.HasPrefix(got, "server/discover,initialize") {
			t.Errorf("frames = %s", got)
		}
		if rec := readEraFile(t, file)[pool.Identity(s.cfg)]; rec.Era != mcpclient.EraLegacy {
			t.Errorf("record = %+v", rec)
		}
		p.Restart(context.Background(), true)
		startOnce(t, p)
		if got := strings.Join(s.frames(t), ","); got != "initialize" {
			t.Errorf("second start = %s", got)
		}
	})

	t.Run("2026-07-28/stdio-compat/slow-modern-process-is-modern", func(t *testing.T) {
		s := newEraServer(t, "modern", map[string]string{"ERAMCP_DISCOVER_DELAY": "500ms"})
		p := newEraPool(s, nil)
		defer p.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if _, err := p.Call(ctx, "", "hello", nil); err != nil {
			t.Fatal(err)
		}
		if era, v := p.Era(); era != mcpclient.EraModern || v != "2026-07-28" {
			t.Errorf("era=%q version=%q", era, v)
		}
	})

	t.Run("2026-07-28/stdio-compat/default-preference-is-modern-first", func(t *testing.T) {
		s := newEraServer(t, "legacy-32601", nil)
		p := pool.New(s.cfg)
		defer p.Close()
		if p.Preference() != mcpclient.PreferModern {
			t.Errorf("preference = %q", p.Preference())
		}
		startOnce(t, p)
		if got := s.frames(t); len(got) == 0 || got[0] != "server/discover" {
			t.Errorf("frames = %v", got)
		}
	})
}

func TestEraIdentity(t *testing.T) {
	// exclusive picks the leasing knobs; they must not change the identity.
	mk := func(exclusive int, headers map[string]string) *config.Resolved {
		ex := &config.Extras{}
		if exclusive > 1 {
			ex = &config.Extras{Sharing: config.SharingExclusive, Scope: "session", Max: exclusive}
		}
		cfg := &config.Config{MCPServers: map[string]*config.Server{
			"s": {Name: "s", Command: "x", Headers: headers, Env: map[string]string{"B": "2", "A": "1"}, Mcpx: ex},
		}}
		r, err := cfg.Resolve("s")
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	h := map[string]string{"a": "1", "b": "2", "c": "3", "d": "4", "e": "5", "f": "6"}
	t.Run("upstream/era-identity-ignores-leasing-knobs", func(t *testing.T) {
		a, b := mk(1, h), mk(9, h)
		if a.Sharing == b.Sharing || a.Max == b.Max {
			t.Fatalf("the two configs should differ in leasing: %v/%d vs %v/%d", a.Sharing, a.Max, b.Sharing, b.Max)
		}
		if pool.Identity(a) != pool.Identity(b) {
			t.Error("sharing, scope and max do not change what a server speaks")
		}
	})
	t.Run("upstream/era-identity-is-deterministic", func(t *testing.T) {
		want := pool.Identity(mk(1, h))
		for i := 0; i < 50; i++ {
			if pool.Identity(mk(1, h)) != want {
				t.Fatal("identity differs between computations of the same config")
			}
		}
	})
	t.Run("upstream/era-identity-follows-the-process-definition", func(t *testing.T) {
		if pool.Identity(mk(1, h)) == pool.Identity(mk(1, map[string]string{"a": "other"})) {
			t.Error("different headers are a different server")
		}
	})
}
