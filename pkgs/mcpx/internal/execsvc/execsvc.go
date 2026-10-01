// Package execsvc runs a script and reports what it produced.
//
// It exists because there were about to be two implementations of "run a
// script": the one `mcpx exec` has always had, and a second one inside the
// daemon for POST /v1/exec. Two would mean two sets of bugs and a script that
// behaves differently depending on who asked, which is precisely what the
// artifacts design is trying to avoid. So the invocation lives here and both
// callers go through it.
//
// The shape of the output is the other reason. `mcpx exec` writes to a
// terminal: stdout is the answer, stderr is diagnostics, and that is right
// for a person. A caller over HTTP wants one document, and a caller watching
// a long run wants frames as they happen. Those are three renderings of the
// same run, so the run produces frames and the caller decides what to do with
// them.
package execsvc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dezren39/mcpx/internal/artifacts"
	"github.com/dezren39/mcpx/internal/defaults"
	"github.com/dezren39/mcpx/internal/logging"
	"github.com/dezren39/mcpx/internal/runner"
)

// Delivery says how artifact bodies reach the caller.
const (
	// DeliveryReference hands back an id and a URI. The caller fetches what
	// it wants, when it wants it, with Range support and parallelism for
	// free. This is the default because it is the only mode whose cost does
	// not scale with what the script happened to produce.
	DeliveryReference = "reference"
	// DeliveryInline base64s the body into the structured result, bounded by
	// artifacts.inlineMaxBytes. For a caller that cannot make a second
	// request.
	DeliveryInline = "inline"
	// DeliveryStream sends bodies as frames after the end frame, so a
	// consumer can read the result and disconnect before paying for them.
	DeliveryStream = "stream"
)

// Output modes.
const (
	// OutputText is what a terminal wants: stdout is the answer.
	OutputText = "text"
	// OutputStructured is one JSON document.
	OutputStructured = "structured"
	// OutputStream is frames as they happen.
	OutputStream = "stream"
)

// CapabilityArtifacts is what a caller declares to say it can receive
// artifacts at all.
//
// Declared rather than inferred, because remoteness cannot be read off the
// transport: a unix socket can be ssh-forwarded and loopback TCP can be a
// container with its own filesystem. Neither tells you whether the caller can
// see the daemon's disk.
const CapabilityArtifacts = "artifacts"

// ArtifactOptions are the caller's terms for receiving artifacts.
type ArtifactOptions struct {
	Delivery string `json:"delivery,omitempty"`
	// MaxBytes is the per-artifact ceiling the caller accepts for inline or
	// streamed delivery. Anything larger stays a reference.
	MaxBytes int64 `json:"maxBytes,omitempty"`
	// SharedFs is a directory the caller can read. Artifacts are placed
	// there -- hardlinked, not copied -- instead of being transferred.
	SharedFs string `json:"sharedFs,omitempty"`
	// Dir is where the CLI writes artifacts for the person who ran it. It is
	// SharedFs by another name for a local caller, kept separate because one
	// is "I can see your disk" and the other is "put them here for me".
	Dir string `json:"dir,omitempty"`
}

// TaskOptions ask for a handle now and the result later.
type TaskOptions struct {
	// TTL is milliseconds the result is kept, as MCP spells it.
	TTL int64 `json:"ttl,omitempty"`
}

// Options is the ExecOptions of the design, on the wire.
//
// Deliberately one flat struct that grows rather than a new endpoint per
// capability: the issue's step 7 is that exec gains options, because the
// caller declaring what it can receive is the thing that makes the script
// identical everywhere.
type Options struct {
	Runtime      string            `json:"runtime,omitempty"`
	Timeout      string            `json:"timeout,omitempty"`
	Permissions  string            `json:"permissions,omitempty"`
	Placeholders map[string]any    `json:"placeholders,omitempty"`
	Session      string            `json:"session,omitempty"`
	Cwd          string            `json:"cwd,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	Stdin        string            `json:"stdin,omitempty"`
	Output       string            `json:"output,omitempty"`
	Capabilities []string          `json:"capabilities,omitempty"`
	Artifacts    *ArtifactOptions  `json:"artifacts,omitempty"`
	Task         *TaskOptions      `json:"task,omitempty"`

	// NS restricts the generated client to these namespaces, and Args are
	// handed to the script. Neither is in the design's list, and both are
	// here because `mcpx exec --ns` and `mcpx run script a b` already exist:
	// an option the CLI has and /v1 does not is a parity hole, which is the
	// one thing this repository checks for automatically.
	NS   []string `json:"ns,omitempty"`
	Args []string `json:"args,omitempty"`
	// Export calls a named export instead of the default one.
	Export string `json:"export,omitempty"`

	// What follows shapes the program the way the CLI's flags do locally.
	// Before #192 a remote `mcpx exec` parsed every one of these and sent
	// none, so the daemon ran a different program from the one asked for.
	// Phase lines and the launcher arrive as source: a path would name the
	// daemon's filesystem, and the caller meant theirs.

	// TypeCheck is off, on or strict; empty takes the daemon's default.
	TypeCheck string `json:"typecheck,omitempty"`
	// Launcher replaces the generated launcher with this template text;
	// "none" runs the script with no launcher at all.
	Launcher     string `json:"launcher,omitempty"`
	LauncherName string `json:"launcherName,omitempty"`
	// AllowRepeat names launcher placeholders permitted to resolve twice.
	AllowRepeat []string `json:"allowRepeat,omitempty"`
	// CaptureConsole false leaves console.* alone; absent takes the default.
	CaptureConsole *bool `json:"captureConsole,omitempty"`
	// Phases are lines run around the script, as runner.Phases.
	Phases *Phases `json:"phases,omitempty"`
}

// Phases is runner.Phases on the wire.
type Phases struct {
	Before    []string `json:"before,omitempty"`
	Prefix    []string `json:"prefix,omitempty"`
	OnSuccess []string `json:"onSuccess,omitempty"`
	OnError   []string `json:"onError,omitempty"`
	Suffix    []string `json:"suffix,omitempty"`
}

// WantsArtifacts reports whether the caller declared it can receive them.
func (o Options) WantsArtifacts() bool {
	for _, c := range o.Capabilities {
		if c == CapabilityArtifacts {
			return true
		}
	}
	// Naming a delivery mode or a directory is declaring the capability;
	// making somebody write both would be a trap with no upside.
	return o.Artifacts != nil
}

// Frame is one event in an exec stream.
//
// The order is fixed and stated in docs/exec.md: start, then log, emit and
// stdout interleaved as they happen, then artifact metadata, then result or
// error, then end. Bodies, when they are streamed at all, come strictly
// after end so that a consumer can read the answer and disconnect before
// paying for them.
type Frame struct {
	Type string `json:"type"`

	RunID   string          `json:"runId,omitempty"`
	Level   string          `json:"level,omitempty"`
	Message string          `json:"message,omitempty"`
	Attrs   map[string]any  `json:"attrs,omitempty"`
	Value   json.RawMessage `json:"value,omitempty"`
	Text    string          `json:"text,omitempty"`

	Artifact *Artifact `json:"artifact,omitempty"`

	// ID, Seq and Data carry a body chunk under delivery "stream".
	ID   string `json:"id,omitempty"`
	Seq  int    `json:"seq,omitempty"`
	Data string `json:"data,omitempty"`

	Error string `json:"error,omitempty"`

	ExitCode   *int  `json:"exitCode,omitempty"`
	DurationMs int64 `json:"durationMs,omitempty"`
}

// Frame types.
const (
	FrameStart        = "start"
	FrameLog          = "log"
	FrameEmit         = "emit"
	FrameStdout       = "stdout"
	FrameArtifact     = "artifact"
	FrameResult       = "result"
	FrameError        = "error"
	FrameEnd          = "end"
	FrameArtifactData = "artifact.chunk"
	FrameArtifactEnd  = "artifact.end"
)

// Artifact is one artifact as a caller receives it: always the metadata,
// plus whatever the declared delivery adds.
type Artifact struct {
	artifacts.Meta
	// Data is the base64 body under inline delivery.
	Data string `json:"data,omitempty"`
	// Path is where it was placed under sharedFs or --artifacts-dir.
	Path string `json:"path,omitempty"`
}

// LogRecord is one of the script's structured records, in the result.
type LogRecord struct {
	Time    string         `json:"ts"`
	Level   string         `json:"level"`
	Message string         `json:"msg"`
	Attrs   map[string]any `json:"attrs,omitempty"`
}

// Result is the structured answer.
type Result struct {
	RunID      string            `json:"runId"`
	Result     any               `json:"result,omitempty"`
	Emits      []json.RawMessage `json:"emits"`
	Logs       []LogRecord       `json:"logs"`
	Stdout     string            `json:"stdout"`
	Artifacts  []Artifact        `json:"artifacts"`
	ExitCode   int               `json:"exitCode"`
	DurationMs int64             `json:"durationMs"`
	Runtime    string            `json:"runtime,omitempty"`
	TimedOut   bool              `json:"timedOut,omitempty"`
	Error      string            `json:"error,omitempty"`
}

// Source supplies the generated client the script is run against.
//
// An interface because the two callers reach it differently: the daemon
// generates it from the registry it already holds, and the CLI asks the
// daemon for it over the socket. Neither detail belongs here.
type Source interface {
	// ClientModule is the generated TypeScript client.
	ClientModule(ctx context.Context, ns []string) (string, error)
	// Globals is the ambient declaration file. Editor convenience; an error
	// is not fatal.
	Globals(ctx context.Context, ns []string) (string, error)
	// Namespaces are the names to bind as bare identifiers in a snippet.
	Namespaces(ctx context.Context, ns []string) ([]string, error)
}

// Limits are the resolved settings one service runs under.
type Limits struct {
	Timeout     time.Duration
	Runtime     string
	Permissions string
	// Setup holds declared runtimes, the auto order and user profiles.
	Setup runner.Setup
	// SetupErr is why Setup could not be read; every run reports it.
	SetupErr        error
	CaptureConsole  bool
	Typecheck       string
	Delivery        string
	InlineMaxBytes  int64
	ChunkBytes      int64
	InterceptImages bool
}

// DefaultLimits are the built-in values, before any configuration.
func DefaultLimits() Limits {
	return Limits{
		Timeout:         defaults.ExecTimeout,
		Runtime:         "auto",
		Permissions:     defaults.Permissions,
		CaptureConsole:  defaults.CaptureConsole,
		Delivery:        defaults.ExecDelivery,
		InlineMaxBytes:  defaults.ArtifactInlineMaxBytes,
		ChunkBytes:      defaults.ArtifactChunkBytes,
		InterceptImages: defaults.ArtifactInterceptImages,
	}
}

// Service runs scripts.
type Service struct {
	// Src generates the client module.
	Src Source
	// Store is the artifact outbox. Nil disables artifacts entirely, which
	// is what a daemon whose state directory is unusable falls back to.
	Store *artifacts.Store
	// Endpoint and Socket are how the script reaches the daemon. Socket is
	// empty when the script will not be on the daemon's machine.
	Endpoint string
	Socket   string
	// Local reports whether the script's filesystem is the daemon's. It
	// decides whether artifact({path}) can be registered by link rather than
	// by transfer, and it is a fact only the caller knows.
	Local bool
	// Limits are the resolved settings.
	Limits Limits
	// WorkDir holds the generated client between runs, so the runtime's
	// module cache stays warm. Empty means a temporary directory per run.
	WorkDir string
	// WorkRoot keeps one working directory per distinct client, so two
	// configurations cannot overwrite each other's generated code.
	WorkRoot string
	// Release frees the pooled instances a finished run created.
	Release func(ctx context.Context, session string) error
	// Logs, when set, receives every record the script produced, for the
	// durable log.
	Logs func(logging.Record)
}

// Request is one execution.
type Request struct {
	// Source is inline TypeScript; File is a path. Exactly one.
	Source string
	File   string
	Opts   Options
	// RunID, when set, is used instead of a fresh one: the daemon registers
	// a script's run for question correlation before the script starts.
	RunID string
}

// Sink receives frames in order. Returning an error stops the run, which is
// how a disconnected HTTP client kills the script it was watching.
type Sink func(Frame) error

// Run executes the script, building the runner options itself.
//
// This is the /v1 and MCP entry point: everything the caller can ask for
// arrives as Options and nothing is inherited from a terminal that is not
// there.
func (s *Service) Run(ctx context.Context, req Request, sink Sink) (*Result, error) {
	if req.Source == "" && req.File == "" {
		return nil, errors.New("exec: need source or a file")
	}
	if s.Limits.SetupErr != nil {
		return nil, s.Limits.SetupErr
	}
	opts := req.Opts

	timeout := s.Limits.Timeout
	if opts.Timeout != "" {
		d, err := time.ParseDuration(opts.Timeout)
		if err != nil {
			return nil, fmt.Errorf("exec: timeout %q: %w", opts.Timeout, err)
		}
		timeout = d
	}

	clientSrc, err := s.Src.ClientModule(ctx, opts.NS)
	if err != nil {
		return nil, err
	}
	globals, _ := s.Src.Globals(ctx, opts.NS)

	capture := s.Limits.CaptureConsole
	if opts.CaptureConsole != nil {
		capture = *opts.CaptureConsole
	}
	ropts := runner.Options{
		Source:         req.Source,
		File:           req.File,
		ClientSource:   clientSrc,
		GlobalsSource:  globals,
		WorkDir:        s.WorkDir,
		WorkRoot:       s.WorkRoot,
		Runtime:        firstNonEmpty(opts.Runtime, s.Limits.Runtime),
		Setup:          s.Limits.Setup,
		Timeout:        timeout,
		Permissions:    firstNonEmpty(opts.Permissions, s.Limits.Permissions),
		CaptureConsole: capture,
		TypeCheck:      firstNonEmpty(opts.TypeCheck, s.Limits.Typecheck),
		Launcher:       opts.Launcher,
		LauncherName:   opts.LauncherName,
		AllowRepeat:    opts.AllowRepeat,
		Export:         opts.Export,
		Args:           opts.Args,
		Dir:            opts.Cwd,
		// Explicitly not the daemon's standard input. A script run on
		// somebody else's behalf reading the daemon's stdin would be reading
		// whatever started the daemon.
		Stdin: strings.NewReader(opts.Stdin),
	}
	if req.RunID != "" {
		ropts.Env = map[string]string{"MCPX_RUN": req.RunID}
	}
	if ph := opts.Phases; ph != nil {
		ropts.Phases = runner.Phases{Before: ph.Before, Prefix: ph.Prefix,
			OnSuccess: ph.OnSuccess, OnError: ph.OnError, Suffix: ph.Suffix}
	}
	if len(opts.Placeholders) > 0 {
		ropts.Placeholders = stringify(opts.Placeholders)
	}
	if req.Source != "" {
		prelude, perr := s.prelude(ctx, opts.NS, capture)
		if perr != nil {
			return nil, perr
		}
		ropts.Prelude = prelude
	}
	return s.RunWith(ctx, ropts, opts, sink)
}

// RunWith executes a script whose runner options the caller assembled.
//
// The CLI assembles its own, because `mcpx run` genuinely has more knobs than
// /v1/exec does -- launcher templates, phases, --keep, a typecheck mode -- and
// flattening all of that into the wire Options would make the wire type a
// mirror of one command's flag list. What must not be duplicated is the part
// after the process starts: which frames are produced, how artifacts are
// reconciled, when media is intercepted and how delivery is applied. That is
// all here, and it is the same code for both callers.
//
// Hooks the caller already set are chained rather than replaced, so the CLI
// keeps rendering logs to a terminal while the service collects the same
// records for the result.
func (s *Service) RunWith(ctx context.Context, ropts runner.Options, opts Options, sink Sink) (*Result, error) {
	if sink == nil {
		sink = func(Frame) error { return nil }
	}
	if ropts.Env == nil {
		ropts.Env = map[string]string{}
	}
	runID := ropts.Env["MCPX_RUN"]
	if runID == "" {
		runID = NewRunID()
	}
	session := firstNonEmpty(opts.Session, ropts.Env["MCPX_SESSION"], "exec-"+runID)
	for k, v := range s.scriptEnv(runID, session, opts) {
		if _, taken := ropts.Env[k]; !taken {
			ropts.Env[k] = v
		}
	}

	res := &Result{RunID: runID, Emits: []json.RawMessage{}, Logs: []LogRecord{},
		Artifacts: []Artifact{}}

	if err := sink(Frame{Type: FrameStart, RunID: runID}); err != nil {
		return nil, err
	}

	// An error from the sink means the consumer has gone. It is recorded
	// rather than acted on immediately so that the run is torn down through
	// one path -- cancelling the context -- rather than by unwinding from
	// wherever the write happened to fail.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	//
	// Serialised: the runner calls the stdout writer and the stderr record
	// hooks from separate goroutines, and an HTTP sink writing two frames at
	// once interleaves them on the wire (#270).
	var sinkErr error
	var sinkMu sync.Mutex
	emitFrame := func(f Frame) {
		sinkMu.Lock()
		defer sinkMu.Unlock()
		if sinkErr != nil {
			return
		}
		if err := sink(f); err != nil {
			sinkErr = err
			cancel()
		}
	}

	var stdout strings.Builder
	priorStdout := ropts.Stdout
	ropts.Stdout = writerFunc(func(p []byte) (int, error) {
		stdout.Write(p)
		emitFrame(Frame{Type: FrameStdout, Text: string(p)})
		if priorStdout != nil {
			return priorStdout.Write(p)
		}
		return len(p), nil
	})
	var stderr *tailBuffer
	if ropts.Log == nil {
		// Records arrive on the script's stderr behind a sentinel; without a
		// writer the runner forwards stderr verbatim and they are never
		// parsed. Discarding the rendering is right for a caller that wants
		// the records as data.
		ropts.Log = logging.NewWriter(io.Discard, logging.FormatText, slog.LevelDebug)
		if ropts.Stderr == nil {
			// Kept, not discarded. A script that runs and exits non-zero is
			// not an error to the runner, so nothing else fills in Error:
			// discarding stderr left a caller with exitCode 1 and no way at
			// all to find out why -- which is most of what /v1/exec exists
			// to tell an agent.
			stderr = &tailBuffer{limit: defaults.ExecStderrLimit}
			ropts.Stderr = stderr
		}
	}

	priorLogs := ropts.CollectLogs
	ropts.CollectLogs = func(r logging.Record) {
		rec := LogRecord{
			Time:    r.Time.Format(time.RFC3339Nano),
			Level:   logging.LevelName(r.Level),
			Message: r.Msg,
			Attrs:   r.Attrs,
		}
		res.Logs = append(res.Logs, rec)
		if s.Logs != nil {
			s.Logs(r)
		}
		emitFrame(Frame{Type: FrameLog, Level: rec.Level, Message: rec.Message, Attrs: rec.Attrs})
		if priorLogs != nil {
			priorLogs(r)
		}
	}
	priorResult := ropts.OnResult
	ropts.OnResult = func(v logging.Streamed) {
		res.Emits = append(res.Emits, v.Value)
		emitFrame(Frame{Type: FrameEmit, Value: v.Value})
		if priorResult != nil {
			priorResult(v)
		}
	}
	priorArtifact := ropts.OnArtifact
	ropts.OnArtifact = func(a logging.Artifacted) {
		// Re-read from the store so the frame carries what the daemon
		// recorded rather than what the script said, which is the difference
		// between a fact and a claim.
		art := s.lookup(a.ID, a.Name)
		res.Artifacts = append(res.Artifacts, art)
		frameArt := art
		emitFrame(Frame{Type: FrameArtifact, Artifact: &frameArt})
		if priorArtifact != nil {
			priorArtifact(a)
		}
	}

	rres, rerr := runner.Run(runCtx, ropts)
	if s.Release != nil && session != "" {
		relCtx, relCancel := context.WithTimeout(context.WithoutCancel(ctx), defaults.ShutdownGrace)
		_ = s.Release(relCtx, session)
		relCancel()
	}
	if sinkErr != nil {
		// The consumer left. Nothing more can be delivered, and the script
		// has already been killed by the cancelled context.
		return res, sinkErr
	}
	if rres == nil {
		// The script never started: a missing runtime, an unreadable
		// launcher, a type error found before execution. The result carries
		// it for a caller reading one document, and the error is returned as
		// well for a caller that expects a failed command to fail.
		msg := "the script did not start"
		if rerr != nil {
			msg = rerr.Error()
		}
		res.Error = msg
		res.ExitCode = 1
		emitFrame(Frame{Type: FrameError, Error: msg})
		code := res.ExitCode
		emitFrame(Frame{Type: FrameEnd, ExitCode: &code})
		if rerr == nil {
			rerr = errors.New(msg)
		}
		return res, rerr
	}
	res.Stdout = stdout.String()
	res.ExitCode = rres.ExitCode
	res.DurationMs = rres.Duration.Milliseconds()
	res.Runtime = rres.Runtime
	res.TimedOut = rres.TimedOut
	if rerr != nil {
		res.Error = rerr.Error()
	}
	// A non-zero exit is not an error to the runner, so if nothing above
	// explained it, the script's own message is the explanation.
	if res.Error == "" && res.ExitCode != 0 && stderr != nil {
		if msg := strings.TrimSpace(stripANSI(stderr.String())); msg != "" {
			res.Error = msg
			emitFrame(Frame{Type: FrameError, Error: msg})
		}
	}

	// A script whose stdout is JSON almost always means it as its answer, so
	// it is offered parsed as well as raw. The same rule `mcpx --json run`
	// has always followed, kept identical so the two agree.
	if trimmed := strings.TrimSpace(res.Stdout); trimmed != "" {
		var parsed any
		if json.Unmarshal([]byte(trimmed), &parsed) == nil {
			res.Result = parsed
		}
	}

	// Anything the script registered that did not reach the stderr channel
	// -- a crash after the upload, a runtime that buffered the last line --
	// is still in the index. Reconciled here so the result is the store's
	// account rather than the script's.
	s.reconcile(res, runID)

	if s.Limits.InterceptImages && opts.WantsArtifacts() {
		s.interceptMedia(res, runID, session)
	}

	if err := s.deliver(res, opts); err != nil {
		res.Error = strings.TrimSpace(res.Error + "\n" + err.Error())
	}

	if res.Error != "" {
		emitFrame(Frame{Type: FrameError, Error: res.Error})
	} else if res.Result != nil {
		b, merr := json.Marshal(res.Result)
		if merr == nil {
			emitFrame(Frame{Type: FrameResult, Value: b})
		}
	}
	code := res.ExitCode
	emitFrame(Frame{Type: FrameEnd, ExitCode: &code, DurationMs: res.DurationMs})

	// Bodies last, and only when asked for. This is the whole point of the
	// ordering: a consumer that has read the result may disconnect here and
	// pay nothing for artifacts it does not want.
	if deliveryOf(opts, s.Limits) == DeliveryStream {
		s.streamBodies(res, opts, emitFrame)
	}
	return res, sinkErr
}

// scriptEnv is everything the script and the generated client read out of the
// environment.
func (s *Service) scriptEnv(runID, session string, opts Options) map[string]string {
	env := map[string]string{
		"MCPX_SESSION":  session,
		"MCPX_ENDPOINT": s.Endpoint,
		"MCPX_SOCKET":   s.Socket,
		"MCPX_RUN":      runID,
		// The script does not ask where it is running; artifact() does, and
		// only to choose between handing over a path and handing over bytes.
		// Both produce the same artifact.
		"MCPX_ARTIFACTS_LOCAL": boolEnv(s.Local),
		"MCPX_SESSION_ID":      session,
		"MCPX_PID":             strconv.Itoa(os.Getpid()),
	}
	if opts.Cwd != "" {
		env["MCPX_CWD"] = opts.Cwd
	}
	for k, v := range opts.Env {
		env[k] = v
	}
	return env
}

func (s *Service) prelude(ctx context.Context, ns []string, capture bool) (string, error) {
	names, err := s.Src.Namespaces(ctx, ns)
	if err != nil {
		return "", err
	}
	return Prelude(names, capture), nil
}

// Prelude imports the client and binds every namespace as a bare identifier.
//
// Exported because the CLI builds the same thing for its local path, and two
// preludes would mean a snippet that compiles in one place and not the other.
func Prelude(names []string, captureConsole bool) string {
	var b strings.Builder
	b.WriteString("// --- mcpx prelude (generated) ---\n")
	fmt.Fprintf(&b, "import tools, { call, readResource, artifact, log, emit, ToolError, "+
		"installGlobals, captureConsole } from %q;\n", "./"+runner.ClientFileName)
	b.WriteString("installGlobals();\n")
	if captureConsole {
		b.WriteString("captureConsole();\n")
	}
	if len(names) > 0 {
		fmt.Fprintf(&b, "const { %s } = tools;\n", strings.Join(names, ", "))
	}
	b.WriteString("void [tools, call, readResource, artifact, log, emit, ToolError")
	for _, n := range names {
		b.WriteString(", " + n)
	}
	b.WriteString("];\n// --- end prelude ---\n\n")
	return b.String()
}

// lookup reads back what the store recorded for an artifact the script
// announced.
func (s *Service) lookup(id, name string) Artifact {
	if s.Store != nil && id != "" {
		if m, err := s.Store.Get(id); err == nil {
			return Artifact{Meta: m}
		}
	}
	// The store lost it, or there is no store. A name and an id is still
	// more useful than silence: it says what the script thought it made.
	return Artifact{Meta: artifacts.Meta{ID: id, Name: name, URI: artifacts.URI(id)}}
}

// reconcile adds anything in the index for this run that the stream missed.
func (s *Service) reconcile(res *Result, runID string) {
	if s.Store == nil {
		return
	}
	known := map[string]bool{}
	for _, a := range res.Artifacts {
		known[a.ID] = true
	}
	list, err := s.Store.List(artifacts.Filter{Run: runID, Limit: defaults.ArtifactListLimit})
	if err != nil {
		return
	}
	// Oldest first, so the result lists them in the order the script made
	// them rather than in the index's newest-first order.
	for i := len(list) - 1; i >= 0; i-- {
		if !known[list[i].ID] {
			res.Artifacts = append(res.Artifacts, Artifact{Meta: list[i]})
		}
	}
}

// deliver applies the caller's terms to every artifact in the result.
func (s *Service) deliver(res *Result, opts Options) error {
	if len(res.Artifacts) == 0 || s.Store == nil {
		return nil
	}
	if !opts.WantsArtifacts() {
		// The caller never said it could receive them. The metadata stays --
		// it is small, and knowing a file exists is the difference between
		// fetching it later and never knowing it was there.
		return nil
	}
	mode := deliveryOf(opts, s.Limits)
	dir := ""
	if opts.Artifacts != nil {
		dir = firstNonEmpty(opts.Artifacts.Dir, opts.Artifacts.SharedFs)
	}
	var errs []string
	for i := range res.Artifacts {
		a := &res.Artifacts[i]
		if dir != "" {
			// A shared filesystem beats every transfer mode: the caller can
			// already read the bytes, so hardlinking them into its directory
			// costs an inode and no I/O.
			p, err := s.Store.Export(a.Meta, dir)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", a.Name, err))
				continue
			}
			a.Path = p
			continue
		}
		if mode == DeliveryInline && s.withinInline(a.Meta, opts) {
			b, _, err := s.Store.Bytes(a.ID)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", a.Name, err))
				continue
			}
			a.Data = base64.StdEncoding.EncodeToString(b)
		}
	}
	if len(errs) > 0 {
		return errors.New("artifact delivery: " + strings.Join(errs, "; "))
	}
	return nil
}

// withinInline reports whether an artifact is small enough to be base64'd
// into the result, honouring both the caller's ceiling and the store's.
func (s *Service) withinInline(m artifacts.Meta, opts Options) bool {
	limit := s.Limits.InlineMaxBytes
	if opts.Artifacts != nil && opts.Artifacts.MaxBytes > 0 && opts.Artifacts.MaxBytes < limit {
		limit = opts.Artifacts.MaxBytes
	}
	return limit <= 0 || m.Size <= limit
}

// streamBodies sends artifact bodies after the end frame.
func (s *Service) streamBodies(res *Result, opts Options, emit func(Frame)) {
	if s.Store == nil {
		return
	}
	chunk := s.Limits.ChunkBytes
	if chunk <= 0 {
		chunk = defaults.ArtifactChunkBytes
	}
	for _, a := range res.Artifacts {
		if a.Path != "" {
			continue // already on a disk the caller can read
		}
		if opts.Artifacts != nil && opts.Artifacts.MaxBytes > 0 && a.Size > opts.Artifacts.MaxBytes {
			continue // larger than the caller agreed to receive
		}
		f, _, err := s.Store.OpenBody(a.ID)
		if err != nil {
			emit(Frame{Type: FrameError, ID: a.ID, Error: err.Error()})
			continue
		}
		buf := make([]byte, chunk)
		seq := 0
		for {
			n, rerr := f.Read(buf)
			if n > 0 {
				emit(Frame{Type: FrameArtifactData, ID: a.ID, Seq: seq,
					Data: base64.StdEncoding.EncodeToString(buf[:n])})
				seq++
			}
			if rerr != nil {
				break
			}
		}
		f.Close()
		emit(Frame{Type: FrameArtifactEnd, ID: a.ID})
	}
}

func deliveryOf(opts Options, lim Limits) string {
	if opts.Artifacts != nil && opts.Artifacts.Delivery != "" {
		return opts.Artifacts.Delivery
	}
	if lim.Delivery != "" {
		return lim.Delivery
	}
	return DeliveryReference
}

// NewRunID identifies one execution, for the artifact index and the frames.
func NewRunID() string {
	return strings.TrimPrefix(artifacts.NewID(), "art-")[:16]
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func boolEnv(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// stringify renders caller-supplied placeholder values as the strings a
// launcher template substitutes.
//
// A string is used verbatim; anything else is JSON. A caller passing an
// object means it to appear as an object literal in the generated program,
// and JSON is a subset of what a TypeScript source position accepts.
func stringify(in map[string]any) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		if s, ok := v.(string); ok {
			out[k] = s
			continue
		}
		b, err := json.Marshal(v)
		if err != nil {
			continue
		}
		out[k] = string(b)
	}
	return out
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// ArtifactsDirFor is where the CLI writes artifacts when asked for a
// directory relative to a working directory.
func ArtifactsDirFor(cwd, dir string) string {
	if dir == "" || filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join(cwd, dir)
}

// tailBuffer keeps the last limit bytes written to it.
//
// The tail rather than the head: a stack trace ends with the line that
// matters, and a script that floods stderr floods it from the start.
type tailBuffer struct {
	limit int
	buf   []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if t.limit > 0 && len(p) > t.limit {
		p = p[len(p)-t.limit:]
	}
	t.buf = append(t.buf, p...)
	if t.limit > 0 && len(t.buf) > t.limit {
		t.buf = t.buf[len(t.buf)-t.limit:]
	}
	return n, nil
}

func (t *tailBuffer) String() string {
	if t == nil {
		return ""
	}
	return string(t.buf)
}

// ansi matches the escape sequences a runtime colours its errors with.
//
// Deno writes them whenever it thinks it has a terminal, and this reaches a
// caller as JSON, where they are noise an agent has to read past.
var ansi = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string { return ansi.ReplaceAllString(s, "") }
