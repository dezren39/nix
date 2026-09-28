// Package config loads mcpx configuration.
//
// The on-disk format is deliberately the same `mcpServers` object that Claude
// Desktop, Claude Code, Codex and most other MCP hosts already use, so an
// existing config can be dropped in unchanged. mcpx-specific knobs live under
// an optional per-server "mcpx" key, which other hosts ignore.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Sharing and Scope control how many live processes mcpx keeps for a server
// and how they are handed out to callers.
// Sharing says how many callers may use one server process at a time.
type Sharing string

const (
	// SharingShared lets any number of callers use one process at once. MCP
	// multiplexes by JSON-RPC id, so this is correct for a server that holds
	// no per-caller state.
	SharingShared Sharing = "shared"
	// SharingExclusive gives one caller at a time; others queue. Correct when
	// a single call sequence must not be interleaved with another's.
	SharingExclusive Sharing = "exclusive"
)

// Scope says what a server process is keyed by. One live process per distinct
// key value, bounded by Max.
//
// The split from Sharing is the point: "how many callers share a process" and
// "what decides which process you get" are independent questions. A browser
// wants one process per session AND one caller at a time; a search index wants
// one process for everything AND unlimited concurrent callers.
type Scope string

const (
	// ScopeGlobal is a single key: everything shares one process.
	ScopeGlobal Scope = "global"
	// ScopeRepo keys on the git common directory, so every worktree of one
	// clone shares a process.
	ScopeRepo Scope = "repo"
	// ScopeWorktree keys on the git top level, so each worktree gets its own.
	ScopeWorktree Scope = "worktree"
	// ScopeCwd keys on the caller's working directory.
	ScopeCwd Scope = "cwd"
	// ScopeSession keys on the caller's session id. mcpx cannot discover this
	// on its own -- a subagent and its parent have different ids, the same
	// cwd and different pids -- so the caller supplies it through
	// MCPX_SESSION_ID or --session. Without one it degrades to ScopeCall.
	ScopeSession Scope = "session"
	// ScopeParentSession keys on the caller's parent session id, so a parent
	// agent and all of its subagents share one process. Falls back to the
	// session id, then to ScopeCall.
	ScopeParentSession Scope = "parent-session"
	// ScopePid keys on the calling process id, and the process is released
	// when that pid exits rather than waiting for an idle timeout.
	ScopePid Scope = "pid"
	// ScopeCall is a fresh key per invocation.
	ScopeCall Scope = "call"
)

// ValidScopes lists every accepted scope, for error messages.
var ValidScopes = []Scope{
	ScopeGlobal, ScopeRepo, ScopeWorktree, ScopeCwd,
	ScopeSession, ScopeParentSession, ScopePid, ScopeCall,
}

func validScope(s Scope) bool {
	for _, v := range ValidScopes {
		if v == s {
			return true
		}
	}
	return false
}

// Extras are the mcpx-specific per-server options.
type Extras struct {
	// Sharing defaults to "shared"; Scope defaults to "global". Together the
	// defaults describe a stateless server, which most are.
	Sharing      Sharing `json:"sharing,omitempty"`
	Scope        Scope   `json:"scope,omitempty"`
	Max          int     `json:"max,omitempty"`
	Min          int     `json:"min,omitempty"`
	IdleTimeout  string  `json:"idleTimeout,omitempty"`
	CallTimeout  string  `json:"callTimeout,omitempty"`
	StartTimeout string  `json:"startTimeout,omitempty"`
	// Namespace overrides the generated TypeScript namespace name.
	Namespace string `json:"namespace,omitempty"`
	// Disabled skips the server entirely.
	Disabled bool `json:"disabled,omitempty"`
	// Logging overrides the global settings for records about this server, so
	// a single misbehaving server can be made verbose without drowning in the
	// rest. Nested rather than flat, because "logging.level" reads as one
	// subject with settings while "logLevel" reads as an unrelated key that
	// happens to share a prefix.
	Logging *LoggingConfig `json:"logging,omitempty"`
	// Prelude is prepended to this namespace's `types` output. It exists so a
	// config author can state a convention the server's own schemas do not --
	// chrome-devtools-mcp, for instance, requires a pageId on nearly every
	// call and never says where one comes from.
	Prelude string `json:"prelude,omitempty"`
	// Profiles names the groups this server belongs to. `mcpx --profile web`
	// selects by them.
	Profiles []string `json:"profiles,omitempty"`
	// Default says whether this server is included when no profile is
	// requested. Nil means inherit; the built-in default is true.
	Default *bool `json:"default,omitempty"`
	// Description is surfaced by `mcpx ls` so an agent can decide whether
	// to pull the namespace in without loading any schemas.
	Description string `json:"description,omitempty"`
	// Tools, when non-empty, restricts the exposed tools to this allowlist.
	Tools []string `json:"tools,omitempty"`
	// ExcludeTools removes tools from the exposed set.
	ExcludeTools []string `json:"excludeTools,omitempty"`
}

// Server is one MCP server definition.
type Server struct {
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	URL       string            `json:"url,omitempty"`
	Transport string            `json:"transport,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Cwd       string            `json:"cwd,omitempty"`
	// AliasOf names another server whose process definition this entry reuses.
	// An alias is a second *view* -- its own namespace, tool subset, prelude
	// and description -- over the same command. Whether it also shares a
	// running process depends on whether its leasing matches; see PoolID.
	AliasOf string  `json:"aliasOf,omitempty"`
	Mcpx    *Extras `json:"mcpx,omitempty"`

	// Name is filled in by the loader; it is the key in mcpServers.
	Name string `json:"-"`
}

// Config is the whole file.
type Config struct {
	MCPServers map[string]*Server `json:"mcpServers"`

	// Defaults applied to every server that does not override them.
	Defaults Extras `json:"defaults,omitempty"`

	// Pool is the same set of knobs under the name the settings registry
	// uses. Both spellings exist because "defaults" reads naturally beside
	// mcpServers and "pool" is what `mcpx config --schema` calls it; having
	// one silently do nothing would be worse than having two.
	Pool Extras `json:"pool,omitempty"`

	// Logging sets defaults for rendering and verbosity. Flags and environment
	// variables still win, so a config states the habit and a flag states the
	// exception.
	Logging LoggingConfig `json:"logging,omitempty"`

	// SocketPath overrides the daemon unix socket location.
	SocketPath string `json:"socketPath,omitempty"`
	// Script carries settings for generated snippets.
	Script ScriptConfig `json:"script,omitempty"`
	// Permissions is the default sandbox setting for scripts. Empty means wide
	// open, which is the deliberate default: a script is written by whoever
	// could have run the command directly.
	Permissions string `json:"permissions,omitempty"`
	// Runtime picks the JavaScript runtime for `mcpx run`/`exec`
	// ("auto", "deno", "bun", "node").
	Runtime string `json:"runtime,omitempty"`

	// Path is the nearest file this config came from ("" for defaults).
	Path string `json:"-"`
	// Sources lists every file that contributed, nearest first.
	Sources []string `json:"-"`
	// Origin maps a server name to the file that defined the winning entry.
	Origin map[string]string `json:"-"`

	// scriptLayers holds each contributing file's Script block, nearest first.
	scriptLayers []ScriptConfig
}

// ScriptPrefix returns the resolved prefix lines.
func (c *Config) ScriptPrefix(extra []any) []string {
	return ResolveLines(append([][]any{extra}, c.layerField(func(s ScriptConfig) []any { return s.Prefix })...))
}

// ScriptSuffix returns the resolved suffix lines.
func (c *Config) ScriptSuffix(extra []any) []string {
	return ResolveLines(append([][]any{extra}, c.layerField(func(s ScriptConfig) []any { return s.Suffix })...))
}

// ScriptPhase resolves any named phase.
func (c *Config) ScriptPhase(name string, extra []any) []string {
	get := func(s ScriptConfig) []any {
		switch name {
		case "before":
			return s.Before
		case "prefix":
			return s.Prefix
		case "onSuccess":
			return s.OnSuccess
		case "onError":
			return s.OnError
		case "suffix":
			return s.Suffix
		}
		return nil
	}
	return ResolveLines(append([][]any{extra}, c.layerField(get)...))
}

func (c *Config) layerField(get func(ScriptConfig) []any) [][]any {
	layers := [][]any{get(c.Script)}
	for _, l := range c.scriptLayers {
		layers = append(layers, get(l))
	}
	return layers
}

// ScriptConfig shapes the code mcpx generates around an `exec` snippet.
type ScriptConfig struct {
	// Prefix is emitted after the namespace bindings and before the snippet.
	// Suffix is emitted after it.
	//
	// Each is a list whose elements are strings, or null. A null splices in
	// whatever this setting inherited from further out, so a nearer config can
	// add to a farther one instead of only replacing it:
	//
	//     "prefix": ["import { h } from '@lib/h.ts';", null]
	//
	// A list with no null replaces outright, which is the other thing people
	// want and the one that is harder to express if inheritance is implicit.
	Prefix []any `json:"prefix,omitempty"`
	Suffix []any `json:"suffix,omitempty"`
	// Before runs ahead of the standard surface being installed; OnSuccess and
	// OnError run after the entry point returns or throws. Every point is
	// named, because a launcher that is only half-configurable invites
	// someone forking it to reach the part that is not.
	Before    []any `json:"before,omitempty"`
	OnSuccess []any `json:"onSuccess,omitempty"`
	OnError   []any `json:"onError,omitempty"`
}

// ResolveLines folds a layered prefix or suffix into final lines.
//
// Layers arrive nearest-first. Each is applied to what it inherited: a null
// element becomes the inherited lines at that position, and a layer with no
// null discards them. Applying nearest last means the nearest layer decides.
func ResolveLines(layers [][]any) []string {
	var inherited []string
	for i := len(layers) - 1; i >= 0; i-- {
		layer := layers[i]
		if layer == nil {
			continue
		}
		var next []string
		for _, el := range layer {
			if el == nil {
				next = append(next, inherited...)
				continue
			}
			if s, ok := el.(string); ok && s != "" {
				next = append(next, s)
			}
		}
		inherited = next
	}
	return inherited
}

// LoggingConfig is a logging default, at file level or per server.
type LoggingConfig struct {
	// Format is one of the logging package's format names.
	Format string `json:"format,omitempty"`
	// Level is the minimum level to emit.
	Level string `json:"level,omitempty"`
	// Dir is where durable logs are written. Empty uses the state directory.
	Dir string `json:"dir,omitempty"`
	// Include names the ambient blocks gathered on lifecycle records.
	Include string `json:"include,omitempty"`
	// Source decides which levels carry a call site. It accepts a level name,
	// so "warn" traces warnings and errors; true means every level and false
	// means none. Capture costs about 5 microseconds per record in a script,
	// which is worth paying where something went wrong and not worth paying
	// on routine progress.
	Source string `json:"source,omitempty"`
}

func pickLoggingLevel(vals ...*LoggingConfig) string {
	for _, v := range vals {
		if v != nil && v.Level != "" {
			return v.Level
		}
	}
	return ""
}

// Resolved is a Server with all defaults folded in and durations parsed.
type Resolved struct {
	*Server
	Sharing      Sharing
	Scope        Scope
	Max          int
	Min          int
	IdleTimeout  time.Duration
	CallTimeout  time.Duration
	StartTimeout time.Duration
	Namespace    string
	Description  string
	Prelude      string
	LogLevel     string
	Tools        map[string]bool
	ExcludeTools map[string]bool
	Profiles     []string
	Default      bool
}

// VisibleTool reports whether a tool survives this server's allow and deny
// lists. Filtering is a property of the view, not of the process: two aliases
// of one server expose different subsets while sharing a single child.
func (r *Resolved) VisibleTool(name string) bool {
	if r.ExcludeTools[name] {
		return false
	}
	if len(r.Tools) > 0 && !r.Tools[name] {
		return false
	}
	return true
}

// PoolID identifies a runnable process configuration. Two servers with the
// same PoolID share one pool, which is what makes an alias free: a second view
// over the same command with the same leasing is the same browser, not a
// second one. Anything that changes the process or how it is handed out
// belongs here; anything that only changes presentation does not.
func (r *Resolved) PoolID() string {
	h := sha256.New()
	write := func(parts ...string) {
		for _, p := range parts {
			h.Write([]byte(p))
			h.Write([]byte{0})
		}
	}
	write(r.Command)
	write(r.Args...)
	write(r.Cwd, r.URL, r.Transport)
	keys := make([]string, 0, len(r.Env))
	for k := range r.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		write(k, r.Env[k])
	}
	for k, v := range r.Headers {
		write(k, v)
	}
	write(string(r.Sharing), string(r.Scope))
	write(strconv.Itoa(r.Max), strconv.Itoa(r.Min))
	write(r.IdleTimeout.String(), r.CallTimeout.String(), r.StartTimeout.String())
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// Stdio reports whether the server is launched as a child process.
func (r *Resolved) Stdio() bool { return r.Command != "" }

func parseDur(s string, def time.Duration) time.Duration {
	if s == "" {
		return def
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return def
	}
	return d
}

func pick[T comparable](vals ...T) T {
	var zero T
	for _, v := range vals {
		if v != zero {
			return v
		}
	}
	return zero
}

var nsSanitize = regexp.MustCompile(`[^a-zA-Z0-9_]`)

// SanitizeNamespace turns an arbitrary server name into a valid TypeScript
// identifier.
func SanitizeNamespace(s string) string {
	out := nsSanitize.ReplaceAllString(s, "_")
	if out == "" {
		return "_"
	}
	if out[0] >= '0' && out[0] <= '9' {
		out = "_" + out
	}
	return out
}

// Resolve folds file defaults and built-in defaults into a server definition.
func (c *Config) Resolve(name string) (*Resolved, error) {
	s, ok := c.MCPServers[name]
	if !ok {
		return nil, fmt.Errorf("unknown server %q", name)
	}
	// An alias takes its process definition from its target and keeps its own
	// presentation. Chains are rejected rather than followed: one level is a
	// view, two is a puzzle.
	if s.AliasOf != "" {
		target, ok := c.MCPServers[s.AliasOf]
		if !ok {
			return nil, fmt.Errorf("server %q aliases unknown server %q", name, s.AliasOf)
		}
		if target.AliasOf != "" {
			return nil, fmt.Errorf("server %q aliases %q, which is itself an alias; point at the original",
				name, s.AliasOf)
		}
		merged := *target
		merged.Name = name
		merged.AliasOf = s.AliasOf
		merged.Mcpx = s.Mcpx
		s = &merged
	}
	ex := s.Mcpx
	if ex == nil {
		ex = &Extras{}
	}
	// "defaults" wins over "pool" where both are given, because it is the
	// older spelling and the one an existing config already uses.
	d := mergeExtras(c.Defaults, c.Pool)

	sharing := Sharing(pick(string(ex.Sharing), string(d.Sharing), string(SharingShared)))
	switch sharing {
	case SharingShared, SharingExclusive:
	default:
		return nil, fmt.Errorf("server %q: invalid sharing %q; want %q or %q",
			name, sharing, SharingShared, SharingExclusive)
	}

	scope := Scope(pick(string(ex.Scope), string(d.Scope), string(ScopeGlobal)))
	if !validScope(scope) {
		return nil, fmt.Errorf("server %q: invalid scope %q; want one of %s",
			name, scope, scopeList())
	}

	max := pick(ex.Max, d.Max, DefaultMax)
	// A single-key scope can never need more than one process.
	if scope == ScopeGlobal {
		max = 1
	}
	if max < 1 {
		max = 1
	}

	r := &Resolved{
		Server:       s,
		Sharing:      sharing,
		Scope:        scope,
		Max:          max,
		Min:          pick(ex.Min, d.Min, 0),
		IdleTimeout:  parseDur(pick(ex.IdleTimeout, d.IdleTimeout), DefaultIdleTimeout),
		CallTimeout:  parseDur(pick(ex.CallTimeout, d.CallTimeout), DefaultCallTimeout),
		StartTimeout: parseDur(pick(ex.StartTimeout, d.StartTimeout), DefaultStartTimeout),
		Namespace:    pick(ex.Namespace, SanitizeNamespace(name)),
		Description:  ex.Description,
		Prelude:      pick(ex.Prelude, d.Prelude),
		LogLevel:     pickLoggingLevel(ex.Logging, d.Logging),
		Tools:        toSet(ex.Tools),
		ExcludeTools: toSet(ex.ExcludeTools),
		Profiles:     append(append([]string{}, d.Profiles...), ex.Profiles...),
		Default:      boolOr(ex.Default, d.Default, true),
	}
	if r.Min > r.Max {
		r.Min = r.Max
	}
	return r, nil
}

func toSet(v []string) map[string]bool {
	if len(v) == 0 {
		return nil
	}
	m := make(map[string]bool, len(v))
	for _, s := range v {
		m[s] = true
	}
	return m
}

// ResolveAll returns every enabled server, sorted by name.
func (c *Config) ResolveAll() ([]*Resolved, error) {
	names := make([]string, 0, len(c.MCPServers))
	for n, s := range c.MCPServers {
		if s.Mcpx != nil && s.Mcpx.Disabled {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]*Resolved, 0, len(names))
	for _, n := range names {
		r, err := c.Resolve(n)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// SearchPath returns the ordered list of config locations mcpx will try.
func SearchPath() []string {
	var out []string
	add := func(p string) {
		if p != "" {
			out = append(out, p)
		}
	}
	if p := os.Getenv("MCPX_CONFIG"); p != "" {
		return []string{p}
	}
	if wd, err := os.Getwd(); err == nil {
		// Walk up from the working directory so a repo-local config wins.
		// `.config/mcpx` comes first at every level: it is the more explicit
		// spelling and nests with other tools' configuration.
		dir := wd
		for {
			add(filepath.Join(dir, ".config", "mcpx", "config.json"))
			add(filepath.Join(dir, ".mcpx.json"))
			add(filepath.Join(dir, ".mcpx", "config.json"))
			add(filepath.Join(dir, ".mcp.json"))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			add(filepath.Join(xdg, "mcpx", "config.json"))
		}
		add(filepath.Join(home, ".config", "mcpx", "config.json"))
		add(filepath.Join(home, ".mcpx.json"))
	}
	add("/etc/mcpx/config.json")
	return out
}

// Load merges every config on the search path, nearest first.
//
// Merging rather than taking the first file is what lets a project *add* a
// server. First-file-wins meant a repo-local config hid every user-level
// server, so adding one server for one repository required copying the whole
// file and keeping the copy current.
//
// A nearer file wins per server name and may set "disabled": true to drop one
// it inherited. Top-level scalars and defaults follow the same rule. An
// explicit --config is used alone, because naming a file means meaning it.
func Load(explicit string) (*Config, error) {
	if explicit != "" {
		b, err := os.ReadFile(explicit)
		if err != nil {
			return nil, fmt.Errorf("read config %s: %w", explicit, err)
		}
		c, err := parse(b)
		if err != nil {
			return nil, fmt.Errorf("parse config %s: %w", explicit, err)
		}
		c.Path = explicit
		c.Sources = []string{explicit}
		c.Origin = originsOf(c, explicit)
		return c, nil
	}

	merged := &Config{MCPServers: map[string]*Server{}, Origin: map[string]string{}}
	for _, p := range SearchPath() {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		c, err := parse(b)
		if err != nil {
			return nil, fmt.Errorf("parse config %s: %w", p, err)
		}
		if merged.Path == "" {
			merged.Path = p
		}
		merged.Sources = append(merged.Sources, p)
		mergeInto(merged, c, p)
	}
	return merged, nil
}

// mergeInto folds a farther config into a nearer one. Only unset fields are
// taken, so nearer always wins without needing to know which fields exist.
func mergeInto(near, far *Config, farPath string) {
	for name, s := range far.MCPServers {
		if _, taken := near.MCPServers[name]; taken {
			continue
		}
		near.MCPServers[name] = s
		near.Origin[name] = farPath
	}
	if near.SocketPath == "" {
		near.SocketPath = far.SocketPath
	}
	if near.Runtime == "" {
		near.Runtime = far.Runtime
	}
	near.Permissions = pick(near.Permissions, far.Permissions)
	// Script lines layer rather than merge: the whole point is that a nearer
	// file can say "mine, then whatever I inherited".
	near.scriptLayers = append(near.scriptLayers, far.Script)
	near.Logging.Format = pick(near.Logging.Format, far.Logging.Format)
	near.Logging.Level = pick(near.Logging.Level, far.Logging.Level)
	near.Logging.Source = pick(near.Logging.Source, far.Logging.Source)
	near.Logging.Dir = pick(near.Logging.Dir, far.Logging.Dir)
	near.Logging.Include = pick(near.Logging.Include, far.Logging.Include)
	near.Defaults = mergeExtras(near.Defaults, far.Defaults)
	near.Pool = mergeExtras(near.Pool, far.Pool)
}

// mergeExtras takes each unset field from the farther defaults.
func mergeExtras(near, far Extras) Extras {
	near.Sharing = pick(near.Sharing, far.Sharing)
	near.Scope = pick(near.Scope, far.Scope)
	near.Max = pick(near.Max, far.Max)
	near.Min = pick(near.Min, far.Min)
	near.IdleTimeout = pick(near.IdleTimeout, far.IdleTimeout)
	near.CallTimeout = pick(near.CallTimeout, far.CallTimeout)
	near.StartTimeout = pick(near.StartTimeout, far.StartTimeout)
	near.Namespace = pick(near.Namespace, far.Namespace)
	near.Description = pick(near.Description, far.Description)
	near.Prelude = pick(near.Prelude, far.Prelude)
	if len(near.Tools) == 0 {
		near.Tools = far.Tools
	}
	if len(near.ExcludeTools) == 0 {
		near.ExcludeTools = far.ExcludeTools
	}
	return near
}

func originsOf(c *Config, path string) map[string]string {
	out := map[string]string{}
	for name := range c.MCPServers {
		out[name] = path
	}
	return out
}

func parse(b []byte) (*Config, error) {
	c := &Config{}
	if err := json.Unmarshal(stripComments(b), c); err != nil {
		return nil, err
	}
	if c.MCPServers == nil {
		c.MCPServers = map[string]*Server{}
	}
	for name, s := range c.MCPServers {
		s.Name = name
		if s.Command == "" && s.URL == "" && s.AliasOf == "" {
			return nil, fmt.Errorf("server %q: needs command, url or aliasOf", name)
		}
		if s.URL != "" && s.Transport == "" {
			s.Transport = "http"
		}
	}
	return c, nil
}

// stripComments removes // and /* */ comments so JSONC configs load. It is
// string-literal aware.
// StripJSONC removes JSONC comments, string-literal aware. Exported so that
// anything else reading the same files parses them the same way; a file that
// loads here and fails elsewhere is the worst kind of inconsistency.
func StripJSONC(b []byte) []byte { return stripComments(b) }

func stripComments(b []byte) []byte {
	var out strings.Builder
	out.Grow(len(b))
	inStr, inLine, inBlock, esc := false, false, false, false
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch {
		case inLine:
			if c == '\n' {
				inLine = false
				out.WriteByte(c)
			}
		case inBlock:
			if c == '*' && i+1 < len(b) && b[i+1] == '/' {
				inBlock = false
				i++
			}
		case inStr:
			out.WriteByte(c)
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
		default:
			if c == '"' {
				inStr = true
				out.WriteByte(c)
			} else if c == '/' && i+1 < len(b) && b[i+1] == '/' {
				inLine = true
				i++
			} else if c == '/' && i+1 < len(b) && b[i+1] == '*' {
				inBlock = true
				i++
			} else {
				out.WriteByte(c)
			}
		}
	}
	return []byte(out.String())
}

func scopeList() string {
	names := make([]string, 0, len(ValidScopes))
	for _, v := range ValidScopes {
		names = append(names, string(v))
	}
	return strings.Join(names, ", ")
}

// boolOr returns the first set pointer, else the fallback.
func boolOr(vals ...any) bool {
	for _, v := range vals {
		switch x := v.(type) {
		case *bool:
			if x != nil {
				return *x
			}
		case bool:
			return x
		}
	}
	return true
}

// Profile decides which servers a request covers.
type Profile struct {
	// Names selected with --profile. Empty means "whatever is default".
	Names []string
	// SkipDefault drops servers that would be included by default, leaving
	// only those matching Names.
	SkipDefault bool
	// All includes every configured server regardless.
	All bool
}

// Includes reports whether a server is covered.
//
// With no profile requested, a server is included when it is default-on. With
// profiles requested, a server is included when it belongs to one of them, and
// additionally when it is default-on unless --skip-default says otherwise. So
// `--profile web` adds the web servers to the usual set, and
// `--profile web --skip-default` narrows to exactly the web servers.
func (p Profile) Includes(r *Resolved) bool {
	if p.All {
		return true
	}
	if len(p.Names) > 0 {
		for _, want := range p.Names {
			for _, has := range r.Profiles {
				if strings.EqualFold(want, has) {
					return true
				}
			}
		}
		if p.SkipDefault {
			return false
		}
	}
	return r.Default
}
