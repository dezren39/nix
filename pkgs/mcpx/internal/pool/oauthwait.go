package pool

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dezren39/mcpx/internal/config"
)

// mcp-remote parks an unfinished OAuth sign-in on disk: the per-server
// directory under ~/.mcp-auth/mcp-remote-*/<md5(url)> gains client_info.json
// and code_verifier when the browser flow starts, and tokens.json only when
// it completes. While that state holds, every mcp-remote start blocks opening
// a browser and waiting, so mcpx sees nothing but a discover timeout. Naming
// the state (and the command that finishes it) turns a one-minute mystery
// into a one-line instruction.

// oauthWait describes a detected unfinished mcp-remote sign-in.
type oauthWait struct {
	URL     string
	Dir     string
	Command string // the command line that finishes the sign-in
}

// pendingOAuth reports whether cfg is an mcp-remote stdio server whose OAuth
// sign-in was started but never finished.
func pendingOAuth(cfg *config.Resolved) (oauthWait, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return oauthWait{}, false
	}
	return pendingOAuthIn(cfg, filepath.Join(home, ".mcp-auth"))
}

// pendingOAuthIn is pendingOAuth with the mcp-auth root made explicit, for tests.
func pendingOAuthIn(cfg *config.Resolved, authRoot string) (oauthWait, bool) {
	if cfg == nil || !cfg.Stdio() {
		return oauthWait{}, false
	}
	if filepath.Base(cfg.Command) != "mcp-remote" {
		return oauthWait{}, false
	}
	url := ""
	for _, a := range cfg.Args {
		if strings.HasPrefix(a, "https://") || strings.HasPrefix(a, "http://") {
			url = a
			break
		}
	}
	if url == "" {
		return oauthWait{}, false
	}
	sum := md5.Sum([]byte(url))
	key := hex.EncodeToString(sum[:])
	dirs, _ := filepath.Glob(filepath.Join(authRoot, "mcp-remote-*", key))
	for _, dir := range dirs {
		if !exists(filepath.Join(dir, "client_info.json")) || !exists(filepath.Join(dir, "code_verifier")) {
			continue // sign-in never got as far as the browser step
		}
		if exists(filepath.Join(dir, "tokens.json")) {
			continue // sign-in finished; the failure is something else
		}
		return oauthWait{
			URL:     url,
			Dir:     dir,
			Command: cfg.Command + " " + url,
		}, true
	}
	return oauthWait{}, false
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// oauthWaitError wraps a start failure with the sign-in explanation when the
// server is parked on an unfinished OAuth flow; otherwise it returns err.
func (p *Pool) oauthWaitError(err error) error {
	w, ok := pendingOAuth(p.cfg)
	if !ok {
		return err
	}
	return fmt.Errorf("server %q is waiting for OAuth sign-in: an mcp-remote sign-in for %s was started but never finished (no tokens in %s); run `%s` in a terminal and complete the browser sign-in, then retry (start failed with: %v)",
		p.cfg.Name, w.URL, w.Dir, w.Command, err)
}
