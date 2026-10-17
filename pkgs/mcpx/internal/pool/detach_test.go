package pool_test

import (
	"context"
	"errors"
	"testing"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/pool"
	"github.com/dezren39/mcpx/internal/testsupport"
)

func TestDetachRefusesWhileACallIsInFlight(t *testing.T) {
	bin := testsupport.FakeMCPBinary(t)
	p := pool.New(resolved(t, bin, &config.Extras{Sharing: config.SharingExclusive, Scope: config.ScopeSession, Max: 2}))
	defer p.Close()

	lease, err := p.Acquire(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Detach(); !errors.Is(err, pool.ErrBusy) {
		t.Fatalf("detach with a call in flight: %v", err)
	}
	if st := p.Status(); st.Live != 1 {
		t.Fatalf("a refused detach must leave the child serving, %d live", st.Live)
	}
	lease.Release()

	h, err := p.Detach()
	if err != nil {
		t.Fatalf("detach once the call finished: %v", err)
	}
	if len(h.Instances) != 1 {
		t.Fatalf("want the one child handed on, got %d", len(h.Instances))
	}
	p.Commit()
}
