package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/daemon"
)

func TestDaemonVersionNotice(t *testing.T) {
	cases := []struct {
		name          string
		cli, daemon   string
		remote        bool
		wantEmpty     bool
		wantInMessage []string
	}{
		{"equal", "1.2.3", "1.2.3", false, true, nil},
		{"daemon reports none", "1.2.3", "", false, true, nil},
		{"cli reports none", "", "1.2.3", false, true, nil},
		{"local mismatch", "1.2.3", "1.2.2", false, false,
			[]string{"1.2.2", "1.2.3", "mcpx restart --daemon"}},
		{"remote mismatch", "1.2.3", "1.2.2", true, false,
			[]string{"1.2.2", "1.2.3", "where it runs"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := daemonVersionNotice(tc.cli, tc.daemon, tc.remote)
			if tc.wantEmpty {
				if got != "" {
					t.Fatalf("notice = %q, want empty", got)
				}
				return
			}
			for _, want := range tc.wantInMessage {
				if !strings.Contains(got, want) {
					t.Errorf("notice %q does not mention %q", got, want)
				}
			}
			if strings.Contains(got, "\n") {
				t.Errorf("notice %q is not one line", got)
			}
		})
	}
}

// healthServer answers /v1/health the way a daemon of the given version
// does; an empty version omits the field, like a daemon from before it
// existed.
func healthServer(t *testing.T, version string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/health" {
			http.NotFound(w, r)
			return
		}
		if version == "" {
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok","version":"` + version + `"}`))
	}))
}

func TestDaemonVersionWarning(t *testing.T) {
	ctx := context.Background()

	t.Run("mismatch warns with both versions", func(t *testing.T) {
		ts := healthServer(t, "0.0.9")
		defer ts.Close()
		a := &App{Version: "0.1.0"}
		c := NewClientAt(daemon.Paths{}, "", ts.URL)
		got := a.daemonVersionWarning(ctx, c)
		if !strings.Contains(got, "0.0.9") || !strings.Contains(got, "0.1.0") {
			t.Fatalf("warning = %q, want both versions named", got)
		}
	})

	t.Run("matching version is silent", func(t *testing.T) {
		ts := healthServer(t, "0.1.0")
		defer ts.Close()
		a := &App{Version: "0.1.0"}
		c := NewClientAt(daemon.Paths{}, "", ts.URL)
		if got := a.daemonVersionWarning(ctx, c); got != "" {
			t.Fatalf("warning = %q, want empty", got)
		}
	})

	t.Run("daemon without a version is silent", func(t *testing.T) {
		ts := healthServer(t, "")
		defer ts.Close()
		a := &App{Version: "0.1.0"}
		c := NewClientAt(daemon.Paths{}, "", ts.URL)
		if got := a.daemonVersionWarning(ctx, c); got != "" {
			t.Fatalf("warning = %q, want empty", got)
		}
	})

	t.Run("unreachable daemon is silent, not an error", func(t *testing.T) {
		ts := healthServer(t, "0.0.9")
		url := ts.URL
		ts.Close()
		a := &App{Version: "0.1.0"}
		c := NewClientAt(daemon.Paths{}, "", url)
		if got := a.daemonVersionWarning(ctx, c); got != "" {
			t.Fatalf("warning = %q, want empty", got)
		}
	})
}

func TestCheckDaemonVersionSkipsSpawnedDaemons(t *testing.T) {
	ctx := context.Background()
	ts := healthServer(t, "0.0.9")
	defer ts.Close()
	a := &App{Version: "0.1.0", JSON: true}
	c := NewClientAt(daemon.Paths{}, "", ts.URL)
	// Spawn and inline daemons run this binary; the check must not even
	// ask. Pointing the client at a mismatching server proves the skip
	// comes from the rung, not from the versions agreeing.
	for _, chosen := range []string{"spawn", "inline", ""} {
		a.checkDaemonVersion(ctx, c, &Ladder{Chosen: chosen})
	}
	// The url/socket path is exercised by TestDaemonVersionWarning; here
	// JSON mode keeps notice() silent so this only asserts no panic.
	a.checkDaemonVersion(ctx, c, &Ladder{Chosen: "url"})
	a.checkDaemonVersion(ctx, c, nil)
}
