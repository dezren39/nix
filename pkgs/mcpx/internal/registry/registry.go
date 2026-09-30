// Package registry discovers MCP servers from a registry.
//
// The official MCP Registry publishes an OpenAPI specification that other
// registries implement, which is the useful part: writing against the spec
// rather than against one host means the same code reaches the official
// registry, a vendor's subregistry, and whatever an organisation runs
// internally to control what its agents can install.
//
// HAPI was the other candidate and is not usable: its framework is not open
// source and its public API returns 404. The official registry is
// unauthenticated, returns real data, and is backed by the people who define
// the protocol.
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/dezren39/mcpx/internal/defaults"
)

// DefaultURL is the official registry.
const DefaultURL = "https://registry.modelcontextprotocol.io"

// Client talks to a registry.
type Client struct {
	BaseURL string
	HTTP    *http.Client

	pageSize int
	maxPages int
	timeout  time.Duration
}

// Options are the knobs a caller resolved for this client.
//
// They are passed in rather than read here because this package has no access
// to a resolved settings set and should not grow one: a client for an HTTP
// API that reaches for a global to find its own timeout is a client nobody
// can test twice in one process. A zero field means the built-in default,
// which is what registry.timeout and registry.pageSize were silently doing
// for every caller before anything passed them.
type Options struct {
	// Timeout bounds one request, and one search however many pages it
	// takes. Zero uses defaults.RegistryTimeout.
	Timeout time.Duration
	// PageSize is how many entries one request asks for. It is not the
	// number of results: Search asks for pages of this size until it has
	// the limit it was given. Zero uses defaults.RegistryPageSize.
	PageSize int
	// MaxPages caps the requests one search makes. Zero uses
	// defaults.RegistryMaxPages.
	MaxPages int
}

// New builds a client.
func New(base string, opt Options) *Client {
	if strings.TrimSpace(base) == "" {
		base = DefaultURL
	}
	if opt.Timeout <= 0 {
		opt.Timeout = defaults.RegistryTimeout
	}
	if opt.PageSize <= 0 {
		opt.PageSize = defaults.RegistryPageSize
	}
	if opt.MaxPages <= 0 {
		opt.MaxPages = defaults.RegistryMaxPages
	}
	return &Client{
		BaseURL:  strings.TrimRight(base, "/"),
		HTTP:     &http.Client{Timeout: opt.Timeout},
		pageSize: opt.PageSize,
		maxPages: opt.MaxPages,
		timeout:  opt.Timeout,
	}
}

// Server is one entry.
type Server struct {
	Name        string    `json:"name"`
	Title       string    `json:"title,omitempty"`
	Description string    `json:"description"`
	Version     string    `json:"version"`
	WebsiteURL  string    `json:"websiteUrl,omitempty"`
	Repository  *Repo     `json:"repository,omitempty"`
	Packages    []Package `json:"packages,omitempty"`
	Remotes     []Remote  `json:"remotes,omitempty"`
}

// Repo is where the source lives.
type Repo struct {
	URL    string `json:"url"`
	Source string `json:"source"`
}

// Package is one way to install a server locally.
type Package struct {
	RegistryType     string     `json:"registryType"`
	Identifier       string     `json:"identifier"`
	Version          string     `json:"version,omitempty"`
	RuntimeHint      string     `json:"runtimeHint,omitempty"`
	Transport        *Transport `json:"transport,omitempty"`
	RuntimeArguments []Argument `json:"runtimeArguments,omitempty"`
	PackageArguments []Argument `json:"packageArguments,omitempty"`
	EnvironmentVars  []Variable `json:"environmentVariables,omitempty"`
}

// Remote is a server somebody else is already running.
type Remote struct {
	Type    string     `json:"type"`
	URL     string     `json:"url"`
	Headers []Variable `json:"headers,omitempty"`
}

// Transport describes how to speak to a package once it is running.
type Transport struct {
	Type string `json:"type"`
	URL  string `json:"url,omitempty"`
}

// Argument is one command-line argument the package declares.
type Argument struct {
	Type        string `json:"type"` // positional or named
	Name        string `json:"name,omitempty"`
	Value       string `json:"value,omitempty"`
	Default     string `json:"default,omitempty"`
	Description string `json:"description,omitempty"`
	IsRequired  bool   `json:"isRequired,omitempty"`
	IsSecret    bool   `json:"isSecret,omitempty"`
	Format      string `json:"format,omitempty"`
}

// Variable is an environment variable or header the server needs.
type Variable struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Default     string `json:"default,omitempty"`
	IsRequired  bool   `json:"isRequired,omitempty"`
	IsSecret    bool   `json:"isSecret,omitempty"`
}

type listResponse struct {
	Servers []struct {
		Server Server         `json:"server"`
		Meta   map[string]any `json:"_meta,omitempty"`
	} `json:"servers"`
	Metadata struct {
		NextCursor string `json:"nextCursor"`
		Count      int    `json:"count"`
	} `json:"metadata"`
}

// Results is what one search found.
type Results struct {
	Servers []Server `json:"servers"`
	// Truncated says the registry had more than was returned: the limit was
	// reached with a page still unread, or the page cap was. Without it a
	// short list and a complete one look identical, which is how every
	// search used to end silently at one page.
	Truncated bool `json:"truncated"`
}

// Search finds servers whose name matches, following the registry's cursor
// until it has limit of them, the registry has no more, or MaxPages requests
// have been made.
//
// A limit of zero or less means registry.limit's built-in default. It used to
// mean the page size, which made registry.pageSize a second result count that
// only `--limit 0` could reach; the two are different questions once a search
// spans pages, and callers holding a resolved settings set pass registry.limit
// themselves.
//
// The registry matches on name only, as a substring. That is worth knowing
// before sending a sentence: "weather" finds things, "a server for weather
// forecasts" finds nothing.
func (c *Client) Search(ctx context.Context, query string, limit int) (Results, error) {
	if limit <= 0 {
		limit = defaults.RegistryLimit
	}
	// One deadline for the whole walk. The per-request timeout alone would
	// let a slow registry that always has another page hold the caller --
	// a daemon handler, possibly -- for MaxPages times the timeout.
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	// Empty rather than nil, so "nothing matched" is [] in JSON and not null.
	res := Results{Servers: []Server{}}
	cursor := ""
	// Every cursor already followed. A registry that hands back one it has
	// already given -- a constant cursor, or a cycle -- would otherwise be
	// paged until maxPages, and each page appends the same rows again: the
	// caller gets `limit` results that are the first page repeated, and
	// Truncated says more exist. Cheap to hold, since maxPages bounds it.
	seen := map[string]bool{}
	// Server names already collected, so a repeated page cannot deliver the
	// same server twice.
	have := map[string]bool{}
	for page := 1; ; page++ {
		q := url.Values{}
		q.Set("version", "latest")
		// Never more than is still wanted, so the last page does not fetch
		// rows only to throw them away.
		q.Set("limit", fmt.Sprint(min(c.pageSize, limit-len(res.Servers))))
		if s := strings.TrimSpace(query); s != "" {
			q.Set("search", s)
		}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		var doc listResponse
		if err := c.get(ctx, "/v0/servers?"+q.Encode(), &doc); err != nil {
			return Results{}, err
		}
		for _, e := range doc.Servers {
			if len(res.Servers) == limit {
				// The registry sent more than it was asked for.
				res.Truncated = true
				break
			}
			// A name is one server, so the same name twice is the registry
			// repeating itself across pages, not two results. Dropping it
			// here rather than trusting the cursor check alone: a frozen
			// cursor can only be recognised after it has been followed once,
			// and that one extra page would otherwise reach the caller as
			// duplicate rows.
			if have[e.Server.Name] {
				continue
			}
			have[e.Server.Name] = true
			res.Servers = append(res.Servers, e.Server)
		}
		cursor = doc.Metadata.NextCursor
		if cursor == "" {
			return res, nil
		}
		if seen[cursor] {
			// Not Truncated: the registry is repeating itself, so there is no
			// evidence of anything beyond what has already been collected,
			// and saying "more exist" would send the caller looking for it.
			return res, nil
		}
		seen[cursor] = true
		if len(res.Servers) >= limit || page >= c.maxPages {
			res.Truncated = true
			return res, nil
		}
	}
}

// Get fetches one server by its reverse-DNS name.
func (c *Client) Get(ctx context.Context, name string) (*Server, error) {
	var doc struct {
		Server Server `json:"server"`
	}
	path := "/v0/servers/" + url.PathEscape(name) + "/versions/latest"
	if err := c.get(ctx, path, &doc); err != nil {
		// Falling back to a search means a partial name still resolves, which
		// is what somebody typing from memory will give.
		found, serr := c.Search(ctx, name, defaults.RegistryNameFallback)
		hits := found.Servers
		if serr != nil || len(hits) == 0 {
			return nil, err
		}
		for _, h := range hits {
			if strings.EqualFold(h.Name, name) {
				return &h, nil
			}
		}
		if len(hits) == 1 {
			return &hits[0], nil
		}
		names := make([]string, 0, len(hits))
		for _, h := range hits {
			names = append(names, h.Name)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("%q matches several servers:\n  %s",
			name, strings.Join(names, "\n  "))
	}
	return &doc.Server, nil
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("reaching %s: %w", c.BaseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned %s for %s", c.BaseURL, resp.Status, path)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Install is a server translated into something mcpx can run.
type Install struct {
	// Namespace is the name it will have locally.
	Namespace string
	Command   string
	Args      []string
	URL       string
	Transport string
	// Env are the variables the server declares. Values are only filled in
	// where the registry gave a default; a secret is never invented.
	Env map[string]string
	// Needs lists variables that must be set before it will work, so the
	// caller can say so rather than the server failing at start.
	Needs []Variable
	// How describes the choice made, for a human deciding whether it is
	// right.
	How string
}

// Runtimes maps a package registry to the command that runs one without
// installing it first.
//
// Ephemeral runners rather than global installs: a server pinned in a config
// file should not also require a machine to have been prepared, and `npx -y`
// is what every MCP client already does.
var Runtimes = map[string]struct {
	Command string
	Args    []string
}{
	"npm":   {"npx", []string{"-y"}},
	"pypi":  {"uvx", nil},
	"oci":   {"docker", []string{"run", "--rm", "-i"}},
	"nuget": {"dnx", nil},
}

// ToInstall picks how to run a server.
//
// Remotes are preferred when present, because nothing has to be installed and
// nothing runs locally. The registry's own guidance says the same.
func (s Server) ToInstall(preferLocal bool) (*Install, error) {
	ns := Namespace(s.Name)

	if !preferLocal {
		for _, r := range s.Remotes {
			if r.Type == "streamable-http" || r.Type == "sse" {
				in := &Install{
					Namespace: ns, URL: r.URL, Transport: "http",
					Env: map[string]string{},
					How: "remote " + r.Type + "; nothing runs locally",
				}
				for _, h := range r.Headers {
					if h.IsRequired {
						in.Needs = append(in.Needs, h)
					}
				}
				return in, nil
			}
		}
	}

	for _, p := range s.Packages {
		rt, ok := Runtimes[strings.ToLower(p.RegistryType)]
		if !ok {
			continue
		}
		in := &Install{
			Namespace: ns,
			Command:   rt.Command,
			Env:       map[string]string{},
			How:       p.RegistryType + " package " + p.Identifier + " via " + rt.Command,
		}
		if p.RuntimeHint != "" && p.RuntimeHint != rt.Command {
			// The publisher named a runner; trust it over the default, since
			// they know something about their own package that a table does
			// not.
			in.Command = p.RuntimeHint
			in.How = p.RegistryType + " package " + p.Identifier + " via " + p.RuntimeHint + " (publisher's hint)"
		}
		args := append([]string{}, rt.Args...)
		args = append(args, renderArgs(p.RuntimeArguments)...)

		ident := p.Identifier
		if p.Version != "" && strings.EqualFold(p.RegistryType, "npm") {
			ident += "@" + p.Version
		}
		args = append(args, ident)
		args = append(args, renderArgs(p.PackageArguments)...)
		in.Args = args

		for _, v := range p.EnvironmentVars {
			if v.Default != "" && !v.IsSecret {
				in.Env[v.Name] = v.Default
				continue
			}
			if v.IsRequired {
				in.Needs = append(in.Needs, v)
			}
		}
		if p.Transport != nil && p.Transport.Type != "" && p.Transport.Type != "stdio" {
			in.Transport = p.Transport.Type
			in.URL = p.Transport.URL
		}
		return in, nil
	}

	if len(s.Remotes) > 0 {
		r := s.Remotes[0]
		return &Install{
			Namespace: ns, URL: r.URL, Transport: "http", Env: map[string]string{},
			How: "remote " + r.Type + " (no installable package was offered)",
		}, nil
	}
	return nil, fmt.Errorf("%s offers no package mcpx knows how to run; "+
		"it lists %s", s.Name, describeOffers(s))
}

func describeOffers(s Server) string {
	var parts []string
	for _, p := range s.Packages {
		parts = append(parts, p.RegistryType)
	}
	for _, r := range s.Remotes {
		parts = append(parts, "remote:"+r.Type)
	}
	if len(parts) == 0 {
		return "nothing"
	}
	return strings.Join(parts, ", ")
}

// renderArgs turns declared arguments into a command line.
//
// A required argument with no value is left out rather than guessed. The
// server will complain about it clearly; a wrong value would fail in a way
// that looks like the server is broken.
func renderArgs(args []Argument) []string {
	var out []string
	for _, a := range args {
		value := a.Value
		if value == "" {
			value = a.Default
		}
		switch a.Type {
		case "named":
			if a.Name == "" {
				continue
			}
			if value == "" {
				continue
			}
			out = append(out, a.Name, value)
		default:
			if value == "" {
				continue
			}
			out = append(out, value)
		}
	}
	return out
}

// Namespace turns a reverse-DNS registry name into a local one.
//
// "io.github.user/weather" becomes "weather". The prefix is how the registry
// keeps names unique across publishers, and carrying it into a config file
// would make every tool call read io_github_user_weather_get_forecast.
func Namespace(name string) string {
	if _, after, ok := strings.Cut(name, "/"); ok {
		name = after
	}
	if i := strings.LastIndex(name, "."); i >= 0 && i < len(name)-1 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '-', r == '_':
			return '_'
		}
		return -1
	}, name)
	return strings.Trim(strings.ToLower(name), "_")
}
