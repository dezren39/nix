// Package preflight checks a run before anything executes.
//
// The failures worth catching here all share a shape: they are knowable
// before any work starts, and discovering them later costs far more than
// discovering them now. A missing prefix file surfaces as a syntax error in
// generated code. A setting that cannot take effect surfaces as silence. A
// broken import surfaces after the servers have already been started.
package preflight

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Problem is one thing wrong, with enough detail to fix it.
type Problem struct {
	// What is the short description.
	What string
	// Where is the setting, file or flag responsible.
	Where string
	// Fix is the suggested remedy, when there is an obvious one.
	Fix string
	// Fatal marks a problem that must stop the run. A non-fatal problem is
	// reported and continues, because a warning nobody can suppress becomes
	// noise and then becomes invisible.
	Fatal bool
}

func (p Problem) String() string {
	s := p.What
	if p.Where != "" {
		s = p.Where + ": " + s
	}
	if p.Fix != "" {
		s += "\n      " + p.Fix
	}
	return s
}

// Report is everything found.
type Report struct {
	Problems []Problem
}

// Add records a problem.
func (r *Report) Add(p Problem) { r.Problems = append(r.Problems, p) }

// Err returns an error when anything fatal was found.
func (r *Report) Err() error {
	var fatal []string
	for _, p := range r.Problems {
		if p.Fatal {
			fatal = append(fatal, "  "+p.String())
		}
	}
	if len(fatal) == 0 {
		return nil
	}
	sort.Strings(fatal)
	return fmt.Errorf("cannot start:\n%s", strings.Join(fatal, "\n"))
}

// Warnings returns the non-fatal problems.
func (r *Report) Warnings() []Problem {
	var out []Problem
	for _, p := range r.Problems {
		if !p.Fatal {
			out = append(out, p)
		}
	}
	return out
}

// PathCheck is one path that must resolve.
type PathCheck struct {
	// Path is what to check.
	Path string
	// Where names the setting it came from.
	Where string
	// MustExist makes absence fatal.
	MustExist bool
	// WantDir requires a directory; WantFile requires a regular file.
	WantDir  bool
	WantFile bool
	// Writable requires the location to be writable, creating it if it is a
	// directory that is missing.
	Writable bool
}

// CheckPaths resolves and verifies every referenced location.
//
// Doing this in one pass up front means a run with three bad paths reports
// three problems, not the first one and then two more runs.
func CheckPaths(checks []PathCheck) *Report {
	r := &Report{}
	for _, c := range checks {
		if strings.TrimSpace(c.Path) == "" {
			continue
		}
		info, err := os.Stat(c.Path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			if c.Writable && c.WantDir {
				if mkErr := os.MkdirAll(c.Path, 0o755); mkErr != nil {
					r.Add(Problem{
						What:  "does not exist and cannot be created: " + mkErr.Error(),
						Where: c.Where, Fatal: true,
					})
					continue
				}
				continue
			}
			r.Add(Problem{
				What:  "does not exist: " + c.Path,
				Where: c.Where, Fatal: c.MustExist,
				Fix: fixFor(c),
			})
			continue
		case err != nil:
			r.Add(Problem{What: err.Error(), Where: c.Where, Fatal: c.MustExist})
			continue
		}
		if c.WantDir && !info.IsDir() {
			r.Add(Problem{
				What:  "is a file, but a directory was expected: " + c.Path,
				Where: c.Where, Fatal: true,
			})
		}
		if c.WantFile && info.IsDir() {
			r.Add(Problem{
				What:  "is a directory, but a file was expected: " + c.Path,
				Where: c.Where, Fatal: true,
			})
		}
		if c.Writable && !writable(c.Path, info) {
			r.Add(Problem{
				What: "is not writable: " + c.Path, Where: c.Where, Fatal: true,
			})
		}
	}
	return r
}

func fixFor(c PathCheck) string {
	if c.WantDir {
		return "mkdir -p " + c.Path
	}
	return ""
}

func writable(path string, info os.FileInfo) bool {
	if info.IsDir() {
		probe := filepath.Join(path, ".mcpx-write-probe")
		f, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return false
		}
		_ = f.Close()
		_ = os.Remove(probe)
		return true
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// TypeCheckMode selects how thoroughly a generated program is checked.
type TypeCheckMode string

const (
	TypeCheckOff    TypeCheckMode = "off"
	TypeCheckOn     TypeCheckMode = "on"
	TypeCheckStrict TypeCheckMode = "strict"
)

// TypeCheck resolves and checks a program without running it.
//
// This is the check that answers "will every import resolve and every name
// exist" before any server is started. Deno does it in one command; the
// globals mcpx installs are declared in the generated .d.ts beside the
// script, so they are visible to the checker without being visible to the
// runtime twice.
//
// It is off by default because on a cold module cache it costs a second or
// two, and most runs do not need it. It is worth turning on in anything
// scheduled, where the cost is irrelevant and a broken import at three in the
// morning is not.
func TypeCheck(ctx context.Context, runtimeBin, entry string, mode TypeCheckMode, timeout time.Duration) *Report {
	r := &Report{}
	if mode == TypeCheckOff || mode == "" {
		return r
	}
	base := filepath.Base(runtimeBin)
	if !strings.Contains(base, "deno") {
		// Node and Bun strip types rather than checking them, so there is no
		// equivalent single command. Saying so is better than pretending the
		// check ran.
		r.Add(Problem{
			What:  "type checking is only available under Deno; " + base + " strips types rather than checking them",
			Where: "script.typecheck",
		})
		return r
	}
	args := []string{"check"}
	if mode == TypeCheckStrict {
		args = append(args, "--all")
	}
	args = append(args, entry)

	cctx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		cctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(cctx, runtimeBin, args...)
	cmd.Dir = filepath.Dir(entry)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return r
	}
	if cctx.Err() != nil {
		r.Add(Problem{
			What:  "type checking did not finish within " + timeout.String(),
			Where: "script.typecheck",
			Fix:   "raise the timeout, or set script.typecheck off",
		})
		return r
	}
	r.Add(Problem{
		What:  "the program does not type check:\n" + indent(strings.TrimSpace(string(out))),
		Where: "script.typecheck",
		Fatal: true,
	})
	return r
}

func indent(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = "      " + l
	}
	return strings.Join(lines, "\n")
}

// CheckEnvPairs validates KEY=VALUE arguments.
//
// A malformed pair is currently ignored, which means a typo silently fails to
// set the variable and the script behaves as though it was never asked for.
func CheckEnvPairs(pairs []string, where string) *Report {
	r := &Report{}
	for _, p := range pairs {
		if p == "" {
			continue
		}
		k, _, ok := strings.Cut(p, "=")
		if !ok {
			r.Add(Problem{
				What:  fmt.Sprintf("%q is not KEY=VALUE", p),
				Where: where, Fatal: true,
				Fix: "write " + p + "=<value>, or drop it",
			})
			continue
		}
		if strings.TrimSpace(k) == "" {
			r.Add(Problem{
				What: fmt.Sprintf("%q has an empty name", p), Where: where, Fatal: true,
			})
		}
		if strings.ContainsAny(k, " \t") {
			r.Add(Problem{
				What:  fmt.Sprintf("%q has whitespace in its name", p),
				Where: where, Fatal: true,
			})
		}
	}
	return r
}

// Merge folds several reports into one.
func Merge(reports ...*Report) *Report {
	out := &Report{}
	for _, r := range reports {
		if r == nil {
			continue
		}
		out.Problems = append(out.Problems, r.Problems...)
	}
	return out
}
