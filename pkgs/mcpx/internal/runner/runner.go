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
	"os"
	"os/exec"

	"github.com/dezren39/mcpx/internal/logging"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Runtime is a JavaScript runtime capable of executing TypeScript directly.
type Runtime struct {
	Name string
	Bin  string
	Args func(script string) []string
}

// Permissions renders a sandbox setting into runtime flags.
//
// Only Deno has a permission model to speak of; bun and node run with the
// user's own authority whatever is asked, which is stated here rather than
// pretended otherwise.
//
// The default is wide open. A script is written by the same person who could
// have run the command directly, and a half-sandbox invites working around it
// rather than reasoning about it. Narrowing is available for the cases where
// it is genuinely wanted.
func Permissions(spec string) []string {
	switch strings.ToLower(strings.TrimSpace(spec)) {
	case "", "all", "none", "off", "unsandboxed":
		return []string{"--allow-all"}
	case "net":
		return []string{"--allow-net", "--allow-env"}
	case "read":
		return []string{"--allow-read", "--allow-env"}
	case "readnet", "read-net":
		return []string{"--allow-read", "--allow-net", "--allow-env"}
	case "strict":
		// Enough to reach the daemon and nothing else.
		return []string{"--allow-net=127.0.0.1", "--allow-env"}
	}
	// Anything else is passed through verbatim, so an unusual combination does
	// not require a new keyword here.
	return strings.Fields(spec)
}

// Detect picks a runtime. An explicit preference wins; otherwise the first
// available of deno, bun, node is used.
func Detect(prefer string, perms []string) (*Runtime, error) {
	candidates := []string{"deno", "bun", "node"}
	if prefer != "" && prefer != "auto" {
		candidates = []string{prefer}
	}
	var tried []string
	for _, name := range candidates {
		bin, err := exec.LookPath(name)
		if err != nil {
			tried = append(tried, name)
			continue
		}
		switch name {
		case "deno":
			return &Runtime{Name: "deno", Bin: bin, Args: func(s string) []string {
				// --no-check skips type checking: the generated client is
				// machine-written and already correct, and a type error in the
				// agent's script surfaces at runtime anyway.
				args := []string{"run", "--quiet", "--no-check"}
				args = append(args, perms...)
				return append(args, s)
			}}, nil
		case "bun":
			return &Runtime{Name: "bun", Bin: bin, Args: func(s string) []string {
				return []string{"run", s}
			}}, nil
		case "node":
			return &Runtime{Name: "node", Bin: bin, Args: func(s string) []string {
				return []string{"--no-warnings", "--experimental-strip-types", s}
			}}, nil
		}
	}
	return nil, fmt.Errorf("no JavaScript runtime found (tried %s); install deno, bun or node",
		strings.Join(tried, ", "))
}

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
	// Runtime preference ("auto", "deno", "bun", "node").
	Runtime string
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
	Phases         Phases
	Stdout, Stderr interface{ Write([]byte) (int, error) }
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
	perms := Permissions(opts.Permissions)
	rt, err := Detect(opts.Runtime, perms)
	if err != nil {
		return nil, err
	}

	workDir := opts.WorkDir
	cleanup := func() {}
	if workDir == "" {
		d, err := os.MkdirTemp("", "mcpx-run-")
		if err != nil {
			return nil, err
		}
		workDir = d
		cleanup = func() { _ = os.RemoveAll(d) }
	} else if err := os.MkdirAll(workDir, 0o700); err != nil {
		return nil, err
	}
	defer cleanup()

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
		scriptPath = filepath.Join(workDir, "script.ts")
		src := opts.Prelude + opts.Source
		if err := os.WriteFile(scriptPath, []byte(src), 0o600); err != nil {
			return nil, err
		}
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
	if opts.File != "" {
		launcher, lerr := writeLauncher(workDir, scriptPath, opts.Export,
			opts.Phases, opts.CaptureConsole)
		if lerr != nil {
			return nil, lerr
		}
		if launcher != "" {
			scriptPath = launcher
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
	cmd.Env = append(os.Environ(), "MCPX_RUNTIME="+rt.Name)
	// Resolved here rather than by the caller: only the runner knows the final
	// location of the entry script and the client it wrote beside it.
	cmd.Env = append(cmd.Env, "MCPX_ENTRY="+scriptPath, "MCPX_CLIENT="+clientPath)
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
	cmd.Stdin = os.Stdin
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
			case <-time.After(2 * time.Second):
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
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
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
func writeLauncher(workDir, scriptPath, export string, ph Phases, captureConsole bool) (string, error) {
	dir := filepath.Dir(scriptPath)
	base := filepath.Base(scriptPath)
	name := "." + strings.TrimSuffix(base, filepath.Ext(base)) + ".mcpx-entry.ts"
	launcher := filepath.Join(dir, name)

	argsJSON, _ := json.Marshal([]string{})
	_ = argsJSON

	consoleCall := "// console left alone"
	if captureConsole {
		consoleCall = "captureConsole();"
	}

	body := fmt.Sprintf(`// Generated by mcpx. Runs %s.
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

const mod = await import(%q);
const entry = wanted ? (mod as any)[wanted] : (mod as any).default;

if (wanted && typeof entry !== "function") {
  const names = Object.keys(mod).filter((k) => typeof (mod as any)[k] === "function");
  throw new Error(
    "no exported function " + JSON.stringify(wanted) + " in %s" +
      (names.length ? "; found " + names.join(", ") : ""),
  );
}

const __started = performance.now();
try {
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

	if err := writeIfChanged(launcher, body); err != nil {
		return "", err
	}
	return launcher, nil
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
