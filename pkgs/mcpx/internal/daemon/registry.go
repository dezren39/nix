package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dezren39/mcpx/internal/codegen"
	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/mcpclient"
	"github.com/dezren39/mcpx/internal/pool"
)

// Registry holds one pool per configured server plus the schema cache.
//
// Discovery (`ls`, `types`, `search`) is answered entirely from the cache, so
// it never blocks on a child process and never pays a tools/list round trip.
// Child processes start on first actual tool call.
type Registry struct {
	cfg   *config.Config
	paths Paths
	hash  string

	mu    sync.RWMutex
	pools map[string]*pool.Pool
	order []string

	sessMu   sync.Mutex
	sessions map[string]*sessionState

	logf func(string, ...any)
}

type sessionState struct {
	lastSeen time.Time
	servers  map[string]bool
}

// cacheFile is the persisted schema cache.
type cacheFile struct {
	Version    int                     `json:"version"`
	ConfigHash string                  `json:"configHash"`
	SavedAt    time.Time               `json:"savedAt"`
	Servers    map[string]*cachedEntry `json:"servers"`
}

type cachedEntry struct {
	Tools     []mcpclient.Tool     `json:"tools"`
	Resources []mcpclient.Resource `json:"resources"`
	FetchedAt time.Time            `json:"fetchedAt"`
}

const cacheVersion = 2

// NewRegistry builds pools from config and seeds them from the disk cache.
func NewRegistry(cfg *config.Config, paths Paths, logf func(string, ...any)) (*Registry, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	servers, err := cfg.ResolveAll()
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(cfg.MCPServers)

	r := &Registry{
		cfg:      cfg,
		paths:    paths,
		hash:     HashConfig(raw),
		pools:    make(map[string]*pool.Pool, len(servers)),
		sessions: map[string]*sessionState{},
		logf:     logf,
	}
	seen := map[string]string{}
	for _, s := range servers {
		if prev, dup := seen[s.Namespace]; dup {
			return nil, fmt.Errorf("servers %q and %q both map to namespace %q; set mcpx.namespace on one of them",
				prev, s.Name, s.Namespace)
		}
		seen[s.Namespace] = s.Name
		r.pools[s.Name] = pool.New(s)
		r.order = append(r.order, s.Name)
	}
	sort.Strings(r.order)
	r.loadCache()
	return r, nil
}

// ConfigHash fingerprints the server definitions.
func (r *Registry) ConfigHash() string { return r.hash }

// Names returns server names in stable order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string(nil), r.order...)
}

// Pool looks a server up by name or namespace.
func (r *Registry) Pool(nameOrNS string) (*pool.Pool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if p, ok := r.pools[nameOrNS]; ok {
		return p, true
	}
	for _, p := range r.pools {
		if p.Namespace() == nameOrNS {
			return p, true
		}
	}
	return nil, false
}

func (r *Registry) loadCache() {
	b, err := os.ReadFile(r.paths.SchemaCachePath(r.hash))
	if err != nil {
		return
	}
	var cf cacheFile
	if err := json.Unmarshal(b, &cf); err != nil || cf.Version != cacheVersion || cf.ConfigHash != r.hash {
		return
	}
	for name, e := range cf.Servers {
		if p, ok := r.pools[name]; ok {
			p.SetSchemas(e.Tools, e.Resources, e.FetchedAt)
		}
	}
	r.logf("loaded schema cache for %d servers", len(cf.Servers))
}

// SaveCache persists every cached schema.
func (r *Registry) SaveCache() error {
	cf := cacheFile{Version: cacheVersion, ConfigHash: r.hash, SavedAt: time.Now(), Servers: map[string]*cachedEntry{}}
	r.mu.RLock()
	for name, p := range r.pools {
		tools, res, at := p.CachedSchemas()
		if at.IsZero() {
			continue
		}
		cf.Servers[name] = &cachedEntry{Tools: tools, Resources: res, FetchedAt: at}
	}
	r.mu.RUnlock()

	if len(cf.Servers) == 0 {
		return nil
	}
	b, err := json.Marshal(cf)
	if err != nil {
		return err
	}
	path := r.paths.SchemaCachePath(r.hash)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Warm fetches schemas for every server that has none cached, in parallel, and
// persists the result. A server that fails to start does not block the others.
func (r *Registry) Warm(ctx context.Context, force bool) map[string]error {
	r.mu.RLock()
	pools := make([]*pool.Pool, 0, len(r.pools))
	for _, p := range r.pools {
		pools = append(pools, p)
	}
	r.mu.RUnlock()

	errs := map[string]error{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, p := range pools {
		_, _, at := p.CachedSchemas()
		if !force && !at.IsZero() {
			continue
		}
		wg.Add(1)
		go func(p *pool.Pool) {
			defer wg.Done()
			start := time.Now()
			_, _, err := p.RefreshSchemas(ctx)
			mu.Lock()
			if err != nil {
				errs[p.Name()] = err
				r.logf("warm %s failed after %s: %v", p.Name(), time.Since(start).Truncate(time.Millisecond), err)
			} else {
				r.logf("warm %s ok in %s", p.Name(), time.Since(start).Truncate(time.Millisecond))
			}
			mu.Unlock()
		}(p)
	}
	wg.Wait()
	if err := r.SaveCache(); err != nil {
		r.logf("save cache: %v", err)
	}
	return errs
}

// NamespaceInfo is one row of `mcpx ls`.
type NamespaceInfo struct {
	Namespace   string `json:"namespace"`
	Server      string `json:"server"`
	Tools       int    `json:"tools"`
	Resources   int    `json:"resources"`
	Description string `json:"description,omitempty"`
	Live        int    `json:"live"`
	Mode        string `json:"mode"`
	Error       string `json:"error,omitempty"`
	Cached      bool   `json:"cached"`
}

// Namespaces lists every configured namespace using only cached data.
func (r *Registry) Namespaces() []NamespaceInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]NamespaceInfo, 0, len(r.order))
	for _, name := range r.order {
		p := r.pools[name]
		tools, res, at := p.CachedSchemas()
		st := p.Status()
		out = append(out, NamespaceInfo{
			Namespace:   p.Namespace(),
			Server:      name,
			Tools:       len(tools),
			Resources:   len(res),
			Description: p.Config().Description,
			Live:        st.Live,
			Mode:        st.Mode,
			Error:       st.LastError,
			Cached:      !at.IsZero(),
		})
	}
	return out
}

// ToolInfo describes one tool for search results.
type ToolInfo struct {
	Namespace   string          `json:"namespace"`
	Server      string          `json:"server"`
	Tool        string          `json:"tool"`
	Function    string          `json:"function"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
	Score       int             `json:"score,omitempty"`
}

// Tools returns every cached tool, optionally restricted to namespaces.
func (r *Registry) Tools(namespaces []string) []ToolInfo {
	want := map[string]bool{}
	for _, n := range namespaces {
		want[n] = true
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	var out []ToolInfo
	for _, name := range r.order {
		p := r.pools[name]
		if len(want) > 0 && !want[p.Namespace()] && !want[name] {
			continue
		}
		tools, _, _ := p.CachedSchemas()
		for _, t := range tools {
			out = append(out, ToolInfo{
				Namespace:   p.Namespace(),
				Server:      name,
				Tool:        t.Name,
				Function:    p.Namespace() + "." + codegen.ToolFuncName(t.Name),
				Description: t.Description,
				InputSchema: t.InputSchema,
			})
		}
	}
	return out
}

// Search ranks tools against a free-text query. Exact and prefix matches on
// the tool name outrank description hits, which is enough to let an agent find
// the right tool without loading every schema.
func (r *Registry) Search(query string, limit int) []ToolInfo {
	terms := strings.Fields(strings.ToLower(query))
	all := r.Tools(nil)
	if len(terms) == 0 {
		if limit > 0 && len(all) > limit {
			all = all[:limit]
		}
		return all
	}

	scored := rank(all, terms, true)
	if len(scored) == 0 {
		// Every term must match by default, which narrows well but dead-ends on
		// a query like "screenshot click" where the terms name different tools.
		// Fall back to any-term matching rather than reporting nothing.
		scored = rank(all, terms, false)
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		return scored[i].Function < scored[j].Function
	})
	if limit > 0 && len(scored) > limit {
		scored = scored[:limit]
	}
	return scored
}

// rank scores tools against terms. When requireAll is set a tool must match
// every term; otherwise one match is enough.
func rank(all []ToolInfo, terms []string, requireAll bool) []ToolInfo {
	scored := make([]ToolInfo, 0, len(all))
	for _, t := range all {
		name := strings.ToLower(t.Tool)
		fn := strings.ToLower(t.Function)
		desc := strings.ToLower(t.Description)
		ns := strings.ToLower(t.Namespace)

		score, matchedAll := 0, true
		for _, term := range terms {
			switch {
			case name == term:
				score += 100
			case strings.HasPrefix(name, term):
				score += 60
			case strings.Contains(name, term):
				score += 40
			case strings.Contains(fn, term):
				score += 30
			case strings.Contains(ns, term):
				score += 20
			case strings.Contains(desc, term):
				score += 10
			default:
				matchedAll = false
			}
		}
		if (requireAll && !matchedAll) || score == 0 {
			continue
		}
		t.Score = score
		t.InputSchema = nil // keep search output small
		scored = append(scored, t)
	}
	return scored
}

// CodegenNamespaces builds the codegen model for the requested namespaces.
// An empty list means every namespace.
func (r *Registry) CodegenNamespaces(names []string) ([]codegen.Namespace, error) {
	want := map[string]bool{}
	for _, n := range names {
		for _, part := range strings.Split(n, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				want[part] = true
			}
		}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	matched := map[string]bool{}
	var out []codegen.Namespace
	for _, name := range r.order {
		p := r.pools[name]
		ns := p.Namespace()
		if len(want) > 0 && !want[ns] && !want[name] {
			continue
		}
		matched[ns], matched[name] = true, true
		tools, _, _ := p.CachedSchemas()
		cn := codegen.Namespace{Name: ns, Server: name, Description: p.Config().Description}
		for _, t := range tools {
			cn.Tools = append(cn.Tools, codegen.Tool{
				Name:        t.Name,
				Description: t.Description,
				InputSchema: t.InputSchema,
			})
		}
		out = append(out, cn)
	}
	var unknown []string
	for n := range want {
		if !matched[n] {
			unknown = append(unknown, n)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return out, fmt.Errorf("unknown namespace(s): %s", strings.Join(unknown, ", "))
	}
	return out, nil
}

// Call dispatches a tool call to the right pool.
func (r *Registry) Call(ctx context.Context, server, tool, session string, args any) (json.RawMessage, error) {
	p, ok := r.Pool(server)
	if !ok {
		return nil, fmt.Errorf("unknown server or namespace %q", server)
	}
	r.touchSession(session, p.Name())
	return p.Call(ctx, session, tool, args)
}

// ReadResource dispatches a resource read.
func (r *Registry) ReadResource(ctx context.Context, server, uri, session string) (json.RawMessage, error) {
	p, ok := r.Pool(server)
	if !ok {
		return nil, fmt.Errorf("unknown server or namespace %q", server)
	}
	r.touchSession(session, p.Name())
	return p.ReadResource(ctx, session, uri)
}

func (r *Registry) touchSession(key, server string) {
	if key == "" {
		return
	}
	r.sessMu.Lock()
	s, ok := r.sessions[key]
	if !ok {
		s = &sessionState{servers: map[string]bool{}}
		r.sessions[key] = s
	}
	s.lastSeen = time.Now()
	s.servers[server] = true
	r.sessMu.Unlock()
}

// ReleaseSession frees every pinned instance held for a session. The CLI calls
// this when a script finishes, which is what returns a browser to the pool
// promptly instead of waiting for the idle timer.
func (r *Registry) ReleaseSession(key string) int {
	if key == "" {
		return 0
	}
	r.sessMu.Lock()
	s := r.sessions[key]
	delete(r.sessions, key)
	r.sessMu.Unlock()
	if s == nil {
		return 0
	}
	n := 0
	for server := range s.servers {
		if p, ok := r.Pool(server); ok {
			n += p.ReleaseSession(key)
		}
	}
	if n > 0 {
		r.logf("released %d instance(s) for session %s", n, key)
	}
	return n
}

// Reap drops idle instances and abandoned sessions. Called on a timer.
func (r *Registry) Reap() {
	now := time.Now()

	var stale []string
	r.sessMu.Lock()
	for k, s := range r.sessions {
		if now.Sub(s.lastSeen) > 30*time.Minute {
			stale = append(stale, k)
		}
	}
	r.sessMu.Unlock()
	for _, k := range stale {
		r.ReleaseSession(k)
	}

	r.mu.RLock()
	pools := make([]*pool.Pool, 0, len(r.pools))
	for _, p := range r.pools {
		pools = append(pools, p)
	}
	r.mu.RUnlock()
	for _, p := range pools {
		if n := p.ReapIdle(now); n > 0 {
			r.logf("reaped %d idle %s instance(s)", n, p.Name())
		}
	}
}

// Status snapshots every pool.
func (r *Registry) Status() []pool.Status {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]pool.Status, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.pools[name].Status())
	}
	return out
}

// Restart stops instances for one server, or all servers when name is empty.
func (r *Registry) Restart(name string) (int, error) {
	if name == "" {
		r.mu.RLock()
		pools := make([]*pool.Pool, 0, len(r.pools))
		for _, p := range r.pools {
			pools = append(pools, p)
		}
		r.mu.RUnlock()
		n := 0
		for _, p := range pools {
			n += p.Restart()
		}
		return n, nil
	}
	p, ok := r.Pool(name)
	if !ok {
		return 0, fmt.Errorf("unknown server or namespace %q", name)
	}
	return p.Restart(), nil
}

// Close shuts every pool down. The caller is responsible for persisting the
// schema cache first; doing it here would write after the daemon has already
// told its client that it stopped.
func (r *Registry) Close() {
	r.mu.RLock()
	pools := make([]*pool.Pool, 0, len(r.pools))
	for _, p := range r.pools {
		pools = append(pools, p)
	}
	r.mu.RUnlock()
	for _, p := range pools {
		p.Close()
	}
}
