package settings_test

import (
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/settings"
)

// setterSources is where each setter's code lives. A variable written from
// anywhere else is written by a part of mcpx the contract does not know
// about, and fails the test until it is named.
var setterSources = map[settings.Setter]string{
	settings.SetByCLI:      "internal/cli/",
	settings.SetByExec:     "internal/execsvc/",
	settings.SetByConsumer: "internal/daemon/",
	settings.SetByRunner:   "internal/runner/",
	settings.SetByPlugin:   "plugin/opencode/",
}

func setterOf(file string) (settings.Setter, bool) {
	for s, dir := range setterSources {
		if strings.HasPrefix(file, dir) {
			return s, true
		}
	}
	return "", false
}

func contract() map[string]settings.EnvVar {
	out := map[string]settings.EnvVar{}
	for _, v := range settings.ScriptEnv() {
		out[v.Name] = v
	}
	return out
}

func TestScriptEnvIsWellFormed(t *testing.T) {
	sch, err := settings.New(settings.Registry())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, v := range settings.ScriptEnv() {
		if seen[v.Name] {
			t.Errorf("%s is declared twice", v.Name)
		}
		seen[v.Name] = true
		if !envNameRE.MatchString(v.Name) {
			t.Errorf("%q is not an MCPX_ variable", v.Name)
		}
		if v.Meaning == "" || v.ReadBy == "" || len(v.SetBy) == 0 {
			t.Errorf("%s does not say what it is, who sets it and who reads it", v.Name)
		}
		for _, s := range v.SetBy {
			if _, ok := setterSources[s]; !ok {
				t.Errorf("%s names a setter %q with no sources", v.Name, s)
			}
		}
		// A variable that is also a setting says so, and names the right one:
		// a child that is mcpx will read it as that setting whether or not
		// the table admits it.
		decl, owned := sch.ByEnv(v.Name)
		switch {
		case owned && v.Setting != decl.Path:
			t.Errorf("%s is the setting %s, but the table says %q", v.Name, decl.Path, v.Setting)
		case !owned && v.Setting != "":
			t.Errorf("%s claims to be the setting %s, which does not read it", v.Name, v.Setting)
		}
	}
}

// TestTheScriptEnvironmentIsWhatTheCodeSets holds the table to the code in
// both directions, per setter. A name written and not declared is an
// interface nobody documented; a setter declared and not writing is a
// document describing an interface that does not exist -- the drift that
// left the docs naming MCPX_SESSION and MCPX_CALL for what the plugin sets.
func TestTheScriptEnvironmentIsWhatTheCodeSets(t *testing.T) {
	root := repoRoot(t)
	goScan := scanGoEnv(t, root)
	pluginSets, _ := scanPluginEnv(t, root)

	got := map[string]map[settings.Setter][]string{}
	var unattributed []string
	for _, s := range append(append([]envSite{}, goScan.sets...), pluginSets...) {
		setter, ok := setterOf(s.file)
		if !ok {
			unattributed = append(unattributed, s.name+" at "+s.at()+" ("+s.how+")")
			continue
		}
		if got[s.name] == nil {
			got[s.name] = map[settings.Setter][]string{}
		}
		got[s.name][setter] = append(got[s.name][setter], s.at())
	}
	sort.Strings(unattributed)
	for _, u := range unattributed {
		t.Errorf("%s is written into a child's environment by code that is none of "+
			"the setters in ScriptEnv; add a Setter for it", u)
	}

	table := contract()
	names := make([]string, 0, len(got))
	for n := range got {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		entry, declared := table[name]
		for setter, at := range got[name] {
			if !declared {
				t.Errorf("%s is set by %s (%s) and ScriptEnv does not declare it",
					name, setter, strings.Join(at, ", "))
				continue
			}
			if !hasSetter(entry.SetBy, setter) {
				t.Errorf("%s is set by %s (%s), and ScriptEnv does not list it as a setter",
					name, setter, strings.Join(at, ", "))
			}
		}
	}
	for _, v := range settings.ScriptEnv() {
		for _, setter := range v.SetBy {
			if len(got[v.Name][setter]) == 0 {
				t.Errorf("ScriptEnv says %s sets %s, and nothing under %s does",
					setter, v.Name, setterSources[setter])
			}
		}
	}
}

func hasSetter(list []settings.Setter, s settings.Setter) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// readers are the consumers the ReadBy column names, with how each is found
// in the code. The column is checked against them so that it cannot claim a
// reader that does not read, or omit one that does.
var readerMarkers = []struct {
	marker string
	what   string
}{
	{"the generated client", "the TypeScript mcpx generates"},
	{"`mcpx` run inside", "mcpx's own Go code, by name or as a setting"},
	{"the plugin", "the opencode plugin"},
}

func TestTheReadByColumnIsWhatTheCodeReads(t *testing.T) {
	root := repoRoot(t)
	goScan := scanGoEnv(t, root)
	_, pluginReads := scanPluginEnv(t, root)

	byReader := map[string]map[string]string{}
	add := func(marker, name, at string) {
		if byReader[marker] == nil {
			byReader[marker] = map[string]string{}
		}
		if _, ok := byReader[marker][name]; !ok {
			byReader[marker][name] = at
		}
	}
	for _, s := range goScan.template {
		add("the generated client", s.name, s.at())
	}
	for _, s := range goScan.reads {
		// A lookup by call outside the settings package is mcpx reading its
		// own environment. An index is a map mcpx built, not its
		// environment -- the exec service consulting what it is about to set.
		if strings.HasPrefix(s.how, "call") && !strings.HasPrefix(s.file, "internal/settings/") {
			add("`mcpx` run inside", s.name, s.at())
		}
	}
	for _, s := range pluginReads {
		add("the plugin", s.name, s.at())
	}

	// The scan finding nothing would make every claim below vacuous. These
	// two are pinned by e2e tests, so their absence means the scanner broke.
	if byReader["the generated client"]["MCPX_SESSION"] == "" {
		t.Fatal("the scan found no read of MCPX_SESSION by the generated client, which " +
			"internal/e2e/runtime_test.go proves exists; the scanner is broken")
	}
	if byReader["`mcpx` run inside"]["MCPX_SESSION_ID"] == "" {
		t.Fatal("the scan found no read of MCPX_SESSION_ID by mcpx itself; the scanner is broken")
	}

	for _, v := range settings.ScriptEnv() {
		for _, r := range readerMarkers {
			claims := strings.Contains(v.ReadBy, r.marker)
			at, reads := byReader[r.marker][v.Name]
			if r.marker == "`mcpx` run inside" && v.Setting != "" {
				// Read through the settings, which is the right way and does
				// not show up as a lookup by name.
				reads = true
			}
			switch {
			case claims && !reads:
				t.Errorf("%s says %s reads it, and nothing in %s does", v.Name, r.marker, r.what)
			case !claims && reads:
				t.Errorf("%s is read by %s (%s), and its ReadBy does not say so", v.Name, r.marker, at)
			}
		}
	}
}

// TestTheGeneratedClientReadsOnlyWhatItIsGiven: the client runs inside the
// script, so any variable it reads is one mcpx promised to set. One read and
// never declared is a client depending on something nobody provides.
func TestTheGeneratedClientReadsOnlyWhatItIsGiven(t *testing.T) {
	table := contract()
	for _, s := range scanGoEnv(t, repoRoot(t)).template {
		if _, ok := table[s.name]; !ok {
			t.Errorf("the generated TypeScript reads %s (%s), which ScriptEnv does not declare",
				s.name, s.at())
		}
	}
}

var update = flag.Bool("update", false, "rewrite generated documentation")

const (
	envDocBegin = "<!-- BEGIN GENERATED: settings.ScriptEnv(); `go test ./internal/settings -run TestTheEnvironmentDocIsCurrent -update` -->"
	envDocEnd   = "<!-- END GENERATED -->"
)

// TestTheEnvironmentDocIsCurrent regenerates the table in
// docs/environment.md and fails when the file differs. Stronger than
// checking the names appear: a meaning or a setter can change too.
func TestTheEnvironmentDocIsCurrent(t *testing.T) {
	path := filepath.Join(repoRoot(t), "docs", "environment.md")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the environment contract should be documented: %v", err)
	}
	doc := string(b)
	i := strings.Index(doc, envDocBegin)
	j := strings.Index(doc, envDocEnd)
	if i < 0 || j < i {
		t.Fatalf("docs/environment.md has lost its generated-section markers")
	}
	want := doc[:i] + envDocBegin + "\n\n" + renderScriptEnv() + "\n" + doc[j:]
	if want == doc {
		return
	}
	if *update {
		if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Errorf("docs/environment.md is stale; run\n  go test ./internal/settings -run TestTheEnvironmentDocIsCurrent -update")
}

func renderScriptEnv() string {
	var b strings.Builder
	b.WriteString("| variable | set by | when | what it is | read by |\n")
	b.WriteString("| --- | --- | --- | --- | --- |\n")
	for _, v := range settings.ScriptEnv() {
		var by []string
		for _, s := range v.SetBy {
			by = append(by, s.Describe())
		}
		when := v.When
		if when == "" {
			when = "always"
		}
		read := v.ReadBy
		if v.Setting != "" {
			read += "; also the setting `" + v.Setting + "`"
		}
		b.WriteString("| `" + v.Name + "` | " + cell(strings.Join(by, "; ")) + " | " +
			cell(when) + " | " + cell(v.Meaning) + " | " + cell(read) + " |\n")
	}
	return b.String()
}

func cell(s string) string { return strings.ReplaceAll(s, "|", `\|`) }
