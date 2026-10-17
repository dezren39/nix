package config_test

import (
	"reflect"
	"testing"

	"github.com/dezren39/mcpx/internal/config"
)

func TestSecretNamesAreTheUnionOfServerAndToolRequirements(t *testing.T) {
	r := &config.Resolved{
		Server:          &config.Server{Name: "demo"},
		RequiresSecrets: []string{"B", "A"},
		Preconditions: map[string]config.Precondition{
			"t1": {RequiresSecret: "A"},
			"t2": {RequiresSecret: "C"},
			"t3": {RequiresServer: "other"},
		},
	}
	want := []string{"A", "B", "C"}
	if got := r.SecretNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("SecretNames = %v; want %v: a tool's requiresSecret is a requirement of the server", got, want)
	}
	if (&config.Resolved{Server: &config.Server{Name: "none"}}).SecretNames() != nil {
		t.Error("a server that requires nothing names nothing")
	}
}

func TestSecretValuePrecedenceIsRuntimeThenServerThenDaemon(t *testing.T) {
	t.Setenv("PREC_TOKEN", "from-daemon")
	runtime := map[string]string{}
	r := &config.Resolved{
		Server:  &config.Server{Name: "demo", Env: map[string]string{"PREC_TOKEN": "from-server"}},
		Runtime: func(name string) (string, bool) { v, ok := runtime[name]; return v, ok },
	}

	if v, ok := r.SecretValue("PREC_TOKEN"); !ok || v != "from-server" {
		t.Fatalf("with no runtime value the server's own environment wins: %q, %v", v, ok)
	}
	runtime["PREC_TOKEN"] = "from-runtime"
	if v, _ := r.SecretValue("PREC_TOKEN"); v != "from-runtime" {
		t.Fatalf("a runtime value wins over both: %q", v)
	}
	delete(runtime, "PREC_TOKEN")
	r.Server.Env = nil
	if v, _ := r.SecretValue("PREC_TOKEN"); v != "from-daemon" {
		t.Fatalf("with neither, the daemon's environment: %q", v)
	}
}

func TestAnEmptyValueIsNoValue(t *testing.T) {
	t.Setenv("EMPTY_TOKEN", "")
	r := &config.Resolved{
		Server:          &config.Server{Name: "demo", Env: map[string]string{"EMPTY_KEY": ""}},
		RequiresSecrets: []string{"EMPTY_TOKEN", "EMPTY_KEY"},
		Runtime:         func(string) (string, bool) { return "", true },
	}
	if _, ok := r.SecretValue("EMPTY_TOKEN"); ok {
		t.Error("an empty environment variable is not a value")
	}
	if _, ok := r.SecretValue("EMPTY_KEY"); ok {
		t.Error("an empty configured value is not a value")
	}
	if got := r.MissingSecrets(); !reflect.DeepEqual(got, []string{"EMPTY_KEY", "EMPTY_TOKEN"}) {
		t.Errorf("MissingSecrets = %v; both are missing", got)
	}
}

func TestMissingSecretsListsOnlyTheGaps(t *testing.T) {
	t.Setenv("PRESENT_IN_ENV", "yes")
	r := &config.Resolved{
		Server:          &config.Server{Name: "demo"},
		RequiresSecrets: []string{"PRESENT_IN_ENV", "ABSENT_EVERYWHERE"},
	}
	if got := r.MissingSecrets(); !reflect.DeepEqual(got, []string{"ABSENT_EVERYWHERE"}) {
		t.Fatalf("MissingSecrets = %v", got)
	}
}

// A runtime value must satisfy a precondition the same way the environment
// does. Otherwise a value given through mcpx would leave every tool of the
// server hidden, which is the failure this whole feature exists to remove.
func TestARuntimeValueSatisfiesThePreconditionsAnEnvironmentWould(t *testing.T) {
	withValue := &config.Resolved{
		Server:          &config.Server{Name: "demo"},
		RequiresSecrets: []string{"RUNTIME_ONLY_TOKEN"},
		Runtime: func(name string) (string, bool) {
			return "v", name == "RUNTIME_ONLY_TOKEN"
		},
	}
	if ok, reason := withValue.PassesPreconditions("any", nil); !ok {
		t.Fatalf("a runtime value should pass the server's requirement: %s", reason)
	}
	if !withValue.VisibleTool("any") {
		t.Error("a tool is visible once its server's secret has a value")
	}

	withoutValue := &config.Resolved{
		Server:          &config.Server{Name: "demo"},
		RequiresSecrets: []string{"RUNTIME_ONLY_TOKEN"},
	}
	if ok, _ := withoutValue.PassesPreconditions("any", nil); ok {
		t.Error("with no value anywhere the requirement fails, as it always did")
	}
}
