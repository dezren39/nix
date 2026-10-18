package pool

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/config"
)

const oauthTestURL = "https://mcp.example.com/v1/mcp?toolsets=core"

func oauthCfg(command string, args ...string) *config.Resolved {
	return &config.Resolved{Server: &config.Server{Name: "datadog", Command: command, Args: args}}
}

// authDir builds <root>/mcp-remote-v1/<md5(url)> populated with files.
func authDir(t *testing.T, url string, files ...string) string {
	t.Helper()
	root := t.TempDir()
	sum := md5.Sum([]byte(url))
	dir := filepath.Join(root, "mcp-remote-v1", hex.EncodeToString(sum[:]))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestPendingOAuthDetectsUnfinishedSignIn(t *testing.T) {
	root := authDir(t, oauthTestURL, "client_info.json", "code_verifier")
	w, ok := pendingOAuthIn(oauthCfg("mcp-remote", oauthTestURL), root)
	if !ok {
		t.Fatal("unfinished sign-in not detected")
	}
	if w.URL != oauthTestURL || !strings.Contains(w.Dir, "mcp-remote-v1") {
		t.Errorf("wait = %+v", w)
	}
	if w.Command != "mcp-remote "+oauthTestURL {
		t.Errorf("command = %q", w.Command)
	}
}

func TestPendingOAuthFinishedSignInIsNotWaiting(t *testing.T) {
	root := authDir(t, oauthTestURL, "client_info.json", "code_verifier", "tokens.json")
	if _, ok := pendingOAuthIn(oauthCfg("mcp-remote", oauthTestURL), root); ok {
		t.Error("finished sign-in reported as waiting")
	}
}

func TestPendingOAuthNeverStartedIsNotWaiting(t *testing.T) {
	root := authDir(t, oauthTestURL) // directory exists, no files yet
	if _, ok := pendingOAuthIn(oauthCfg("mcp-remote", oauthTestURL), root); ok {
		t.Error("never-started sign-in reported as waiting")
	}
	if _, ok := pendingOAuthIn(oauthCfg("mcp-remote", oauthTestURL), t.TempDir()); ok {
		t.Error("missing auth dir reported as waiting")
	}
}

func TestPendingOAuthIgnoresOtherServers(t *testing.T) {
	root := authDir(t, oauthTestURL, "client_info.json", "code_verifier")
	if _, ok := pendingOAuthIn(oauthCfg("some-other-server", oauthTestURL), root); ok {
		t.Error("non-mcp-remote server reported as waiting")
	}
	if _, ok := pendingOAuthIn(oauthCfg("mcp-remote", "--help"), root); ok {
		t.Error("mcp-remote without a URL arg reported as waiting")
	}
	http := &config.Resolved{Server: &config.Server{Name: "x", URL: oauthTestURL}}
	if _, ok := pendingOAuthIn(http, root); ok {
		t.Error("direct-HTTP server reported as waiting")
	}
}

func TestOAuthWaitErrorMessage(t *testing.T) {
	root := authDir(t, oauthTestURL, "client_info.json", "code_verifier")
	// Point HOME at a tree holding the auth root so pendingOAuth finds it.
	authParent := t.TempDir()
	if err := os.Rename(root, filepath.Join(authParent, ".mcp-auth")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", authParent)
	p := &Pool{cfg: oauthCfg("mcp-remote", oauthTestURL)}
	err := p.oauthWaitError(errors.New("server/discover: context deadline exceeded"))
	msg := err.Error()
	for _, want := range []string{"waiting for OAuth sign-in", oauthTestURL, "mcp-remote " + oauthTestURL, "context deadline exceeded"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q missing %q", msg, want)
		}
	}
	// A server with no pending sign-in keeps its original error untouched.
	p2 := &Pool{cfg: oauthCfg("other", oauthTestURL)}
	if got := p2.oauthWaitError(errors.New("boom")); got.Error() != "boom" {
		t.Errorf("unrelated error rewritten: %v", got)
	}
}
