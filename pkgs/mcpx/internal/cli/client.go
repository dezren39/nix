// Package cli implements the mcpx command line.
package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dezren39/mcpx/internal/defaults"
	"github.com/dezren39/mcpx/internal/events"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/daemon"
	"github.com/dezren39/mcpx/internal/mcpclient"
	"github.com/dezren39/mcpx/internal/settings"
)

// Client talks to the daemon over its unix socket, starting one if needed.
type Client struct {
	paths daemon.Paths
	hc    *http.Client
	cfg   string
	// endpoint is empty for the local socket, or a base URL for a daemon
	// somewhere else.
	endpoint string
	// set is this process's resolved configuration.
	set *settings.Set
	// callSettings is the JSON header carrying whatever call-scoped settings
	// this invocation actually chose. Computed once: it cannot change within
	// a run, and recomputing it per request would put a schema walk on the
	// path of every call.
	callSettings string
}

// clientSettings is the resolved configuration a client should use.
//
// Read through the package-level App the script resolver already uses,
// because a client is built from several places that do not carry one and
// threading it through every call site would be a wide change for one map
// lookup. A process that never set one -- a test -- gets the declared
// defaults, which is the right answer rather than a nil dereference.
func clientSettings() *settings.Set {
	if plumbingApp != nil {
		return plumbingApp.Settings()
	}
	sch, err := settings.New(settings.Registry())
	if err != nil {
		panic("mcpx: settings registry is invalid: " + err.Error())
	}
	return settings.NewSet(sch)
}

// callHeader is the call-scoped settings this client should send.
//
// Only what somebody actually set: sending a default would make it
// indistinguishable, at the daemon, from a deliberate choice, and the daemon
// would then prefer a client's inherited default over its own configured
// value.
func callHeader(set *settings.Set) string {
	ov := map[string]string{}
	for _, decl := range set.Schema().All() {
		if decl.Scope != settings.ScopeCall {
			continue
		}
		v, ok := set.Value(decl.Path)
		if !ok || v.Origin.Layer == settings.LayerDefault {
			continue
		}
		ov[decl.Path] = v.Raw
	}
	if len(ov) == 0 {
		return ""
	}
	b, err := json.Marshal(ov)
	if err != nil {
		return ""
	}
	return string(b)
}

// NewClient builds a socket-backed API client.
func NewClient(paths daemon.Paths, configPath string) *Client {
	return NewClientAt(paths, configPath, "")
}

// NewClientAt targets a specific daemon.
//
// An empty endpoint means the local unix socket, which is the case that
// matters for speed: no network stack, no port, and the filesystem
// permissions are the access control.
//
// A URL points somewhere else -- another machine on a VPN, a container, a
// shared daemon for a team. The whole API is already HTTP over that socket,
// so pointing it at a real address costs nothing in code and was only ever
// prevented by the dialler being hardcoded.
func NewClientAt(paths daemon.Paths, configPath, endpoint string) *Client {
	set := clientSettings()
	c := &Client{paths: paths, cfg: configPath, set: set,
		callSettings: callHeader(set),
		endpoint:     strings.TrimRight(endpoint, "/")}
	timeout := set.Duration("http.requestTimeout")
	idle := set.Duration("http.idleConnTimeout")

	if c.endpoint == "" {
		c.hc = &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", paths.Socket)
				},
				MaxIdleConns:    set.Int("http.idleConns"),
				IdleConnTimeout: idle,
			},
		}
		return c
	}

	// A remote endpoint may also be a socket path, written as a URL, because
	// "which socket" is a question somebody will have on a machine running
	// two daemons.
	if sock, ok := strings.CutPrefix(c.endpoint, "unix://"); ok {
		c.endpoint = ""
		c.hc = &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", sock)
				},
				MaxIdleConns: set.Int("http.idleConns"), IdleConnTimeout: idle,
			},
		}
		return c
	}
	c.hc = &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			// Keep-alive matters far more over a network than it does over a
			// socket: without it every call pays a handshake, which on a VPN
			// is most of the latency.
			MaxIdleConns:        set.Int("http.remoteIdleConns"),
			MaxIdleConnsPerHost: set.Int("http.remoteIdleConns"),
			IdleConnTimeout:     idle,
			ForceAttemptHTTP2:   true,
		},
	}
	return c
}

// Remote reports whether this client talks to a daemon it cannot start.
// Socket is the unix socket this client is actually using, or "" when the
// daemon is somewhere else.
//
// Asked of the connection rather than recomputed from the configuration.
// Inline mode listens on a private temporary socket, so deriving the path
// from the config key gave the wrong answer -- and an empty one, which the
// generated client turned into "cannot reach the mcpx daemon at ".
func (c *Client) Socket() string {
	if c.endpoint != "" {
		return ""
	}
	return c.paths.Socket
}

func (c *Client) Remote() bool { return c.endpoint != "" }

// ErrNoDaemon means nothing is listening on the socket.
var ErrNoDaemon = errors.New("mcpx daemon is not running")

func (c *Client) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	if body == nil {
		return c.send(ctx, method, path, nil, "")
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return c.send(ctx, method, path, b, "application/json")
}

// send is do for a body that is already bytes, which is what an artifact
// is: marshalling a file into JSON to have the daemon unmarshal it again
// would cost a base64 pass in each direction for nothing.
func (c *Client) send(ctx context.Context, method, path string, body []byte, contentType string) ([]byte, error) {
	b, _, err := c.sendWith(ctx, method, path, body, contentType, nil)
	return b, err
}

// sendWith is send with extra request headers, also returning the status so
// a caller can tell one 2xx answer from another.
func (c *Client) sendWith(ctx context.Context, method, path string, body []byte, contentType string, headers map[string]string) ([]byte, int, error) {
	resp, err := c.open(ctx, method, path, body, contentType, headers)
	if err != nil {
		return nil, 0, err
	}
	return c.readReply(resp)
}

// open sends a request and returns the response unread, for a caller that
// consumes it as a stream.
func (c *Client) open(ctx context.Context, method, path string, body []byte, contentType string, headers map[string]string) (*http.Response, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	base := c.endpoint
	if base == "" {
		// Any host works over a unix socket; the dialler ignores it. "mcpx"
		// reads better in a log than "localhost" when no host was involved.
		base = "http://mcpx"
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil && contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if c.callSettings != "" {
		req.Header.Set(daemon.CallSettingsHeader, c.callSettings)
	}
	if caps := mcpclient.ClientCapabilitiesFrom(ctx); len(caps) > 0 {
		req.Header.Set(daemon.ClientCapsHeader, string(caps))
	}
	if v := mcpclient.CallerVersionFrom(ctx); v != "" {
		req.Header.Set(daemon.CallerVersionHeader, v)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		if isDialErr(err) {
			return nil, ErrNoDaemon
		}
		return nil, err
	}
	return resp, nil
}

// readReply reads a whole response, turning a non-2xx status into an
// *HTTPError.
func (c *Client) readReply(resp *http.Response) ([]byte, int, error) {
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		msg := fmt.Sprintf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
		if json.Unmarshal(b, &e) == nil && e.Error != "" {
			msg = e.Error
		}
		return nil, resp.StatusCode, &HTTPError{Status: resp.StatusCode, Body: b, Msg: msg}
	}
	return b, resp.StatusCode, nil
}

// HTTPError is a refusal from the daemon, with the body kept.
//
// The body matters for the routes whose failure is itself an answer: a
// recipe that could not be filled in is telling the caller what it needed,
// and collapsing that to a string would throw the useful half away.
type HTTPError struct {
	Status int
	Body   []byte
	Msg    string
}

func (e *HTTPError) Error() string { return e.Msg }

// HTTPStatus lets a caller one hop further -- /v1/tools, relaying a tool
// that made a daemon call -- answer with the status the daemon gave.
func (e *HTTPError) HTTPStatus() int { return e.Status }

func isDialErr(err error) bool {
	var oe *net.OpError
	if errors.As(err, &oe) && oe.Op == "dial" {
		return true
	}
	return errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED)
}

// Ping reports whether a daemon is reachable.
func (c *Client) Ping(ctx context.Context) bool {
	cctx, cancel := context.WithTimeout(ctx, c.set.Duration("autostart.pingTimeout"))
	defer cancel()
	_, err := c.do(cctx, http.MethodGet, "/v1/health", nil)
	return err == nil
}

// EnsureDaemon starts a background daemon if one is not already running and
// waits for it to become reachable. This is what makes every command work with
// no setup step.
func (c *Client) EnsureDaemon(ctx context.Context) error {
	if c.Remote() {
		// Starting a local daemon because a remote one is unreachable would
		// silently answer from the wrong machine, which is worse than
		// failing.
		if c.Ping(ctx) {
			return nil
		}
		return fmt.Errorf("no mcpx daemon at %s; it is not started from here", c.endpoint)
	}
	if c.Ping(ctx) {
		return nil
	}
	if !c.set.Bool("daemon.autostart") {
		return fmt.Errorf("%w and daemon.autostart is off, so mcpx will not start one",
			ErrNoDaemon)
	}
	exe := c.set.String("autostart.bin")
	if exe == "" {
		var err error
		if exe, err = os.Executable(); err != nil {
			return err
		}
	}
	// An auto-started daemon shuts itself down once a repo stops being used,
	// so visiting many projects does not accumulate idle processes. A daemon
	// started deliberately (launchd, `mcpx daemon`) has no idle timer.
	args := append([]string{}, c.set.List("autostart.args")...)
	if idle := c.set.Duration("autostart.idleExit"); idle > 0 {
		args = append(args, "--idle-exit", idle.String())
	}
	if c.cfg != "" {
		args = append(args, "--config", c.cfg)
	}
	logPath := c.paths.State + "/daemon.log"
	if err := c.paths.EnsureDirs(); err != nil {
		return err
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, defaults.PrivateMode)
	if err != nil {
		return err
	}
	defer logFile.Close()

	cmd := exec.Command(exe, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	_ = cmd.Process.Release()

	deadline := time.Now().Add(c.set.Duration("autostart.connectTimeout"))
	for time.Now().Before(deadline) {
		if c.Ping(ctx) {
			return nil
		}
		time.Sleep(c.set.Duration("autostart.pollInterval"))
	}
	tail, _ := os.ReadFile(logPath)
	if max := c.set.Bytes("autostart.logTail"); int64(len(tail)) > max {
		tail = tail[int64(len(tail))-max:]
	}
	return fmt.Errorf("daemon did not become ready; see %s\n%s", logPath, strings.TrimSpace(string(tail)))
}

// Profile is the caller's profile selection, threaded onto every discovery
// request so a request sees the same server set the CLI does.
type Profile struct {
	Names       []string
	SkipDefault bool
	All         bool
}

func (p Profile) query() string {
	q := ""
	if len(p.Names) > 0 {
		q += "&profile=" + urlEscape(strings.Join(p.Names, ","))
	}
	if p.SkipDefault {
		q += "&skipDefault=1"
	}
	if p.All {
		q += "&allProfiles=1"
	}
	return q
}

// Namespaces lists configured namespaces within a profile.
func (c *Client) Namespaces(ctx context.Context, prof Profile) ([]daemon.NamespaceInfo, error) {
	b, err := c.do(ctx, http.MethodGet, "/v1/namespaces?_"+prof.query(), nil)
	if err != nil {
		return nil, err
	}
	var out []daemon.NamespaceInfo
	return out, json.Unmarshal(b, &out)
}

// Types returns TypeScript declarations for the given namespaces.
func (c *Client) Types(ctx context.Context, ns []string, instructions bool, prof Profile) (string, error) {
	q := "?instructions=" + boolParam(instructions) + prof.query()
	if len(ns) > 0 {
		q += "&ns=" + strings.Join(ns, ",")
	}
	b, err := c.do(ctx, http.MethodGet, "/v1/types"+q, nil)
	return string(b), err
}

// Catalog returns a budgeted listing across namespaces.
func (c *Client) Catalog(ctx context.Context, ns []string, budget int, bias string, prof Profile) (string, error) {
	q := fmt.Sprintf("?budget=%d", budget) + prof.query()
	if len(ns) > 0 {
		q += "&ns=" + strings.Join(ns, ",")
	}
	if bias != "" {
		q += "&bias=" + urlEscape(bias)
	}
	b, err := c.do(ctx, http.MethodGet, "/v1/catalog"+q, nil)
	return string(b), err
}

func boolParam(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// ClientModule returns the generated script client source.
func (c *Client) ClientModule(ctx context.Context, ns []string, session string, prof Profile) (string, error) {
	q := "?session=" + session + prof.query()
	if len(ns) > 0 {
		q += "&ns=" + strings.Join(ns, ",")
	}
	b, err := c.do(ctx, http.MethodGet, "/v1/client.ts"+q, nil)
	return string(b), err
}

// Globals returns the ambient declarations for the installed globals.
func (c *Client) Globals(ctx context.Context, ns []string, prof Profile) (string, error) {
	q := "?_" + prof.query()
	if len(ns) > 0 {
		q += "&ns=" + strings.Join(ns, ",")
	}
	b, err := c.do(ctx, http.MethodGet, "/v1/globals.d.ts"+q, nil)
	return string(b), err
}

// Search ranks tools against a query.
func (c *Client) Search(ctx context.Context, q string, limit int, semantic ...bool) ([]daemon.ToolInfo, error) {
	semQ := ""
	if len(semantic) > 0 && semantic[0] {
		semQ = "&semantic=true"
	}
	b, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/v1/search?q=%s&limit=%d%s", urlEscape(q), limit, semQ), nil)
	if err != nil {
		return nil, err
	}
	var out []daemon.ToolInfo
	return out, json.Unmarshal(b, &out)
}

// Tools lists tools for the given namespaces.
func (c *Client) Tools(ctx context.Context, ns []string) ([]daemon.ToolInfo, error) {
	q := ""
	if len(ns) > 0 {
		q = "?ns=" + strings.Join(ns, ",")
	}
	b, err := c.do(ctx, http.MethodGet, "/v1/tools"+q, nil)
	if err != nil {
		return nil, err
	}
	var out []daemon.ToolInfo
	return out, json.Unmarshal(b, &out)
}

// CallResult is a raw tools/call response.
type CallResult struct {
	Result     json.RawMessage `json:"result"`
	DurationMs int64           `json:"durationMs"`
}

// Call invokes a tool, sending the caller's context so the daemon can resolve
// whatever scope the server is configured for.
func (c *Client) Call(ctx context.Context, server, tool string, cc config.CallContext, args json.RawMessage) (*CallResult, error) {
	b, err := c.do(ctx, http.MethodPost, "/v1/call", map[string]any{
		"server": server, "tool": tool, "args": args, "context": cc,
	})
	if err != nil {
		return nil, err
	}
	var out CallResult
	return &out, json.Unmarshal(b, &out)
}

// BatchCallItem is one call to be executed in a batch.
type BatchCallItem struct {
	Server    string          `json:"server,omitempty"`
	Namespace string          `json:"namespace,omitempty"`
	Tool      string          `json:"tool"`
	Name      string          `json:"name,omitempty"`
	Args      json.RawMessage `json:"args,omitempty"`
}

// BatchResultItem is the result of one call in a batch.
type BatchResultItem struct {
	Index      int             `json:"index"`
	Server     string          `json:"server,omitempty"`
	Tool       string          `json:"tool"`
	Status     string          `json:"status"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      string          `json:"error,omitempty"`
	IsError    bool            `json:"isError,omitempty"`
	DurationMs int64           `json:"durationMs,omitempty"`
}

// BatchResponse is the response to /v1/batch.
type BatchResponse struct {
	Results []BatchResultItem `json:"results"`
}

// Batch invokes multiple tools in a single turn.
func (c *Client) Batch(ctx context.Context, calls []BatchCallItem, stopOnError bool, parallel bool, cc config.CallContext) (*BatchResponse, error) {
	b, err := c.do(ctx, http.MethodPost, "/v1/batch", map[string]any{
		"calls":       calls,
		"stopOnError": stopOnError,
		"parallel":    parallel,
		"context":     cc,
	})
	if err != nil {
		return nil, err
	}
	var out BatchResponse
	return &out, json.Unmarshal(b, &out)
}

// CallRelayed is Call for mcpx's MCP server, relaying what its client asked
// of the call to the upstream and handing the upstream's progress and log
// messages to notify as they arrive. A call that produced none is answered
// as plain JSON, exactly as Call is.
func (c *Client) CallRelayed(ctx context.Context, server, tool string, cc config.CallContext, args json.RawMessage, relay daemon.CallRelay, notify func(method string, params json.RawMessage)) (*CallResult, error) {
	body, err := json.Marshal(map[string]any{
		"server": server, "tool": tool, "args": args, "context": cc, "relay": relay,
	})
	if err != nil {
		return nil, err
	}
	resp, err := c.open(ctx, http.MethodPost, "/v1/call", body, "application/json", nil)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), daemon.RelayContentType) {
		b, _, err := c.readReply(resp)
		if err != nil {
			return nil, err
		}
		var out CallResult
		return &out, json.Unmarshal(b, &out)
	}
	defer resp.Body.Close()
	dec := json.NewDecoder(resp.Body)
	for {
		var f daemon.RelayFrame
		if err := dec.Decode(&f); err != nil {
			return nil, fmt.Errorf("/v1/call: relayed stream ended without a result: %w", err)
		}
		if f.Method != "" {
			notify(f.Method, f.Params)
			continue
		}
		// The last line: the reply the call would have had as plain JSON.
		fake := &http.Response{StatusCode: f.Status, Body: io.NopCloser(bytes.NewReader(f.Body))}
		b, _, err := c.readReply(fake)
		if err != nil {
			return nil, err
		}
		var out CallResult
		return &out, json.Unmarshal(b, &out)
	}
}

// InputRequiredError is a call that stopped to ask something nobody here
// can answer. The call is still running on the daemon; the error carries
// the question and how to answer it.
type InputRequiredError struct {
	Doc daemon.InputRequired
}

func (e *InputRequiredError) Error() string { return strings.TrimRight(e.Doc.Text, "\n") }

// ExitCode is ExitInputRequired, so main exits with it.
func (e *InputRequiredError) ExitCode() int { return ExitInputRequired }

// CallReporting is Call for a caller that cannot answer a question mid-call.
// A question comes back as *InputRequiredError instead of holding the call
// open until it expires.
func (c *Client) CallReporting(ctx context.Context, server, tool string, cc config.CallContext, args json.RawMessage) (*CallResult, error) {
	body, err := json.Marshal(map[string]any{
		"server": server, "tool": tool, "args": args, "context": cc,
	})
	if err != nil {
		return nil, err
	}
	b, status, err := c.sendWith(ctx, http.MethodPost, "/v1/call", body, "application/json",
		map[string]string{daemon.InputHeader: daemon.InputReport})
	if err != nil {
		return nil, err
	}
	if status == http.StatusAccepted {
		var ir struct {
			InputRequired *daemon.InputRequired `json:"inputRequired"`
		}
		if json.Unmarshal(b, &ir) == nil && ir.InputRequired != nil {
			return nil, &InputRequiredError{Doc: *ir.InputRequired}
		}
	}
	var out CallResult
	return &out, json.Unmarshal(b, &out)
}

// ReleaseCaller frees instances a finished caller created.
func (c *Client) ReleaseCaller(ctx context.Context, callID string) error {
	_, err := c.do(ctx, http.MethodPost, "/v1/session/release", map[string]string{"session": callID})
	return err
}

// Status returns the daemon status document.
func (c *Client) Status(ctx context.Context) (map[string]any, error) {
	b, err := c.do(ctx, http.MethodGet, "/v1/status", nil)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	return out, json.Unmarshal(b, &out)
}

// Refresh re-reads every server's schemas.
func (c *Client) Refresh(ctx context.Context) (map[string]any, error) {
	b, err := c.do(ctx, http.MethodPost, "/v1/refresh", nil)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	return out, json.Unmarshal(b, &out)
}

// RestartReply is what POST /v1/restart answers.
type RestartReply struct {
	Stopped int  `json:"stopped"`
	Started int  `json:"started"`
	Failed  int  `json:"failed"`
	Lazy    bool `json:"lazy"`
	Servers []struct {
		Server  string   `json:"server"`
		Stopped int      `json:"stopped"`
		Started []string `json:"started"`
		Failed  []struct {
			Key   string `json:"key"`
			Error string `json:"error"`
		} `json:"failed"`
		Skipped []struct {
			Key   string `json:"key"`
			Error string `json:"error"`
		} `json:"skipped"`
		Note string `json:"note"`
	} `json:"servers"`
}

// Restart restarts a server (or all when empty); lazy only stops.
func (c *Client) Restart(ctx context.Context, server string, lazy bool) (RestartReply, error) {
	var out RestartReply
	b, err := c.do(ctx, http.MethodPost, "/v1/restart", map[string]any{"server": server, "lazy": lazy})
	if err != nil {
		return out, err
	}
	return out, json.Unmarshal(b, &out)
}

// Shutdown asks the daemon to exit.
func (c *Client) Shutdown(ctx context.Context) error {
	_, err := c.do(ctx, http.MethodPost, "/v1/shutdown", nil)
	return err
}

// RestartDaemon asks the daemon to restart itself.
func (c *Client) RestartDaemon(ctx context.Context) error {
	_, err := c.do(ctx, http.MethodPost, "/v1/daemon/restart", nil)
	return err
}

// Endpoint returns the daemon's loopback base URL.
func (c *Client) Endpoint(ctx context.Context) (string, error) {
	b, err := c.do(ctx, http.MethodGet, "/v1/health", nil)
	if err != nil {
		return "", err
	}
	var out struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return "", err
	}
	return out.Endpoint, nil
}

func urlEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.', r == '~':
			b.WriteRune(r)
		default:
			for _, by := range []byte(string(r)) {
				fmt.Fprintf(&b, "%%%02X", by)
			}
		}
	}
	return b.String()
}

// Prompts lists every prompt the configured servers offer.
//
// Prompts are the part of MCP that is not tools: a server saying "here is the
// wording that works for this" rather than "here is a function". mcpx used to
// report none, which threw away everything a server published that was not a
// tool.
func (c *Client) Prompts(ctx context.Context, ns []string) ([]daemon.PromptInfo, error) {
	q := ""
	if len(ns) > 0 {
		q = "?ns=" + strings.Join(ns, ",")
	}
	b, err := c.do(ctx, http.MethodGet, "/v1/prompts"+q, nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Prompts []daemon.PromptInfo `json:"prompts"`
	}
	return out.Prompts, json.Unmarshal(b, &out)
}

// Resources lists every resource the configured servers offer.
func (c *Client) Resources(ctx context.Context, ns []string) ([]daemon.ResourceInfo, error) {
	q := ""
	if len(ns) > 0 {
		q = "?ns=" + strings.Join(ns, ",")
	}
	b, err := c.do(ctx, http.MethodGet, "/v1/resources"+q, nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Resources []daemon.ResourceInfo `json:"resources"`
	}
	return out.Resources, json.Unmarshal(b, &out)
}

// GetPrompt renders one prompt with its arguments filled in.
func (c *Client) GetPrompt(ctx context.Context, server, name string, args map[string]string, cc config.CallContext) (json.RawMessage, error) {
	b, err := c.do(ctx, http.MethodPost, "/v1/prompt", map[string]any{
		"server": server, "name": name, "arguments": args,
		"sessionId": cc.SessionID, "callId": cc.CallID,
	})
	if err != nil {
		return nil, err
	}
	var out struct {
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, errors.New(out.Error)
	}
	return out.Result, nil
}

// ReadResource reads one resource from a namespace.
func (c *Client) ReadResource(ctx context.Context, server, uri string, cc config.CallContext) (json.RawMessage, error) {
	// "context", which is what /v1/resource reads. This sent sessionId and
	// callId, which it does not, so every read landed in a fresh anonymous
	// scope whatever session the caller named.
	b, err := c.do(ctx, http.MethodPost, "/v1/resource", map[string]any{
		"server": server, "uri": uri, "context": cc,
	})
	if err != nil {
		return nil, err
	}
	var out struct {
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, errors.New(out.Error)
	}
	return out.Result, nil
}

// ResourceTemplates lists every templated resource.
func (c *Client) ResourceTemplates(ctx context.Context) ([]daemon.ResourceInfo, error) {
	return c.ResourceTemplatesIn(ctx, nil)
}

// ResourceTemplatesIn lists the templated resources of some namespaces.
func (c *Client) ResourceTemplatesIn(ctx context.Context, ns []string) ([]daemon.ResourceInfo, error) {
	path := "/v1/resource-templates"
	if len(ns) > 0 {
		path += "?ns=" + url.QueryEscape(strings.Join(ns, ","))
	}
	b, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		ResourceTemplates []daemon.ResourceInfo `json:"resourceTemplates"`
	}
	return out.ResourceTemplates, json.Unmarshal(b, &out)
}

// Stream reads the daemon's event stream until the context ends, calling fn
// for each event.
//
// Server-sent events over the same connection everything else uses, so a
// remote daemon streams exactly as a local one does. Resumes from the last
// sequence seen when the connection drops, which is what makes a flaky
// network lossless rather than merely reconnecting.
func (c *Client) Stream(ctx context.Context, f events.Filter, fn func(events.Event)) error {
	var last uint64
	for ctx.Err() == nil {
		q := url.Values{}
		if len(f.Kinds) > 0 {
			q.Set("kinds", strings.Join(f.Kinds, ","))
		}
		if f.Session != "" {
			q.Set("session", f.Session)
		}
		if f.Server != "" {
			q.Set("server", f.Server)
		}
		if len(f.URIs) > 0 {
			q.Set("uri", strings.Join(f.URIs, ","))
		}
		if last > 0 {
			q.Set("since", strconv.FormatUint(last, 10))
		}
		base := c.endpoint
		if base == "" {
			base = "http://mcpx"
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/events?"+q.Encode(), nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "text/event-stream")
		// No timeout on a stream; the context bounds it instead. The
		// client's ordinary timeout would cut a healthy stream off.
		streamClient := &http.Client{Transport: c.hc.Transport}
		resp, err := streamClient.Do(req)
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(c.set.Duration("events.reconnect")):
			}
			continue
		}
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, c.set.Bytes("http.streamBufferInit")),
			int(c.set.Bytes("http.streamBufferMax")))
		var data strings.Builder
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "data: "):
				data.WriteString(strings.TrimPrefix(line, "data: "))
			case line == "":
				if data.Len() > 0 {
					var e events.Event
					if json.Unmarshal([]byte(data.String()), &e) == nil && e.Kind != "" {
						if e.Seq > last {
							last = e.Seq
						}
						fn(e)
					}
					data.Reset()
				}
			}
		}
		resp.Body.Close()
	}
	return ctx.Err()
}
