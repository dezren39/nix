package settings_test

import (
	"flag"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/settings"
)

// protocolNames is the table the user chose, written out here rather than
// read from the registry: a test that iterated the registry's own alias table
// would pass after an alias was dropped from it.
var protocolNames = map[string]string{
	"prefer-discover": "prefer-discover", "modern": "prefer-discover", "prefer-modern": "prefer-discover",
	"prefer-stateless": "prefer-discover", "prefer-newest": "prefer-discover",
	"prefer-initialize": "prefer-initialize", "legacy": "prefer-initialize", "prefer-legacy": "prefer-initialize",
	"prefer-session": "prefer-initialize", "prefer-oldest": "prefer-initialize",
	"force-discover": "force-discover", "force-modern": "force-discover", "force-stateless": "force-discover",
	"force-initialize": "force-initialize", "force-legacy": "force-initialize", "force-session": "force-initialize",
	"follow": "follow",
}

// TestEveryProtocolAliasResolvesToItsCanonicalNameFromEverySource reads each
// spelling through the config file, the environment, the flag and a runtime
// change, and requires every reader to see only the canonical name.
func TestEveryProtocolAliasResolvesToItsCanonicalNameFromEverySource(t *testing.T) {
	s := schema(t)
	const path = "upstream.protocol"
	sources := map[string]func(*settings.Set, string) error{
		"file": func(set *settings.Set, v string) error {
			return s.ApplyFile(set, map[string]any{"upstream": map[string]any{"protocol": v}}, "/x/.mcpx.json", 0)
		},
		"env": func(set *settings.Set, v string) error {
			return s.ApplyEnv(set, []string{"MCPX_UPSTREAM_PROTOCOL=" + v})
		},
		"flag": func(set *settings.Set, v string) error {
			fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
			b := s.Bind(fs, "daemon")
			if err := fs.Parse([]string{"--upstream-protocol", v}); err != nil {
				return err
			}
			return b.ApplyTo(set)
		},
		"runtime": func(set *settings.Set, v string) error { return set.SetRuntime(path, v) },
	}
	for src, apply := range sources {
		for given, want := range protocolNames {
			// Case is not significant for an enum, so an upper-cased alias
			// must normalise too.
			for _, spelling := range []string{given, strings.ToUpper(given)} {
				set := settings.NewSet(s)
				if err := apply(set, spelling); err != nil {
					t.Errorf("%s %q: refused: %v", src, spelling, err)
					continue
				}
				if got := set.String(path); got != want {
					t.Errorf("%s %q: resolved to %q, want %q", src, spelling, got, want)
				}
			}
		}
	}
}

func TestAnUnknownProtocolIsRefusedListingTheNamesAndAliases(t *testing.T) {
	s := schema(t)
	set := settings.NewSet(s)
	err := s.ApplyEnv(set, []string{"MCPX_UPSTREAM_PROTOCOL=newest"})
	if err == nil {
		t.Fatal("an unknown protocol should be refused")
	}
	for name := range protocolNames {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("the refusal should list %q: %v", name, err)
		}
	}
}

func TestAnAmbiguousEnumAliasIsRejectedAtStartup(t *testing.T) {
	base := settings.Setting{Path: "x.y", Kind: settings.KindEnum, Default: "a", Name: "x", Short: "x",
		Enum: []string{"a", "b"}}
	for name, aliases := range map[string]map[string][]string{
		"alias names two values": {"a": {"old"}, "b": {"old"}},
		"alias is a value":       {"a": {"b"}},
		"alias of undeclared":    {"c": {"old"}},
		"alias differs in case":  {"a": {"B"}},
	} {
		set := base
		set.EnumAliases = aliases
		if _, err := settings.New([]settings.Setting{set}); err == nil {
			t.Errorf("%s: should be rejected", name)
		}
	}
	ok := base
	ok.EnumAliases = map[string][]string{"a": {"old"}}
	if _, err := settings.New([]settings.Setting{ok}); err != nil {
		t.Errorf("a well-formed alias table was rejected: %v", err)
	}
}
