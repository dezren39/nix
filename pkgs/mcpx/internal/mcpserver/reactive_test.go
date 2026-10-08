package mcpserver_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/catalog"
	"github.com/dezren39/mcpx/internal/mcpserver"
)

func TestReactiveToolDiscovery(t *testing.T) {
	s := mcpserver.New(newBackend(), "mcpx", "test")
	s.Reactive = true
	s.ReactiveWorkingSet = catalog.NewWorkingSet(5, 1*time.Hour, nil)

	// Add some extras
	s = s.WithExtras([]mcpserver.Extra{
		{
			Tool: mcpserver.Tool{Name: "git_status", Description: "Show working tree status"},
			Call: func(ctx context.Context, raw json.RawMessage) (string, error) { return "clean", nil },
		},
		{
			Tool: mcpserver.Tool{Name: "git_commit", Description: "Record changes to the repository"},
			Call: func(ctx context.Context, raw json.RawMessage) (string, error) { return "committed", nil },
		},
		{
			Tool: mcpserver.Tool{Name: "browser_click", Description: "Click element on web page"},
			Call: func(ctx context.Context, raw json.RawMessage) (string, error) { return "clicked", nil },
		},
	})

	ctx := context.Background()

	// Initial surface: only request_tools and batch_call
	tools, err := s.SurfaceForTest(ctx)
	if err != nil {
		t.Fatalf("surface failed: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools initially, got %d", len(tools))
	}
	if tools[0].Name != "request_tools" || tools[1].Name != "batch_call" {
		t.Fatalf("expected request_tools and batch_call, got %+v", tools)
	}

	// Capture notifications
	var notifications []string
	conn := s.ConnWithSend("conn1", func(frame any) error {
		if m, ok := frame.(map[string]any); ok {
			if method, ok := m["method"].(string); ok {
				notifications = append(notifications, method)
			}
		}
		return nil
	})
	s.SetDefaultConnForTest(conn)

	// Invoke request_tools for git
	reqArgs := json.RawMessage(`{"query": "git commit changes", "limit": 2}`)
	res, err := s.InvokeForTest(ctx, "request_tools", reqArgs)
	if err != nil {
		t.Fatalf("request_tools failed: %v", err)
	}

	if !strings.Contains(res, "git_commit") {
		t.Fatalf("expected git_commit in activated tools, got: %s", res)
	}

	// Check notification was sent
	foundNotif := false
	for _, n := range notifications {
		if n == "notifications/tools/list_changed" {
			foundNotif = true
			break
		}
	}
	if !foundNotif {
		t.Fatalf("expected notifications/tools/list_changed, got: %v", notifications)
	}

	// Now check surface again: should include git tools!
	tools2, err := s.SurfaceForTest(ctx)
	if err != nil {
		t.Fatalf("surface failed: %v", err)
	}
	if len(tools2) < 3 {
		t.Fatalf("expected at least 3 tools after activation, got %d", len(tools2))
	}

	hasGitCommit := false
	for _, t := range tools2 {
		if t.Name == "git_commit" {
			hasGitCommit = true
		}
	}
	if !hasGitCommit {
		t.Fatalf("expected git_commit in surface after activation")
	}

	// Permissiveness check: calling browser_click directly even if not yet in advertised set!
	clickRes, err := s.InvokeForTest(ctx, "browser_click", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("permissive call failed: %v", err)
	}
	if clickRes != "clicked" {
		t.Fatalf("expected 'clicked', got %s", clickRes)
	}

	// Verify that browser_click was auto-activated into the surface working set
	tools3, err := s.SurfaceForTest(ctx)
	if err != nil {
		t.Fatalf("surface failed: %v", err)
	}
	hasBrowserClick := false
	for _, t := range tools3 {
		if t.Name == "browser_click" {
			hasBrowserClick = true
			break
		}
	}
	if !hasBrowserClick {
		t.Fatalf("expected browser_click to be auto-activated into surface after unadvertised invocation")
	}

	if len(notifications) < 2 {
		t.Fatalf("expected at least 2 notifications after auto-activation, got %d", len(notifications))
	}
}
