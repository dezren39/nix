package registry_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/registry"
)

func TestNamespaceStripsTheReverseDNSPrefix(t *testing.T) {
	// Carrying it into a config file would make every tool call read
	// io_github_user_weather_get_forecast.
	for _, c := range []struct{ in, want string }{
		{"io.github.user/weather", "weather"},
		{"com.pulsemcp/playwright-stealth", "playwright_stealth"},
		{"io.github.microsoft/playwright-mcp", "playwright_mcp"},
		{"plain", "plain"},
	} {
		if got := registry.Namespace(c.in); got != c.want {
			t.Errorf("Namespace(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestARemoteIsPreferredOverAPackage(t *testing.T) {
	// Nothing has to be installed and nothing runs locally.
	s := registry.Server{
		Name:     "x/y",
		Remotes:  []registry.Remote{{Type: "streamable-http", URL: "https://example.com/mcp"}},
		Packages: []registry.Package{{RegistryType: "npm", Identifier: "thing"}},
	}
	in, err := s.ToInstall(false)
	if err != nil {
		t.Fatal(err)
	}
	if in.URL != "https://example.com/mcp" || in.Command != "" {
		t.Errorf("got %+v", in)
	}
}

func TestPreferLocalTakesThePackageInstead(t *testing.T) {
	s := registry.Server{
		Name:     "x/y",
		Remotes:  []registry.Remote{{Type: "streamable-http", URL: "https://example.com/mcp"}},
		Packages: []registry.Package{{RegistryType: "npm", Identifier: "thing", Version: "1.2.3"}},
	}
	in, err := s.ToInstall(true)
	if err != nil {
		t.Fatal(err)
	}
	if in.Command != "npx" {
		t.Errorf("got %q", in.Command)
	}
	// Pinned, because a config file that silently upgrades is a config file
	// that breaks on a morning nobody changed anything.
	if !strings.Contains(strings.Join(in.Args, " "), "thing@1.2.3") {
		t.Errorf("the version should be pinned: %v", in.Args)
	}
}

func TestEachPackageTypeGetsARunnerThatNeedsNoInstall(t *testing.T) {
	for _, c := range []struct{ kind, want string }{
		{"npm", "npx"}, {"pypi", "uvx"}, {"oci", "docker"},
	} {
		s := registry.Server{Name: "a/b", Packages: []registry.Package{
			{RegistryType: c.kind, Identifier: "thing"},
		}}
		in, err := s.ToInstall(true)
		if err != nil {
			t.Fatalf("%s: %v", c.kind, err)
		}
		if in.Command != c.want {
			t.Errorf("%s should run via %s, got %s", c.kind, c.want, in.Command)
		}
	}
}

func TestAPublishersRuntimeHintWins(t *testing.T) {
	// They know something about their own package that a table does not.
	s := registry.Server{Name: "a/b", Packages: []registry.Package{
		{RegistryType: "npm", Identifier: "thing", RuntimeHint: "bunx"},
	}}
	in, _ := s.ToInstall(true)
	if in.Command != "bunx" {
		t.Errorf("got %q", in.Command)
	}
}

func TestRequiredSecretsAreReportedNotInvented(t *testing.T) {
	// A wrong value fails in a way that looks like the server is broken.
	s := registry.Server{Name: "a/b", Packages: []registry.Package{{
		RegistryType: "npm", Identifier: "thing",
		EnvironmentVars: []registry.Variable{
			{Name: "API_KEY", IsRequired: true, IsSecret: true, Description: "your key"},
			{Name: "REGION", Default: "us-east-1"},
		},
	}}}
	in, err := s.ToInstall(true)
	if err != nil {
		t.Fatal(err)
	}
	if in.Env["REGION"] != "us-east-1" {
		t.Errorf("a non-secret default should be carried: %v", in.Env)
	}
	if _, invented := in.Env["API_KEY"]; invented {
		t.Error("a secret must never be invented")
	}
	if len(in.Needs) != 1 || in.Needs[0].Name != "API_KEY" {
		t.Errorf("it should be reported as needed: %+v", in.Needs)
	}
}

func TestAServerWithNothingRunnableSaysWhatItOffered(t *testing.T) {
	s := registry.Server{Name: "a/b", Packages: []registry.Package{
		{RegistryType: "brew", Identifier: "thing"},
	}}
	_, err := s.ToInstall(false)
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if !strings.Contains(err.Error(), "brew") {
		t.Errorf("the message should say what was on offer: %v", err)
	}
}

func TestSearchSendsWhatTheRegistryExpects(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{
			"servers": []any{map[string]any{
				"server": map[string]any{"name": "io.github.a/b", "description": "d", "version": "1"},
			}},
		})
	}))
	defer srv.Close()

	res, err := registry.New(srv.URL, registry.Options{}).Search(context.Background(), "weather", 5)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Servers; len(got) != 1 || got[0].Name != "io.github.a/b" {
		t.Errorf("got %+v", got)
	}
	// version=latest matters: without it the registry returns every version
	// of everything and the list is unusable.
	for _, want := range []string{"search=weather", "limit=5", "version=latest"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("%q missing from %q", want, gotQuery)
		}
	}
}

func TestAFailingRegistryNamesItself(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()
	_, err := registry.New(srv.URL, registry.Options{}).Search(context.Background(), "x", 1)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), srv.URL) {
		t.Errorf("the error should name which registry failed: %v", err)
	}
}

func TestTheDefaultIsTheOfficialRegistry(t *testing.T) {
	if c := registry.New("", registry.Options{}); c.BaseURL != registry.DefaultURL {
		t.Errorf("got %q", c.BaseURL)
	}
}
