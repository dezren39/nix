package pool_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/mcpclient"
	"github.com/dezren39/mcpx/internal/pool"
)

// eraFor acquires on behalf of a caller speaking version ("" = none) and
// reports which era and instance served it.
func eraFor(t *testing.T, p *pool.Pool, version string) (mcpclient.Era, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	lease, err := p.Acquire(mcpclient.WithCallerVersion(ctx, version), "")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	return lease.Client().Era, lease.InstanceID()
}

// Under protocol: follow a legacy caller gets a legacy session of a dual-era
// server -- the only kind a server can send requests to its client on --
// while a modern caller keeps the modern one, and the era cache still
// records the server's own (modern) era.
func TestFollowGivesLegacyCallerALegacySession(t *testing.T) {
	s := newEraServer(t, "dual", nil)
	s.cfg.Protocol = "follow"
	s.cfg.Max = 4
	eras := pool.OpenEraFile(filepath.Join(t.TempDir(), "eras.json"))
	p := followPool(s, eras)
	defer p.Close()

	modernEra, modernID := eraFor(t, p, "2026-07-28")
	if modernEra != mcpclient.EraModern {
		t.Fatalf("modern caller got %s, want modern", modernEra)
	}
	legacyEra, legacyID := eraFor(t, p, "2025-11-25")
	if legacyEra != mcpclient.EraLegacy {
		t.Fatalf("legacy caller got %s, want legacy", legacyEra)
	}
	if legacyID == modernID {
		t.Fatalf("legacy caller shared the modern instance %s", modernID)
	}
	if e, id := eraFor(t, p, "2025-06-18"); e != mcpclient.EraLegacy || id != legacyID {
		t.Fatalf("second legacy caller got %s on %s, want the legacy instance %s", e, id, legacyID)
	}
	if e, id := eraFor(t, p, "2026-07-28"); e != mcpclient.EraModern || id != modernID {
		t.Fatalf("modern caller got %s on %s after the legacy session, want %s", e, id, modernID)
	}
	if e, _ := eraFor(t, p, ""); e != mcpclient.EraModern {
		t.Fatalf("a call with no caller revision got %s, want modern", e)
	}
	if rec, ok := eras.Get(pool.Identity(s.cfg)); !ok || rec.Era != mcpclient.EraModern {
		t.Fatalf("era cache = %+v, %v; want modern -- the legacy session must not overwrite it", rec, ok)
	}
}

// A modern-only server has no legacy session to give: the legacy caller is
// served from the modern one rather than failed.
func TestFollowFallsBackWhenServerIsModernOnly(t *testing.T) {
	s := newEraServer(t, "modern", nil)
	s.cfg.Protocol = "follow"
	s.cfg.Max = 4
	p := followPool(s, nil)
	defer p.Close()
	if e, _ := eraFor(t, p, "2025-11-25"); e != mcpclient.EraModern {
		t.Fatalf("legacy caller got %s, want the modern session", e)
	}
	if e, _ := eraFor(t, p, "2025-11-25"); e != mcpclient.EraModern {
		t.Fatalf("second legacy caller got %s, want the modern session", e)
	}
}

// Without follow, the caller's revision changes nothing.
func TestModernPreferenceIgnoresCallerEra(t *testing.T) {
	s := newEraServer(t, "dual", nil)
	s.cfg.Max = 4
	p := followPool(s, nil)
	defer p.Close()
	if e, _ := eraFor(t, p, "2025-11-25"); e != mcpclient.EraModern {
		t.Fatalf("legacy caller under protocol modern got %s, want modern", e)
	}
}

// followPool is newEraPool with a probe timeout long enough that a busy
// machine cannot turn a dual server's discover into a legacy win.
func followPool(s *eraServer, eras pool.EraStore) *pool.Pool {
	p := pool.New(s.cfg)
	p.Hooks = &pool.Hooks{Eras: eras, ProbeTimeout: 10 * time.Second}
	return p
}

// Each era lane has its own Max. With a global scope (Max 1), a modern call
// in progress must not stall a legacy caller -- and the reverse -- which is
// what a single shared slot did: a legacy call the server never finished
// left every later call waiting for the slot.
func TestFollowLanesHaveTheirOwnCapacity(t *testing.T) {
	s := newEraServer(t, "dual", nil)
	s.cfg.Protocol = "follow"
	s.cfg.Max = 1
	p := followPool(s, nil)
	defer p.Close()

	for _, held := range []string{"2026-07-28", "2025-11-25"} {
		other := map[string]string{"2026-07-28": "2025-11-25", "2025-11-25": "2026-07-28"}[held]
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		lease, err := p.Acquire(mcpclient.WithCallerVersion(ctx, held), "")
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
		second, err := p.Acquire(mcpclient.WithCallerVersion(ctx, other), "")
		cancel()
		if err != nil {
			lease.Release()
			t.Fatalf("a %s caller waited on the %s caller's slot: %v", other, held, err)
		}
		second.Release()
		lease.Release()
	}
}
