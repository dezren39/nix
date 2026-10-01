// Package runner executes agent-authored TypeScript against the generated
// mcpx client.
//
// The design choice that matters here: the client module is written to a real
// file on disk and imported with a relative path. lootbox imported its client
// over HTTP with `deno run --reload`, which forced a re-download and a full
// type check of the module graph on every single execution and cost ~10s per
// script. A local file lets the runtime's own module cache do its job, so the
// same work takes tens of milliseconds.
package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"

	"github.com/dezren39/mcpx/internal/defaults"
	"github.com/dezren39/mcpx/internal/launcher"
	"github.com/dezren39/mcpx/internal/logging"
	"github.com/dezren39/mcpx/internal/preflight"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Options configure one script execution.
type Options struct {
	// Source is the TypeScript to run. Exactly one of Source or File.
	Source string
	// File is a path to a script. Its directory is used as the working dir.
	File string
	// ClientSource is the generated client module.
	ClientSource string
	// WorkDir holds the generated client; defaults to a per-session temp dir.
	WorkDir string
	// WorkRoot is where the runner keeps one directory per distinct client,
	// named for the hash of its contents.
	//
	// Sharing a single directory between every run was wrong twice: two
	// projects overwrote each other's generated client, so a script was
	// type-checked and run against another project's catalogue, and the
	// runtime's check cache was invalidated every time the catalogue
	// changed. Content addressing fixes both -- same catalogue, same
	// directory, and two different ones never meet.
	//
	// Ignored when WorkDir is set.
	WorkRoot string
	// Runtime is "auto", a kind (deno, bun, node), a name declared in
	// Setup.Runtimes, or a path or binary name whose basename is a kind.
	Runtime string
	// Setup carries the configured runtimes, auto-detection order and
	// permission profiles. The zero value means the built-ins.
	Setup Setup
	// Timeout bounds execution; 0 means no limit.
	Timeout time.Duration
	// Env adds environment variables.
	Env map[string]string
	// Args are passed through to the script.
	Args []string
	// Prelude is prepended to an inline Source snippet. The CLI builds it so
	// every namespace is in scope as a bare identifier as well as via `tools`.
	Prelude string
	// Dir is the working directory for the script. Empty inherits the caller's.
	Dir string
	// Log renders structured records the script emits. When nil, the script's
	// stderr is forwarded unchanged.
	Log *logging.Writer
	// Enrich adds ambient context to every record.
	Enrich map[string]any
	// CollectLogs receives each parsed record, for `run --json`.
	CollectLogs func(logging.Record)
	// OnResult receives values a script streamed with emit().
	OnResult func(logging.Streamed)
	// OnArtifact receives files a script registered with artifact().
	OnArtifact func(logging.Artifacted)
	// Export names the function to call instead of the default export.
	Export string
	// Permissions is the sandbox setting; empty means wide open.
	Permissions string
	// GlobalsSource is the ambient declaration file written beside the client.
	GlobalsSource string
	// CaptureConsole mirrors console output into the record stream.
	CaptureConsole bool
	// Phases are lines injected at named points in the generated launcher.
	// Every point a user might want is named, because a launcher that is
	// half-configurable invites forking it.
	Phases Phases
	// Launcher replaces the generated shim. Empty means the built-in
	// template; LauncherNone means run the script with no shim at all.
	Launcher string
	// LauncherName is where Launcher came from, for error messages.
	LauncherName string
	// AllowRepeat names placeholders permitted to resolve more than once.
	AllowRepeat []string
	// PlaceholderFiles declare additional @names a launcher may use.
	PlaceholderFiles []string
	// TypeCheck resolves and checks the generated program before running it.
	TypeCheck string
	// TypeCheckTimeout bounds that check.
	TypeCheckTimeout time.Duration
	Stdout, Stderr   interface{ Write([]byte) (int, error) }
	// Stdin is what the script reads. Nil inherits the caller's, which is
	// right for a terminal and wrong for a daemon: a script run on somebody
	// else's behalf must not be able to read the daemon's standard input.
	Stdin io.Reader
	// Placeholders are values a caller supplied for @names a launcher
	// template refers to. They lose to the built-in fills, which name the
	// machinery the launcher cannot work without.
	Placeholders map[string]string
}

// Phases are the injection points in a file script's launcher, in the order
// they run.
type Phases struct {
	// Before runs first, ahead of even the globals being installed.
	Before []string
	// Prefix runs after the standard surface is installed and before the
	// module is imported, so it can patch what the script will see.
	Prefix []string
	// OnSuccess runs when the entry point returns, with result.value set.
	OnSuccess []string
	// OnError runs when it throws, with result.error set. The error is
	// re-thrown afterwards; this is a hook, not a handler.
	OnError []string
	// Suffix runs in a finally, on both paths.
	Suffix []string
}

// Empty reports whether any phase carries lines.
func (p Phases) Empty() bool {
	return len(p.Before) == 0 && len(p.Prefix) == 0 &&
		len(p.OnSuccess) == 0 && len(p.OnError) == 0 && len(p.Suffix) == 0
}

// Result reports how a script run finished.
type Result struct {
	ExitCode int
	Duration time.Duration
	Runtime  string
	Script   string
	Client   string
	TimedOut bool
	// Stdout is captured only when Options.Stdout is nil.
	Stdout string
}

const clientFileName = "mcpx-client.ts"

// Run generates the client, writes the script and executes it.
func Run(ctx context.Context, opts Options) (*Result, error) {
	if opts.Source == "" && opts.File == "" {
		return nil, errors.New("runner: need Source or File")
	}
	rt, err := Resolve(opts.Runtime, opts.Permissions, opts.Setup)
	if err != nil {
		return nil, err
	}

	workDir := opts.WorkDir
	cleanup := func() {}
	switch {
	case workDir != "":
		if err := os.MkdirAll(workDir, 0o700); err != nil {
			return nil, err
		}
	case opts.WorkRoot != "":
		sum := sha256.Sum256([]byte(opts.ClientSource))
		workDir = filepath.Join(opts.WorkRoot, hex.EncodeToString(sum[:])[:16])
		if err := os.MkdirAll(workDir, 0o700); err != nil {
			return nil, err
		}
		pruneWorkDirs(opts.WorkRoot, workDir)
	default:
		d, err := os.MkdirTemp("", "mcpx-run-")
		if err != nil {
			return nil, err
		}
		workDir = d
		cleanup = func() { _ = os.RemoveAll(d) }
	}
	// Called through the variable, not captured by value: the script block
	// below extends cleanup, and `defer cleanup()` would have deferred the
	// function as it stood here -- which is why every generated program was
	// left behind in a shared working directory.
	defer func() { cleanup() }()

	clientPath := filepath.Join(workDir, clientFileName)
	if err := writeIfChanged(clientPath, opts.ClientSource); err != nil {
		return nil, err
	}
	if opts.GlobalsSource != "" {
		// Best effort: an editor convenience should never fail a run.
		_ = writeIfChanged(filepath.Join(workDir, GlobalsFileName), opts.GlobalsSource)
	}

	scriptPath := opts.File
	if scriptPath == "" {
		// Named for its own contents, which does three things at once: two
		// concurrent runs never write the same path unless they are running
		// the same program, the same program run twice reuses the path and
		// so hits the runtime's type-check cache, and a different program
		// cannot be mistaken for it.
		src := opts.Prelude + opts.Source
		sum := sha256.Sum256([]byte(src))
		scriptPath = filepath.Join(workDir, "script-"+hex.EncodeToString(sum[:])[:16]+".ts")
		// Written only when absent or different, so a concurrent run of the
		// same program does not rewrite the file underneath it.
		if err := writeIfChanged(scriptPath, src); err != nil {
			return nil, err
		}
		// Kept, not deleted: it is the cache entry. Bounded instead.
		prunePrograms(workDir, scriptPath)
	} else {
		abs, err := filepath.Abs(scriptPath)
		if err != nil {
			return nil, err
		}
		scriptPath = abs
		// A file script imports the client from the same directory, so place a
		// copy next to it. This keeps `import { tools } from "./mcpx-client.ts"`
		// working for hand-written scripts too.
		sideCar := filepath.Join(filepath.Dir(scriptPath), clientFileName)
		if err := writeIfChanged(sideCar, opts.ClientSource); err != nil {
			return nil, fmt.Errorf("write client next to script: %w", err)
		}
		if opts.GlobalsSource != "" {
			_ = writeIfChanged(filepath.Join(filepath.Dir(scriptPath), GlobalsFileName), opts.GlobalsSource)
		}
		clientPath = sideCar
	}

	// A module with a default export is a program with an entry point; one
	// without is a program that ran on import. Supporting both is what lets a
	// single file be imported as a library and still invoked directly.
	{
		launcher, lerr := writeLauncher(workDir, scriptPath, opts.Export,
			opts.Phases, opts.CaptureConsole, opts)
		if lerr != nil {
			return nil, lerr
		}
		if launcher != "" {
			scriptPath = launcher
		}
	}

	// Checked after the launcher is written, because the launcher is what
	// actually gets run and therefore what has to be valid. Doing it here
	// also means a broken import is found before a single server starts.
	if mode := opts.TypeCheck; mode != "" && mode != string(preflight.TypeCheckOff) {
		timeout := opts.TypeCheckTimeout
		if timeout == 0 {
			timeout = defaults.TypecheckTimeout
		}
		rep := preflight.TypeCheck(ctx, rt.Bin, scriptPath, preflight.TypeCheckMode(mode), timeout)
		if err := rep.Err(); err != nil {
			return nil, err
		}
		for _, w := range rep.Warnings() {
			fmt.Fprintln(os.Stderr, "mcpx:", w.String())
		}
	}

	runCtx := ctx
	var cancel context.CancelFunc
	if opts.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	// Every supported runtime passes anything after the script path straight
	// through to the script, so no "--" separator is needed.
	args := append(rt.Args(scriptPath), opts.Args...)

	cmd := exec.Command(rt.Bin, args...)
	// Inherit the caller's working directory rather than the script's. A
	// relative path in a script should mean what it means on the command line;
	// the client import still resolves against the script file, because ESM
	// resolves relative specifiers against the importing module.
	cmd.Dir = opts.Dir
	// The kind, not the configured name: a script branching on its runtime
	// cares whether it is under Deno, not what somebody called the binary.
	cmd.Env = append(os.Environ(), "MCPX_RUNTIME="+rt.Kind)
	// Resolved here rather than by the caller: only the runner knows the final
	// location of the entry script and the client it wrote beside it.
	cmd.Env = append(cmd.Env, "MCPX_ENTRY="+scriptPath, "MCPX_CLIENT="+clientPath)
	if allowsRead(rt.Kind, rt.Perms) {
		cmd.Env = append(cmd.Env, "MCPX_ALLOW_READ=1")
	}
	for k, v := range opts.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if opts.Stdout != nil {
		cmd.Stdout = opts.Stdout
	} else {
		cmd.Stdout = os.Stdout
	}
	// Structured records arrive interleaved on stderr and must be pulled out
	// before anything else sees them.
	var stderrDone chan struct{}
	if opts.Log != nil {
		pr, pw, perr := os.Pipe()
		if perr != nil {
			return nil, perr
		}
		cmd.Stderr = pw
		passthrough := opts.Stderr
		if passthrough == nil {
			passthrough = os.Stderr
		}
		stderrDone = make(chan struct{})
		go func() {
			defer close(stderrDone)
			defer pr.Close()
			_ = logging.Stream(pr, opts.Log, logging.StreamOptions{
				Enrich:      opts.Enrich,
				Passthrough: passthrough,
				Collect:     opts.CollectLogs,
				Result:      opts.OnResult,
				Artifact:    opts.OnArtifact,
			})
		}()
		defer func() {
			pw.Close()
			<-stderrDone
		}()
	} else if opts.Stderr != nil {
		cmd.Stderr = opts.Stderr
	} else {
		cmd.Stderr = os.Stderr
	}
	if opts.Stdin != nil {
		cmd.Stdin = opts.Stdin
	} else {
		cmd.Stdin = os.Stdin
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", rt.Name, err)
	}

	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	res := &Result{Runtime: rt.Name, Script: scriptPath, Client: clientPath}
	select {
	case <-runCtx.Done():
		res.TimedOut = true
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
			select {
			case <-waitCh:
			case <-time.After(defaults.ShutdownGrace):
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				<-waitCh
			}
		}
		res.Duration = time.Since(start)
		res.ExitCode = 124
		return res, fmt.Errorf("script timed out after %s", opts.Timeout)
	case err := <-waitCh:
		res.Duration = time.Since(start)
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				res.ExitCode = ee.ExitCode()
				return res, nil
			}
			return res, err
		}
		return res, nil
	}
}

// writeIfChanged avoids touching mtime when content is identical, which keeps
// the runtime's compile cache warm across runs.
func writeIfChanged(path, content string) error {
	if existing, err := os.ReadFile(path); err == nil {
		if sha256sum(existing) == sha256sum([]byte(content)) {
			return nil
		}
	}
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func sha256sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// ClientFileName is the name of the generated client module on disk.
const ClientFileName = clientFileName

// GlobalsFileName holds ambient declarations for the installed globals.
const GlobalsFileName = "mcpx-globals.d.ts"

// writeLauncher emits a shim that imports the user's module and calls its
// entry point if it has one.
//
// The shim is written beside the script so its relative import resolves, and
// so the generated client next to the script is the one both files see.
// LauncherNone, given as the launcher, means no shim at all: the script is
// handed to the runtime untouched. Nothing is installed, nothing is captured,
// nothing wraps the error. It is the escape hatch for a script that wants the
// runtime and none of the harness.
const LauncherNone = "none"

func writeLauncher(workDir, scriptPath, export string, ph Phases, captureConsole bool, opts Options) (string, error) {
	if strings.TrimSpace(opts.Launcher) == LauncherNone {
		return "", nil
	}
	dir := filepath.Dir(scriptPath)
	base := filepath.Base(scriptPath)
	stem := "." + strings.TrimSuffix(base, filepath.Ext(base))

	argsJSON, _ := json.Marshal([]string{})
	_ = argsJSON

	consoleCall := "// console left alone"
	if captureConsole {
		consoleCall = "captureConsole();"
	}

	body := fmt.Sprintf(`// Generated by mcpx. Runs %s.
//
// The reference is what makes the ambient declarations load. Without it the
// globals file sits beside this one and is never read, so a type check
// reported "Cannot find name 'emit'" for every script that used one -- and
// so could not report the mistakes it exists to find.
/// <reference path="./`+GlobalsFileName+`" />
//
// The module is imported dynamically rather than with a static import, so that
// lines injected before it genuinely run first. A static import is hoisted and
// would evaluate the module ahead of anything else whatever the source order.
import {
  log, emit, installGlobals, captureConsole, releaseConsole,
} from %q;

const argv = (globalThis as any).Deno?.args ?? (globalThis as any).process?.argv?.slice(2) ?? [];
const wanted = %q;

/** What is about to run. Visible to every phase. */
const script = {
  path: %q,
  name: %q,
  args: argv as string[],
  export: wanted || "default",
};

/** How the run ended. Filled in before onSuccess, onError and suffix. */
const result: { value?: unknown; error?: unknown; ok: boolean; ms: number } = {
  ok: true,
  ms: 0,
};
void [log, emit, script, result, releaseConsole];

// phase: before
%s

installGlobals();
%s

// phase: prefix
%s

const __started = performance.now();
try {
  // Inside the try, because a script's top-level code runs here: outside it,
  // a snippet that threw -- which is how most snippets fail -- skipped
  // onError and suffix entirely, the two phases that exist for exactly that.
  const mod = await import(%q);
  const entry = wanted ? (mod as any)[wanted] : (mod as any).default;

  if (wanted && typeof entry !== "function") {
    const names = Object.keys(mod).filter((k) => typeof (mod as any)[k] === "function");
    throw new Error(
      "no exported function " + JSON.stringify(wanted) + " in %s" +
        (names.length ? "; found " + names.join(", ") : ""),
    );
  }

  if (typeof entry === "function") {
    // A default export is the program's main and receives argv as an array.
    // A named export is being called as a function, so arguments are spread:
    // --export f a b reads as f(a, b).
    const value = wanted ? await entry(...argv) : await entry(argv);
    result.value = value;
    if (value !== undefined) {
      console.log(typeof value === "string" ? value : JSON.stringify(value, null, 2));
    }
  }
  result.ms = performance.now() - __started;
  // phase: onSuccess
%s
} catch (err) {
  result.ok = false;
  result.error = err;
  result.ms = performance.now() - __started;
  // phase: onError. A hook, not a handler: the error is re-thrown below so the
  // exit status still reflects what happened.
%s
  throw err;
} finally {
  result.ms = result.ms || performance.now() - __started;
  // phase: suffix
%s
}
`, base, "./"+ClientFileName, export,
		scriptPath, strings.TrimSuffix(base, filepath.Ext(base)),
		indentLines(ph.Before, ""), consoleCall, indentLines(ph.Prefix, ""),
		"./"+base, base,
		indentLines(ph.OnSuccess, "  "), indentLines(ph.OnError, "  "),
		indentLines(ph.Suffix, "  "))

	if custom := strings.TrimSpace(opts.Launcher); custom != "" {
		var err error
		body, err = expandCustom(custom, opts, scriptPath, base, export, ph, captureConsole)
		if err != nil {
			return "", err
		}
	}

	// Named for what it contains, not for the script it runs.
	//
	// One name per script meant every concurrent run of that script wrote
	// the same file: six runs of one script with six different launchers all
	// executed the fourth. Placeholders make the same mistake quieter --
	// two runs of one script with different @values are two different
	// programs sharing a path.
	//
	// The hash also keeps the runtime's type-check cache: the same launcher
	// again is the same file, so it is not re-checked.
	sum := sha256.Sum256([]byte(body))
	launcher := filepath.Join(dir, stem+"."+hex.EncodeToString(sum[:])[:12]+".mcpx-entry.ts")
	if err := writeIfChanged(launcher, body); err != nil {
		return "", err
	}
	pruneEntries(dir, stem, launcher)
	return launcher, nil
}

// expandCustom fills a user-supplied launcher template.
//
// The fills are the same fragments the built-in template uses, so a custom
// launcher can be a rearrangement rather than a rewrite -- @globals and @entry
// carry their full meaning, and a template that only wants to add a guard
// around the call does not have to reproduce the import machinery.
func expandCustom(text string, opts Options, scriptPath, base, export string,
	ph Phases, captureConsole bool) (string, error) {

	name := opts.LauncherName
	if name == "" {
		name = "custom launcher"
	}
	consoleCall := "// console left alone"
	if captureConsole {
		consoleCall = "captureConsole();"
	}
	importLine := fmt.Sprintf("const mod = await import(%q);", "./"+base)
	// Braced, so that a template naming @entry twice produces two runs rather
	// than a redeclaration error. Allowing a repeat and then emitting code
	// that cannot compile would be a worse answer than refusing it outright.
	entryBlock := fmt.Sprintf(`{
  %s
  const entry = %s ? (mod as any)[%s] : (mod as any).default;
  if (typeof entry === "function") {
    const __v = %s ? await entry(...argv) : await entry(argv);
    result.value = __v;
    if (__v !== undefined) {
      console.log(typeof __v === "string" ? __v : JSON.stringify(__v, null, 2));
    }
  }
}`, importLine, jsonString(export), jsonString(export), jsonString(export))

	fill := launcher.Fill{
		launcher.Header:    launcherHeader(scriptPath, base, export),
		launcher.Globals:   "installGlobals();",
		launcher.Console:   consoleCall,
		launcher.Import:    importLine,
		launcher.Entry:     entryBlock,
		launcher.Before:    indentLines(ph.Before, ""),
		launcher.Prefix:    indentLines(ph.Prefix, ""),
		launcher.OnSuccess: indentLines(ph.OnSuccess, ""),
		launcher.OnError:   indentLines(ph.OnError, ""),
		launcher.Suffix:    indentLines(ph.Suffix, ""),
	}
	defs, derr := launcher.LoadDefinitions(opts.PlaceholderFiles)
	if derr != nil {
		return "", derr
	}
	var berr error
	fill, berr = launcher.Bind(fill, defs)
	if berr != nil {
		return "", berr
	}
	// Caller-supplied values lose to the built-in fills. @entry naming
	// something other than the entry point would be a launcher that silently
	// runs the wrong thing, and no option is worth that.
	for k, v := range opts.Placeholders {
		p := launcher.Placeholder(strings.TrimPrefix(k, "@"))
		if _, taken := fill[p]; taken {
			continue
		}
		fill[p] = v
	}

	var allow []launcher.Placeholder
	for _, a := range opts.AllowRepeat {
		allow = append(allow, launcher.Placeholder(strings.TrimPrefix(a, "@")))
	}
	tpl := launcher.Template{Text: text, Name: name}

	// A launcher that never reaches the script is legal -- someone may be
	// testing a prefix in isolation -- but it is almost never meant, so it is
	// worth saying once rather than leaving them to wonder why nothing ran.
	if !referencesEntry(tpl) {
		fmt.Fprintf(os.Stderr,
			"mcpx: %s refers to neither @entry nor @import, so the script will not run\n", name)
	}
	return launcher.Expand(tpl, fill, launcher.Options{AllowRepeat: allow, Defs: defs})
}

func referencesEntry(t launcher.Template) bool {
	for _, p := range launcher.Used(t) {
		if p == launcher.Entry || p == launcher.Import {
			return true
		}
	}
	return false
}

func jsonString(v string) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// launcherHeader is the preamble every launcher needs: the client import and
// the script and result objects each phase can read.
func launcherHeader(scriptPath, base, export string) string {
	return fmt.Sprintf(`import {
  log, emit, installGlobals, captureConsole, releaseConsole,
} from %q;

const argv = (globalThis as any).Deno?.args ?? (globalThis as any).process?.argv?.slice(2) ?? [];
const wanted = %s;

const script = { path: %q, name: %q, args: argv as string[], export: wanted || "default" };
const result: { value?: unknown; error?: unknown; ok: boolean; ms: number } = { ok: true, ms: 0 };
void [log, emit, script, result, releaseConsole, installGlobals, captureConsole];`,
		"./"+ClientFileName, jsonString(export), scriptPath,
		strings.TrimSuffix(base, filepath.Ext(base)))
}

// indentLines joins lines with an indent, or yields a comment when empty so
// the generated file never has a bare blank where code was expected.
func indentLines(lines []string, indent string) string {
	if len(lines) == 0 {
		return indent + "// (no lines configured)"
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = indent + l
	}
	return strings.Join(out, "\n")
}

// allowsRead reports whether the script may read files, which the generated
// client's file helpers check before trying rather than leaving the failure
// to the sandbox.
func allowsRead(kind string, perms []string) bool {
	switch kind {
	case KindDeno:
		for _, perm := range perms {
			if perm == "--allow-all" || strings.HasPrefix(perm, "--allow-read") {
				return true
			}
		}
		return false
	case KindNode:
		// Without --permission node enforces nothing.
		narrowed := false
		for _, perm := range perms {
			if perm == "--permission" || perm == "--experimental-permission" {
				narrowed = true
			}
			if strings.HasPrefix(perm, "--allow-fs-read") {
				return true
			}
		}
		return !narrowed
	}
	return true
}

// prunePrograms bounds how many generated programs a client directory keeps.
//
// Each is a cache entry for the type checker, worth keeping while it is
// still being run and worth nothing afterwards. The newest survive.
func prunePrograms(dir, keep string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type aged struct {
		path string
		at   time.Time
	}
	var progs []aged
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "script-") || !strings.HasSuffix(name, ".ts") {
			continue
		}
		p := filepath.Join(dir, name)
		if p == keep {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		progs = append(progs, aged{p, info.ModTime()})
	}
	if len(progs) < defaults.ExecPrograms {
		return
	}
	sort.Slice(progs, func(i, j int) bool { return progs[i].at.After(progs[j].at) })
	for _, p := range progs[defaults.ExecPrograms-1:] {
		_ = os.Remove(p.path)
	}
}

// pruneWorkDirs bounds how many client directories are kept.
//
// One per distinct catalogue, and a catalogue changes whenever a server is
// added or updates its schema, so without a bound this grows for as long as
// mcpx is used. The newest are kept, because they are the ones whose cache
// is worth having.
func pruneWorkDirs(root, keep string) {
	ents, err := os.ReadDir(root)
	if err != nil || len(ents) <= defaults.ExecWorkDirs {
		return
	}
	type aged struct {
		path string
		at   time.Time
	}
	var dirs []aged
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(root, e.Name())
		if p == keep {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		dirs = append(dirs, aged{p, info.ModTime()})
	}
	if len(dirs) < defaults.ExecWorkDirs {
		return
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].at.After(dirs[j].at) })
	for _, d := range dirs[defaults.ExecWorkDirs-1:] {
		_ = os.RemoveAll(d.path)
	}
}

// pruneEntries bounds how many generated entry points one script leaves
// beside itself.
//
// They live next to the user's file so that a relative import of the client
// resolves, which means they are visible and must not accumulate. The newest
// survive, because those are the ones still being run.
func pruneEntries(dir, stem, keep string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type aged struct {
		path string
		at   time.Time
	}
	var found []aged
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, stem+".") || !strings.HasSuffix(name, ".mcpx-entry.ts") {
			continue
		}
		p := filepath.Join(dir, name)
		if p == keep {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		found = append(found, aged{p, info.ModTime()})
	}
	if len(found) < defaults.ExecEntries {
		return
	}
	sort.Slice(found, func(i, j int) bool { return found[i].at.After(found[j].at) })
	for _, f := range found[defaults.ExecEntries-1:] {
		_ = os.Remove(f.path)
	}
}
