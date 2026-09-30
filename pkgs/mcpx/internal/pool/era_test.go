package pool_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
	p.Restart()
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
		began := time.Now()
		startOnce(t, p2)
		if got := strings.Join(s.frames(t), ","); got != "initialize" {
			t.Errorf("frames after restart = %s", got)
		}
		// A silent legacy server costs the probe timeout cold; warm it
		// must not.
		if d := time.Since(began); d >= 150*time.Millisecond {
			t.Errorf("warm start took %v, which is the probe timeout", d)
		}
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
		for _, junk := range []string{`{"version":1,"servers":{"x":`, `not json`, `{"version":999,"servers":{}}`} {
			if err := os.WriteFile(file, []byte(junk), 0o600); err != nil {
				t.Fatal(err)
			}
			p := newEraPool(s, pool.OpenEraFile(file))
			startOnce(t, p)
			p.Close()
			if got := s.frames(t); len(got) == 0 || got[0] != "server/discover" {
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
		s.cfg.Protocol = "force-modern"
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
		p.Restart()
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
