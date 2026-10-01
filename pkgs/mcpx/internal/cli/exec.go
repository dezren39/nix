package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/dezren39/mcpx/internal/defaults"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dezren39/mcpx/internal/artifacts"
	"github.com/dezren39/mcpx/internal/execsvc"
)

// execOptions is what the exec and run commands resolved from their flags,
// their environment and their configuration.
type execOptions struct {
	// Output is text, structured or stream.
	Output string
	// Where is auto, local or remote.
	Where string
	// ArtifactsDir receives this run's artifacts.
	ArtifactsDir string
	// Delivery is reference, inline or stream.
	Delivery string
}

func (a *App) resolveExecOptions() execOptions {
	set := a.Settings()
	return execOptions{
		Output:       set.String("exec.output"),
		Where:        set.String("exec.where"),
		ArtifactsDir: set.String("artifacts.dir"),
		Delivery:     set.String("artifacts.delivery"),
	}
}

// runsRemotely decides where a script runs.
//
// The rule is that a person at a terminal gets a local run and everybody else
// gets whatever works. A local run owns the terminal, reads the real standard
// input and resolves relative paths against the directory the command was
// typed in; none of those are true of a daemon on another machine, so a
// remote run has to be asked for or implied by the daemon being remote.
func (a *App) runsRemotely(c *Client, where string) (bool, error) {
	switch where {
	case "remote":
		return true, nil
	case "local":
		if c.Remote() {
			// Refused rather than attempted. Running locally against a remote
			// daemon does work -- the script calls back over the network --
			// but a file the script writes lands here while the servers are
			// there, and the artifact story silently becomes a different one.
			// Saying so beats a subtly different result.
			return false, errors.New("--local with a remote daemon.endpoint: the script " +
				"would run here while the servers run there. Drop --local, or point " +
				"daemon.endpoint at a local daemon")
		}
		return false, nil
	default:
		return c.Remote(), nil
	}
}

// artifactOptions turns the resolved flags into the wire form.
func (e execOptions) artifactOptions(cwd string) *execsvc.ArtifactOptions {
	if e.ArtifactsDir == "" && e.Delivery == "" {
		return nil
	}
	out := &execsvc.ArtifactOptions{Delivery: e.Delivery}
	if e.ArtifactsDir != "" {
		out.Dir = execsvc.ArtifactsDirFor(cwd, e.ArtifactsDir)
	}
	return out
}

// localStore opens the daemon's artifact store from this side.
//
// The same directory, not a copy. When the daemon is on this machine the
// store is a shared SQLite index in WAL mode and a tree of blobs, so the CLI
// reading it sees exactly what the daemon wrote -- and can hardlink a blob
// into an output directory rather than fetching its own copy of bytes that
// are already here.
func (a *App) localStore() (*artifacts.Store, error) {
	set := a.Settings()
	if !set.Bool("artifacts.enabled") {
		return nil, errors.New("artifacts are disabled (artifacts.enabled)")
	}
	paths, _ := a.resolvePathsForConfig()
	return artifacts.Open(artifacts.Options{
		Dir:      filepath.Join(paths.State, "artifacts"),
		TTL:      set.Duration("artifacts.ttl"),
		MaxBytes: set.Bytes("artifacts.maxBytes"),
		Quota:    set.Bytes("artifacts.quota"),
	})
}

// localService builds the service the CLI's own runs go through.
//
// Src is nil: the CLI assembles its own runner options, because `mcpx run`
// has launchers, phases and a typecheck mode that the wire Options
// deliberately do not mirror. Everything after the process starts is the
// shared implementation.
func (a *App) localService(store *artifacts.Store, socket string) *execsvc.Service {
	set := a.Settings()
	lim := execsvc.DefaultLimits()
	lim.Delivery = set.String("artifacts.delivery")
	lim.InlineMaxBytes = set.Bytes("artifacts.inlineMaxBytes")
	lim.ChunkBytes = set.Bytes("artifacts.chunkBytes")
	lim.InterceptImages = set.Bool("artifacts.interceptImages")
	return &execsvc.Service{
		Store: store,
		// The script runs in this process's machine, and the daemon whose
		// store it is has to be the same one for a path to mean anything to
		// both. A socket is the evidence for that: a remote endpoint has
		// none, and then artifact({path}) sends bytes instead.
		Local:  socket != "",
		Limits: lim,
	}
}

// ---- the remote path ----

// execRemote runs a script on the daemon and renders the answer.
func (a *App) execRemote(ctx context.Context, c *Client, req map[string]any, eo execOptions) error {
	if eo.Output == execsvc.OutputStream {
		return a.execRemoteStream(ctx, c, req, eo)
	}
	body, err := c.do(ctx, http.MethodPost, "/v1/exec", req)
	if err != nil {
		return err
	}
	var res execsvc.Result
	if err := json.Unmarshal(body, &res); err != nil {
		return err
	}
	return a.renderExecResult(&res, eo)
}

// execRemoteStream consumes the NDJSON framing.
//
// NDJSON rather than SSE for a program: a reader with a JSON decoder should
// not have to strip "data: " off every line first. SSE is the default on the
// endpoint because a browser can consume it with no code at all.
func (a *App) execRemoteStream(ctx context.Context, c *Client, req map[string]any, eo execOptions) error {
	resp, err := c.stream(ctx, http.MethodPost, "/v1/exec", req, "application/x-ndjson")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	// The same ceiling the daemon writes with, rather than a second number
	// that happens to be larger today.
	sc.Buffer(make([]byte, 0, int(defaults.HTTPStreamBufferInit)), int(defaults.HTTPStreamBufferMax))
	var res execsvc.Result
	pending := map[string]*os.File{}
	defer func() {
		for _, f := range pending {
			f.Close()
		}
	}()
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var f execsvc.Frame
		if json.Unmarshal([]byte(line), &f) != nil {
			continue
		}
		if err := a.applyFrame(&res, f, eo, pending); err != nil {
			return err
		}
		// Frames are echoed as they arrive when the caller asked for a
		// stream: that is what it asked for.
		fmt.Fprintln(os.Stdout, line)
	}
	return sc.Err()
}

// applyFrame folds one frame into the accumulating result and writes any
// artifact body it carries.
//
// Bodies are written to a temporary name and renamed on the artifact.end
// frame. A stream that is cut halfway must not leave something that looks
// like a complete file, because the next thing to read that directory cannot
// tell the difference.
func (a *App) applyFrame(res *execsvc.Result, f execsvc.Frame, eo execOptions, pending map[string]*os.File) error {
	switch f.Type {
	case execsvc.FrameStart:
		res.RunID = f.RunID
	case execsvc.FrameEmit:
		res.Emits = append(res.Emits, f.Value)
	case execsvc.FrameStdout:
		res.Stdout += f.Text
	case execsvc.FrameArtifact:
		if f.Artifact != nil {
			res.Artifacts = append(res.Artifacts, *f.Artifact)
		}
	case execsvc.FrameError:
		res.Error = f.Error
	case execsvc.FrameEnd:
		if f.ExitCode != nil {
			res.ExitCode = *f.ExitCode
		}
		res.DurationMs = f.DurationMs
	case execsvc.FrameArtifactData:
		if eo.ArtifactsDir == "" {
			return nil
		}
		w, ok := pending[f.ID]
		if !ok {
			if err := os.MkdirAll(eo.ArtifactsDir, 0o700); err != nil {
				return err
			}
			tmp, err := os.CreateTemp(eo.ArtifactsDir, ".incoming-*")
			if err != nil {
				return err
			}
			pending[f.ID] = tmp
			w = tmp
		}
		raw, err := base64.StdEncoding.DecodeString(f.Data)
		if err != nil {
			return err
		}
		_, err = w.Write(raw)
		return err
	case execsvc.FrameArtifactEnd:
		w, ok := pending[f.ID]
		if !ok {
			return nil
		}
		delete(pending, f.ID)
		if err := w.Close(); err != nil {
			return err
		}
		name := "artifact"
		for i := range res.Artifacts {
			if res.Artifacts[i].ID == f.ID {
				name = res.Artifacts[i].Name
				dest := filepath.Join(eo.ArtifactsDir, artifacts.SanitizeName(name))
				if err := os.Rename(w.Name(), dest); err != nil {
					return err
				}
				res.Artifacts[i].Path = dest
				return nil
			}
		}
		return os.Rename(w.Name(), filepath.Join(eo.ArtifactsDir, artifacts.SanitizeName(name)))
	}
	return nil
}

// renderExecResult prints a structured answer the way the caller asked for.
func (a *App) renderExecResult(res *execsvc.Result, eo execOptions) error {
	if err := a.fetchArtifacts(res, eo); err != nil {
		return err
	}
	switch {
	case a.JSON || eo.Output == execsvc.OutputStructured:
		if err := a.out(res); err != nil {
			return err
		}
	default:
		for _, e := range res.Emits {
			fmt.Fprintln(os.Stdout, string(e))
		}
		if res.Stdout != "" {
			fmt.Fprint(os.Stdout, res.Stdout)
		}
		a.reportArtifacts(res)
	}
	if res.Error != "" && res.ExitCode == 0 {
		return errors.New(res.Error)
	}
	if res.ExitCode != 0 {
		if res.Error != "" {
			fmt.Fprintln(os.Stderr, "mcpx:", res.Error)
		}
		os.Exit(res.ExitCode)
	}
	return nil
}

// fetchArtifacts pulls bodies the daemon did not place for us.
//
// Inline bodies are already here and are decoded. Everything else is a GET
// per artifact, which over a unix socket costs about 0.2ms and over a network
// buys range requests and parallelism the single stream cannot.
func (a *App) fetchArtifacts(res *execsvc.Result, eo execOptions) error {
	if eo.ArtifactsDir == "" || len(res.Artifacts) == 0 {
		return nil
	}
	if err := os.MkdirAll(eo.ArtifactsDir, 0o700); err != nil {
		return err
	}
	c, err := a.ensure(context.Background())
	if err != nil {
		return err
	}
	for i := range res.Artifacts {
		art := &res.Artifacts[i]
		if art.Path != "" {
			continue // the daemon placed it; it shares this filesystem
		}
		var body []byte
		if art.Data != "" {
			body, err = base64.StdEncoding.DecodeString(art.Data)
			if err != nil {
				return err
			}
		} else {
			resp, gerr := c.stream(context.Background(), http.MethodGet,
				"/v1/artifacts/"+art.ID, nil, "")
			if gerr != nil {
				return gerr
			}
			body, err = io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				return err
			}
		}
		dest := filepath.Join(eo.ArtifactsDir, artifacts.SanitizeName(art.Name))
		if err := writeNew(dest, body); err != nil {
			return err
		}
		art.Path = dest
	}
	return nil
}

// writeNew writes through a temporary name so a failure leaves nothing that
// looks finished.
func writeNew(dest string, body []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".incoming-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dest)
}

// reportArtifacts tells the person at the terminal what came back.
//
// On stderr, because stdout is the script's answer and a caller piping it
// into something else should not find a file listing in the middle.
func (a *App) reportArtifacts(res *execsvc.Result) {
	for _, art := range res.Artifacts {
		where := art.Path
		if where == "" {
			where = art.URI
		}
		fmt.Fprintf(os.Stderr, "mcpx: artifact %s  %s  %d bytes  %s\n",
			art.Name, art.Mime, art.Size, where)
	}
}

// stream issues a request and hands back the live response.
//
// do() reads the whole body, which is right for every other call and wrong
// for the two that are a stream by design.
func (c *Client) stream(ctx context.Context, method, path string, body any, accept string) (*http.Response, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	base := c.endpoint
	if base == "" {
		base = "http://mcpx"
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	// A stream has no deadline: the whole point is that it lasts as long as
	// the work does. The client's own timeout would cut it off mid-run.
	hc := *c.hc
	hc.Timeout = 0
	resp, err := hc.Do(req)
	if err != nil {
		if isDialErr(err) {
			return nil, ErrNoDaemon
		}
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(b, &e) == nil && e.Error != "" {
			return nil, errors.New(e.Error)
		}
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return resp, nil
}

// execRemoteFlags are the values a remote run carries over from the command
// line. Grouped so the call site does not grow a parameter every time an
// option is added.
type execRemoteFlags struct {
	timeout     time.Duration
	runtime     string
	permissions string
	export      string
	session     string
	shape       scriptShape
}

// execOnDaemon sends the script to the daemon and renders what comes back.
//
// The script text travels, not the path. A path would mean the daemon's
// filesystem, and a caller typing `mcpx run ./report.ts --remote` means the
// file in front of them. The consequence is that relative imports do not
// survive the trip. Every flag that shapes the program does (#192): the shape
// was resolved by the caller and is sent as source.
func (a *App) execOnDaemon(ctx context.Context, c *Client, fs *flag.FlagSet, inline bool,
	ns []string, session string, eo execOptions, rf execRemoteFlags) error {

	sh := rf.shape
	source := sh.inlineSource(fs.Args())
	var args []string
	phases := &execsvc.Phases{Before: sh.before, OnSuccess: sh.onSuccess, OnError: sh.onError}
	if !inline {
		// The file's text travels and the daemon writes it to its own disk,
		// where the launcher imports it -- so --export works as it does
		// locally. Relative imports are what cannot survive the trip.
		path, err := resolveScript(fs.Arg(0))
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		source = string(b)
		args = fs.Args()[1:]
		// A file keeps its own scope, so prefix and suffix run in the
		// launcher around it rather than being spliced into it.
		phases.Prefix, phases.Suffix = sh.prefix, sh.suffix
	}

	cwd, _ := os.Getwd()
	opts := execsvc.Options{
		Runtime:     rf.runtime,
		Permissions: rf.permissions,
		Session:     session,
		// The caller's context, not the daemon's. A remote daemon has a
		// different working directory and a different environment, and a
		// script that reads either should see the one belonging to whoever
		// asked.
		Cwd:       cwd,
		Output:    eo.Output,
		NS:        ns,
		Args:      args,
		Artifacts: eo.artifactOptions(cwd),

		Export:         rf.export,
		Env:            withInputReport(sh.env, a.stdoutOverride == nil),
		TypeCheck:      sh.typecheck,
		Launcher:       sh.launcher,
		LauncherName:   sh.launcherName,
		AllowRepeat:    sh.allowRepeat,
		CaptureConsole: &sh.captureConsole,
	}
	if len(phases.Before)+len(phases.Prefix)+len(phases.OnSuccess)+
		len(phases.OnError)+len(phases.Suffix) > 0 {
		opts.Phases = phases
	}
	if rf.timeout > 0 {
		opts.Timeout = rf.timeout.String()
	}
	if opts.Artifacts != nil {
		opts.Capabilities = []string{execsvc.CapabilityArtifacts}
	}
	// Standard input is forwarded only when something is actually piping it
	// in. Reading a terminal here would block forever waiting for a script
	// that may never ask.
	if !isTerminal(os.Stdin) {
		if b, err := io.ReadAll(os.Stdin); err == nil {
			opts.Stdin = string(b)
		}
	}
	// Text is how the answer is shown here, not what crosses the wire: the
	// daemon's text answer is the script's bare stdout, which execRemote
	// then tried to decode as a Result -- so plain output failed with "invalid
	// character" and JSON-looking output decoded into an empty Result and
	// printed nothing. The structured answer is rendered as text locally.
	if opts.Output != execsvc.OutputStream {
		opts.Output = execsvc.OutputStructured
	}
	return a.execRemote(ctx, c, map[string]any{"source": source, "options": opts}, eo)
}

// Artifacts lists what the daemon holds, optionally for one run or session.
func (c *Client) Artifacts(ctx context.Context, run, session string) ([]artifacts.Meta, error) {
	q := url.Values{}
	if run != "" {
		q.Set("run", run)
	}
	if session != "" {
		q.Set("session", session)
	}
	path := "/v1/artifacts"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	b, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Artifacts []artifacts.Meta `json:"artifacts"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out.Artifacts, nil
}

// ArtifactBody fetches one artifact as a string and its media type.
//
// Text comes back as itself; anything else is base64, because the one
// transport this feeds -- MCP resources/read -- carries text. A caller that
// wants bytes should GET /v1/artifacts/{id}, which is why that route exists
// with Range support.
func (c *Client) ArtifactBody(ctx context.Context, id string) (string, string, error) {
	resp, err := c.stream(ctx, http.MethodGet, "/v1/artifacts/"+id, nil, "")
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}
	mime := resp.Header.Get("Content-Type")
	if strings.HasPrefix(mime, "text/") || strings.HasPrefix(mime, "application/json") {
		return string(body), mime, nil
	}
	return base64.StdEncoding.EncodeToString(body), mime, nil
}

// withInputReport adds MCPX_INPUT=report to a remote run's environment: the
// caller is a terminal command with nobody to answer a question mid-call, so
// the script should exit ExitInputRequired rather than wait (#286).
func withInputReport(env map[string]string, report bool) map[string]string {
	out := make(map[string]string, len(env)+1)
	for k, v := range env {
		out[k] = v
	}
	if report {
		out["MCPX_INPUT"] = "report"
	}
	return out
}
