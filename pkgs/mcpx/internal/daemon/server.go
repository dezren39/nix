package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/dezren39/mcpx/internal/codegen"
	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/defaults"
	"github.com/dezren39/mcpx/internal/elicit"
	"github.com/dezren39/mcpx/internal/events"
	"github.com/dezren39/mcpx/internal/logging"
	"github.com/dezren39/mcpx/internal/logstore"
	"github.com/dezren39/mcpx/internal/mcpclient"
	"github.com/dezren39/mcpx/internal/mcpserver"
	"github.com/dezren39/mcpx/internal/pool"
	"github.com/dezren39/mcpx/internal/settings"
	"github.com/dezren39/mcpx/internal/tasks"
)

// Server is the local HTTP API.
//
// It listens on a unix socket (for the CLI, no port to collide with) and on a
// loopback TCP port (for generated script clients, because fetch() over a unix
// socket is not portable across Deno, Bun and Node).
type Server struct {
	reg *Registry
	// augment is Options.Augment, kept for reloads.
	augment func(*config.Config) error
	paths   Paths
	cfg     *config.Config
	logger  *log.Logger

	// Address is the interface the TCP listener binds. Empty is loopback.
	Address string

	// MCP is mcpx's own MCP server, mounted on the daemon's listeners.
	//
	// Supplied from outside because the daemon cannot import the CLI that
	// builds it -- the dependency runs one way, and inverting it to put the
	// two HTTP servers together would be a worse cure than the disease.
	// Nil means the daemon serves /v1 only.
	MCP http.Handler
	// MCPPath is where MCP is mounted.
	MCPPath string
	// MCPTool invokes one of mcpx's own MCP tools by name, for the plain
	// POST projection of it. Supplied alongside MCP, for the same reason.
	MCPTool func(ctx context.Context, tool string, args json.RawMessage) (string, error)

	// Events carries everything the daemon notices, to every subscriber.
	Events *events.Bus

	// Origins decides which browser origins may use the daemon at all; see
	// refuseBrowserPages. The zero value allows loopback origins only.
	Origins mcpserver.OriginPolicy

	httpSrv  *http.Server
	tcpLn    net.Listener
	unixLn   net.Listener
	endpoint string
	version  string
	started  time.Time

	// idleExit stops a daemon that nobody has used for this long and that
	// holds no live MCP instances. Auto-started daemons set it so that a repo
	// visited once does not leave a process behind forever; a daemon run under
	// launchd leaves it at zero and stays up.
	idleExit time.Duration

	// sink is the durable log POST /v1/log appends to. Nil when the log
	// could not be opened, which the daemon survives and the route reports.
	sink    *logging.FileSink
	lastReq atomic.Int64

	// cfgMu guards cfg, contentHash and stamp. The daemon re-reads its
	// configuration from a request goroutine now, so what used to be
	// write-once state is not.
	cfgMu sync.RWMutex
	// reloadMu serialises reloads, so two requests arriving together do not
	// each rebuild the pool map from the same starting point.
	reloadMu sync.Mutex
	// contentHash is what the configuration files said when they were last
	// read, so an edit made behind the daemon's back is noticed rather than
	// silently ignored until a restart.
	contentHash string
	// stamp is the cheap version of the same question: modification times
	// and sizes, so the common case costs a stat rather than a read.
	stamp string

	// set is the daemon's resolved configuration. Read on every request
	// rather than copied into fields at startup, so a runtime change through
	// PUT /v1/settings reaches the code that acts on it without a restart.
	set *settings.Set

	// tasks holds calls a client asked to run in the background. Created on
	// first use, because a daemon that never runs one should carry nothing.
	taskOnce sync.Once
	tasks    *tasks.Store

	tokenStore *logstore.TokenStore

	// consumer holds the policy and the schema history behind the things
	// mcpx asks for itself. See internal/daemon/consumer.go.
	consumer *consumerState
	artifactState
}

// Options configure the daemon.
type Options struct {
	Config  *config.Config
	Paths   Paths
	Version string
	// TCPPort of 0 picks a free ephemeral port.
	TCPPort int
	Logger  *log.Logger
	// Warm fetches all schemas at startup instead of on first use.
	Warm bool
	// IdleExit stops an unused daemon after this long. Zero means never.
	IdleExit time.Duration
	// Sink is the durable log the daemon already writes; POST /v1/log
	// appends to the same one rather than opening a second writer.
	Sink *logging.FileSink
	// Settings is the resolved configuration. Nil means the built-in
	// defaults, which is what a test that only wants a daemon should get
	// rather than a nil dereference.
	Settings *settings.Set
	// Augment adds servers that are not written in a config file -- an
	// adapted program is one -- to every config the daemon loads, at start
	// and on each reload, so they go through the same pools, cache, codegen
	// and /v1 routes as a configured server. Nil adds nothing.
	Augment func(*config.Config) error
}

// NewServer builds the daemon but does not listen yet.
func NewServer(opts Options) (*Server, error) {
	if opts.Logger == nil {
		opts.Logger = log.New(os.Stderr, "", log.LstdFlags|log.Lmicroseconds)
	}
	if err := opts.Paths.EnsureDirs(); err != nil {
		return nil, err
	}
	if opts.Augment != nil && opts.Config != nil {
		if err := opts.Augment(opts.Config); err != nil {
			return nil, err
		}
	}
	reg, err := NewRegistry(opts.Config, opts.Paths, func(f string, a ...any) {
		opts.Logger.Printf(f, a...)
	})
	if err != nil {
		return nil, err
	}
	if opts.Settings == nil {
		sch, serr := settings.New(settings.Registry())
		if serr != nil {
			return nil, serr
		}
		opts.Settings = settings.NewSet(sch)
	}
	// logging.trace, which is MCPX_TRACE. It was read here by name, so a
	// configuration file or `mcpx daemon --logging-trace` could not turn it on.
	if opts.Settings.Bool("logging.trace") {
		pool.Trace = func(f string, a ...any) { opts.Logger.Printf(f, a...) }
	}
	srv := &Server{
		set:      opts.Settings,
		reg:      reg,
		paths:    opts.Paths,
		cfg:      opts.Config,
		logger:   opts.Logger,
		version:  opts.Version,
		idleExit: opts.IdleExit,
		sink:     opts.Sink,
		augment:  opts.Augment,
		Events:   events.New(opts.Settings.Int("events.history")),
	}
	// The broker is optional: a daemon whose state directory cannot hold a
	// database still serves tools, and answers every server question with
	// cancel rather than hanging.
	broker, berr := OpenBroker(opts.Paths)
	if berr != nil {
		opts.Logger.Printf("elicitation disabled: %v", berr)
		broker = nil
	}
	reg.UseSettings(opts.Settings)
	reg.InstallHooks(srv.Events, broker, nil)
	srv.initConsumer()
	srv.tokenStore, _ = logstore.NewTokenStore(filepath.Join(opts.Paths.State, "tokens.db"))
	if opts.Config != nil {
		srv.contentHash = ContentFingerprint(opts.Config.Sources)
		srv.stamp = stampConfig(opts.Config.Sources)
	}
	srv.lastReq.Store(time.Now().UnixNano())
	return srv, nil
}

// Registry exposes the pool registry (used by the in-process fast path).
func (s *Server) Registry() *Registry { return s.reg }

// Endpoint is the loopback base URL once Listen has run.
func (s *Server) Endpoint() string { return s.endpoint }

// Listen binds both listeners and publishes the daemon record.
func (s *Server) Listen(tcpPort int) error {
	// A stale socket from a crashed daemon must not block a fresh one.
	if err := s.clearStaleSocket(); err != nil {
		return err
	}
	unixLn, err := net.Listen("unix", s.paths.Socket)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.paths.Socket, err)
	}
	if err := os.Chmod(s.paths.Socket, defaults.PrivateMode); err != nil {
		unixLn.Close()
		return err
	}
	// Loopback unless told otherwise. Binding wider is a deliberate act:
	// the API is unauthenticated, so the network it is on is the access
	// control, and that has to be somebody's decision rather than a default.
	host := s.Address
	if host == "" {
		host = s.set.String("daemon.address")
	}
	tcpLn, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(tcpPort)))
	if err != nil {
		unixLn.Close()
		return fmt.Errorf("listen tcp: %w", err)
	}
	s.unixLn, s.tcpLn = unixLn, tcpLn
	s.endpoint = "http://" + tcpLn.Addr().String()
	s.started = time.Now()

	cfgPath := ""
	if cfg := s.currentConfig(); cfg != nil {
		cfgPath = cfg.Path
	}
	return s.paths.WriteInfo(Info{
		PID:        os.Getpid(),
		Socket:     s.paths.Socket,
		Endpoint:   s.endpoint,
		ConfigPath: cfgPath,
		ConfigHash: s.reg.ConfigHash(),
		Version:    s.version,
		StartedAt:  s.started.Format(time.RFC3339),
	})
}

func (s *Server) clearStaleSocket() error {
	if _, err := os.Stat(s.paths.Socket); err != nil {
		return nil
	}
	c, err := net.DialTimeout("unix", s.paths.Socket, s.set.Duration("daemon.socketProbeTimeout"))
	if err == nil {
		c.Close()
		return fmt.Errorf("a daemon is already listening on %s", s.paths.Socket)
	}
	s.logger.Printf("removing stale socket %s", s.paths.Socket)
	return os.Remove(s.paths.Socket)
}

// Serve runs until the context is cancelled or a signal arrives.
// ServeInline runs the daemon's API inside the calling process, on a private
// socket, until the context ends.
//
// This is the last rung of the connection ladder: no separate process at all.
// It serves the identical handler the real daemon serves, so inline mode
// cannot drift from daemon mode -- there is only one API, hosted in two
// places.
//
// What changes is lifetime. Servers start when first called and die when the
// calling process exits, so nothing is pooled across invocations and a
// stateful server -- a browser -- cannot outlive one command. That is the
// price of not having a daemon, and it is paid on every run.
func (s *Server) ServeInline(ctx context.Context, socket string) error {
	_ = os.Remove(socket)
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	s.unixLn = ln
	mux := http.NewServeMux()
	s.routes(mux)
	s.httpSrv = &http.Server{Handler: s.refuseBrowserPages(mux),
		ReadHeaderTimeout: s.set.Duration("http.readHeaderTimeout")}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(),
			s.set.Duration("http.shutdownGrace"))
		defer cancel()
		_ = s.httpSrv.Shutdown(shutdown)
		s.reg.Close()
		_ = os.Remove(socket)
	}()
	go func() { _ = s.httpSrv.Serve(ln) }()
	return nil
}

func (s *Server) Serve(ctx context.Context) error {
	mux := http.NewServeMux()
	s.routes(mux)
	s.httpSrv = &http.Server{
		Handler:           s.refuseBrowserPages(s.trackActivity(mux)),
		ReadHeaderTimeout: s.set.Duration("http.readHeaderTimeout"),
	}

	errCh := make(chan error, 2)
	go func() { errCh <- s.httpSrv.Serve(s.unixLn) }()
	go func() { errCh <- s.httpSrv.Serve(s.tcpLn) }()

	s.logger.Printf("mcpx %s listening on %s and %s", s.version, s.paths.Socket, s.endpoint)
	if cfg := s.currentConfig(); cfg != nil && cfg.Path != "" {
		s.logger.Printf("config: %s (%d servers)", cfg.Path, len(cfg.MCPServers))
	} else {
		s.logger.Printf("no config file found; run `mcpx init` to create one")
	}

	reapT := time.NewTicker(s.set.Duration("daemon.reapInterval"))
	defer reapT.Stop()
	saveT := time.NewTicker(s.set.Duration("daemon.saveInterval"))
	defer saveT.Stop()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigCh)

	for {
		select {
		case <-ctx.Done():
			return s.shutdown()
		case sig := <-sigCh:
			if sig == syscall.SIGHUP {
				s.logger.Printf("received SIGHUP, reloading configuration")
				s.reloadMu.Lock()
				added, removed, err := s.reloadConfig()
				s.reloadMu.Unlock()
				if err != nil {
					s.logger.Printf("SIGHUP reload error: %v", err)
				} else {
					s.logger.Printf("SIGHUP reload successful: %d added, %d removed", len(added), len(removed))
				}
				continue
			}
			s.logger.Printf("received %s, shutting down", sig)
			return s.shutdown()
		case err := <-errCh:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				_ = s.shutdown()
				return err
			}
		case <-reapT.C:
			s.watchConfig()
			s.reg.Reap()
			if s.shouldIdleExit() {
				s.logger.Printf("no activity for %s and no live instances; exiting", s.idleExit)
				return s.shutdown()
			}
		case <-saveT.C:
			if err := s.reg.SaveCache(); err != nil {
				s.logger.Printf("save cache: %v", err)
			}
		}
	}
}

// trackActivity records the time of the last request so the idle-exit timer
// only fires on a genuinely unused daemon.
func (s *Server) trackActivity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.lastReq.Store(time.Now().UnixNano())
		// Checked here so that an edit is live on the *next command*, not on
		// the next reap tick. Two stats and a string compare; the read only
		// happens when something moved.
		s.watchConfig()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) shouldIdleExit() bool {
	if s.idleExit <= 0 {
		return false
	}
	if time.Since(time.Unix(0, s.lastReq.Load())) < s.idleExit {
		return false
	}
	for _, st := range s.reg.Status() {
		if st.Live > 0 {
			return false
		}
	}
	return true
}

func (s *Server) shutdown() error {
	// Flush the schema cache before the listeners go away. A client learns the
	// daemon has stopped by watching the socket stop answering, so anything
	// persisted after that point races with whatever the client does next --
	// including removing the state directory.
	if err := s.reg.SaveCache(); err != nil {
		s.logger.Printf("save cache: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(),
		s.set.Duration("http.shutdownGrace"))
	defer cancel()
	if s.httpSrv != nil {
		_ = s.httpSrv.Shutdown(ctx)
	}
	s.reg.Close()
	if s.tokenStore != nil {
		_ = s.tokenStore.Close()
	}
	_ = os.Remove(s.paths.Socket)
	_ = os.Remove(s.paths.Info)
	s.logger.Printf("stopped")
	return nil
}

// WarmAsync fetches schemas in the background so the first real call is fast
// without delaying the listener.
func (s *Server) WarmAsync() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(),
			s.set.Duration("daemon.warmTimeout"))
		defer cancel()
		start := time.Now()
		errs := s.reg.Warm(ctx, false)
		s.logger.Printf("warm complete in %s (%d failed)", time.Since(start).Truncate(time.Millisecond), len(errs))
	}()
}

// ---- routes ----

func (s *Server) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/health", s.handleHealth)
	mux.HandleFunc("GET /v1/status", s.handleStatus)
	// Server-sent events, because they are plain HTTP: they go through every
	// proxy that HTTP goes through, reconnect themselves in a browser, and
	// need nothing but a GET to consume. A WebSocket would buy
	// bidirectionality nobody needs here -- answers go back as ordinary
	// POSTs -- at the cost of an upgrade that half of all middleboxes
	// mishandle.
	mux.HandleFunc("GET /v1/events", s.handleEvents)
	// Elicitation over the daemon's own API, so a client that already holds
	// the socket -- the plugin, the TUI -- can see and answer questions
	// without spawning the binary to do it. The 135x difference between a
	// socket call and a spawn is exactly the difference that matters for
	// something sitting on the path of every tool call.
	mux.HandleFunc("GET /v1/elicit", s.handleElicitList)
	mux.HandleFunc("GET /v1/elicit/{id}", s.handleElicitGet)
	mux.HandleFunc("POST /v1/elicit/{id}/{action}", s.handleElicitAnswer)
	mux.HandleFunc("POST /v1/log", s.handleLogRecord)
	mux.HandleFunc("GET /v1/resource-templates", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"resourceTemplates": s.reg.ResourceTemplates(splitCSV(r.URL.Query().Get("ns"))),
		})
	})
	mux.HandleFunc("GET /v1/prompts", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"prompts": s.reg.Prompts(splitCSV(r.URL.Query().Get("ns"))),
		})
	})
	mux.HandleFunc("GET /v1/resources", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"resources": s.reg.Resources(splitCSV(r.URL.Query().Get("ns"))),
		})
	})
	mux.HandleFunc("POST /v1/prompt", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Server    string            `json:"server"`
			Name      string            `json:"name"`
			Arguments map[string]string `json:"arguments"`
			SessionID string            `json:"sessionId"`
			CallID    string            `json:"callId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		cc := config.CallContext{SessionID: req.SessionID, CallID: req.CallID}
		out, err := s.reg.GetPrompt(r.Context(), req.Server, req.Name, req.Arguments, cc)
		if err != nil {
			writeJSON(w, failureStatus(err), map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"result": out})
	})
	mux.HandleFunc("GET /v1/namespaces", s.handleNamespaces)
	mux.HandleFunc("GET /v1/tools", s.handleTools)
	mux.HandleFunc("GET /v1/search", s.handleSearch)
	mux.HandleFunc("GET /v1/types", s.handleTypes)
	mux.HandleFunc("GET /v1/catalog", s.handleCatalog)
	mux.HandleFunc("GET /v1/client.ts", s.handleClient)
	mux.HandleFunc("GET /v1/globals.d.ts", s.handleGlobals)
	mux.HandleFunc("POST /v1/call", s.handleCall)
	mux.HandleFunc("POST /v1/batch", s.handleBatch)
	mux.HandleFunc("POST /v1/resource", s.handleResource)
	mux.HandleFunc("POST /v1/session/release", s.handleRelease)
	mux.HandleFunc("POST /v1/refresh", s.handleRefresh)
	mux.HandleFunc("POST /v1/restart", s.handleRestart)
	mux.HandleFunc("GET /dashboard", s.handleDashboard)
	mux.HandleFunc("GET /v1/dashboard", s.handleDashboard)
	mux.HandleFunc("GET /v1/metrics/tokens", s.handleMetricsTokens)
	mux.HandleFunc("GET /v1/metrics/tools", s.handleMetricsTools)
	mux.HandleFunc("POST /v1/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("POST /v1/embeddings", s.handleEmbeddings)
	mux.HandleFunc("GET /v1/embeddings/export", s.handleEmbeddingsExport)
	mux.HandleFunc("POST /v1/embeddings/import", s.handleEmbeddingsImport)
	mux.HandleFunc("POST /v1/feedback", s.handleFeedbackSubmit)
	mux.HandleFunc("GET /v1/interactions", s.handleInteractionsList)
	mux.HandleFunc("GET /v1/interactions/{id}", s.handleInteractionGet)
	mux.HandleFunc("POST /v1/daemon/restart", s.handleDaemonRestart)
	mux.HandleFunc("POST /v1/shutdown", s.handleShutdown)
	s.routesExec(mux)
	s.routesSettings(mux)
	// Everything declared in internal/api that is not above. A parity test
	// fails if the two ever disagree.
	s.routesV1Ops(mux)
	s.routesProto(mux)
	// mcpx's own MCP surface, on the listeners the daemon already has.
	// Two HTTP servers with overlapping /v1 prefixes was one owner too
	// many: whichever you reached decided which half of the API existed.
	if s.MCP != nil {
		path := s.MCPPath
		if path == "" {
			path = defaults.ProtoMCPPath
		}
		mux.Handle(path, s.MCP)
	}
	s.routesConsumer(mux)
	s.routesResolve(mux)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func writeText(w http.ResponseWriter, code int, s string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(s))
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{
		"status":   "ok",
		"version":  s.version,
		"pid":      os.Getpid(),
		"uptime":   time.Since(s.started).Truncate(time.Second).String(),
		"endpoint": s.endpoint,
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	cfgPath := ""
	if cfg := s.currentConfig(); cfg != nil {
		cfgPath = cfg.Path
	}
	writeJSON(w, 200, map[string]any{
		"version":  s.version,
		"pid":      os.Getpid(),
		"uptime":   time.Since(s.started).Truncate(time.Second).String(),
		"endpoint": s.endpoint,
		"socket":   s.paths.Socket,
		"config":   cfgPath,
		"servers":  s.reg.Status(),
	})
}

// profileOf reads the profile selection from a request's query string.
func profileOf(r *http.Request) config.Profile {
	q := r.URL.Query()
	return config.Profile{
		Names:       splitWords(q.Get("profile")),
		SkipDefault: q.Get("skipDefault") == "1",
		All:         q.Get("allProfiles") == "1",
	}
}

func (s *Server) handleNamespaces(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.reg.Namespaces(profileOf(r)))
}

func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	ns := splitCSV(r.URL.Query().Get("ns"))
	writeJSON(w, 200, s.reg.Tools(ns))
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		cs := s.callSettings(r)
		if cs != nil {
			limit = cs.Int("search.limit")
		} else {
			limit = defaults.SearchLimit
		}
	}
	semStr := r.URL.Query().Get("semantic")
	semantic := semStr == "true" || semStr == "1"
	if !semantic {
		cs := s.callSettings(r)
		if cs != nil {
			semantic = cs.Bool("search.semantic")
		}
	}
	writeJSON(w, 200, s.reg.SearchSemantic(r.URL.Query().Get("q"), limit, semantic))
}

func (s *Server) handleTypes(w http.ResponseWriter, r *http.Request) {
	nss, err := s.reg.CodegenNamespaces(splitCSV(r.URL.Query().Get("ns")), profileOf(r))
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	if r.URL.Query().Get("instructions") == "0" {
		for i := range nss {
			nss[i].Instructions = ""
		}
	}
	writeText(w, 200, codegen.Declarations(nss))
}

func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	nss, err := s.reg.CodegenNamespaces(splitCSV(r.URL.Query().Get("ns")), profileOf(r))
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	cs := s.callSettings(r)
	budget, _ := strconv.Atoi(r.URL.Query().Get("budget"))
	if budget <= 0 {
		budget = cs.Int("catalog.budget")
	}
	bias := splitWords(r.URL.Query().Get("bias"))
	if len(bias) == 0 {
		bias = cs.List("catalog.bias")
	}
	writeText(w, 200, codegen.Catalog(nss, codegen.CatalogOptions{
		Budget: budget,
		Bias:   bias,
	}))
}

func splitWords(s string) []string {
	return strings.Fields(strings.ReplaceAll(s, ",", " "))
}

func (s *Server) handleClient(w http.ResponseWriter, r *http.Request) {
	nss, err := s.reg.CodegenNamespaces(splitCSV(r.URL.Query().Get("ns")), profileOf(r))
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	session := r.URL.Query().Get("session")
	w.Header().Set("Content-Type", "application/typescript")
	_, _ = w.Write([]byte(codegen.Module(nss, s.endpoint, session)))
}

func (s *Server) handleGlobals(w http.ResponseWriter, r *http.Request) {
	nss, err := s.reg.CodegenNamespaces(splitCSV(r.URL.Query().Get("ns")), profileOf(r))
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	w.Header().Set("Content-Type", "application/typescript")
	_, _ = w.Write([]byte(codegen.GlobalDeclarations(nss)))
}

type callReq struct {
	Server  string             `json:"server"`
	Tool    string             `json:"tool"`
	Args    json.RawMessage    `json:"args"`
	Context config.CallContext `json:"context"`
	// Session is the shorthand a caller may send instead of a full context.
	Session string `json:"session"`
	// Task, when present, asks for a handle now and the result later.
	Task *struct {
		TTL int64 `json:"ttl"`
	} `json:"task"`
	// Relay, when present, asks for the upstream's progress and log
	// messages during the call; see CallRelay.
	Relay *CallRelay `json:"relay,omitempty"`
}

// callContext merges the JSON body with the header shorthands, so a plain
// curl can still reach a scoped server without constructing a context object.
func callContext(r *http.Request, body config.CallContext, session string) config.CallContext {
	cc := body
	if cc.SessionID == "" {
		cc.SessionID = firstNonEmpty(session, r.Header.Get("X-Mcpx-Session"))
	}
	if cc.CallID == "" {
		cc.CallID = firstNonEmpty(r.Header.Get("X-Mcpx-Call"), cc.SessionID)
	}
	// With nothing to name the caller, the scope fallback would put every
	// such request in one shared "call:anonymous" instance -- the accidental
	// sharing config/scope.go promises it avoids. A fresh id per request is
	// the per-call isolation it promises instead; the lease reaper releases it.
	if cc.CallID == "" {
		cc.CallID = string(logging.NewTraceID("anon"))
	}
	if cc.ParentSessionID == "" {
		cc.ParentSessionID = r.Header.Get("X-Mcpx-Parent-Session")
	}
	if cc.Cwd == "" {
		cc.Cwd = r.Header.Get("X-Mcpx-Cwd")
	}
	if cc.PID == 0 {
		if n, err := strconv.Atoi(r.Header.Get("X-Mcpx-Pid")); err == nil {
			cc.PID = n
		}
	}
	if !cc.Ephemeral {
		cc.Ephemeral = r.Header.Get("X-Mcpx-Ephemeral") == "1"
	}
	return cc
}

func (s *Server) handleCall(w http.ResponseWriter, r *http.Request) {
	var req callReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, s.set.Bytes("http.callBodyLimit"))).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if req.Server == "" || req.Tool == "" {
		writeErr(w, 400, errors.New("server and tool are required"))
		return
	}
	cc := callContext(r, req.Context, req.Session)

	var args any = map[string]any{}
	if len(req.Args) > 0 {
		if err := json.Unmarshal(req.Args, &args); err != nil {
			writeErr(w, 400, fmt.Errorf("args: %w", err))
			return
		}
	}

	// A caller that asked for a task gets a handle immediately and collects
	// from /v1/tasks, rather than holding this request open for however long
	// the tool takes.
	if req.Task != nil {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"task": s.startCallTask(req.Task.TTL, req.Server, req.Tool, cc, args)})
		return
	}

	if r.Header.Get(InputHeader) == InputReport && s.reg.broker != nil {
		s.handleCallReporting(w, r, req, cc, args)
		return
	}

	ctx := withRun(withCallSettings(withClientCaps(r.Context(), r), s.callSettings(r)), r.Header.Get("X-Mcpx-Run"))
	reply := writeJSON
	if req.Relay != nil {
		rw := &relayWriter{w: w}
		ctx = mcpclient.WithRelay(ctx, rw.relay(req.Relay))
		reply = func(_ http.ResponseWriter, code int, v any) { rw.finish(code, v) }
	}
	start := time.Now()
	res, err := s.reg.Call(ctx, req.Server, req.Tool, cc, args)
	dur := time.Since(start).Truncate(time.Millisecond)

	toolKey := req.Server + ":" + req.Tool
	var inBytes, outBytes []byte
	inBytes, _ = json.Marshal(args)
	if err == nil {
		outBytes, _ = json.Marshal(res)
	}

	if fb := s.reg.FeedbackStore(); fb != nil {
		_ = fb.RecordInteraction(logstore.InteractionRecord{
			TraceID:   cc.CallID,
			Timestamp: start,
			SessionID: cc.SessionID,
			Source:    "call",
			Input:     string(inBytes),
			Output:    string(outBytes),
			ToolsUsed: []string{toolKey},
		})
	}

	if err != nil {
		s.logger.Printf("call %s.%s failed in %s: %v", req.Server, req.Tool, dur, err)
		reply(w, failureStatus(err), callErrorBody(err))
		return
	}
	reply(w, 200, map[string]any{"result": res, "durationMs": dur.Milliseconds(), "traceId": cc.CallID})
}

type batchCallReqItem struct {
	Server    string          `json:"server"`
	Namespace string          `json:"namespace"`
	Tool      string          `json:"tool"`
	Name      string          `json:"name"`
	Args      json.RawMessage `json:"args"`
	Arguments json.RawMessage `json:"arguments"`
}

type batchReq struct {
	Calls       []batchCallReqItem `json:"calls"`
	StopOnError *bool              `json:"stopOnError"`
	Parallel    bool               `json:"parallel"`
	Context     config.CallContext `json:"context"`
	Session     string             `json:"session"`
}

func (s *Server) handleBatch(w http.ResponseWriter, r *http.Request) {
	var req batchReq
	limit := defaults.HTTPCallBodyLimit
	if s.set != nil {
		limit = s.set.Bytes("http.callBodyLimit")
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit)).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if len(req.Calls) == 0 {
		writeJSON(w, 200, map[string]any{"results": []any{}})
		return
	}

	stopOnErr := true
	if req.StopOnError != nil {
		stopOnErr = *req.StopOnError
	}

	cc := callContext(r, req.Context, req.Session)
	ctx := withRun(withCallSettings(withClientCaps(r.Context(), r), s.callSettings(r)), r.Header.Get("X-Mcpx-Run"))

	results := make([]map[string]any, len(req.Calls))

	runOne := func(ctx context.Context, i int, item batchCallReqItem) (map[string]any, error) {
		server := item.Server
		if server == "" {
			server = item.Namespace
		}
		tool := item.Tool
		if tool == "" {
			tool = item.Name
		}
		if server == "" && strings.Contains(tool, ".") {
			server, tool, _ = strings.Cut(tool, ".")
		}
		if server == "" || tool == "" {
			return map[string]any{
				"index":   i,
				"server":  server,
				"tool":    tool,
				"status":  "error",
				"error":   "server and tool are required",
				"isError": true,
			}, errors.New("server and tool are required")
		}

		rawArgs := item.Args
		if len(rawArgs) == 0 {
			rawArgs = item.Arguments
		}
		var args any = map[string]any{}
		if len(rawArgs) > 0 {
			if err := json.Unmarshal(rawArgs, &args); err != nil {
				return map[string]any{
					"index":   i,
					"server":  server,
					"tool":    tool,
					"status":  "error",
					"error":   fmt.Sprintf("args: %v", err),
					"isError": true,
				}, err
			}
		}

		start := time.Now()
		res, err := s.reg.Call(ctx, server, tool, cc, args)
		dur := time.Since(start).Truncate(time.Millisecond)

		if err != nil {
			return map[string]any{
				"index":      i,
				"server":     server,
				"tool":       tool,
				"status":     "error",
				"error":      err.Error(),
				"isError":    true,
				"durationMs": dur.Milliseconds(),
			}, err
		}

		return map[string]any{
			"index":      i,
			"server":     server,
			"tool":       tool,
			"status":     "success",
			"result":     res,
			"durationMs": dur.Milliseconds(),
		}, nil
	}

	if req.Parallel {
		var wg sync.WaitGroup
		wg.Add(len(req.Calls))
		for i, item := range req.Calls {
			go func(i int, item batchCallReqItem) {
				defer wg.Done()
				res, _ := runOne(ctx, i, item)
				results[i] = res
			}(i, item)
		}
		wg.Wait()
	} else {
		for i, item := range req.Calls {
			res, err := runOne(ctx, i, item)
			results[i] = res
			if stopOnErr && err != nil {
				results = results[:i+1]
				break
			}
		}
	}

	writeJSON(w, 200, map[string]any{"results": results})
}

type resourceReq struct {
	Server  string             `json:"server"`
	URI     string             `json:"uri"`
	Context config.CallContext `json:"context"`
	Session string             `json:"session"`
}

func (s *Server) handleResource(w http.ResponseWriter, r *http.Request) {
	var req resourceReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, s.set.Bytes("http.bodyLimit"))).Decode(&req); err != nil {
		writeErr(w, 400, err)
		return
	}
	cc := callContext(r, req.Context, req.Session)
	res, err := s.reg.ReadResource(r.Context(), req.Server, req.URI, cc)
	if err != nil {
		writeErr(w, failureStatus(err), err)
		return
	}
	writeJSON(w, 200, map[string]any{"result": res})
}

func (s *Server) handleRelease(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Session string `json:"session"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, s.set.Bytes("http.controlBodyLimit"))).Decode(&req)
	if req.Session == "" {
		req.Session = r.Header.Get("X-Mcpx-Session")
	}
	writeJSON(w, 200, map[string]any{"released": s.reg.ReleaseCaller(req.Session)})
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	// The configuration is re-read first. Refreshing schemas while still
	// serving a server list from a file read hours ago answered half the
	// question and made the other half look broken: an entry added by hand,
	// or by `mcpx registry add`, stayed invisible until the daemon was
	// restarted.
	added, removed, rerr := s.reloadConfig()
	if rerr != nil {
		s.logger.Printf("reload during refresh: %v", rerr)
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.set.Duration("daemon.refreshTimeout"))
	defer cancel()
	errs := s.reg.Warm(ctx, true)
	out := map[string]string{}
	for k, v := range errs {
		out[k] = v.Error()
	}
	writeJSON(w, 200, map[string]any{
		"namespaces": s.reg.Namespaces(config.Profile{All: true}),
		"added":      added, "removed": removed, "errors": out})
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Server string `json:"server"`
		Lazy   bool   `json:"lazy"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, s.set.Bytes("http.controlBodyLimit"))).Decode(&req)
	res, err := s.reg.Restart(r.Context(), req.Server, req.Lazy)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	stopped, started, failed := 0, 0, 0
	for _, x := range res {
		stopped += x.Stopped
		started += len(x.Started)
		failed += len(x.Failed)
	}
	// 200 even when a replacement failed: the restart ran and the body says
	// what came back. The CLI turns failed > 0 into a non-zero exit.
	writeJSON(w, 200, map[string]any{
		"stopped": stopped, "started": started, "failed": failed,
		"lazy": req.Lazy, "servers": res})
}

func (s *Server) handleShutdown(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{"status": "stopping"})
	go func() {
		time.Sleep(defaults.DaemonRestartSettle)
		p, _ := os.FindProcess(os.Getpid())
		_ = p.Signal(syscall.SIGTERM)
	}()
}

func (s *Server) handleDaemonRestart(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{"status": "restarting"})
	go func() {
		time.Sleep(defaults.DaemonRestartSettle)
		if s.reg != nil {
			if err := s.reg.SaveCache(); err != nil && s.logger != nil {
				s.logger.Printf("save cache before restart: %v", err)
			}
		}
		execPath, err := os.Executable()
		if err == nil {
			if s.httpSrv != nil {
				grace := defaults.HTTPShutdownGrace
				if s.set != nil {
					grace = s.set.Duration("http.shutdownGrace")
				}
				shutdownCtx, cancel := context.WithTimeout(context.Background(), grace)
				_ = s.httpSrv.Shutdown(shutdownCtx)
				cancel()
			}
			if s.reg != nil {
				s.reg.Close()
			}
			if s.paths.Socket != "" {
				_ = os.Remove(s.paths.Socket)
			}
			if s.paths.Info != "" {
				_ = os.Remove(s.paths.Info)
			}
			if s.logger != nil {
				s.logger.Printf("re-executing daemon: %s", execPath)
			}
			if execErr := syscall.Exec(execPath, os.Args, os.Environ()); execErr != nil && s.logger != nil {
				s.logger.Printf("re-exec failed: %v", execErr)
			}
		}
		p, _ := os.FindProcess(os.Getpid())
		_ = p.Signal(syscall.SIGTERM)
	}()
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// handleEvents streams the event bus as server-sent events.
//
// Query parameters select what to hear:
//
//	kinds=elicit,server   prefixes; empty means everything
//	session=s1            one session's events only
//	server=github         one server's
//	uri=file:///a          resource updates for these URIs
//	since=42              replay everything after sequence 42
//
// Last-Event-ID is honoured as well as since, because that is the header a
// browser's EventSource sends on reconnect -- so a reconnecting browser
// resumes without any code knowing it disconnected.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	q := r.URL.Query()
	f := events.Filter{
		Kinds:   splitCSV(q.Get("kinds")),
		Session: q.Get("session"),
		Server:  q.Get("server"),
	}
	// Naming resources subscribes to them upstream for as long as this
	// stream is open. See watchResources in routes_proto.go.
	var watch *resourceWatch
	f.URIs, watch = s.watchResources(r.Context(), f.Server, splitCSV(q.Get("uri")))
	defer watch.release()
	raw := firstNonEmptyStr(r.Header.Get("Last-Event-ID"), q.Get("since"))
	since, _ := strconv.ParseUint(raw, 10, 64)
	// Present-but-zero replays everything retained; absent is live only.
	sub, gap := s.Events.SubscribeFrom(f, since, raw != "")
	defer sub.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Nginx buffers event streams by default, which turns a live stream
	// into one that arrives all at once when the connection closes.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	fmt.Fprintf(w, "retry: %d\n", s.set.Duration("http.sseRetry").Milliseconds())
	if gap {
		// Said in-band, so a subscriber can resynchronise instead of
		// trusting a stream with a hole in it.
		fmt.Fprintf(w, "event: gap\ndata: {\"since\":%d,\"latest\":%d}\n\n", since, s.Events.Latest())
	}
	if len(f.URIs) > 0 {
		watch.announce(w)
	}
	flusher.Flush()

	// A comment every so often keeps idle proxies from deciding the
	// connection is dead. The default sits under the usual sixty.
	ping := time.NewTicker(s.set.Duration("http.ssePing"))
	defer ping.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		case e, open := <-sub.C:
			if !open {
				return
			}
			b, err := json.Marshal(e)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.Seq, e.Kind, b)
			flusher.Flush()
		}
	}
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func (s *Server) handleElicitList(w http.ResponseWriter, r *http.Request) {
	if s.reg.broker == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	// The ceiling was a constant inside the broker, so elicit.pendingLimit
	// was a number `mcpx settings` reported and nothing enforced. It is
	// passed in rather than read there because the broker is a store and a
	// store should not be reading policy.
	pending, err := s.reg.broker.Pending(elicit.Filter{
		Session:  r.URL.Query().Get("session"),
		Audience: elicit.Audience(r.URL.Query().Get("audience")),
		Limit:    s.callSettings(r).Int("elicit.pendingLimit"),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if pending == nil {
		pending = []elicit.Request{}
	}
	writeJSON(w, http.StatusOK, pending)
}

func (s *Server) handleElicitGet(w http.ResponseWriter, r *http.Request) {
	if s.reg.broker == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "elicitation is disabled"})
		return
	}
	req, ok, err := s.reg.broker.Get(r.PathValue("id"))
	if err != nil || !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "no elicitation " + r.PathValue("id")})
		return
	}
	out := map[string]any{"request": req}
	if ans, answered, _ := s.reg.broker.Lookup(req.ID); answered {
		out["answer"] = ans
	}
	writeJSON(w, http.StatusOK, out)
}

// handleElicitAnswer takes accept, decline or cancel as the last path
// segment, so the action is in the URL and the body is only ever content.
func (s *Server) handleElicitAnswer(w http.ResponseWriter, r *http.Request) {
	if s.reg.broker == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "elicitation is disabled"})
		return
	}
	action := elicit.Action(r.PathValue("action"))
	switch action {
	case elicit.Accept, elicit.Decline, elicit.Cancel:
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "action is accept, decline or cancel"})
		return
	}
	var content json.RawMessage
	if action == elicit.Accept {
		b, _ := io.ReadAll(io.LimitReader(r.Body, s.set.Bytes("http.bodyLimit")))
		if len(b) > 0 {
			content = b
		}
	}
	by := r.Header.Get("X-Mcpx-Answerer")
	if by == "" {
		by = "api"
	}
	if err := s.reg.broker.Respond(elicit.Answer{
		ID: r.PathValue("id"), Action: action, Content: content, By: by,
	}); err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	s.Events.Publish(events.Event{Kind: events.ElicitAnswered,
		Data: mustJSON(map[string]any{"id": r.PathValue("id"), "action": action, "by": by})})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleLogRecord is `mcpx log record` over the daemon's own API.
//
// The opencode plugin records a timing after every tool call. Spawning the
// binary for that costs ~23ms a call; this costs a fraction of a
// millisecond over the socket. The body is exactly what the command takes,
// parsed by the same function, so a record cannot differ by how it arrived.
//
// The level is a query parameter rather than a body field so that it can
// never collide with an attribute the caller happens to call "level".
func (s *Server) handleLogRecord(w http.ResponseWriter, r *http.Request) {
	if s.sink == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "the durable log is unavailable in this daemon"})
		return
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, s.set.Bytes("http.bodyLimit")))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	rec, err := logging.ExternalRecord(b, r.URL.Query().Get("level"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	s.sink.Write(rec, nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
