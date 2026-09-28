package settings_test

import (
	"flag"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/settings"
)

func schema(t *testing.T) *settings.Schema {
	t.Helper()
	s, err := settings.New(settings.Registry())
	if err != nil {
		t.Fatalf("the registry should be internally consistent: %v", err)
	}
	return s
}

func TestRegistryHasNoCollidingFlagsOrVariables(t *testing.T) {
	// New() rejects duplicates, so reaching this line is the assertion. The
	// failure this guards against is two settings quietly sharing a flag,
	// where one silently wins and the other looks broken.
	schema(t)
}

func TestEverySettingIsReachableFromAllThreeSurfaces(t *testing.T) {
	for _, set := range schema(t).All() {
		if set.FlagName() == "" {
			t.Errorf("%s has no flag", set.Path)
		}
		if !strings.HasPrefix(set.EnvName(), "MCPX_") {
			t.Errorf("%s has a variable that is not namespaced: %s", set.Path, set.EnvName())
		}
		if set.Name == "" || set.Short == "" {
			t.Errorf("%s is undocumented; a knob nobody can find is not a feature", set.Path)
		}
	}
}

func TestEveryDefaultSurvivesItsOwnValidator(t *testing.T) {
	// A default that skips validation is a default that can be invalid, and
	// that surfaces on someone else's machine rather than here.
	for _, set := range schema(t).All() {
		if err := settings.Validate(set, set.Default); err != nil {
			t.Errorf("%s has an invalid default %q: %v", set.Path, set.Default, err)
		}
	}
}

func TestDerivedNamesAreWhatAUserWouldGuess(t *testing.T) {
	s := schema(t)
	for _, want := range []struct{ path, flag, env string }{
		{"logging.level", "logging-level", "MCPX_LOGGING_LEVEL"},
		{"pool.idleTimeout", "pool-idle-timeout", "MCPX_POOL_IDLE_TIMEOUT"},
		{"script.onError", "script-on-error", "MCPX_SCRIPT_ON_ERROR"},
	} {
		set, ok := s.Lookup(want.path)
		if !ok {
			t.Fatalf("%s should exist", want.path)
		}
		if set.FlagName() != want.flag {
			t.Errorf("%s flag = --%s, want --%s", want.path, set.FlagName(), want.flag)
		}
		if set.EnvName() != want.env {
			t.Errorf("%s env = %s, want %s", want.path, set.EnvName(), want.env)
		}
	}
}

func TestTwoSpellingsOfOneSettingOnOneCommandLineIsAnError(t *testing.T) {
	s := schema(t)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(nopWriter{})
	b := s.Bind(fs, "run")
	if err := fs.Parse([]string{"--logging-level", "debug", "--log-level", "warn"}); err != nil {
		t.Fatalf("both spellings should parse: %v", err)
	}
	set := settings.NewSet(s)
	err := b.ApplyTo(set)
	if err == nil {
		t.Fatal("the same setting under two names should be refused, not silently raced")
	}
	if !strings.Contains(err.Error(), "logging.level") {
		t.Errorf("the error should name the setting: %v", err)
	}
}

func TestTheSameFlagTwiceTakesTheLastOne(t *testing.T) {
	// A person editing their own command line is not a conflict.
	s := schema(t)
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(nopWriter{})
	b := s.Bind(fs, "run")
	if err := fs.Parse([]string{"--logging-level", "debug", "--logging-level", "error"}); err != nil {
		t.Fatal(err)
	}
	set := settings.NewSet(s)
	if err := b.ApplyTo(set); err != nil {
		t.Fatalf("repeating one spelling is fine: %v", err)
	}
	if got := set.String("logging.level"); got != "error" {
		t.Errorf("last wins: got %q", got)
	}
}

func TestTwoVariablesForOneSettingWithDifferentValuesIsAnError(t *testing.T) {
	// Unlike a command line, the environment has no order, so there is
	// genuinely nothing to prefer.
	s := schema(t)
	set := settings.NewSet(s)
	err := s.ApplyEnv(set, []string{
		"MCPX_LOGGING_LEVEL=debug",
		"MCPX_LOG_LEVEL=error",
	})
	if err == nil {
		t.Fatal("two variables disagreeing about one setting should be refused")
	}
	if !strings.Contains(err.Error(), "no order") {
		t.Errorf("the error should explain why it cannot choose: %v", err)
	}
}

func TestTwoVariablesAgreeingIsFine(t *testing.T) {
	s := schema(t)
	set := settings.NewSet(s)
	if err := s.ApplyEnv(set, []string{
		"MCPX_LOGGING_LEVEL=debug",
		"MCPX_LOG_LEVEL=debug",
	}); err != nil {
		t.Fatalf("agreement is not a conflict: %v", err)
	}
}

func TestPrecedenceRunsDefaultThenFileThenEnvThenFlag(t *testing.T) {
	s := schema(t)
	set := settings.NewSet(s)
	if got := set.String("logging.level"); got != "info" {
		t.Fatalf("default should be info, got %q", got)
	}
	_ = s.ApplyFile(set, map[string]any{"logging": map[string]any{"level": "warn"}}, "/far/.mcpx.json", 0)
	if got := set.String("logging.level"); got != "warn" {
		t.Fatalf("file should beat default, got %q", got)
	}
	_ = s.ApplyFile(set, map[string]any{"logging": map[string]any{"level": "error"}}, "/near/.mcpx.json", 1)
	if got := set.String("logging.level"); got != "error" {
		t.Fatalf("nearer file should beat farther, got %q", got)
	}
	_ = s.ApplyEnv(set, []string{"MCPX_LOGGING_LEVEL=debug"})
	if got := set.String("logging.level"); got != "debug" {
		t.Fatalf("env should beat file, got %q", got)
	}
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(nopWriter{})
	b := s.Bind(fs, "run")
	_ = fs.Parse([]string{"--logging-level", "info"})
	if err := b.ApplyTo(set); err != nil {
		t.Fatal(err)
	}
	if got := set.String("logging.level"); got != "info" {
		t.Fatalf("flag should beat env, got %q", got)
	}
}

func TestAValueRemembersWhatItOverrode(t *testing.T) {
	// "Why is this not what my config says" is the most common configuration
	// question, and the answer is always in this list.
	s := schema(t)
	set := settings.NewSet(s)
	_ = s.ApplyFile(set, map[string]any{"logging": map[string]any{"level": "warn"}}, "/a/.mcpx.json", 0)
	_ = s.ApplyEnv(set, []string{"MCPX_LOGGING_LEVEL=debug"})

	v, _ := set.Value("logging.level")
	if v.Origin.Layer != settings.LayerEnv {
		t.Errorf("origin should be the environment, got %v", v.Origin)
	}
	if len(v.Shadowed) == 0 {
		t.Fatal("the overridden config value should be remembered")
	}
	if !strings.Contains(v.Shadowed[0].Detail, ".mcpx.json") {
		t.Errorf("the shadowed origin should name the file: %v", v.Shadowed)
	}
}

func TestAnInvalidValueIsRejectedWithTheSourceNamed(t *testing.T) {
	s := schema(t)
	set := settings.NewSet(s)
	err := s.ApplyEnv(set, []string{"MCPX_POOL_IDLE_TIMEOUT=forever"})
	if err == nil {
		t.Fatal("a value that is not a duration should be refused")
	}
	if !strings.Contains(err.Error(), "MCPX_POOL_IDLE_TIMEOUT") {
		t.Errorf("the error should say where the bad value came from: %v", err)
	}
}

func TestBareFlagUsesItsImpliedValue(t *testing.T) {
	s := schema(t)
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(nopWriter{})
	b := s.Bind(fs, "run")
	if err := fs.Parse([]string{"--log-source", "run"}); err != nil {
		t.Fatal(err)
	}
	set := settings.NewSet(s)
	if err := b.ApplyTo(set); err != nil {
		t.Fatal(err)
	}
	if got := set.String("logging.source"); got != "all" {
		t.Errorf("a bare --log-source should mean all, got %q", got)
	}
	if fs.Arg(0) != "run" {
		t.Errorf("the bare flag should not have swallowed the next argument, got %q", fs.Arg(0))
	}
}

func TestNullSplicesInWhatTheLowerLayerGave(t *testing.T) {
	got := settings.Splice([]string{"./mine", settings.NullMarker, "../last"},
		[]string{"/usual/a", "/usual/b"})
	want := []string{"./mine", "/usual/a", "/usual/b", "../last"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestAListWithoutNullReplacesOutright(t *testing.T) {
	got := settings.Splice([]string{"./only"}, []string{"/usual"})
	if len(got) != 1 || got[0] != "./only" {
		t.Errorf("a list with no marker should replace: %v", got)
	}
}

func TestByteSizesAcceptBothDecimalAndBinarySuffixes(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int64
	}{
		{"1024", 1024}, {"16MB", 16_000_000}, {"16MiB", 16 << 20},
		{"1G", 1 << 30}, {"2.5MB", 2_500_000},
	} {
		got, err := settings.ParseBytes(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParseBytes(%q) = %d, %v; want %d", c.in, got, err, c.want)
		}
	}
}

func TestRequirementsCatchASettingThatWouldBeSilentlyInert(t *testing.T) {
	s, err := settings.New([]settings.Setting{
		{Path: "a.on", Kind: settings.KindBool, Default: "false", Name: "A", Short: "a"},
		{
			Path: "a.detail", Kind: settings.KindString, Default: "", Name: "D", Short: "d",
			Requires: []settings.Requirement{{
				Path: "a.on", Equals: "true",
				Because: "the detail is only read when the feature is on",
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	set := settings.NewSet(s)
	_ = set.Apply("a.detail", "x", settings.Origin{Layer: settings.LayerFlag, Detail: "--a-detail"})
	if err := set.CheckRequirements(); err == nil {
		t.Fatal("setting a value that cannot take effect should be reported up front")
	} else if !strings.Contains(err.Error(), "only read when") {
		t.Errorf("the reason should be in the message: %v", err)
	}
}

func TestPlumbingIsHiddenFromOrdinaryHelpButStillDescribed(t *testing.T) {
	s := schema(t)
	plain := s.Describe(false)
	full := s.Describe(true)
	if strings.Contains(plain, "plumbing.") {
		t.Error("plumbing should not clutter ordinary help")
	}
	if !strings.Contains(full, "plumbing.allowTsJsOverlap") {
		t.Error("plumbing should still be discoverable when asked for")
	}
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }
