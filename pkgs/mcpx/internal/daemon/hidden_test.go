package daemon

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/config"
)

// excludeTools removed a tool from every listing and left it callable by
// name: the filter was a property of what mcpx showed, not of what it did.
// Each of the three ways a tool call reaches a pool must refuse it, and must
// do so before starting anything -- the command here does not exist, so a
// call that got past the check fails differently.
func TestAHiddenToolCannotBeCalled(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{MCPServers: map[string]*config.Server{
		"demo": {Command: filepath.Join(dir, "never-run"), Mcpx: &config.Extras{
			ExcludeTools: []string{"delete_*", "/^execute_/"}}},
		// Same process, no filter: hiding is per view.
		"open": {AliasOf: "demo"},
	}}
	r, err := NewRegistry(cfg, Paths{State: dir, Cache: dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cc := config.CallContext{}
	paths := map[string]func(server, tool string) error{
		"Call": func(s, tool string) error { _, err := r.Call(ctx, s, tool, cc, map[string]any{}); return err },
		"CallReporting": func(s, tool string) error {
			_, err := r.CallReporting(ctx, "id-r", s, tool, cc, map[string]any{})
			return err
		},
		"CallAsk": func(s, tool string) error {
			_, err := r.CallAsk(ctx, "id-a", s, tool, cc, map[string]any{})
			return err
		},
	}
	for name, call := range paths {
		for _, tool := range []string{"delete_workflow", "execute_workflow"} {
			err := call("demo", tool)
			var hidden HiddenTool
			if !errors.As(err, &hidden) {
				t.Errorf("%s demo.%s: want HiddenTool, got %v", name, tool, err)
			}
			if failureStatus(err) != http.StatusBadRequest {
				t.Errorf("%s demo.%s: status %d, want 400", name, tool, failureStatus(err))
			}
		}
		// Reached the pool, and failed there because nothing can start.
		for _, c := range [][2]string{{"demo", "get_workflow"}, {"open", "delete_workflow"}} {
			var hidden HiddenTool
			if err := call(c[0], c[1]); err == nil || errors.As(err, &hidden) {
				t.Errorf("%s %s.%s should not be hidden, got %v", name, c[0], c[1], err)
			}
		}
	}
}
