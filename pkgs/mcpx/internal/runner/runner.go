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
	"errors"
	"fmt"
	"os"
	"os/exec"
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

// Detect picks a runtime. An explicit preference wins; otherwise the first
// available of deno, bun, node is used.
func Detect(prefer string) (*Runtime, error) {
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
				// agent's script surfaces at runtime anyway. This is the single
				// biggest win over lootbox's default pipeline.
				return []string{"run", "--quiet", "--no-check", "--allow-all", s}
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
	Dir            string
	Stdout, Stderr interface{ Write([]byte) (int, error) }
}

// Result reports how a script run finished.
type Result struct {
	ExitCode int
	Duration time.Duration
	Runtime  string
	Script   string
	Client   string
	TimedOut bool
}

const clientFileName = "mcpx-client.ts"

// Run generates the client, writes the script and executes it.
func Run(ctx context.Context, opts Options) (*Result, error) {
	if opts.Source == "" && opts.File == "" {
		return nil, errors.New("runner: need Source or File")
	}
	rt, err := Detect(opts.Runtime)
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
		clientPath = sideCar
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
	if opts.Stderr != nil {
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
