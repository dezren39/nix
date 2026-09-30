package e2e_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// pagedRegistry serves total entries in pages, the way the official registry
// does, and records the `limit` each request asked for. Local, like
// stubRegistry: no test reaches the real one.
type pagedRegistry struct {
	total  int
	mu     sync.Mutex
	limits []string
}

func (p *pagedRegistry) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p.mu.Lock()
	p.limits = append(p.limits, q.Get("limit"))
	p.mu.Unlock()
	off, _ := strconv.Atoi(q.Get("cursor"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	var rows []any
	for i := off; i < off+limit && i < p.total; i++ {
		rows = append(rows, map[string]any{"server": map[string]any{
			"name": fmt.Sprintf("io.example/s%03d", i), "description": "d", "version": "1",
		}})
	}
	next := ""
	if off+len(rows) < p.total {
		next = strconv.Itoa(off + len(rows))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"servers":  rows,
		"metadata": map[string]any{"nextCursor": next, "count": len(rows)},
	})
}

// asked returns the limits requested so far and forgets them.
func (p *pagedRegistry) asked() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.limits
	p.limits = nil
	return out
}

// withRegistry points an env at a paged registry, plus any other settings.
// Appended rather than replaced: a later duplicate wins in an exec'd
// environment, which is how the other overrides in this package work.
func withRegistry(t *testing.T, e *env, total int, extra ...string) *pagedRegistry {
	t.Helper()
	p := &pagedRegistry{total: total}
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)
	e.envVars = append(append(e.envVars, "MCPX_REGISTRY_URL="+srv.URL), extra...)
	return p
}

func TestRegistryPageSizeChangesWhatTheCLIAsksFor(t *testing.T) {
	// #179. registry.pageSize was read and then discarded unless the limit
	// was zero, so MCPX_REGISTRY_PAGE_SIZE did nothing on any ordinary
	// invocation. The assertion is on the requests, because the answer is
	// the same either way -- that is what made the bug invisible.
	e := newEnv(t, oneServer)
	p := withRegistry(t, e, 12)

	out := e.run("registry", "search", "x", "--limit", "5")
	if got := p.asked(); !reflect.DeepEqual(got, []string{"5"}) {
		t.Fatalf("at the default page size one request should do: %q", got)
	}
	if !strings.Contains(out, "5 shown; more exist -- raise --limit") {
		t.Errorf("twelve exist and five were shown; the output should say so:\n%s", out)
	}

	e.envVars = append(e.envVars, "MCPX_REGISTRY_PAGE_SIZE=2")
	e.run("registry", "search", "x", "--limit", "5")
	if got := p.asked(); !reflect.DeepEqual(got, []string{"2", "2", "1"}) {
		t.Errorf("registry.pageSize=2 should fetch 5 as 2+2+1, asked for %q", got)
	}

	var doc struct {
		Servers   []map[string]any `json:"servers"`
		Truncated *bool            `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(jsonOf(t, e.run("--json", "registry", "search", "x", "--limit", "5"))), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Servers) != 5 || doc.Truncated == nil || !*doc.Truncated {
		t.Errorf("--json should carry the servers and truncated=true: %+v", doc)
	}
}

func TestRegistryLimitReachesTheCLI(t *testing.T) {
	// --limit used to be declared by the command with its own default of
	// 20, so registry.limit from a file or MCPX_REGISTRY_LIMIT reached the
	// daemon's route and never `mcpx registry search`.
	e := newEnv(t, oneServer)
	p := withRegistry(t, e, 12, "MCPX_REGISTRY_LIMIT=3")

	out := e.run("registry", "search", "x")
	if got := p.asked(); !reflect.DeepEqual(got, []string{"3"}) {
		t.Errorf("MCPX_REGISTRY_LIMIT=3 should be the limit sent, got %q", got)
	}
	if !strings.Contains(out, "3 servers.") {
		t.Errorf("three should be shown:\n%s", out)
	}
	// An explicit --limit still wins over the setting.
	e.run("registry", "search", "x", "--limit", "4")
	if got := p.asked(); !reflect.DeepEqual(got, []string{"4"}) {
		t.Errorf("--limit should override the variable, got %q", got)
	}
}

func TestTheRegistryPageCapSaysItStoppedEarly(t *testing.T) {
	// Short of the limit and still truncated means the cap stopped it,
	// and telling that person to raise --limit would be wrong advice.
	e := newEnv(t, oneServer)
	p := withRegistry(t, e, 12, "MCPX_REGISTRY_PAGE_SIZE=2", "MCPX_REGISTRY_MAX_PAGES=2")

	out := e.run("registry", "search", "x", "--limit", "10")
	if got := p.asked(); !reflect.DeepEqual(got, []string{"2", "2"}) {
		t.Errorf("registry.maxPages=2 should stop after two requests, got %q", got)
	}
	if !strings.Contains(out, "4 shown; more exist -- the search stopped at registry.maxPages") {
		t.Errorf("the output should name the cap, not --limit:\n%s", out)
	}
}

func TestRegistrySearchOverTheAPIPagesAndSaysWhenItStopped(t *testing.T) {
	e := newEnv(t, oneServer)
	// Set before the daemon starts: these are client-scoped, so the daemon
	// reads its own.
	p := withRegistry(t, e, 12, "MCPX_REGISTRY_PAGE_SIZE=2")
	e.run("ls")
	c := e.socketClient(t)

	doc := getJSON(t, c, "/v1/registry/search?q=x&limit=5")
	if got := p.asked(); !reflect.DeepEqual(got, []string{"2", "2", "1"}) {
		t.Errorf("the daemon should page by registry.pageSize, asked for %q", got)
	}
	servers, _ := doc["servers"].([]any)
	if len(servers) != 5 || doc["truncated"] != true {
		t.Errorf("want five servers and truncated=true: %v", doc)
	}

	doc = getJSON(t, c, "/v1/registry/search?q=x&limit=50")
	servers, _ = doc["servers"].([]any)
	if len(servers) != 12 || doc["truncated"] != false {
		t.Errorf("all twelve fit, so nothing was cut off: %d servers, truncated=%v",
			len(servers), doc["truncated"])
	}
}
