package cli

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/dezren39/mcpx/internal/settings"
)

var errStopAfterFlags = errors.New("stop after flags")

// nonDefault picks a valid value for a setting that differs from its default,
// or "" when none of the candidates validate.
func nonDefault(s settings.Setting) string {
	var cands []string
	switch s.Kind {
	case settings.KindBool:
		if s.Default == "true" {
			cands = []string{"false"}
		} else {
			cands = []string{"true"}
		}
	case settings.KindEnum:
		cands = append(cands, s.Enum...)
	case settings.KindInt:
		cands = []string{"7", "3"}
	case settings.KindDuration:
		cands = []string{"7s", "3m"}
	case settings.KindBytes:
		cands = []string{"7MB", "3KB"}
	default:
		cands = []string{"probe-value", "/tmp/mcpx-probe", "a,b"}
	}
	for _, c := range cands {
		if c == s.Default {
			continue
		}
		if settings.Validate(s, c) == nil {
			return c
		}
	}
	return ""
}

// TestEverySettingFlagReachesTheResolvedValue is the guard for #231: a flag a
// command accepts must change what the command resolves, not merely parse.
// Each command is run with each of its setting flags and stopped the moment
// its flags are folded in; the setting must then come from the flag layer
// with the value given. A hand-declared flag that shadows the generated one,
// or a value folded in after it was read, fails here.
func TestEverySettingFlagReachesTheResolvedValue(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	t.Setenv("MCPX_STATE_DIR", filepath.Join(dir, "state"))
	t.Setenv("MCPX_CACHE_DIR", filepath.Join(dir, "cache"))
	cfg := filepath.Join(dir, "mcpx.json")
	// preset names a preset that has to exist, so the probe value is defined
	// here rather than skipped: the point is to prove --preset reaches the
	// resolved set like every other setting flag.
	if err := os.WriteFile(cfg, []byte(`{"mcpServers":{},"presets":{"probe-value":["--json"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	parseFlagsHook = func(*flag.FlagSet) error { return errStopAfterFlags }
	t.Cleanup(func() { parseFlagsHook = nil })

	sch, err := settings.New(settings.Registry())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	names := (&App{}).HandlerNames()
	sort.Strings(names)
	checked := 0
	for _, cmd := range names {
		// Reach check: does this command parse through parseFlags at all?
		probe := &App{ConfigPath: cfg}
		if err := probe.Handlers()[cmd](ctx, nil); !errors.Is(err, errStopAfterFlags) {
			continue
		}
		cmdName := cmd
		for _, s := range sch.ForCommand(cmdName) {
			val := nonDefault(s)
			if val == "" {
				continue
			}
			for _, name := range append([]string{s.FlagName()}, s.FlagAliases...) {
				if handFlagsMeaningOther[cmd][name] {
					continue
				}
				a := &App{ConfigPath: cfg}
				err := a.Handlers()[cmd](ctx, []string{"--" + name + "=" + val})
				if err != nil && !errors.Is(err, errStopAfterFlags) {
					// A requirement refusing the value proves it arrived.
					if v, ok := a.Settings().Value(s.Path); ok && v.Origin.Layer == settings.LayerFlag {
						checked++
						continue
					}
					t.Errorf("%s --%s=%s: %v", cmd, name, val, err)
					continue
				}
				v, ok := a.Settings().Value(s.Path)
				if !ok || v.Origin.Layer != settings.LayerFlag {
					t.Errorf("%s --%s=%s parsed but %s did not take it (origin %v)",
						cmd, name, val, s.Path, v)
					continue
				}
				checked++
			}
		}
	}
	if checked < 100 {
		t.Fatalf("only %d (command, flag) pairs checked; the probe is not reaching the commands", checked)
	}
}

// TestStrictUnknownKeysFlagRefuses is #231 itself: the flag must refuse a
// config with an unknown key in both positions, as the variable does.
func TestStrictUnknownKeysFlagRefuses(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	cfg := filepath.Join(dir, "mcpx.json")
	if err := os.WriteFile(cfg, []byte(`{"mcpServers":{},"nonsenseKey":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	parseFlagsHook = func(*flag.FlagSet) error { return errStopAfterFlags }
	t.Cleanup(func() { parseFlagsHook = nil })

	a := &App{ConfigPath: cfg}
	err := a.CmdLs(context.Background(), []string{"--plumbing-strict-unknown-keys"})
	if err == nil || errors.Is(err, errStopAfterFlags) {
		t.Fatalf("after the command: want a refusal, got %v", err)
	}

	g := &App{ConfigPath: cfg}
	rest, err := g.ApplyGlobalSettingFlags([]string{"--plumbing-strict-unknown-keys", "ls"})
	if err == nil {
		t.Fatalf("before the command: want a refusal, got rest=%v", rest)
	}

	ok := &App{ConfigPath: cfg}
	rest, err = ok.ApplyGlobalSettingFlags([]string{"--plumbing-strict-unknown-keys=false", "ls"})
	if err != nil || len(rest) != 1 || rest[0] != "ls" {
		t.Fatalf("a false value must pass and consume only the flag: rest=%v err=%v", rest, err)
	}
}
