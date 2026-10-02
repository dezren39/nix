package codegen

import (
	"regexp"
	"sort"
	"testing"
)

// Every global installGlobals puts on globalThis is declared in
// mcpx-globals.d.ts, or a type-checked script that uses it fails with
// "Cannot find name" -- search() and describe() did, while the skill
// examples and the client's own docs told scripts to call them bare.
func TestGlobalDeclarationsCoverEveryInstalledGlobal(t *testing.T) {
	nss := []Namespace{{Name: "demo"}}
	installed := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\s*g\.(\w+) \?\?= `).FindAllStringSubmatch(Module(nss, "", ""), -1) {
		installed[m[1]] = true
	}
	if len(installed) < 10 {
		t.Fatalf("found %d installed globals; the pattern no longer matches installGlobals", len(installed))
	}
	declared := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\s*const (\w+):`).FindAllStringSubmatch(GlobalDeclarations(nss), -1) {
		declared[m[1]] = true
	}
	var missing []string
	for name := range installed {
		if !declared[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("installed on globalThis but not declared in mcpx-globals.d.ts: %v", missing)
	}
}
