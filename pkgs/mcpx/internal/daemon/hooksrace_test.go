package daemon

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/events"
)

// OnListChanged runs on an upstream client's goroutine whenever a server
// says its list changed, and read r.pools without r.mu while Reload -- a
// config edit -- replaced it under r.mu. Run with -race; without it this
// passes either way, which is why it went unseen.
func TestListChangedDuringReloadDoesNotRace(t *testing.T) {
	dir := t.TempDir()
	cfgOf := func(names ...string) *config.Config {
		c := &config.Config{MCPServers: map[string]*config.Server{}}
		for _, n := range names {
			c.MCPServers[n] = &config.Server{Command: filepath.Join(dir, "never-run-"+n)}
		}
		return c
	}
	r, err := NewRegistry(cfgOf("a"), Paths{State: dir, Cache: dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.InstallHooks(events.New(0), nil, nil)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 300; i++ {
			r.hooks.OnListChanged("a", "tools")
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 300; i++ {
			names := []string{"a"}
			if i%2 == 0 {
				names = append(names, "b")
			}
			if _, _, err := r.Reload(cfgOf(names...)); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	wg.Wait()
}
