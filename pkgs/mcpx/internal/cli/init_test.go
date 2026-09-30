package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dezren39/mcpx/internal/config"
)

// TestStarterConfigMeansWhatItSays loads what `mcpx init` writes and checks
// each server resolves to what its comment promises.
//
// The starter file said "mode": "session" under a comment promising one
// browser per script run. The loader has no mode -- it was split into
// sharing and scope -- and dropped the key, so every new user's browser
// server was one shared process: the most important knob in the file, inert
// from the first command.
func TestStarterConfigMeansWhatItSays(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".mcpx.json")
	if err := os.WriteFile(p, []byte(starterConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(p)
	if err != nil {
		t.Fatalf("the starter config does not load: %v", err)
	}
	if len(c.Ignored) > 0 {
		t.Errorf("the starter config carries keys mcpx does not read: %+v", c.Ignored)
	}
	browser, err := c.Resolve("chrome-devtools")
	if err != nil {
		t.Fatal(err)
	}
	if browser.Scope != config.ScopeSession || browser.Sharing != config.SharingExclusive || browser.Max != 4 {
		t.Errorf("chrome-devtools promises one browser per session, one caller at a time, up to 4; "+
			"got scope=%s sharing=%s max=%d", browser.Scope, browser.Sharing, browser.Max)
	}
	search, err := c.Resolve("example-stateless")
	if err != nil {
		t.Fatal(err)
	}
	if search.Scope != config.ScopeGlobal || search.Sharing != config.SharingShared {
		t.Errorf("example-stateless promises one process for every caller; got scope=%s sharing=%s",
			search.Scope, search.Sharing)
	}
}
