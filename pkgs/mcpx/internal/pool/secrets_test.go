package pool

import (
	"reflect"
	"testing"

	"github.com/dezren39/mcpx/internal/config"
)

// A child is handed the runtime values of the secrets its own configuration
// requires, and nothing else: a server is not given every secret the daemon
// holds.
func TestRuntimeEnvIsOnlyWhatTheServerRequires(t *testing.T) {
	held := map[string]string{"NEEDED": "n-value", "NOT_NEEDED": "x-value"}
	cfg := &config.Resolved{
		Server:          &config.Server{Name: "demo", Command: "/bin/true"},
		RequiresSecrets: []string{"NEEDED", "MISSING"},
		Runtime: func(name string) (string, bool) {
			v, ok := held[name]
			return v, ok
		},
	}
	p := New(cfg)
	want := map[string]string{"NEEDED": "n-value"}
	if got := p.runtimeEnv(); !reflect.DeepEqual(got, want) {
		t.Fatalf("runtimeEnv = %v; want only the required name that has a value: %v", got, want)
	}
}

func TestRuntimeEnvIsEmptyWithoutAStore(t *testing.T) {
	cfg := &config.Resolved{
		Server:          &config.Server{Name: "demo", Command: "/bin/true"},
		RequiresSecrets: []string{"NEEDED"},
	}
	if got := New(cfg).runtimeEnv(); got != nil {
		t.Fatalf("with no store there is nothing to add: %v", got)
	}
}

// The value a server's configuration reads prefers the runtime store, and the
// daemon's environment is what is left when the store has nothing.
func TestVariablePrefersTheRuntimeStore(t *testing.T) {
	t.Setenv("POOL_VAR_TEST", "from-env")
	held := map[string]string{}
	cfg := &config.Resolved{
		Server: &config.Server{Name: "demo", Command: "/bin/true"},
		Runtime: func(name string) (string, bool) {
			v, ok := held[name]
			return v, ok
		},
	}
	p := New(cfg)
	if got := p.variable("POOL_VAR_TEST"); got != "from-env" {
		t.Fatalf("with nothing held, the environment answers: %q", got)
	}
	held["POOL_VAR_TEST"] = "from-runtime"
	if got := p.variable("POOL_VAR_TEST"); got != "from-runtime" {
		t.Fatalf("a held value answers first: %q", got)
	}
}
