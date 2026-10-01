package pool_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/mcpauth"
	"github.com/dezren39/mcpx/internal/pool"
)

// #240: a server's `auth` block was parsed and never applied, so a configured
// credential silently never reached the upstream.
func TestAuthReachesTheUpstream(t *testing.T) {
	var mu sync.Mutex
	var gotAuth, gotKey string
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		if gotAuth == "" {
			gotAuth = r.Header.Get("Authorization")
			gotKey = r.URL.Query().Get("key")
		}
		mu.Unlock()
		http.Error(w, "no", http.StatusInternalServerError)
	}))
	defer srv.Close()

	resolve := func(t *testing.T, a *mcpauth.Auth) *pool.Pool {
		t.Helper()
		cfg := &config.Config{MCPServers: map[string]*config.Server{
			"remote": {Name: "remote", URL: srv.URL + "/mcp", Auth: a},
		}}
		r, err := cfg.Resolve("remote")
		if err != nil {
			t.Fatal(err)
		}
		p := pool.New(r)
		t.Cleanup(p.Close)
		return p
	}

	t.Run("bearer token from the environment is sent", func(t *testing.T) {
		t.Setenv("MCPX_TEST_TOKEN", "s3cret")
		p := resolve(t, &mcpauth.Auth{Type: "bearer", Token: "${MCPX_TEST_TOKEN}"})
		_, _, _ = p.RefreshSchemas(ctx20(t))
		mu.Lock()
		defer mu.Unlock()
		if gotAuth != "Bearer s3cret" {
			t.Fatalf("Authorization = %q, want %q", gotAuth, "Bearer s3cret")
		}
	})

	t.Run("an unset variable fails before any request", func(t *testing.T) {
		mu.Lock()
		before := hits
		mu.Unlock()
		p := resolve(t, &mcpauth.Auth{Type: "bearer", Token: "${MCPX_TEST_UNSET_TOKEN}"})
		_, _, err := p.RefreshSchemas(ctx20(t))
		if err == nil || !strings.Contains(err.Error(), "MCPX_TEST_UNSET_TOKEN") {
			t.Fatalf("err = %v, want it to name MCPX_TEST_UNSET_TOKEN", err)
		}
		mu.Lock()
		defer mu.Unlock()
		if hits != before {
			t.Errorf("made %d request(s) with a credential known to be missing", hits-before)
		}
	})

	t.Run("query parameter is added to the URL", func(t *testing.T) {
		mu.Lock()
		gotAuth, gotKey = "", ""
		mu.Unlock()
		p := resolve(t, &mcpauth.Auth{Type: "query", Param: "key", ParamValue: "abc"})
		_, _, _ = p.RefreshSchemas(ctx20(t))
		mu.Lock()
		defer mu.Unlock()
		if gotKey != "abc" {
			t.Fatalf("query key = %q, want abc", gotKey)
		}
	})
}
