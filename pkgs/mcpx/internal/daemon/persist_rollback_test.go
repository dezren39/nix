package daemon

import (
	"bytes"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/config"
)

// setupPersist builds a daemon whose project config file is path, holding
// before (or no file when before is nil). Its reloads are refused when refuse
// is set: augment runs on every reload, and a failure there is a reload that
// cannot be applied.
func setupPersist(t *testing.T, before []byte, refuse bool) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	// No user config in play: a reload with no file to read falls back to it.
	t.Setenv("HOME", filepath.Join(dir, "home"))
	path := filepath.Join(dir, ".mcpx.json")

	cfg := &config.Config{MCPServers: map[string]*config.Server{}}
	if before != nil {
		if err := os.WriteFile(path, before, 0o640); err != nil {
			t.Fatal(err)
		}
		loaded, err := config.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		cfg = loaded
	}
	s, err := NewServer(handoffOptions(handoffPaths(t), cfg, "persist"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.reg.Close)
	s.augment = func(*config.Config) error {
		if refuse {
			return errors.New("refused by the test")
		}
		return nil
	}
	return s, path
}

// putPersisted is what `mcpx settings set pool.max 2 --persist project` sends.
func putPersisted(t *testing.T, s *Server) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/v1/settings/pool.max",
		strings.NewReader(`{"value":"2","persist":"project"}`))
	req.SetPathValue("path", "pool.max")
	rec := httptest.NewRecorder()
	s.handleSettingsSet(rec, req)
	return rec
}

// A persisted change the reload refuses is taken back out of the file as well
// as the runtime. The file is written before the reload, so without a rollback
// it holds a value the daemon refused, and the next reload that reads it applies
// that value.
func TestAPersistedChangeTheReloadRefusesLeavesTheFileAsItWas(t *testing.T) {
	// Deliberately not what the encoder would write, so a rewrite of the
	// file would show.
	before := []byte("{  \"mcpServers\" :{},\n  \"pool\": {\"max\": 3}\n}\n")
	cases := []struct {
		name   string
		before []byte
	}{
		{"a file that exists", before},
		{"no file yet", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, path := setupPersist(t, tc.before, true)
			rec := putPersisted(t, s)

			if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "rolled back") {
				t.Fatalf("a refused reload: status %d, want 500 saying it was rolled back:\n%s", rec.Code, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), "was restored to its previous contents") {
				t.Errorf("the refusal should say the file was restored:\n%s", rec.Body)
			}
			got, err := os.ReadFile(path)
			if tc.before == nil {
				if !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("a file the refused write created should be gone, got %q (err %v)", got, err)
				}
			} else {
				if err != nil || !bytes.Equal(got, tc.before) {
					t.Fatalf("the file should be byte-for-byte as before\nwant %q\n got %q (err %v)", tc.before, got, err)
				}
				st, err := os.Stat(path)
				if err != nil || st.Mode().Perm() != 0o640 {
					t.Fatalf("the restored file should keep its mode 0640: %v (err %v)", st, err)
				}
			}
			if _, had := s.set.RuntimeOverride("pool.max"); had {
				t.Error("the runtime override was kept after its reload was refused")
			}
		})
	}
}

// The rollback must not undo a change that reloads. The file keeps the value
// and the runtime applies it.
func TestAPersistedChangeThatReloadsStaysInTheFile(t *testing.T) {
	s, path := setupPersist(t, []byte(`{"mcpServers":{}}`+"\n"), false)
	rec := putPersisted(t, s)
	if rec.Code != http.StatusOK {
		t.Fatalf("a change that reloads: status %d\n%s", rec.Code, rec.Body)
	}
	got, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(got), `"max": 2`) {
		t.Fatalf("the file should hold the persisted value, got %q (err %v)", got, err)
	}
}
