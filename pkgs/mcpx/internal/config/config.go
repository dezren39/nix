// Package config loads mcpx configuration.
//
// The on-disk format is deliberately the same `mcpServers` object that Claude
// Desktop, Claude Code, Codex and most other MCP hosts already use, so an
// existing config can be dropped in unchanged. mcpx-specific knobs live under
// an optional per-server "mcpx" key, which other hosts ignore.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Mode controls how many live processes mcpx keeps for a server and how they
// are handed out to callers.
type Mode string

const (
	// ModeShared keeps one process and multiplexes concurrent JSON-RPC
	// requests over it. Correct for stateless servers (search, docs, db).
	ModeShared Mode = "shared"
	// ModePooled keeps up to Max processes and leases one per call.
	ModePooled Mode = "pooled"
	// ModeSession keeps up to Max processes and pins a lease to a session
	// key for the lifetime of that session. Correct for stateful servers
	// such as chrome-devtools where a script does navigate -> snapshot ->
	// click and every step must hit the same browser.
	ModeSession Mode = "session"
)

// Extras are the mcpx-specific per-server options.
type Extras struct {
	Mode         Mode   `json:"mode,omitempty"`
	Max          int    `json:"max,omitempty"`
	Min          int    `json:"min,omitempty"`
	IdleTimeout  string `json:"idleTimeout,omitempty"`
	CallTimeout  string `json:"callTimeout,omitempty"`
	StartTimeout string `json:"startTimeout,omitempty"`
	// Namespace overrides the generated TypeScript namespace name.
	Namespace string `json:"namespace,omitempty"`
	// Disabled skips the server entirely.
	Disabled bool `json:"disabled,omitempty"`
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
	Mcpx      *Extras           `json:"mcpx,omitempty"`

	// Name is filled in by the loader; it is the key in mcpServers.
	Name string `json:"-"`
}

// Config is the whole file.
type Config struct {
	MCPServers map[string]*Server `json:"mcpServers"`

	// Defaults applied to every server that does not override them.
	Defaults Extras `json:"defaults,omitempty"`

	// SocketPath overrides the daemon unix socket location.
	SocketPath string `json:"socketPath,omitempty"`
	// Runtime picks the JavaScript runtime for `mcpx run`/`exec`
	// ("auto", "deno", "bun", "node").
	Runtime string `json:"runtime,omitempty"`

	// Path is the file this config came from ("" for defaults).
	Path string `json:"-"`
}

// Defaults that apply when neither the server nor the file specifies one.
const (
	DefaultMax          = 4
	DefaultIdleTimeout  = 5 * time.Minute
	DefaultCallTimeout  = 120 * time.Second
	DefaultStartTimeout = 60 * time.Second
)

// Resolved is a Server with all defaults folded in and durations parsed.
type Resolved struct {
	*Server
	Mode         Mode
	Max          int
	Min          int
	IdleTimeout  time.Duration
	CallTimeout  time.Duration
	StartTimeout time.Duration
	Namespace    string
	Description  string
	Tools        map[string]bool
	ExcludeTools map[string]bool
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
	ex := s.Mcpx
	if ex == nil {
		ex = &Extras{}
	}
	d := c.Defaults

	mode := Mode(pick(string(ex.Mode), string(d.Mode), string(ModeShared)))
	switch mode {
	case ModeShared, ModePooled, ModeSession:
	default:
		return nil, fmt.Errorf("server %q: invalid mode %q", name, mode)
	}

	max := pick(ex.Max, d.Max, DefaultMax)
	if mode == ModeShared {
		max = 1
	}
	if max < 1 {
		max = 1
	}

	r := &Resolved{
		Server:       s,
		Mode:         mode,
		Max:          max,
		Min:          pick(ex.Min, d.Min, 0),
		IdleTimeout:  parseDur(pick(ex.IdleTimeout, d.IdleTimeout), DefaultIdleTimeout),
		CallTimeout:  parseDur(pick(ex.CallTimeout, d.CallTimeout), DefaultCallTimeout),
		StartTimeout: parseDur(pick(ex.StartTimeout, d.StartTimeout), DefaultStartTimeout),
		Namespace:    pick(ex.Namespace, SanitizeNamespace(name)),
		Description:  ex.Description,
		Tools:        toSet(ex.Tools),
		ExcludeTools: toSet(ex.ExcludeTools),
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

// Load finds and parses the first config on the search path. An explicit path
// is used verbatim and must exist.
func Load(explicit string) (*Config, error) {
	paths := SearchPath()
	if explicit != "" {
		paths = []string{explicit}
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			if explicit != "" {
				return nil, fmt.Errorf("read config %s: %w", p, err)
			}
			continue
		}
		c, err := parse(b)
		if err != nil {
			return nil, fmt.Errorf("parse config %s: %w", p, err)
		}
		c.Path = p
		return c, nil
	}
	if explicit != "" {
		return nil, fmt.Errorf("config not found: %s", explicit)
	}
	return &Config{MCPServers: map[string]*Server{}}, nil
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
		if s.Command == "" && s.URL == "" {
			return nil, fmt.Errorf("server %q: needs command or url", name)
		}
		if s.URL != "" && s.Transport == "" {
			s.Transport = "http"
		}
	}
	return c, nil
}

// stripComments removes // and /* */ comments so JSONC configs load. It is
// string-literal aware.
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
