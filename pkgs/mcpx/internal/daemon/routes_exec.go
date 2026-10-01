package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dezren39/mcpx/internal/runner"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dezren39/mcpx/internal/artifacts"
	"github.com/dezren39/mcpx/internal/codegen"
	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/defaults"
	"github.com/dezren39/mcpx/internal/execsvc"
	"github.com/dezren39/mcpx/internal/settings"
	"github.com/dezren39/mcpx/internal/tasks"
)

// routesExec registers running a script on the daemon and fetching what it
// produced.
//
// Why the daemon runs scripts at all: `mcpx exec` has always run the script
// in the CLI process, which is right when a person is at a terminal and
// impossible otherwise. A caller with no mcpx binary -- the opencode plugin,
// which holds a socket and nothing else -- cannot run one. A daemon on
// another machine cannot be asked to. POST /v1/exec is the same execution
// through the same service, with the daemon as the host.
func (s *Server) routesExec(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/exec", s.handleExec)
	mux.HandleFunc("POST /v1/artifacts", s.handleArtifactPut)
	mux.HandleFunc("GET /v1/artifacts", s.handleArtifactList)
	mux.HandleFunc("GET /v1/artifacts/{id}", s.handleArtifactGet)
	mux.HandleFunc("DELETE /v1/artifacts/{id}", s.handleArtifactDelete)
}

// ---- resolved settings, daemon side ----

// execSettings resolves the exec and artifact knobs for this daemon.
//
// The daemon is a separate process from the CLI, so it cannot be handed the
// CLI's resolved set. It reads the same two layers that survive a process
// boundary: the configuration file it was started with, and the environment.
// Flags belong to the command that was typed and do not apply to a request
// arriving over a socket an hour later.
func (s *Server) execSettings() *settings.Set {
	s.execSetOnce.Do(func() {
		sch, err := settings.New(settings.Registry())
		if err != nil {
			s.logger.Printf("settings registry is invalid: %v", err)
			return
		}
		set := settings.NewSet(sch)
		if s.cfg != nil && s.cfg.Path != "" {
			if b, rerr := os.ReadFile(s.cfg.Path); rerr == nil {
				var doc map[string]any
				if json.Unmarshal(config.StripJSONC(b), &doc) == nil {
					_ = sch.ApplyFile(set, doc, s.cfg.Path, 0)
				}
			}
		}
		_ = sch.ApplyEnv(set, settings.Environ())
		s.execSet = set
	})
	if s.execSet == nil {
		sch, _ := settings.New(settings.Registry())
		return settings.NewSet(sch)
	}
	return s.execSet
}

// Artifacts opens the outbox, once.
//
// A daemon whose state directory cannot hold a database still serves tools;
// it just cannot hold artifacts, and says so when one is offered rather than
// failing every request.
func (s *Server) Artifacts() (*artifacts.Store, error) {
	s.artifactsOnce.Do(func() {
		set := s.execSettings()
		if !set.Bool("artifacts.enabled") {
			s.artifactsErr = errors.New("artifacts are disabled (artifacts.enabled)")
			return
		}
		st, err := artifacts.Open(artifacts.Options{
			Dir:      filepath.Join(s.paths.State, "artifacts"),
			TTL:      set.Duration("artifacts.ttl"),
			MaxBytes: set.Bytes("artifacts.maxBytes"),
			Quota:    set.Bytes("artifacts.quota"),
		})
		if err != nil {
			s.artifactsErr = err
			return
		}
		s.artifactStore = st
		// Expired artifacts are other people's bytes on this machine's disk.
		// Sweeping them on a timer rather than on access means a store nobody
		// reads still shrinks.
		go s.collectArtifacts(set.Duration("artifacts.gcInterval"))
	})
	return s.artifactStore, s.artifactsErr
}

func (s *Server) collectArtifacts(every time.Duration) {
	if every <= 0 {
		every = defaults.ArtifactGCInterval
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for range t.C {
		if s.artifactStore == nil {
			return
		}
		if n, err := s.artifactStore.GC(time.Now()); err != nil {
			s.logger.Printf("artifact gc: %v", err)
		} else if n > 0 {
			s.logger.Printf("collected %d expired artifacts", n)
		}
	}
}

// ---- the exec service ----

// registrySource generates the client module from the pools the daemon
// already holds, with no HTTP round trip to itself.
type registrySource struct {
	reg      *Registry
	endpoint string
}

func (r registrySource) ClientModule(_ context.Context, ns []string) (string, error) {
	nss, err := r.reg.CodegenNamespaces(ns, config.Profile{})
	if err != nil {
		return "", err
	}
	// No session baked in: several concurrent runs share one generated file
	// and each must keep its own, which is what gives them separate pooled
	// instances. The runner passes it through the environment instead.
	return codegen.Module(nss, r.endpoint, ""), nil
}

func (r registrySource) Globals(_ context.Context, ns []string) (string, error) {
	nss, err := r.reg.CodegenNamespaces(ns, config.Profile{})
	if err != nil {
		return "", err
	}
	return codegen.GlobalDeclarations(nss), nil
}

func (r registrySource) Namespaces(_ context.Context, ns []string) ([]string, error) {
	nss, err := r.reg.CodegenNamespaces(ns, config.Profile{})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(nss))
	for _, n := range nss {
		out = append(out, n.Name)
	}
	return out, nil
}

// ExecService builds the service a /v1/exec request runs through.
func (s *Server) ExecService() *execsvc.Service {
	set := s.execSettings()
	store, _ := s.Artifacts()
	lim := execsvc.DefaultLimits()
	lim.Timeout = set.Duration("exec.timeout")
	lim.Runtime = set.String("script.runtime")
	lim.Permissions = set.String("script.permissions")
	// A malformed definition is reported by the run that needs it, rather
	// than failing every other route that builds a service.
	lim.Setup, lim.SetupErr = runner.ParseSetup(set.String("script.runtimes"),
		set.List("script.runtimeOrder"), set.String("script.profiles"))
	lim.CaptureConsole = set.Bool("script.captureConsole")
	lim.Typecheck = set.String("script.typecheck")
	lim.Delivery = set.String("artifacts.delivery")
	lim.InlineMaxBytes = set.Bytes("artifacts.inlineMaxBytes")
	lim.ChunkBytes = set.Bytes("artifacts.chunkBytes")
	lim.InterceptImages = set.Bool("artifacts.interceptImages")

	return &execsvc.Service{
		Src:      registrySource{reg: s.reg, endpoint: s.endpoint},
		Store:    store,
		Endpoint: s.endpoint,
		Socket:   s.paths.Socket,
		// The script the daemon runs is on the daemon's machine by
		// definition, so a path it hands to artifact() is a path the store
		// can link rather than read.
		Local:  true,
		Limits: lim,
		// Kept between runs so the runtime's module cache stays warm, and
		// one directory per catalogue so two configurations cannot
		// overwrite each other's generated client.
		WorkRoot: filepath.Join(s.paths.Cache, "exec"),
		Release: func(ctx context.Context, session string) error {
			s.reg.ReleaseCaller(session)
			return nil
		},
	}
}

// ---- POST /v1/exec ----

type execReq struct {
	Source  string          `json:"source"`
	File    string          `json:"file"`
	Options execsvc.Options `json:"options"`
}

func (s *Server) handleExec(w http.ResponseWriter, r *http.Request) {
	var req execReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Source) == "" && req.File == "" {
		writeErr(w, http.StatusBadRequest, errors.New("source is required"))
		return
	}
	svc := s.ExecService()

	// A task is a handle now and the result later, which is the right shape
	// for a script that takes minutes: holding a request open that long
	// invites every intermediary between here and the caller to time it out.
	if req.Options.Task != nil {
		store := s.taskStore()
		// A caller that named no retention gets the configured one rather
		// than the built-in constant, which is what tasks.ttl was for.
		ttl := req.Options.Task.TTL
		if ttl <= 0 {
			ttl = s.set.Duration("tasks.ttl").Milliseconds()
		}
		t := store.Start(ttl, func(ctx context.Context) (any, *tasks.Fault) {
			res, err := svc.Run(ctx, execsvc.Request{Source: req.Source, File: req.File, Opts: req.Options}, nil)
			if err != nil && res == nil {
				return nil, &tasks.Fault{Code: -32603, Message: err.Error()}
			}
			return res, nil
		})
		writeJSON(w, http.StatusAccepted, map[string]any{"task": t})
		return
	}

	// Structured unless asked otherwise. A caller over HTTP wants fields, not
	// text it has to scrape; the terminal default lives in the CLI, where
	// there is a terminal.
	switch req.Options.Output {
	case execsvc.OutputStream:
		s.execStream(w, r, svc, req)
	case "", execsvc.OutputStructured, execsvc.OutputText:
		res, err := svc.Run(r.Context(), execsvc.Request{Source: req.Source, File: req.File, Opts: req.Options}, nil)
		if err != nil && res == nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if req.Options.Output == execsvc.OutputText {
			writeText(w, http.StatusOK, res.Stdout)
			return
		}
		writeJSON(w, http.StatusOK, res)
	default:
		writeErr(w, http.StatusBadRequest,
			fmt.Errorf("output must be text, structured or stream, not %q", req.Options.Output))
	}
}

// execStream writes frames as they happen.
//
// SSE by default because it is plain HTTP: it goes through every proxy HTTP
// goes through and needs nothing but a GET-shaped response to consume. NDJSON
// when the caller asks for it, because a program reading this with a JSON
// decoder should not have to strip "data: " off every line first.
func (s *Server) execStream(w http.ResponseWriter, r *http.Request, svc *execsvc.Service, req execReq) {
	ndjson := strings.Contains(r.Header.Get("Accept"), "application/x-ndjson")
	if ndjson {
		w.Header().Set("Content-Type", "application/x-ndjson")
	} else {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
	}
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	enc := func(f execsvc.Frame) error {
		b, err := json.Marshal(f)
		if err != nil {
			return err
		}
		if ndjson {
			if _, err := w.Write(append(b, '\n')); err != nil {
				return err
			}
		} else {
			// The frame type is the SSE event name as well as a field, so a
			// browser's EventSource can filter on it without parsing.
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", f.Type, b); err != nil {
				return err
			}
		}
		if flusher != nil {
			flusher.Flush()
		}
		// A client that has gone away is noticed here rather than at the end.
		// Returning the error stops the script, which is the contract: a
		// cancelled stream must not leave work running on the daemon.
		select {
		case <-r.Context().Done():
			return r.Context().Err()
		default:
		}
		return nil
	}
	_, _ = svc.Run(r.Context(), execsvc.Request{Source: req.Source, File: req.File, Opts: req.Options}, enc)
}

// ---- artifacts ----

// handleArtifactPut is how artifact() in a script registers a file.
//
// The body is the bytes, or -- when the script is on this machine and said so
// with X-Mcpx-Path -- nothing, and the named file is hardlinked instead.
// Reading, hashing into memory and writing again to hand somebody a file they
// can already see would be the wrong shape for the local case, which is most
// cases.
func (s *Server) handleArtifactPut(w http.ResponseWriter, r *http.Request) {
	store, err := s.Artifacts()
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	}
	q := r.URL.Query()
	put := artifacts.PutOptions{
		Name: q.Get("name"),
		// The generic type is not a type. Ignoring it here lets the store
		// guess from the extension instead, and "shot.png" says more than
		// "some bytes" does.
		Mime:    firstNonEmpty(q.Get("mime"), specificType(r.Header.Get("Content-Type"))),
		Run:     q.Get("run"),
		Session: firstNonEmpty(q.Get("session"), r.Header.Get("X-Mcpx-Session")),
	}
	if ttl := q.Get("ttl"); ttl != "" {
		d, perr := time.ParseDuration(ttl)
		if perr != nil {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("ttl %q: %w", ttl, perr))
			return
		}
		put.TTL = d
	}
	var meta artifacts.Meta
	if path := r.Header.Get("X-Mcpx-Path"); path != "" {
		meta, err = store.PutFile(path, put)
	} else {
		meta, err = store.Put(r.Body, put)
	}
	if err != nil {
		code := http.StatusBadRequest
		var tooLarge artifacts.ErrTooLarge
		var quota artifacts.ErrQuota
		if errors.As(err, &tooLarge) || errors.As(err, &quota) {
			// 413 rather than 400: the request was well formed and the
			// server declined it on size, which is exactly what the status
			// means and what a client can act on without parsing prose.
			code = http.StatusRequestEntityTooLarge
		}
		writeErr(w, code, err)
		return
	}
	writeJSON(w, http.StatusCreated, meta)
}

func (s *Server) handleArtifactList(w http.ResponseWriter, r *http.Request) {
	store, err := s.Artifacts()
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	}
	q := r.URL.Query()
	limit := defaults.ArtifactListLimit
	if v := q.Get("limit"); v != "" {
		if n, cerr := strconv.Atoi(v); cerr == nil {
			limit = n
		}
	}
	list, err := store.List(artifacts.Filter{
		Run: q.Get("run"), Session: q.Get("session"), Limit: limit,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"artifacts": list})
}

// handleArtifactGet serves the bytes.
//
// http.ServeContent rather than a copy, because it brings Range, conditional
// requests and the 206 for free -- and Range is what makes fetching the tail
// of a large artifact cheap, which was one of the three things printing the
// file could not do.
func (s *Server) handleArtifactGet(w http.ResponseWriter, r *http.Request) {
	store, err := s.Artifacts()
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	}
	f, meta, err := store.OpenBody(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", meta.Mime)
	// Attachment, and the name quoted: the name came from a script, and a
	// browser deciding to render somebody else's bytes inline is how a store
	// of arbitrary files becomes an XSS surface.
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", artifacts.SanitizeName(meta.Name)))
	w.Header().Set("X-Mcpx-Sha256", meta.SHA256)
	http.ServeContent(w, r, meta.Name, meta.Created, f)
}

func (s *Server) handleArtifactDelete(w http.ResponseWriter, r *http.Request) {
	store, err := s.Artifacts()
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, err)
		return
	}
	if err := store.Delete(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": r.PathValue("id")})
}

// specificType drops the content type that means "no idea".
func specificType(v string) string {
	if strings.HasPrefix(v, "application/octet-stream") {
		return ""
	}
	return v
}

// artifactState is embedded in Server.
type artifactState struct {
	artifactsOnce sync.Once
	artifactStore *artifacts.Store
	artifactsErr  error
	execSetOnce   sync.Once
	execSet       *settings.Set
}

// InlineArtifact renders one artifact as an MCP resource body.
//
// resources/read carries text, so binary comes back base64 with its type
// stated. Exported because the MCP server reaches artifacts through the
// backend's resource functions rather than knowing about this package.
func InlineArtifact(store *artifacts.Store, id string) (string, string, error) {
	b, meta, err := store.Bytes(id)
	if err != nil {
		return "", "", err
	}
	if strings.HasPrefix(meta.Mime, "text/") || meta.Mime == "application/json" {
		return string(b), meta.Mime, nil
	}
	return base64.StdEncoding.EncodeToString(b), meta.Mime, nil
}
