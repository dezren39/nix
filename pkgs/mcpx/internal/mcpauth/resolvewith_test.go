package mcpauth_test

import (
	"testing"

	"github.com/dezren39/mcpx/internal/mcpauth"
)

// A credential that names a variable is read through the lookup it is given,
// so a value the daemon holds in memory reaches an HTTP server the same way an
// environment variable does. The lookup is the only source: nothing here reads
// the process environment.
func TestResolveWithReadsEveryVariableThroughTheLookup(t *testing.T) {
	values := map[string]string{"API_TOKEN": "from-lookup", "PASS_THROUGH": "pass-value"}
	get := func(name string) string { return values[name] }

	a := &mcpauth.Auth{
		Type:  "bearer",
		Token: "${API_TOKEN}",
		Env:   map[string]string{"PASS_THROUGH": ""},
	}
	got, err := a.ResolveWith(get)
	if err != nil {
		t.Fatal(err)
	}
	if got.Headers["Authorization"] != "Bearer from-lookup" {
		t.Errorf("the bearer token should come from the lookup: %v", got.Headers)
	}
	if got.Env["PASS_THROUGH"] != "pass-value" {
		t.Errorf("an empty env entry passes through the lookup too: %v", got.Env)
	}
	if len(got.Missing) != 0 {
		t.Errorf("nothing is missing when the lookup has every name: %v", got.Missing)
	}
}

func TestResolveWithReportsWhatTheLookupLacks(t *testing.T) {
	a := &mcpauth.Auth{Type: "bearer", Token: "${NOT_THERE}"}
	got, err := a.ResolveWith(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Missing) != 1 || got.Missing[0] != "NOT_THERE" {
		t.Errorf("a name the lookup cannot answer for is reported missing: %v", got.Missing)
	}
}
