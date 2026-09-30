package mcpauth_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/mcpauth"
)

func TestBearerExpandsFromTheEnvironment(t *testing.T) {
	// The point of expansion is that the secret is never in the file.
	t.Setenv("MY_TOKEN", "s3cret")
	got, err := (&mcpauth.Auth{Type: "bearer", Token: "${MY_TOKEN}"}).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if got.Headers["Authorization"] != "Bearer s3cret" {
		t.Errorf("got %q", got.Headers["Authorization"])
	}
}

func TestAMissingVariableIsReportedNotSilentlyEmpty(t *testing.T) {
	// Otherwise the failure is a 401 from a server, three layers away from
	// the unset variable that caused it.
	got, err := (&mcpauth.Auth{Type: "bearer", Token: "${DEFINITELY_UNSET_XYZ}"}).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Missing) != 1 || got.Missing[0] != "DEFINITELY_UNSET_XYZ" {
		t.Errorf("the unset variable should be named: %v", got.Missing)
	}
}

func TestBasicEncodesCorrectly(t *testing.T) {
	got, _ := (&mcpauth.Auth{Type: "basic", Username: "user", Password: "pass"}).Resolve()
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass"))
	if got.Headers["Authorization"] != want {
		t.Errorf("got %q want %q", got.Headers["Authorization"], want)
	}
}

func TestBasicHandlesEveryPaddingCase(t *testing.T) {
	// Three lengths mod 3, because a hand-written encoder gets the padding
	// wrong on exactly one of them and the failure is a silent 401.
	for _, c := range []struct{ user, pass string }{
		{"a", "b"}, {"ab", "c"}, {"abc", "de"}, {"longer-user", "and-a-password"},
	} {
		got, _ := (&mcpauth.Auth{Type: "basic", Username: c.user, Password: c.pass}).Resolve()
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte(c.user+":"+c.pass))
		if got.Headers["Authorization"] != want {
			t.Errorf("%q:%q got %q want %q", c.user, c.pass,
				got.Headers["Authorization"], want)
		}
	}
}

func TestEmptyCredentialsSendNoHeaderAtAll(t *testing.T) {
	// "Basic Og==" is an encoded colon: credentials that say nothing. Sending
	// it invites a 401 that looks like a rejected password rather than an
	// absent one.
	got, _ := (&mcpauth.Auth{Type: "basic"}).Resolve()
	if _, set := got.Headers["Authorization"]; set {
		t.Errorf("no credentials should mean no header: %v", got.Headers)
	}
}

func TestAnEmptyEnvValueMeansPassMineThrough(t *testing.T) {
	// The shape that keeps a secret out of the configuration file entirely.
	t.Setenv("PASSTHROUGH_KEY", "value-from-my-env")
	got, _ := (&mcpauth.Auth{Env: map[string]string{"PASSTHROUGH_KEY": ""}}).Resolve()
	if got.Env["PASSTHROUGH_KEY"] != "value-from-my-env" {
		t.Errorf("got %v", got.Env)
	}
}

func TestCustomHeaderAndQuerySchemes(t *testing.T) {
	t.Setenv("K", "abc")
	h, _ := (&mcpauth.Auth{Type: "header", Header: "X-Api-Key", Value: "${K}"}).Resolve()
	if h.Headers["X-Api-Key"] != "abc" {
		t.Errorf("got %v", h.Headers)
	}
	q, _ := (&mcpauth.Auth{Type: "query", Param: "api_key", ParamValue: "${K}"}).Resolve()
	if q.Query["api_key"] != "abc" {
		t.Errorf("got %v", q.Query)
	}
}

func TestOAuthIsDeclaredButNotPerformed(t *testing.T) {
	// A server requiring it should say so before the first request, not fail
	// with a 401 nobody can interpret.
	got, _ := (&mcpauth.Auth{Type: "oauth"}).Resolve()
	if !got.NeedsOAuth {
		t.Error("it should be flagged")
	}
}

func TestAnUnknownTypeNamesTheAlternatives(t *testing.T) {
	_, err := (&mcpauth.Auth{Type: "magic"}).Resolve()
	if err == nil || !strings.Contains(err.Error(), "bearer") {
		t.Errorf("got %v", err)
	}
}

func TestDescribeNeverRevealsALiteral(t *testing.T) {
	// config and doctor output both get pasted into issues.
	got := (&mcpauth.Auth{Type: "bearer", Token: "actual-secret-value"}).Describe()
	if strings.Contains(got, "actual-secret") {
		t.Errorf("a literal must never be printed: %q", got)
	}
	// A reference is safe and useful: it says which variable to set.
	ref := (&mcpauth.Auth{Type: "bearer", Token: "${GITHUB_TOKEN}"}).Describe()
	if !strings.Contains(ref, "GITHUB_TOKEN") {
		t.Errorf("a reference should be shown: %q", ref)
	}
}

func TestNilAuthIsNone(t *testing.T) {
	var a *mcpauth.Auth
	got, err := a.Resolve()
	if err != nil || len(got.Headers) != 0 {
		t.Errorf("got %v %v", got, err)
	}
	if a.Describe() != "none" {
		t.Errorf("got %q", a.Describe())
	}
}
