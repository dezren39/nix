package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// writeShellScript writes an executable /bin/sh script.
func writeShellScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// The successor must see what a fresh start of the unit would give it. The
// unit's start command here is a wrapper that sets a variable the daemon was
// not started with, then execs the binary it is given. The successor is the
// binary, so it can report what its environment holds.
func TestTheSuccessorIsStartedThroughTheUnitStartCommand(t *testing.T) {
	dir := t.TempDir()
	seen := filepath.Join(dir, "seen")
	binary := filepath.Join(dir, "mcpx-next")
	writeShellScript(t, binary, "printf '%s|%s' \"$MCPX_UNIT_TOKEN\" \"$*\" > '"+seen+"'\n")
	wrapper := filepath.Join(dir, "mcpx-start")
	writeShellScript(t, wrapper, "MCPX_UNIT_TOKEN=rotated\nexport MCPX_UNIT_TOKEN\nexec \"$@\"\n")

	s := &Server{}
	cmd, err := s.startSuccessor(binary, []string{wrapper})
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("successor: %v", err)
	}
	got, err := os.ReadFile(seen)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "rotated|daemon --takeover" {
		t.Fatalf("successor saw %q, want the unit's variable and the takeover arguments", got)
	}

	// With no start command the binary runs as it is. The wrapper's variable
	// is absent, which is what the daemon did before the unit had one.
	cmd, err = s.startSuccessor(binary, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("successor without a start command: %v", err)
	}
	got, err = os.ReadFile(seen)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "|daemon --takeover" {
		t.Fatalf("successor without a start command saw %q", got)
	}
}

func postUpgradeWithStart(t *testing.T, client *http.Client, binary string, start []string) (int, map[string]any) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"binary": binary, "start": start})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Post("http://mcpx"+upgradeRoute, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, out
}

func TestAnUpgradeRequestRejectsAnEmptyStartArgument(t *testing.T) {
	_, client := startUpgradeServer(t)
	code, out := postUpgradeWithStart(t, client, "/bin/true", []string{""})
	if code != http.StatusBadRequest {
		t.Fatalf("an empty start argument: %d %v", code, out)
	}
	assertHealthy(t, client)
}
