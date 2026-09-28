package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dezren39/mcpx/internal/config"
)

// tree builds nested directories each holding a config, and chdirs to the
// deepest one.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg-empty"))
	t.Setenv("MCPX_CONFIG", "")
	return root
}

func chdirTo(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

func TestProjectConfigAddsToRatherThanReplacesParent(t *testing.T) {
	root := tree(t, map[string]string{
		".config/mcpx/config.json":      `{"mcpServers":{"parent":{"command":"p"}}}`,
		"proj/.config/mcpx/config.json": `{"mcpServers":{"child":{"command":"c"}}}`,
	})
	chdirTo(t, filepath.Join(root, "proj"))

	c, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.MCPServers["parent"]; !ok {
		t.Fatal("a project config must not hide inherited servers")
	}
	if _, ok := c.MCPServers["child"]; !ok {
		t.Fatal("the project's own server is missing")
	}
}

func TestNearerFileWinsPerServerName(t *testing.T) {
	root := tree(t, map[string]string{
		".config/mcpx/config.json":      `{"mcpServers":{"dup":{"command":"parent"}}}`,
		"proj/.config/mcpx/config.json": `{"mcpServers":{"dup":{"command":"child"}}}`,
	})
	chdirTo(t, filepath.Join(root, "proj"))

	c, _ := config.Load("")
	if got := c.MCPServers["dup"].Command; got != "child" {
		t.Fatalf("nearest must win, got %q", got)
	}
	if c.Origin["dup"] == "" {
		t.Fatal("origin should record which file defined the winner")
	}
}

func TestInheritedServerCanBeDisabledLocally(t *testing.T) {
	root := tree(t, map[string]string{
		".config/mcpx/config.json":      `{"mcpServers":{"noisy":{"command":"p"}}}`,
		"proj/.config/mcpx/config.json": `{"mcpServers":{"noisy":{"command":"p","mcpx":{"disabled":true}}}}`,
	})
	chdirTo(t, filepath.Join(root, "proj"))

	c, _ := config.Load("")
	all, err := c.ResolveAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range all {
		if s.Name == "noisy" {
			t.Fatal("a locally disabled server must not resolve")
		}
	}
}

func TestDefaultsMergeFieldByField(t *testing.T) {
	root := tree(t, map[string]string{
		// scope:session so max is not clamped the way a single-key scope is.
		".config/mcpx/config.json":      `{"defaults":{"idleTimeout":"9m","max":7,"scope":"session"},"mcpServers":{"a":{"command":"x"}}}`,
		"proj/.config/mcpx/config.json": `{"defaults":{"max":2},"mcpServers":{"b":{"command":"y"}}}`,
	})
	chdirTo(t, filepath.Join(root, "proj"))

	c, _ := config.Load("")
	r, err := c.Resolve("a")
	if err != nil {
		t.Fatal(err)
	}
	if r.Max != 2 {
		t.Fatalf("the nearer default should win: max=%d", r.Max)
	}
	if r.IdleTimeout.String() != "9m0s" {
		t.Fatalf("an unset field should be inherited: idle=%s", r.IdleTimeout)
	}
}

func TestSourcesAreRecordedNearestFirst(t *testing.T) {
	root := tree(t, map[string]string{
		".config/mcpx/config.json":      `{"mcpServers":{"a":{"command":"x"}}}`,
		"proj/.config/mcpx/config.json": `{"mcpServers":{"b":{"command":"y"}}}`,
	})
	chdirTo(t, filepath.Join(root, "proj"))

	c, _ := config.Load("")
	if len(c.Sources) != 2 {
		t.Fatalf("both files should be recorded, got %v", c.Sources)
	}
	if filepath.Dir(filepath.Dir(filepath.Dir(c.Sources[0]))) != filepath.Join(root, "proj") {
		t.Fatalf("nearest should be first, got %v", c.Sources)
	}
	if c.Path != c.Sources[0] {
		t.Fatalf("Path should be the nearest source")
	}
}

func TestExplicitConfigIsUsedAlone(t *testing.T) {
	root := tree(t, map[string]string{
		".config/mcpx/config.json":      `{"mcpServers":{"parent":{"command":"p"}}}`,
		"proj/.config/mcpx/config.json": `{"mcpServers":{"child":{"command":"c"}}}`,
	})
	chdirTo(t, filepath.Join(root, "proj"))

	explicit := filepath.Join(root, "proj", ".config", "mcpx", "config.json")
	c, err := config.Load(explicit)
	if err != nil {
		t.Fatal(err)
	}
	// Naming a file means meaning it; nothing else should be folded in.
	if _, ok := c.MCPServers["parent"]; ok {
		t.Fatal("--config must not merge the rest of the chain")
	}
	if len(c.Sources) != 1 {
		t.Fatalf("one source expected, got %v", c.Sources)
	}
}

func TestMalformedFarFileIsReportedNotSkipped(t *testing.T) {
	root := tree(t, map[string]string{
		".config/mcpx/config.json":      `{ this is not json`,
		"proj/.config/mcpx/config.json": `{"mcpServers":{"b":{"command":"y"}}}`,
	})
	chdirTo(t, filepath.Join(root, "proj"))

	if _, err := config.Load(""); err == nil {
		t.Fatal("a broken file in the chain must surface, not be silently ignored")
	}
}
