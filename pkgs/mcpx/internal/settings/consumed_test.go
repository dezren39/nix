package settings_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/settings"
)

// The guard against "declared but never read".
//
// A setting is a promise with three faces: a config key, an MCPX_ variable
// and a flag. Declaring one costs a struct literal; honouring one costs a
// read at the place the decision is made, and the two are in different files
// written weeks apart. Every time that gap has opened, the symptom was the
// same -- the flag parsed, the help described it, and nothing happened. It
// happened to `--typecheck` (#66), to five exec flags at once (#75), to MCP
// sampling (#39), and, when this test was written, to fifty-four settings.
//
// Two things follow from that history, and they are why this is a source scan
// rather than a field on Setting.
//
// First, a `ReadBy: "daemon"` note would live in the same struct literal as
// the declaration. Whoever writes the declaration writes the note in the same
// keystroke, believing both; nothing ever checks the second half. A scan
// asks the code, which cannot be optimistic.
//
// Second -- and this is the part a simpler scan misses -- a setting can be
// half-read. `pool.max` was honoured from a configuration file and ignored
// from MCPX_POOL_MAX and --pool-max, because the file layer reached it
// through config.Config.Pool while the other two only ever landed in the
// resolved Set. Grepping for the path string anywhere would have called that
// live. So the requirement is narrower: the path must appear as the argument
// to a settings accessor on a resolved Set. That is not a stylistic
// preference. The Set is the only reader in the program that has seen all
// three layers; os.Getenv has seen one, a config struct field has seen one,
// and a hand-written flag variable has seen one. Requiring the accessor is
// requiring the whole promise.
//
// TestNoHandRolledSettingEnv below closes the other half: reading a variable
// the registry owns with os.Getenv is how a setting becomes env-only, which
// is the same bug facing the other way.
//
// What a scan cannot see is a value read, passed along, and then ignored.
// registry.pageSize passed this test while the only code that looked at it
// sat behind `if limit <= 0` (#179). Reaching a use rather than a parameter
// is a data-flow question, so it is asserted where behaviour can be observed
// instead: the request count in registry/paging_test.go and
// e2e/registry_test.go changes with the page size.

// accessorRead matches a read through a resolved Set: set.Bool("x.y"),
// a.Settings().Duration("x.y"), cs.Int("x.y"), s.set.String("x.y").
//
// Value is included because a caller that wants provenance rather than the
// value alone still reads the setting.
var accessorRead = regexp.MustCompile(
	`\.(?:Bool|String|Int|Duration|Bytes|List|Value|Given|AboveFile|ListAboveFile)\("([a-zA-Z0-9_.]+)"\)`)

// readViaHelper matches a path handed to a function that reads it for you --
// App.Plumbing, plumbingBool. The helper itself ends in an accessor, so the
// promise is kept; the call site is just one indirection away.
var readViaHelper = regexp.MustCompile(
	`(?:Plumbing|plumbingBool|phaseValues)\("([a-zA-Z0-9_.]+)"`)

// unreadAllowed lists settings that cannot be read through an accessor, with
// the reason. An entry here is a claim somebody has to defend in review;
// there is deliberately no way to silence this test in bulk.
var unreadAllowed = map[string]string{
	"paths.config": "circular by construction (#5): the config search path is " +
		"what produces the settings, so the settings cannot decide it. It is " +
		"declared so that `mcpx config --schema` names the thing, and read by " +
		"config.SearchPath from MCPX_PATHS_CONFIG directly.",
}

func TestEverySettingIsReadSomewhere(t *testing.T) {
	root := repoRoot(t)
	readPaths := map[string][]string{}

	walkGo(t, root, func(path string, body string) {
		rel, _ := filepath.Rel(root, path)
		for i, line := range strings.Split(body, "\n") {
			for _, re := range []*regexp.Regexp{accessorRead, readViaHelper} {
				for _, m := range re.FindAllStringSubmatch(line, -1) {
					readPaths[m[1]] = append(readPaths[m[1]],
						rel+":"+itoa(i+1))
				}
			}
		}
	})

	pluginSrc := pluginSources(t, root)

	var missing []string
	for _, set := range settings.Registry() {
		if reason, ok := unreadAllowed[set.Path]; ok {
			if len(readPaths[set.Path]) > 0 {
				t.Errorf("%s is on the allowlist but is read after all (%s); "+
					"delete the entry, the reason %q no longer holds",
					set.Path, readPaths[set.Path][0], reason)
			}
			continue
		}
		// A plugin-scoped setting is read by TypeScript, which cannot call
		// into Go. The contract between the two is the derived variable
		// name, so that is what gets checked -- in the plugin's own sources,
		// not in a README that could describe a knob nobody wired.
		if set.Scope == settings.ScopePlugin {
			if !strings.Contains(pluginSrc, set.EnvName()) {
				missing = append(missing, set.Path+
					": scope is plugin but "+set.EnvName()+
					" appears nowhere in plugin/**/*.ts")
			}
			continue
		}
		if len(readPaths[set.Path]) == 0 {
			missing = append(missing, set.Path+
				": nothing reads it through a settings accessor, so setting it "+
				"in a config file, in "+set.EnvName()+" or as --"+set.FlagName()+
				" does nothing")
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d setting(s) are declared and never read:\n  %s\n\n"+
			"Wire the code that makes the decision to read the resolved value, "+
			"or delete the setting. If it genuinely cannot be read through the "+
			"Set, add it to unreadAllowed with the reason.",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// envReadDirectly matches os.Getenv("MCPX_...") -- the spelling that makes a
// setting reachable from the environment and nowhere else.
var envReadDirectly = regexp.MustCompile(`os\.Getenv\("(MCPX_[A-Z0-9_]+)"\)`)

// envAllowed lists the variables the program may read by hand, with reasons.
var envAllowed = map[string]string{
	"MCPX_STATE_DIR": "daemon.ResolvePaths runs before any config file has " +
		"been found, so there is no Set to read. The setting paths.state is " +
		"folded in afterwards, in parseFlags.",
	"MCPX_CACHE_DIR": "as MCPX_STATE_DIR.",
	"MCPX_PATHS_SCRIPTS": "script resolution is reached from the daemon, " +
		"which has no App; the Set is preferred when there is one (see " +
		"cli/scripts.go scriptPath).",
}

func TestNoHandRolledSettingEnv(t *testing.T) {
	root := repoRoot(t)
	sch, err := settings.New(settings.Registry())
	if err != nil {
		t.Fatal(err)
	}
	var bad []string
	walkGo(t, root, func(path, body string) {
		rel, _ := filepath.Rel(root, path)
		if strings.HasPrefix(rel, "internal/settings/") {
			return
		}
		for i, line := range strings.Split(body, "\n") {
			for _, m := range envReadDirectly.FindAllStringSubmatch(line, -1) {
				name := m[1]
				if _, ok := envAllowed[name]; ok {
					continue
				}
				decl, owned := sch.ByEnv(name)
				if !owned {
					continue
				}
				bad = append(bad, rel+":"+itoa(i+1)+" reads "+name+
					" directly; it is "+decl.Path+
					", so this ignores the config file and --"+decl.FlagName())
			}
		}
	})
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Errorf("a registry-owned variable is read by hand:\n  %s\n\n"+
			"Read it from the resolved Set instead, which folds the file, the "+
			"variable and the flag. If the read genuinely happens before a Set "+
			"exists, add the variable to envAllowed with the reason.",
			strings.Join(bad, "\n  "))
	}
}

// pluginEnvRead matches env.MCPX_FOO and process.env.MCPX_FOO in TypeScript.
var pluginEnvRead = regexp.MustCompile(`env\.(MCPX_[A-Z0-9_]+)`)

// pluginEnvAllowed lists variables the plugin reads that are deliberately not
// settings, with the reason.
var pluginEnvAllowed = map[string]string{
	"MCPX_DAEMON_ENDPOINT": "daemon.endpoint, whose name the plugin shares " +
		"with the binary rather than owning a plugin-scoped copy.",
	"MCPX_ENDPOINT":  "written by mcpx itself into a script's environment, not a knob.",
	"MCPX_SOCKET":    "the socket override, read by daemon.socketPath before any Set exists.",
	"MCPX_STATE_DIR": "paths.state, shared with the binary for the same reason as the endpoint.",
	"MCPX_PLUGIN_HEADLESS": "the default is computed from whether the plugin is " +
		"on the main thread, so a declared default would be a number that is " +
		"right in one of the two realms the plugin runs in.",
	"MCPX_PLUGIN_DAEMON_TOOLS": "the default is whether tools are on *or* the " +
		"discovery was ambiguous, which is a decision made at boot from what " +
		"was found. Same objection as MCPX_PLUGIN_HEADLESS.",
}

// TestThePluginReadsNoUndeclaredSetting is the guard facing the other way.
//
// The registry exists so that `mcpx settings` can answer what the plugin will
// do without anybody reading TypeScript. A variable the plugin honours and
// the registry has never heard of breaks that promise silently: it works, so
// nobody notices, and the inventory is quietly incomplete. Four were found
// this way.
func TestThePluginReadsNoUndeclaredSetting(t *testing.T) {
	root := repoRoot(t)
	sch, err := settings.New(settings.Registry())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var undeclared []string
	for _, m := range pluginEnvRead.FindAllStringSubmatch(pluginSources(t, root), -1) {
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		if _, ok := pluginEnvAllowed[name]; ok {
			continue
		}
		if _, ok := sch.ByEnv(name); !ok {
			undeclared = append(undeclared, name)
		}
	}
	sort.Strings(undeclared)
	if len(undeclared) > 0 {
		t.Errorf("the plugin honours variables no setting declares:\n  %s\n\n"+
			"Add them to pluginSettings() so `mcpx settings` can report them, "+
			"or to pluginEnvAllowed with the reason they cannot be declared.",
			strings.Join(undeclared, "\n  "))
	}
}

// ---- helpers ----

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("expected the module root at %s: %v", root, err)
	}
	return root
}

// walkGo visits every non-test Go file outside internal/settings.
//
// Tests are excluded on purpose: a setting read only by a test that asserts
// it can be read is exactly the thing this is looking for.
func walkGo(t *testing.T, root string, fn func(path, body string)) {
	t.Helper()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if strings.HasPrefix(rel, "internal/settings"+string(filepath.Separator)) {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		fn(p, string(b))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func pluginSources(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	dir := filepath.Join(root, "plugin")
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		// Sources only. A README can describe a variable nothing reads, and
		// a test can assert one nothing sets.
		if !strings.HasSuffix(name, ".ts") && !strings.HasSuffix(name, ".tsx") {
			return nil
		}
		if strings.HasSuffix(name, ".test.ts") {
			return nil
		}
		body, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		b.Write(body)
		b.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatalf("reading the plugin sources: %v", err)
	}
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
