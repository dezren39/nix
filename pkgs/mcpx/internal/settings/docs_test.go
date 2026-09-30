package settings_test

import (
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/settings"
)

// TestTheDocumentationListsEverySetting keeps docs/configuration.md honest.
//
// A settings inventory written by hand is wrong within two releases, and
// wrong documentation about configuration is worse than none: it sends people
// looking for a knob that does not exist and lets them miss the one that
// does. Rather than trust anybody to remember, the table is generated and
// this fails when it drifts.
func TestTheDocumentationListsEverySetting(t *testing.T) {
	b, err := os.ReadFile("../../docs/configuration.md")
	if err != nil {
		t.Fatalf("the configuration inventory should exist: %v", err)
	}
	doc := string(b)

	s, err := settings.New(settings.Registry())
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	for _, set := range s.All() {
		// Backticked, so a path that only appears inside a sentence about
		// something else does not count as documented.
		if !strings.Contains(doc, "`"+set.Path+"`") {
			missing = append(missing, set.Path)
			continue
		}
		if !strings.Contains(doc, "`"+set.EnvName()+"`") {
			missing = append(missing, set.Path+" (variable "+set.EnvName()+")")
		}
		// The flag column was not checked at all, so a stale spelling in it
		// was invisible -- which mattered the moment name derivation changed
		// and --daemon-lease-t-t-l became --daemon-lease-ttl. Contains() only
		// proves the new name is present, so a row naming both the old and the
		// new would still pass; that is caught by the table being regenerated
		// rather than edited.
		if f := set.FlagName(); f != "" && !strings.Contains(doc, "`--"+f+"`") {
			missing = append(missing, set.Path+" (flag --"+f+")")
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("docs/configuration.md does not list:\n  %s\n"+
			"regenerate the table at the end of it from settings.Registry()",
			strings.Join(missing, "\n  "))
	}
}

// TestTheDocumentationDescribesTheScopes fails if a scope is added without
// saying what it means, which would make the column in the table unreadable.
func TestTheDocumentationDescribesTheScopes(t *testing.T) {
	b, err := os.ReadFile("../../docs/configuration.md")
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
