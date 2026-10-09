package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dezren39/mcpx/internal/catalog"
	"github.com/dezren39/mcpx/internal/codegen"
	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/defaults"
	"github.com/dezren39/mcpx/internal/diagnose"
	"github.com/dezren39/mcpx/internal/elicit"
	"github.com/dezren39/mcpx/internal/events"
	"github.com/dezren39/mcpx/internal/logstore"
	"github.com/dezren39/mcpx/internal/mcpclient"
	"github.com/dezren39/mcpx/internal/pool"
	"github.com/dezren39/mcpx/internal/settings"
)

// Registry holds one pool per configured server plus the schema cache.
//
// Discovery (`ls`, `types`, `search`) is answered entirely from the cache, so
// it never blocks on a child process and never pays a tools/list round trip.
// Child processes start on first actual tool call.
type Registry struct {
	// Events receives what the registry notices. Nil is permitted, so a
	// registry built for a test does not need a bus.
	Events *events.Bus
	// broker holds questions servers ask back.
	broker *elicit.Broker

	cfg   *config.Config
	paths Paths
	hash  string

	mu sync.RWMutex
	// pools may be shared between names; views never are. A view carries the
	// per-name presentation -- namespace, tool subset, prelude, profiles --
	// that must not follow the shared process.
	pools map[string]*pool.Pool
	views map[string]*config.Resolved
	order []string

	vectorIdx     *catalog.VectorIndex
	embedder      catalog.Embedder
	feedbackStore *logstore.FeedbackStore

	sessMu sync.Mutex
	leases map[string]*leaseState

	// asks correlates a question a server asked back to the call that
	// provoked it. Built up front rather than on first use: a question can
	// arrive from any pool at any time, and a table created lazily would be
	// read by that goroutine while another wrote it.
	asks *askTable

	testTools []ToolInfo

	logf     func(string, ...any)
	degraded sync.Map

	// consumer is the policy for questions mcpx raises about its own
	// behaviour, and history is what it knows about schemas that changed.
	// Both are set by the daemon once settings have been resolved; the zero
	// values mean "ask nothing, remember nothing", which is what a registry
	// built for a test should do.
	consumer ConsumerPolicy
	history  *diagnose.History
	// set is the daemon's resolved configuration, so the registry reads a
	// knob at the moment it needs it rather than from a copy taken at
	// startup. That is what makes daemon.leaseTTL changeable at runtime.
	set *settings.Set

	// hooks is what InstallHooks attached, kept so that a pool created by a
	// later reload gets the same wiring. Without it, a server added at
	// runtime would be the one server whose events nobody hears.
	hooks *pool.Hooks
}

// leaseState remembers which pool keys a caller created, so releasing a
// caller can stop exactly those instances.
type leaseState struct {
	lastSeen time.Time
	// owned maps server name to the scope key that caller resolved to, for
	// keys this caller alone can be using.
	owned map[string]string
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
	// Prompts are cached like the rest. Without them a restarted daemon
	// loaded this file, counted itself warm, and served no prompts at all:
	// `mcpx prompts` said "No prompts" for a server that publishes them,
	// and prompts/get and completion/complete failed against it.
	Prompts      []mcpclient.Prompt `json:"prompts"`
	Instructions string             `json:"instructions,omitempty"`
	FetchedAt    time.Time          `json:"fetchedAt"`
}

// cacheVersion is bumped whenever a cached entry gains a field. Adding one
// parses cleanly against an old file and leaves it empty, which presented as
// the destructive-call policy silently never firing: the annotations were
// there upstream and absent from the cache nobody had reason to invalidate.
const cacheVersion = 5

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
		cfg:    cfg,
		paths:  paths,
		hash:   HashConfig(raw),
		pools:  make(map[string]*pool.Pool, len(servers)),
		views:  make(map[string]*config.Resolved, len(servers)),
		leases: map[string]*leaseState{},
		asks:   newAskTable(),
		logf:   logf,
	}
	if paths.State != "" {
		dbPath := filepath.Join(paths.State, "embeddings.db")
		if vIdx, err := catalog.NewVectorIndex(dbPath); err == nil {
			r.vectorIdx = vIdx
		}
		fbPath := filepath.Join(paths.State, "feedback.db")
		if fb, err := logstore.NewFeedbackStore(fbPath); err == nil {
			r.feedbackStore = fb
		}
	}
	r.embedder = &catalog.SubwordEmbedder{}
	seen := map[string]string{}
	// Servers whose process definition and leasing are identical share one
	// pool. That is what makes an alias cheap: a second view over the same
	// command is the same running child, not a rival copy of it.
	byPoolID := map[string]*pool.Pool{}
	for _, s := range servers {
		if prev, dup := seen[s.Namespace]; dup {
			return nil, fmt.Errorf("servers %q and %q both map to namespace %q; set mcpx.namespace on one of them",
				prev, s.Name, s.Namespace)
		}
		seen[s.Namespace] = s.Name

		id := s.PoolID()
		p, shared := byPoolID[id]
		if !shared {
			p = pool.New(s)
			byPoolID[id] = p
		}
		r.pools[s.Name] = p
		r.views[s.Name] = s
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

// View returns the per-name presentation for a server.
func (r *Registry) View(name string) (*config.Resolved, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.views[name]
	if ok {
		return v, true
	}
	for _, cand := range r.views {
		if cand.Namespace == name {
			return cand, true
		}
	}
	return nil, false
}

// visibleTools applies a view's allow and deny lists to the shared cache.
func visibleTools(view *config.Resolved, all []mcpclient.Tool) []mcpclient.Tool {
	if view == nil || (view.Tools.Empty() && view.ExcludeTools.Empty()) {
		return all
	}
	out := make([]mcpclient.Tool, 0, len(all))
	for _, t := range all {
		if view.VisibleTool(t.Name) {
			out = append(out, t)
		}
	}
	return out
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
			p.SetSchemas(e.Tools, e.Resources, e.Prompts, e.Instructions, e.FetchedAt)
		}
	}
	r.logf("loaded schema cache for %d servers", len(cf.Servers))
}

// SaveCache persists every cached schema.
func (r *Registry) SaveCache() error {
	cf := cacheFile{Version: cacheVersion, ConfigHash: r.hash, SavedAt: time.Now(), Servers: map[string]*cachedEntry{}}
	r.mu.RLock()
	for name, p := range r.pools {
		tools, res, prompts, at := p.CachedAll()
		if at.IsZero() {
			continue
		}
		cf.Servers[name] = &cachedEntry{
			Tools: tools, Resources: res, Prompts: prompts,
			Instructions: p.Instructions(), FetchedAt: at,
		}
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
	if err := os.WriteFile(tmp, b, defaults.PrivateMode); err != nil {
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
	// Schemas have just been read, which is the only moment mcpx can tell
	// that one of them changed. Recording it here is what lets a diagnostic
	// say when.
	r.ObserveCatalog()
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
	Sharing     string `json:"sharing"`
	Scope       string `json:"scope"`
	Error       string `json:"error,omitempty"`
	Cached      bool   `json:"cached"`
}

// Namespaces lists every configured namespace using only cached data,
// restricted to the requested profile.
func (r *Registry) Namespaces(prof config.Profile) []NamespaceInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]NamespaceInfo, 0, len(r.order))
	for _, name := range r.order {
		p := r.pools[name]
		view := r.views[name]
		if !prof.Includes(view) {
			continue
		}
		allTools, res, at := p.CachedSchemas()
		tools := visibleTools(view, allTools)
		st := p.Status()
		out = append(out, NamespaceInfo{
			Namespace:   view.Namespace,
			Server:      name,
			Tools:       len(tools),
			Resources:   len(res),
			Description: view.Description,
			Live:        st.Live,
			Sharing:     st.Sharing,
			Scope:       st.Scope,
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
	// Execution is the upstream's tool.execution, verbatim.
	Execution json.RawMessage `json:"execution,omitempty"`
	Score     int             `json:"score,omitempty"`
	// What the upstream published beyond the above, carried so a
	// pass-through listing is the upstream's own (#207).
	Title        string          `json:"title,omitempty"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
	Annotations  json.RawMessage `json:"annotations,omitempty"`
	Icons        json.RawMessage `json:"icons,omitempty"`
	Meta         json.RawMessage `json:"_meta,omitempty"`
}

// Tools returns every cached tool, optionally restricted to namespaces.
func (r *Registry) Tools(namespaces []string) []ToolInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.testTools) > 0 {
		return append([]ToolInfo(nil), r.testTools...)
	}

	want := map[string]bool{}
	for _, n := range namespaces {
		want[n] = true
	}

	var out []ToolInfo
	for _, name := range r.order {
		p := r.pools[name]
		view := r.views[name]
		if len(want) > 0 && !want[view.Namespace] && !want[name] {
			continue
		}
		all, _, _ := p.CachedSchemas()
		for _, t := range visibleTools(view, all) {
			out = append(out, ToolInfo{
				Namespace:    view.Namespace,
				Server:       name,
				Tool:         t.Name,
				Function:     view.Namespace + "." + codegen.ToolFuncName(t.Name),
				Description:  t.Description,
				InputSchema:  t.InputSchema,
				Execution:    t.Execution,
				Title:        t.Title,
				OutputSchema: t.OutputSchema,
				Annotations:  t.Annotations,
				Icons:        t.Icons,
				Meta:         t.Meta,
			})
		}
	}
	return out
}

// Search ranks tools against a free-text query. Exact and prefix matches on
// the tool name outrank description hits, which is enough to let an agent find
// the right tool without loading every schema.
func (r *Registry) Search(query string, limit int) []ToolInfo {
	return r.SearchSemantic(query, limit, false)
}

// SearchSemantic ranks tools against a query, optionally using local dense vector embeddings.
func (r *Registry) SearchSemantic(query string, limit int, semantic bool) []ToolInfo {
	terms := strings.Fields(strings.ToLower(query))
	all := r.Tools(nil)
	if len(all) == 0 {
		return nil
	}
	if len(terms) == 0 {
		if limit > 0 && len(all) > limit {
			all = all[:limit]
		}
		return all
	}

	if !semantic {
		scored := rank(all, terms, true)
		if len(scored) == 0 {
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

	embedder := r.Embedder()
	ctx, cancel := context.WithTimeout(context.Background(), defaults.EmbeddingsSearchTimeout)
	defer cancel()

	queryVec, err := embedder.Embed(ctx, query)
	if err != nil || len(queryVec) == 0 {
		queryVec = catalog.Embed(query)
	}

	keywordScores := make(map[string]int)
	for _, t := range rank(all, terms, false) {
		keywordScores[t.Function] = t.Score
	}

	vIdx := r.VectorIndex()
	scored := make([]ToolInfo, 0, len(all))
	for _, t := range all {
		var tVec catalog.Vector
		if vIdx != nil {
			tVec, _ = vIdx.Get(t.Function)
		}
		if len(tVec) == 0 {
			var err error
			tVec, err = embedder.Embed(ctx, t.Namespace+" "+t.Tool+" "+t.Description)
			if err != nil || len(tVec) == 0 {
				tVec = catalog.Embed(t.Namespace + " " + t.Tool + " " + t.Description)
			}
			if vIdx != nil && len(tVec) > 0 {
				_ = vIdx.Put(t.Function, tVec)
			}
		}
		sim := queryVec.CosineSimilarity(tVec)
		kwScore := keywordScores[t.Function]
		// Combined hybrid score: 60% semantic cosine, 40% keyword
		combined := float32(kwScore)*0.4 + (sim*100.0)*0.6
		if fb := r.FeedbackStore(); fb != nil {
			execMult, retrMult := fb.ToolFeedbackAdjustment(t.Function)
			combined = combined * retrMult * (0.5 + 0.5*execMult)
		}
		if combined > 15.0 || kwScore > 0 {
			tCopy := t
			tCopy.Score = int(combined)
			tCopy.InputSchema = nil
			scored = append(scored, tCopy)
		}
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

// selector is one entry from a types/catalog request: a namespace, or a
// namespace and a single tool within it.
type selector struct {
	ns   string
	tool string
}

func parseSelectors(names []string) []selector {
	var out []selector
	for _, n := range names {
		for _, part := range strings.Split(n, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			// A dot separates namespace from tool. Namespaces are sanitised
			// identifiers and never contain one, so the split is unambiguous.
			if i := strings.Index(part, "."); i > 0 {
				out = append(out, selector{ns: part[:i], tool: part[i+1:]})
				continue
			}
			out = append(out, selector{ns: part})
		}
	}
	return out
}

// CodegenNamespaces builds the codegen model for the requested selectors.
//
// A selector is either a namespace ("chrome_devtools") or one tool within it
// ("chrome_devtools.click"). Asking for a single tool is the difference
// between 4,402 tokens and about 90, which matters when an agent already
// knows the name and only needs the argument shape.
//
// An empty list means every namespace.
func (r *Registry) CodegenNamespaces(names []string, prof config.Profile) ([]codegen.Namespace, error) {
	sels := parseSelectors(names)

	// Namespaces wanted whole, and the specific tools wanted from others.
	whole := map[string]bool{}
	tools := map[string]map[string]bool{}
	for _, sel := range sels {
		if sel.tool == "" {
			whole[sel.ns] = true
			continue
		}
		if tools[sel.ns] == nil {
			tools[sel.ns] = map[string]bool{}
		}
		tools[sel.ns][sel.tool] = true
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.testTools) > 0 {
		byNS := map[string][]codegen.Tool{}
		for _, t := range r.testTools {
			if len(sels) > 0 && !whole[t.Namespace] && (tools[t.Namespace] == nil || !tools[t.Namespace][t.Tool]) {
				continue
			}
			byNS[t.Namespace] = append(byNS[t.Namespace], codegen.Tool{
				Name:        t.Tool,
				Description: t.Description,
				InputSchema: t.InputSchema,
			})
		}
		var nsList []string
		for ns := range byNS {
			nsList = append(nsList, ns)
		}
		sort.Strings(nsList)
		var out []codegen.Namespace
		for _, ns := range nsList {
			out = append(out, codegen.Namespace{
				Name:  ns,
				Tools: byNS[ns],
			})
		}
		return out, nil
	}

	matchedNS := map[string]bool{}
	matchedTool := map[string]bool{}
	var out []codegen.Namespace
	for _, name := range r.order {
		p := r.pools[name]
		view := r.views[name]
		if !prof.Includes(view) {
			continue
		}
		ns := view.Namespace

		wantWhole := whole[ns] || whole[name]
		wantTools := tools[ns]
		if wantTools == nil {
			wantTools = tools[name]
		}
		if len(sels) > 0 && !wantWhole && wantTools == nil {
			continue
		}
		matchedNS[ns], matchedNS[name] = true, true

		all, _, _ := p.CachedSchemas()
		cached := visibleTools(view, all)
		cn := codegen.Namespace{
			Name: ns, Server: name,
			Description: view.Description,
			Prelude:     view.Prelude,
		}
		// Server guidance is long and describes a whole namespace, so it is
		// noise when a single tool was asked for.
		if wantWhole || len(sels) == 0 {
			cn.Instructions = p.Instructions()
		}
		for _, t := range cached {
			if !wantWhole && len(sels) > 0 {
				if !wantTools[t.Name] && !wantTools[codegen.ToolFuncName(t.Name)] {
					continue
				}
				matchedTool[ns+"."+t.Name] = true
				matchedTool[ns+"."+codegen.ToolFuncName(t.Name)] = true
				matchedTool[name+"."+t.Name] = true
			}
			cn.Tools = append(cn.Tools, codegen.Tool{
				Name:         t.Name,
				Description:  t.Description,
				InputSchema:  t.InputSchema,
				OutputSchema: t.OutputSchema,
			})
		}
		out = append(out, cn)
	}

	var unknown []string
	for _, sel := range sels {
		if !matchedNS[sel.ns] {
			unknown = append(unknown, sel.ns)
			continue
		}
		if sel.tool != "" && !matchedTool[sel.ns+"."+sel.tool] {
			unknown = append(unknown, sel.ns+"."+sel.tool)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return out, fmt.Errorf("unknown namespace or tool: %s", strings.Join(unknown, ", "))
	}
	return out, nil
}

// UnknownServer is a request naming a server or namespace that is not
// configured: the caller's mistake, which every /v1 route answers with 400.
// It was 400 from some routes and 502 from others (conflict #12), because
// each route decided by itself and most decided "upstream failed".
type UnknownServer struct{ Name string }

func (e UnknownServer) Error() string { return fmt.Sprintf("unknown server or namespace %q", e.Name) }

// HiddenTool is a call to a tool this server's `tools`/`excludeTools`
// configuration removes. Hiding a tool only from listings left it callable
// by any script that knew its name, so a deny list denied nothing.
type HiddenTool struct{ Server, Tool string }

func (e HiddenTool) Error() string {
	return fmt.Sprintf("tool %q on %q is hidden by its tools/excludeTools configuration", e.Tool, e.Server)
}

// failureStatus is the HTTP status of a failed upstream request: 400 when
// the request named nothing, 502 when the server behind it failed.
func failureStatus(err error) int {
	var unknown UnknownServer
	var hidden HiddenTool
	if errors.As(err, &unknown) || errors.As(err, &hidden) {
		return http.StatusBadRequest
	}
	return http.StatusBadGateway
}

// Call dispatches a tool call, resolving the server's scope against the
// caller's context to pick the instance.
func (r *Registry) Call(ctx context.Context, server, tool string, cc config.CallContext, args any) (json.RawMessage, error) {
	if server == "" {
		if idx := strings.IndexAny(tool, ":."); idx > 0 {
			candServer := tool[:idx]
			candTool := tool[idx+1:]
			if _, ok := r.Pool(candServer); ok {
				server = candServer
				tool = candTool
			}
		}
	}
	if server == "" {
		r.mu.RLock()
		for _, name := range r.order {
			view := r.views[name]
			if view != nil && view.VisibleTool(tool) {
				server = name
				break
			}
		}
		if server == "" {
			for _, name := range r.order {
				p := r.pools[name]
				if p != nil {
					tools, _, _ := p.CachedSchemas()
					for _, t := range tools {
						if t.Name == tool {
							server = name
							break
						}
					}
					if server != "" {
						break
					}
				}
			}
		}
		r.mu.RUnlock()
	}

	p, ok := r.Pool(server)
	if !ok {
		return nil, UnknownServer{Name: server}
	}
	key, err := r.resolveAndGuard(ctx, p, server, tool, cc)
	if err != nil {
		return nil, err
	}
	defer r.joinAsk(ctx, server, key)()
	res, err := p.Call(ctx, key, tool, args)
	if err != nil {
		return nil, r.explainCall(ctx, server, tool, args, err)
	}
	return res, nil
}

// resolveAndGuard picks the instance key for a tool call and applies the
// consumer policies. Every path that calls a tool goes through it: the ask
// path once skipped both, so a destructive-tool guard was off for exactly the
// clients that declared they could answer it.
func (r *Registry) resolveAndGuard(ctx context.Context, p *pool.Pool, server, tool string, cc config.CallContext) (string, error) {
	// The view, not the pool: aliases share one pool and filter differently.
	if view, ok := r.View(server); ok {
		if !view.VisibleTool(tool) {
			if ok, reason := view.PassesPreconditions(tool, func(s string) bool { _, exists := r.Pool(s); return exists }); !ok {
				return "", fmt.Errorf("tool %q failed preconditions: %s", tool, reason)
			}
			return "", HiddenTool{Server: server, Tool: tool}
		}
	}
	key := r.keyFor(p, cc)
	// Two policies sit between resolving the instance and using it, and both
	// are off unless somebody turned them on. See internal/daemon/consumer.go.
	key = r.disambiguate(ctx, p, cc, key)
	if err := r.confirmDestructive(ctx, p, tool, cc); err != nil {
		return "", err
	}
	return key, nil
}

// ReadResource dispatches a resource read.
func (r *Registry) ReadResource(ctx context.Context, server, uri string, cc config.CallContext) (json.RawMessage, error) {
	p, ok := r.Pool(server)
	if !ok {
		return nil, UnknownServer{Name: server}
	}
	key := r.keyFor(p, cc)
	// The listing drops a leading '/', so mcpx://ns/abs/doc names /abs/doc;
	// the server only knows its own spelling.
	return p.ReadResource(ctx, key, upstreamResourceURI(ctx, p, uri))
}

// PromptInfo is one prompt, with the namespace it came from.
type PromptInfo struct {
	Namespace   string                     `json:"namespace"`
	Server      string                     `json:"server"`
	Name        string                     `json:"name"`
	Title       string                     `json:"title,omitempty"`
	Description string                     `json:"description,omitempty"`
	Arguments   []mcpclient.PromptArgument `json:"arguments,omitempty"`
	Icons       json.RawMessage            `json:"icons,omitempty"`
	Meta        json.RawMessage            `json:"_meta,omitempty"`
}

// ResourceInfo is one resource, with the namespace it came from.
type ResourceInfo struct {
	Namespace   string          `json:"namespace"`
	Server      string          `json:"server"`
	URI         string          `json:"uri"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	MimeType    string          `json:"mimeType,omitempty"`
	Title       string          `json:"title,omitempty"`
	Size        *int64          `json:"size,omitempty"`
	Annotations json.RawMessage `json:"annotations,omitempty"`
	Icons       json.RawMessage `json:"icons,omitempty"`
	Meta        json.RawMessage `json:"_meta,omitempty"`
}

// resourceInfo is one cached resource or template under its namespace.
func resourceInfo(ns, server, uri string, res mcpclient.Resource) ResourceInfo {
	return ResourceInfo{
		Namespace: ns, Server: server, URI: uri,
		Name: res.Name, Description: res.Description, MimeType: res.MimeType,
		Title: res.Title, Size: res.Size, Annotations: res.Annotations,
		Icons: res.Icons, Meta: res.Meta,
	}
}

// Prompts aggregates every prompt across the configured servers.
func (r *Registry) Prompts(namespaces []string) []PromptInfo {
	want := map[string]bool{}
	for _, n := range namespaces {
		want[n] = true
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	var out []PromptInfo
	for _, name := range r.order {
		p := r.pools[name]
		view := r.views[name]
		if len(want) > 0 && !want[view.Namespace] && !want[name] {
			continue
		}
		for _, pr := range p.CachedPrompts() {
			out = append(out, PromptInfo{
				Namespace: view.Namespace, Server: name, Name: pr.Name,
				Title: pr.Title, Description: pr.Description, Arguments: pr.Arguments,
				Icons: pr.Icons, Meta: pr.Meta,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Resources aggregates every resource across the configured servers.
func (r *Registry) Resources(namespaces []string) []ResourceInfo {
	want := map[string]bool{}
	for _, n := range namespaces {
		want[n] = true
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	var out []ResourceInfo
	for _, name := range r.order {
		p := r.pools[name]
		view := r.views[name]
		if len(want) > 0 && !want[view.Namespace] && !want[name] {
			continue
		}
		_, resources, _ := p.CachedSchemas()
		for _, res := range resources {
			// Templates share the cache (see pool.CachedTemplates) and
			// have no uri; listing one here put an entry with an empty
			// URI in resources/list, which no revision's schema allows.
			if res.URITemplate != "" {
				continue
			}
			out = append(out, resourceInfo(view.Namespace, name, res.URI, res))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].URI < out[j].URI
	})
	return out
}

// ResourceTemplates aggregates templated resources across servers.
func (r *Registry) ResourceTemplates(namespaces []string) []ResourceInfo {
	want := map[string]bool{}
	for _, n := range namespaces {
		want[n] = true
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []ResourceInfo
	for _, name := range r.order {
		p := r.pools[name]
		view := r.views[name]
		if len(want) > 0 && !want[view.Namespace] && !want[name] {
			continue
		}
		for _, t := range p.CachedTemplates() {
			out = append(out, resourceInfo(view.Namespace, name, t.URITemplate, t))
		}
	}
	return out
}

// GetPrompt renders one prompt.
func (r *Registry) GetPrompt(ctx context.Context, server, name string, args map[string]string, cc config.CallContext) (json.RawMessage, error) {
	p, ok := r.Pool(server)
	if !ok {
		return nil, UnknownServer{Name: server}
	}
	return p.GetPrompt(ctx, r.keyFor(p, cc), name, args)
}

// keyFor resolves a server's scope and records the association so the caller
// can later release exactly what it created.
func (r *Registry) keyFor(p *pool.Pool, cc config.CallContext) string {
	scope := p.Config().Scope
	key, degraded := scope.Key(cc)
	if degraded != "" {
		r.warnDegraded(p.Name(), scope, degraded)
	}
	if cc.CallID != "" {
		r.sessMu.Lock()
		st, ok := r.leases[cc.CallID]
		if !ok {
			st = &leaseState{owned: map[string]string{}}
			r.leases[cc.CallID] = st
		}
		st.lastSeen = time.Now()
		if cc.CallerOwned(key) {
			st.owned[p.Name()] = key
		}
		r.sessMu.Unlock()
	}
	return key
}

// warnDegraded reports a scope that could not be resolved as configured, once
// per server and reason. Silent degradation to per-call isolation would look
// like a performance problem rather than a configuration one.
func (r *Registry) warnDegraded(server string, scope config.Scope, reason string) {
	k := server + "\x00" + string(scope) + "\x00" + reason
	if _, seen := r.degraded.LoadOrStore(k, true); seen {
		return
	}
	r.logf("server %q: scope %q degraded to per-call: %s", server, scope, reason)
}

// ReleaseCaller stops instances created for a caller whose scope made them
// caller-private. A shared or long-lived scope is left alone: another caller
// may legitimately still want it.
func (r *Registry) ReleaseCaller(callID string) int {
	if callID == "" {
		return 0
	}
	r.sessMu.Lock()
	st := r.leases[callID]
	delete(r.leases, callID)
	r.sessMu.Unlock()
	if st == nil {
		return 0
	}
	n := 0
	for server, key := range st.owned {
		if p, ok := r.Pool(server); ok {
			n += p.ReleaseKey(key)
		}
	}
	if n > 0 {
		r.logf("released %d instance(s) for caller %s", n, callID)
	}
	return n
}

// Reap drops idle instances and abandoned sessions. Called on a timer.
func (r *Registry) Reap() {
	now := time.Now()

	var stale []string
	r.sessMu.Lock()
	for k, s := range r.leases {
		if now.Sub(s.lastSeen) > r.leaseTTL() {
			stale = append(stale, k)
		}
	}
	r.sessMu.Unlock()
	for _, k := range stale {
		r.ReleaseCaller(k)
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
	// Several names can share one pool. Report the pool once, under the names
	// that reach it, so a shared instance does not look like two.
	seen := map[*pool.Pool][]string{}
	var order []*pool.Pool
	for _, name := range r.order {
		p := r.pools[name]
		if _, ok := seen[p]; !ok {
			order = append(order, p)
		}
		seen[p] = append(seen[p], r.views[name].Namespace)
	}
	for _, p := range order {
		st := p.Status()
		names := seen[p]
		st.Namespace = strings.Join(names, ", ")
		out = append(out, st)
	}
	return out
}

// ServerRestart is one pool's part of a restart.
type ServerRestart struct {
	Server string `json:"server"`
	pool.RestartResult
}

// Restart restarts one server, or all servers when name is empty. Unless lazy,
// each replacement is started and waited for; see pool.Restart.
func (r *Registry) Restart(ctx context.Context, name string, lazy bool) ([]ServerRestart, error) {
	var pools []*pool.Pool
	if name == "" {
		r.mu.RLock()
		// Several names can reach one pool; restart it once.
		seen := map[*pool.Pool]bool{}
		for _, n := range r.order {
			if p := r.pools[n]; !seen[p] {
				seen[p] = true
				pools = append(pools, p)
			}
		}
		r.mu.RUnlock()
	} else {
		p, ok := r.Pool(name)
		if !ok {
			return nil, fmt.Errorf("unknown server or namespace %q", name)
		}
		pools = []*pool.Pool{p}
	}
	out := make([]ServerRestart, len(pools))
	var wg sync.WaitGroup
	for i, p := range pools {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = ServerRestart{Server: p.Name(), RestartResult: p.Restart(ctx, lazy)}
		}()
	}
	wg.Wait()
	return out, nil
}

// VectorIndex returns the underlying persistent vector index if initialized.
func (r *Registry) VectorIndex() *catalog.VectorIndex {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.vectorIdx
}

// FeedbackStore returns the underlying feedback and interaction store if initialized.
func (r *Registry) FeedbackStore() *logstore.FeedbackStore {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.feedbackStore
}

// Embedder returns the active embedding model engine.
func (r *Registry) Embedder() catalog.Embedder {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.embedder == nil {
		return &catalog.SubwordEmbedder{}
	}
	return r.embedder
}

// UseSettings gives the registry the daemon's resolved configuration.
//
// Separate from NewRegistry for the same reason InstallHooks is: a registry
// built for a test should work without one.
func (r *Registry) UseSettings(set *settings.Set) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.set = set
	if set != nil {
		backend := set.String("embeddings.backend")
		var remote *catalog.RemoteEmbedder
		if u := set.String("embeddings.url"); u != "" {
			remote = catalog.NewRemoteEmbedder(u, set.String("embeddings.apiKey"), set.String("embeddings.model"))
		}
		var wasm *catalog.WasmEmbedder
		if p := set.String("embeddings.wasmPath"); p != "" {
			if w, err := catalog.NewWasmEmbedder(context.Background(), p); err == nil {
				wasm = w
			}
		}
		r.embedder = catalog.NewCascadeEmbedder(backend, remote, wasm)
	}
}

func (r *Registry) leaseTTL() time.Duration {
	if r.set == nil {
		return defaults.LeaseTTL
	}
	return r.set.Duration("daemon.leaseTTL")
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
	vIdx := r.vectorIdx
	fb := r.feedbackStore
	r.mu.RUnlock()
	for _, p := range pools {
		p.Close()
	}
	if vIdx != nil {
		_ = vIdx.Close()
	}
	if fb != nil {
		_ = fb.Close()
	}
}
