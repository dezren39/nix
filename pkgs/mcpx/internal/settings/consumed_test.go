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
// TestNoUndeclaredEnvRead below closes the other half: reading a variable
// the registry owns with os.Getenv is how a setting becomes env-only, which
// is the same bug facing the other way, and reading one nothing declares is
// how a knob becomes undiscoverable.
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
	`(?:Plumbing|plumbingBool|phaseValues|Clamp)\("([a-zA-Z0-9_.]+)"`)

// unreadAllowed lists settings that cannot be read through an accessor, with
// the reason. An entry here is a claim somebody has to defend in review;
// there is deliberately no way to silence this test in bulk.
var unreadAllowed = map[string]string{
	// This entry used to say the setting was "read by config.SearchPath from
	// MCPX_PATHS_CONFIG directly". Nothing reads MCPX_PATHS_CONFIG; the
	// search path honours MCPX_CONFIG, which is paths.configFile below.
	"paths.config": "circular by construction (#5): the config search path is " +
		"what produces the settings, so the settings cannot decide it. It is " +
		"declared so that `mcpx config --schema` names the thing. Nothing reads " +
		"it yet -- not MCPX_PATHS_CONFIG, not --paths-config -- which #5 owns.",
	"paths.configFile": "a bootstrap setting: it names the file the settings are " +
		"read from, so config.SearchPathFrom reads MCPX_CONFIG before any Set " +
		"exists, and the global --config reaches App.ConfigPath the same way.",
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
	// A ceiling is read by every read of a setting it clamps: Set.Value
	// lowers the clamped value to it, so reading one reads both.
	for _, set := range settings.Registry() {
		if set.ClampedBy != "" && len(readPaths[set.Path]) > 0 {
			readPaths[set.ClampedBy] = append(readPaths[set.ClampedBy],
				"ClampedBy of "+set.Path)
		}
	}

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

// rawReadAllowed lists the variables Go code outside internal/settings may
// look up by name even though they are not a script-environment contract
// variable, with the reason. Every one is a bootstrap: the lookup happens
// before there is a resolved Set to ask. An entry nothing needs any more
// fails the test, so the list can only shrink by being wrong.
var rawReadAllowed = map[string]string{
	"MCPX_STATE_DIR": "daemon.ResolvePaths runs before any config file has " +
		"been found, so there is no Set to read. The setting paths.state is " +
		"folded in afterwards, in adoptSettings.",
	"MCPX_CACHE_DIR": "as MCPX_STATE_DIR.",
	"MCPX_PATHS_SCRIPTS": "script resolution is reached from the daemon, " +
		"which has no App; the Set is preferred when there is one (see " +
		"cli/scripts.go scriptPath).",
	"MCPX_CONFIG": "paths.configFile names the file the settings are read " +
		"from, so config.SearchPathFrom cannot ask them.",
}

// TestNoUndeclaredEnvRead is the guard facing inward, and the reason #172
// existed: TestNoHandRolledSettingEnv, which this replaces, only objected to
// a lookup of a variable the registry owned. Forty-four the registry had
// never heard of were read by hand and invisible to it -- a user could find
// MCPX_CONFIG, MCPX_TRACE or MCPX_PERMISSIONS only by reading the source.
//
// Now every lookup outside internal/settings must be one of:
//   - a variable in ScriptEnv, which a parent process -- mcpx itself, or the
//     plugin -- sets for this one. Reading it is the other half of that
//     contract, and the contract documents it;
//   - a bootstrap variable on rawReadAllowed, with the reason.
//
// A registry-owned variable is refused even when it is also in ScriptEnv:
// reading MCPX_LOG_LEVEL by name would ignore the file and the flag.
func TestNoUndeclaredEnvRead(t *testing.T) {
	root := repoRoot(t)
	sch, err := settings.New(settings.Registry())
	if err != nil {
		t.Fatal(err)
	}
	table := contract()
	scan := scanGoEnv(t, root)

	used := map[string]bool{}
	var bad []string
	for _, r := range scan.reads {
		if strings.HasPrefix(r.file, "internal/settings/") {
			continue
		}
		_, allowed := rawReadAllowed[r.name]
		decl, owned := sch.ByEnv(r.name)
		_, contracted := table[r.name]
		switch {
		case owned && allowed:
			used[r.name] = true
		case owned:
			bad = append(bad, r.at()+" reads "+r.name+" by name ("+r.how+"); it is "+
				decl.Path+", so this ignores the config file and --"+decl.FlagName())
		case contracted:
		case allowed:
			used[r.name] = true
		default:
			bad = append(bad, r.at()+" reads "+r.name+" ("+r.how+"), which no setting "+
				"and no ScriptEnv entry declares")
		}
	}
	for _, d := range scan.dynamic {
		bad = append(bad, d.at()+" looks up a variable whose name is built at run "+
			"time from MCPX_ ("+d.how+"); no table can be checked against that")
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Errorf("environment read by hand:\n  %s\n\n"+
			"Declare it: a knob belongs in the registry, read from the resolved "+
			"Set; a value a parent process hands this one belongs in ScriptEnv. "+
			"If the read genuinely happens before a Set exists, add it to "+
			"rawReadAllowed with the reason.",
			strings.Join(bad, "\n  "))
	}

	var stale []string
	for name, reason := range rawReadAllowed {
		if !used[name] {
			stale = append(stale, name+" ("+reason+")")
		}
	}
	sort.Strings(stale)
	for _, s := range stale {
		t.Errorf("rawReadAllowed excuses %s, and no read needs it; delete the entry", s)
	}
}

// TestThePluginReadsNoUndeclaredSetting is the guard facing the other way.
//
// The registry exists so that `mcpx settings` can answer what the plugin will
// do without anybody reading TypeScript. A variable the plugin honours and
// the registry has never heard of breaks that promise silently: it works, so
// nobody notices, and the inventory is quietly incomplete. Four were found
// this way, and two more (MCPX_PLUGIN_HEADLESS, MCPX_PLUGIN_DAEMON_TOOLS)
// sat on an allowlist until they were declared with an "auto" default.
//
// The only other thing the plugin may read is a ScriptEnv variable -- the
// endpoint or socket mcpx gives a script -- which the contract documents.
func TestThePluginReadsNoUndeclaredSetting(t *testing.T) {
	root := repoRoot(t)
	sch, err := settings.New(settings.Registry())
	if err != nil {
		t.Fatal(err)
	}
	table := contract()
	_, reads := scanPluginEnv(t, root)
	if len(reads) == 0 {
		t.Fatal("the scan found no environment reads in the plugin at all; it is broken")
	}
	seen := map[string]bool{}
	var undeclared []string
	for _, r := range reads {
		if seen[r.name] {
			continue
		}
		seen[r.name] = true
		if _, ok := sch.ByEnv(r.name); ok {
			continue
		}
		if _, ok := table[r.name]; ok {
			continue
		}
		undeclared = append(undeclared, r.name+" ("+r.at()+")")
	}
	sort.Strings(undeclared)
	if len(undeclared) > 0 {
		t.Errorf("the plugin honours variables nothing declares:\n  %s\n\n"+
			"Add them to pluginSettings() so `mcpx settings` can report them.",
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
			// vendor/ is other people's code, and in the nix build it is
			// there: buildGoModule copies the vendored modules into the
			// source tree, and reading all of modernc.org/sqlite line by line
			// put this package past go test's ten minutes on a loaded builder.
			if d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "vendor" {
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
