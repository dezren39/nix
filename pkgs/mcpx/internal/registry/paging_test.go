package registry_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/defaults"
	"github.com/dezren39/mcpx/internal/registry"
)

// fakeRegistry pages through total entries the way the official registry
// does: `limit` rows per request, and an opaque `cursor` naming where the
// next page starts. Here the cursor is the offset, which the client must not
// care about.
type fakeRegistry struct {
	total int
	// serve, when set, is how many rows every page has whatever limit was
	// asked for: a registry that ignores the limit it is sent.
	serve int
	// endless answers every request with no rows and a fresh cursor, which
	// is the registry that would hold a naive loop forever.
	endless bool
	// frozen answers every request with the same rows and the same cursor:
	// a registry whose paging is broken, or a proxy dropping the parameter.
	frozen bool
	// delay is spent before each answer.
	delay time.Duration

	mu       sync.Mutex
	requests []url.Values
}

// runaway is where the fake gives up, so a loop that never ends fails the
// test instead of hanging it.
const runaway = 1000

func (f *fakeRegistry) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f.mu.Lock()
	f.requests = append(f.requests, q)
	n := len(f.requests)
	f.mu.Unlock()
	if n > runaway {
		http.Error(w, "runaway pagination", http.StatusInternalServerError)
		return
	}
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	off, _ := strconv.Atoi(q.Get("cursor"))
	if f.endless {
		writePage(w, nil, strconv.Itoa(n))
		return
	}
	if f.frozen {
		var rows []any
		for i := 0; i < f.serve && i < f.total; i++ {
			rows = append(rows, map[string]any{"server": map[string]any{
				"name": name(i), "description": "d", "version": "1",
			}})
		}
		writePage(w, rows, "ALWAYS")
		return
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	if f.serve > 0 {
		limit = f.serve
	}
	var rows []any
	for i := off; i < off+limit && i < f.total; i++ {
		rows = append(rows, map[string]any{"server": map[string]any{
			"name": name(i), "description": "d", "version": "1",
		}})
	}
	next := ""
	if off+len(rows) < f.total {
		next = strconv.Itoa(off + len(rows))
	}
	writePage(w, rows, next)
}

func writePage(w http.ResponseWriter, rows []any, next string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"servers":  rows,
		"metadata": map[string]any{"nextCursor": next, "count": len(rows)},
	})
}

func name(i int) string { return fmt.Sprintf("io.example/s%03d", i) }

// sent is one query parameter from every request, in order.
func (f *fakeRegistry) sent(key string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.requests))
	for _, q := range f.requests {
		out = append(out, q.Get(key))
	}
	return out
}

func search(t *testing.T, f *fakeRegistry, opt registry.Options, limit int) registry.Results {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	res, err := registry.New(srv.URL, opt).Search(context.Background(), "x", limit)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func names(res registry.Results) []string {
	out := make([]string, 0, len(res.Servers))
	for _, s := range res.Servers {
		out = append(out, s.Name)
	}
	return out
}

func first(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, name(i))
	}
	return out
}

func TestSearchFollowsTheCursorAcrossPages(t *testing.T) {
	// nextCursor used to be parsed and dropped, so every search ended at
	// one page whatever the caller asked for.
	f := &fakeRegistry{total: 7}
	res := search(t, f, registry.Options{PageSize: 3}, 50)

	if got := names(res); !reflect.DeepEqual(got, first(7)) {
		t.Errorf("every page, in order: got %v", got)
	}
	if got := f.sent("cursor"); !reflect.DeepEqual(got, []string{"", "3", "6"}) {
		t.Errorf("each request should carry the cursor the last one returned: %q", got)
	}
	if res.Truncated {
		t.Error("the registry ran out before the limit; nothing was cut off")
	}
}

func TestSearchStopsAtTheLimitMidPage(t *testing.T) {
	f := &fakeRegistry{total: 10}
	res := search(t, f, registry.Options{PageSize: 4}, 6)

	if got := names(res); !reflect.DeepEqual(got, first(6)) {
		t.Errorf("got %v", got)
	}
	// The last request asks for what is still wanted, not a full page.
	if got := f.sent("limit"); !reflect.DeepEqual(got, []string{"4", "2"}) {
		t.Errorf("limit per request: %q", got)
	}
	if !res.Truncated {
		t.Error("four more exist and the registry said so; the result must say it too")
	}
}

func TestARegistryThatIgnoresTheLimitIsCutToIt(t *testing.T) {
	f := &fakeRegistry{total: 10, serve: 4}
	res := search(t, f, registry.Options{PageSize: 4}, 6)

	if got := names(res); !reflect.DeepEqual(got, first(6)) {
		t.Errorf("rows beyond the limit should be dropped: %v", got)
	}
	if !res.Truncated {
		t.Error("rows were dropped, so the result is truncated")
	}
}

func TestPageSizeDecidesHowManyRequestsASearchMakes(t *testing.T) {
	// #179: registry.pageSize was read, passed down, and then used only
	// when limit was zero -- so a test that it was *read* passed while it
	// changed nothing. This asserts the change instead: the same search,
	// the same limit, a different page size, a different conversation with
	// the registry.
	for _, c := range []struct {
		pageSize int
		limits   []string
	}{
		{2, []string{"2", "2", "1"}},
		{10, []string{"5"}},
	} {
		f := &fakeRegistry{total: 20}
		res := search(t, f, registry.Options{PageSize: c.pageSize}, 5)
		if got := f.sent("limit"); !reflect.DeepEqual(got, c.limits) {
			t.Errorf("pageSize %d: requests asked for %q, want %q", c.pageSize, got, c.limits)
		}
		if got := names(res); !reflect.DeepEqual(got, first(5)) {
			t.Errorf("pageSize %d changes the requests, never the answer: %v", c.pageSize, got)
		}
	}
}

func TestARegistryThatAlwaysHasAnotherPageIsCutAtThePageCap(t *testing.T) {
	// Empty pages, each with a fresh cursor: the limit is never reached, so
	// only the cap ends it.
	f := &fakeRegistry{endless: true}
	res := search(t, f, registry.Options{PageSize: 5, MaxPages: 4}, 100)

	if n := len(f.sent("cursor")); n != 4 {
		t.Errorf("made %d requests; the cap is 4", n)
	}
	if !res.Truncated {
		t.Error("stopping at the cap with a cursor in hand is truncation, and must say so")
	}
}

func TestAnEmptyCursorEndsTheSearch(t *testing.T) {
	f := &fakeRegistry{total: 5}
	res := search(t, f, registry.Options{PageSize: 5}, 5)

	if n := len(f.sent("cursor")); n != 1 {
		t.Errorf("no cursor means no next page, but %d requests were made", n)
	}
	if got := names(res); !reflect.DeepEqual(got, first(5)) {
		t.Errorf("got %v", got)
	}
	// Exactly the limit, and the registry has nothing after it: complete.
	if res.Truncated {
		t.Error("nothing more exists")
	}
}

func TestANonPositiveLimitIsTheResultDefaultNotThePageSize(t *testing.T) {
	// It used to fall back to the page size, which is what made
	// registry.pageSize a second result count.
	f := &fakeRegistry{total: 1000}
	res := search(t, f, registry.Options{PageSize: 100}, 0)

	if len(res.Servers) != defaults.RegistryLimit {
		t.Errorf("got %d results, want registry.limit's default %d",
			len(res.Servers), defaults.RegistryLimit)
	}
	if got := f.sent("limit"); len(got) != 1 || got[0] != strconv.Itoa(defaults.RegistryLimit) {
		t.Errorf("requests asked for %q", got)
	}
}

func TestTheTimeoutBoundsTheWholeSearchNotEachPage(t *testing.T) {
	// Each page answers well inside the timeout, and there is always
	// another. A per-request bound alone would walk until the page cap --
	// here, far longer than the test is allowed to take.
	f := &fakeRegistry{endless: true, delay: 20 * time.Millisecond}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c := registry.New(srv.URL, registry.Options{
		Timeout: 200 * time.Millisecond, MaxPages: runaway * 10,
	})

	start := time.Now()
	_, err := c.Search(context.Background(), "x", 100)
	if err == nil {
		t.Fatal("a search that outlives the timeout should fail, not return a partial answer as if complete")
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("took %v; the timeout was 200ms", took)
	}
}

// A registry that keeps handing back the same cursor is not an infinite
// supply of results.
//
// Before the cursor was tracked, this produced `limit` rows that were the
// first page repeated -- five copies of the same two servers -- and set
// Truncated, so `mcpx registry search` printed the duplicates and then said
// "more exist". Get() takes hits[0] from such a search, so it resolved a name
// against a row that had been counted five times.
func TestARegistryThatRepeatsACursorIsNotPagedForever(t *testing.T) {
	f := &fakeRegistry{total: 100, serve: 2, frozen: true}
	res := search(t, f, registry.Options{PageSize: 2, MaxPages: 50}, 10)

	if n := len(f.requests); n > 2 {
		t.Errorf("made %d requests to a registry that repeats itself; "+
			"one page then one confirmation is all the evidence there is", n)
	}
	seen := map[string]bool{}
	for _, s := range res.Servers {
		if seen[s.Name] {
			t.Errorf("%s was returned twice; a name is one server", s.Name)
		}
		seen[s.Name] = true
	}
	if res.Truncated {
		t.Error("a registry repeating itself is no evidence that more exist, " +
			"and Truncated sends the caller looking for them")
	}
	if len(res.Servers) != 2 {
		t.Errorf("got %d servers, want the 2 distinct ones the registry has", len(res.Servers))
	}
}
