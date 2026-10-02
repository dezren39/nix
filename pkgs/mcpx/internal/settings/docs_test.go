package settings_test

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/settings"
)

// update is declared in scriptenv_test.go, which regenerates docs/environment.md
// the same way.

const configurationPath = "../../docs/configuration.md"

// The markers separate the prose, which is written, from the inventory, which
// is not. Spelled the way scriptenv_test.go spells them, so the two generated
// documents look the same to a reader who opens both.
const (
	tableBegin = "<!-- BEGIN GENERATED: settings.Registry(); `go test ./internal/settings -run TestTheDocumentationListsEverySetting -update` -->"
	tableEnd   = "<!-- END GENERATED -->"
)

// TestTheDocumentationListsEverySetting keeps docs/configuration.md honest.
//
// A settings inventory written by hand is wrong within two releases, and
// wrong documentation about configuration is worse than none: it sends people
// looking for a knob that does not exist and lets them miss the one that
// does.
//
// The document said it was generated and it was not. Nothing produced it, and
// what did exist -- a Contains() check for the path, the variable and the
// primary flag somewhere in the file -- passed while the table had drifted:
// `exec.output` was missing its `--output` alias and `search.limit` was
// missing `--n`, so two real flags were invisible to anyone reading the
// inventory rather than the source. A Contains() check also cannot see a
// wrong kind, a wrong default, a wrong scope, a row in the wrong section, or
// a row for a setting that no longer exists.
//
// So it is generated now, the way docs/parity.md is: this test regenerates
// the section below the marker and fails when it differs.
func TestTheDocumentationListsEverySetting(t *testing.T) {
	b, err := os.ReadFile(configurationPath)
	if err != nil {
		t.Fatalf("the configuration inventory should exist: %v", err)
	}
	doc := string(b)
	i := strings.Index(doc, tableBegin)
	j := strings.Index(doc, tableEnd)
	if i < 0 || j < i {
		t.Fatalf("docs/configuration.md has lost the markers that separate its "+
			"prose from its generated inventory (%s ... %s)", tableBegin, tableEnd)
	}
	s, err := settings.New(settings.Registry())
	if err != nil {
		t.Fatal(err)
	}
	want := doc[:i+len(tableBegin)] + "\n\n" + settingsTable(s.All()) + doc[j:]
	if *update {
		if err := os.WriteFile(configurationPath, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if doc != want {
		t.Errorf("docs/configuration.md no longer matches settings.Registry(); regenerate it with\n" +
			"  go test ./internal/settings -run TestTheDocumentationListsEverySetting -update")
	}
}

// settingsTable renders every setting, grouped by the first segment of its
// path, which is how someone looks one up: they know the area, not the leaf.
func settingsTable(all []settings.Setting) string {
	groups := map[string][]settings.Setting{}
	for _, set := range all {
		groups[strings.SplitN(set.Path, ".", 2)[0]] = append(groups[strings.SplitN(set.Path, ".", 2)[0]], set)
	}
	names := make([]string, 0, len(groups))
	for n := range groups {
		names = append(names, n)
	}
	sort.Strings(names)

	var b strings.Builder
	for _, n := range names {
		fmt.Fprintf(&b, "### %s\n\n", n)
		b.WriteString("| setting | kind | default | scope | hot | flag | variable | governs |\n" +
			"| --- | --- | --- | --- | --- | --- | --- | --- |\n")
		for _, set := range groups[n] {
			def := "`" + set.Default + "`"
			if set.Default == "" {
				// An empty string is a value; an empty cell reads as an
				// omission. The two were spelled differently in the
				// hand-written table, which is how it was clear nothing
				// generated it.
				def = "*(empty)*"
			}
			flags := []string{"`--" + set.FlagName() + "`"}
			for _, a := range set.FlagAliases {
				flags = append(flags, "`--"+a+"`")
			}
			envs := []string{"`" + set.EnvName() + "`"}
			for _, a := range set.EnvAliases {
				envs = append(envs, "`"+a+"`")
			}
			governs := set.Short
			if len(set.EnumAliases) > 0 {
				// Only where aliases exist: the canonical names are what a
				// reader should write, and the aliases are what they may
				// find in an older file.
				governs += "; one of " + set.EnumWords()
			}
			if set.Plumbing {
				governs = "*(plumbing)* " + governs
			}
			fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s | %s | %s | %s |\n",
				set.Path, set.Kind, def, set.Scope, yesNo(set.Hot),
				strings.Join(flags, ", "), strings.Join(envs, ", "),
				strings.ReplaceAll(governs, "|", "\\|"))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// TestTheDocumentationDescribesTheScopes fails if a scope is added without
// saying what it means, which would make the column in the table unreadable.
// The scopes are explained in the prose above the markers, which is written by
// hand, so nothing else would notice.
func TestTheDocumentationDescribesTheScopes(t *testing.T) {
	b, err := os.ReadFile(configurationPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, sc := range []settings.Scope{
		settings.ScopeDaemon, settings.ScopeClient,
		settings.ScopeCall, settings.ScopePlugin,
	} {
		if !strings.Contains(string(b), "**"+sc.String()+"**") {
			t.Errorf("the %s scope is not explained in docs/configuration.md", sc)
		}
	}
}
