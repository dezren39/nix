package runner

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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

// A narrowed permission profile is a restriction only a runtime that defines
// flags for it can enforce. Bun and Node ran the script anyway with the
// user's full authority, so script.permissions strict read files it said it
// could not.
func TestARestrictedProfileIsRefusedByARuntimeThatCannotEnforceIt(t *testing.T) {
	fakeRuntimes(t, "node", "bun", "deno")
	for _, rt := range []string{"node", "bun"} {
		_, err := Resolve(rt, "strict", Setup{})
		if err == nil {
			t.Fatalf("--runtime %s with strict permissions ran", rt)
		}
		// The refusal names the profile and the runtime and says how to
		// define flags for it.
		for _, want := range []string{`"strict"`, rt, "script.profiles.strict." + rt} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("--runtime %s strict: %q does not mention %q", rt, err, want)
			}
		}
		if got, err := Resolve(rt, "all", Setup{}); err != nil || got.Name != rt {
			t.Errorf("--runtime %s with all permissions should still run: %v %v", rt, got, err)
		}
	}
	if _, err := Resolve("bun", "strict", Setup{}); err == nil || !strings.Contains(err.Error(), "no permission model") {
		t.Errorf("bun refusal should say bun has no permission model: %v", err)
	}
	if got, err := Resolve("deno", "read", Setup{}); err != nil || got.Name != "deno" {
		t.Errorf("deno enforces profiles and should be chosen: %v %v", got, err)
	}
}

func TestAutoDoesNotFallBackPastARuntimeThatCannotEnforceTheProfile(t *testing.T) {
	fakeRuntimes(t, "node", "bun")
	if _, err := Resolve("auto", "strict", Setup{}); err == nil || !strings.Contains(err.Error(), `"strict"`) {
		t.Errorf("auto with strict and no deno: got %v, want a refusal naming the profile", err)
	}
	if got, err := Resolve("auto", "all", Setup{}); err != nil || got.Name != "bun" {
		t.Errorf("auto with all permissions should fall back to bun: %v %v", got, err)
	}
	// node defines readnet, so auto skips bun for it.
	if got, err := Resolve("auto", "readnet", Setup{}); err != nil || got.Name != "node" {
		t.Errorf("auto with readnet should skip bun for node: %v %v", got, err)
	}
	fakeRuntimes(t, "node", "deno")
	if got, err := Resolve("auto", "strict", Setup{}); err != nil || got.Name != "deno" {
		t.Errorf("auto with strict should pick deno when it exists: %v %v", got, err)
	}
}

// --runtime bash used to say "no JavaScript runtime found (tried )": the
// list was only filled when the lookup failed, and bash was found.
func TestARuntimeThatResolvesButIsNotJavaScriptSaysSo(t *testing.T) {
	fakeRuntimes(t, "bash")
	_, err := Resolve("bash", "all", Setup{})
	if err == nil || !strings.Contains(err.Error(), "not a known JavaScript runtime") ||
		!strings.Contains(err.Error(), "script.runtimes") {
		t.Fatalf("got %v", err)
	}
	if _, err := Resolve("nosuch", "all", Setup{}); err == nil || !strings.Contains(err.Error(), "nosuch not found") {
		t.Errorf("missing runtime: %v", err)
	}
}

func TestACustomRuntimeByPathAndByDeclaredName(t *testing.T) {
	fakeRuntimes(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "deno")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// By path: the basename decides the kind.
	got, err := Resolve(bin, "strict", Setup{})
	if err != nil || got.Kind != KindDeno || got.Bin != bin {
		t.Fatalf("by path: %+v %v", got, err)
	}
	// By declared name, with a binary whose name says nothing.
	odd := filepath.Join(dir, "js-engine")
	if err := os.WriteFile(odd, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	set, err := ParseSetup(`{"mydeno":{"kind":"deno","bin":"`+odd+`","args":["--unstable-kv"]}}`, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	got, err = Resolve("mydeno", "strict", set)
	if err != nil || got.Kind != KindDeno || got.Bin != odd || got.Name != "mydeno" {
		t.Fatalf("by name: %+v %v", got, err)
	}
	want := []string{"run", "--quiet", "--no-check", "--unstable-kv", "--allow-net=127.0.0.1", "--allow-env", "s.ts"}
	if a := got.Args("s.ts"); !reflect.DeepEqual(a, want) {
		t.Errorf("argv %q, want %q", a, want)
	}
	// Undeclared, it is not a runtime.
	if _, err := Resolve(odd, "all", Setup{}); err == nil {
		t.Error("js-engine by path with no declaration should be refused")
	}
	// The auto order can name it.
	set.Order = []string{"mydeno"}
	if got, err := Resolve("auto", "all", set); err != nil || got.Name != "mydeno" {
		t.Errorf("auto via order: %+v %v", got, err)
	}
	if _, err := ParseSetup(`{"x":{"kind":"python"}}`, nil, ""); err == nil {
		t.Error("an unknown kind should be refused")
	}
}

func TestProfilesComeFromDefaultsAndUsersOverrideThem(t *testing.T) {
	fakeRuntimes(t, "deno", "node")
	got, err := Resolve("deno", "strict", Setup{})
	if err != nil || !reflect.DeepEqual(got.Perms, []string{"--allow-net=127.0.0.1", "--allow-env"}) {
		t.Fatalf("built-in strict: %+v %v", got, err)
	}
	set, err := ParseSetup("", nil, `{"strict":{"deno":["--allow-net=localhost"],"node":["--permission"]},"tmp":"--allow-read=/tmp --allow-write=/tmp"}`)
	if err != nil {
		t.Fatal(err)
	}
	got, err = Resolve("deno", "strict", set)
	if err != nil || !reflect.DeepEqual(got.Perms, []string{"--allow-net=localhost"}) {
		t.Errorf("user strict should replace the built-in: %+v %v", got, err)
	}
	// Now node can run strict, because the user said how.
	if got, err := Resolve("node", "strict", set); err != nil || !reflect.DeepEqual(got.Perms, []string{"--permission"}) {
		t.Errorf("user strict on node: %+v %v", got, err)
	}
	// A string profile is raw flags for every kind.
	if got, err := Resolve("node", "tmp", set); err != nil || got.Perms[0] != "--allow-read=/tmp" {
		t.Errorf("string profile: %+v %v", got, err)
	}
	if _, err := ParseSetup("", nil, `{"bad":{"deno":["/tmp"]}}`); err == nil {
		t.Error("a bare path in a flag list should be refused")
	}
	if _, err := Resolve("deno", "nosuch", Setup{}); err == nil || !strings.Contains(err.Error(), "nosuch") {
		t.Errorf("unknown profile: %v", err)
	}
}

func TestProfilesComposeInOrderLaterWinning(t *testing.T) {
	fakeRuntimes(t, "deno")
	set, _ := ParseSetup("", nil, `{"tmpread":{"deno":["--allow-read=/tmp"]}}`)
	got, err := Resolve("deno", "read,strict,tmpread", set)
	if err != nil {
		t.Fatal(err)
	}
	// Union in first-seen position; --allow-read from tmpread replaces read's.
	want := []string{"--allow-read=/tmp", "--allow-env", "--allow-net=127.0.0.1"}
	if !reflect.DeepEqual(got.Perms, want) {
		t.Errorf("read,strict,tmpread = %q, want %q", got.Perms, want)
	}
	got, _ = Resolve("deno", "tmpread,read", set)
	if got == nil || got.Perms[0] != "--allow-read" {
		t.Errorf("tmpread,read: later read should win, got %+v", got)
	}
	// raw: takes the rest, commas included.
	got, err = Resolve("deno", "strict,raw:--allow-read=/a,/b --allow-env", set)
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"--allow-net=127.0.0.1", "--allow-env", "--allow-read=/a,/b"}
	if !reflect.DeepEqual(got.Perms, want) {
		t.Errorf("raw composition = %q, want %q", got.Perms, want)
	}
	// Flags without raw: are refused, naming the explicit spelling.
	if _, err := Resolve("deno", "--allow-read", set); err == nil || !strings.Contains(err.Error(), "raw:") {
		t.Errorf("bare flags: %v", err)
	}
	if _, err := Resolve("deno", "raw:/tmp", set); err == nil {
		t.Error("raw: with a non-flag should be refused")
	}
	// A refusal in the middle of a composition names that profile.
	fakeRuntimes(t, "node")
	if _, err := Resolve("node", "readnet,strict", set); err == nil || !strings.Contains(err.Error(), `"strict"`) {
		t.Errorf("readnet,strict on node: %v", err)
	}
}
