package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRuntimes puts executables with these names, and nothing else, on PATH.
func fakeRuntimes(t *testing.T, names ...string) {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

// A narrowed permission profile is a restriction only Deno can enforce. Bun
// and Node ran the script anyway with the user's full authority, so
// script.permissions strict read files it said it could not.
func TestARestrictedProfileIsRefusedByARuntimeThatCannotEnforceIt(t *testing.T) {
	fakeRuntimes(t, "node", "bun", "deno")
	for _, rt := range []string{"node", "bun"} {
		_, err := Detect(rt, Permissions("strict"))
		if err == nil || !strings.Contains(err.Error(), "no permission model") {
			t.Errorf("--runtime %s with strict permissions: got %v, want a refusal", rt, err)
		}
		if got, err := Detect(rt, Permissions("all")); err != nil || got.Name != rt {
			t.Errorf("--runtime %s with all permissions should still run: %v %v", rt, got, err)
		}
	}
	if got, err := Detect("deno", Permissions("read")); err != nil || got.Name != "deno" {
		t.Errorf("deno enforces profiles and should be chosen: %v %v", got, err)
	}
}

func TestAutoDoesNotFallBackPastDenoForARestrictedProfile(t *testing.T) {
	fakeRuntimes(t, "node", "bun")
	if _, err := Detect("auto", Permissions("strict")); err == nil || !strings.Contains(err.Error(), "needs deno") {
		t.Errorf("auto with strict and no deno: got %v, want a refusal naming deno", err)
	}
	// With everything allowed, the fallback is what it always was.
	if got, err := Detect("auto", Permissions("all")); err != nil || got.Name != "bun" {
		t.Errorf("auto with all permissions should fall back to bun: %v %v", got, err)
	}
	fakeRuntimes(t, "node", "deno")
	if got, err := Detect("auto", Permissions("strict")); err != nil || got.Name != "deno" {
		t.Errorf("auto with strict should pick deno when it exists: %v %v", got, err)
	}
}
