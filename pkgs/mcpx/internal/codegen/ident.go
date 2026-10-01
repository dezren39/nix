package codegen

import (
	"regexp"
	"sync"
)

// jsReserved are words a generated identifier may not be: JavaScript's
// reserved words in strict mode (every module is strict), the ones strict
// mode adds, and the names TypeScript refuses as a namespace. A tool named
// `delete` was emitted as `function delete(...)` in `mcpx types` (#215).
var jsReserved = setOf(
	"await", "break", "case", "catch", "class", "const", "continue", "debugger",
	"default", "delete", "do", "else", "enum", "export", "extends", "false",
	"finally", "for", "function", "if", "import", "in", "instanceof", "new",
	"null", "return", "super", "switch", "this", "throw", "true", "try",
	"typeof", "var", "void", "while", "with", "yield",
	"let", "static", "implements", "interface", "package", "private",
	"protected", "public", "arguments", "eval",
	"any", "boolean", "number", "string", "symbol", "never", "unknown",
	"object", "bigint", "undefined",
)

// runtimeGlobals are the globals the generated runtime reads. A namespace is
// a module-level const, so one named `console` or `fetch` would shadow the
// global inside the runtime and break every call.
var runtimeGlobals = setOf(
	"Array", "Bun", "Date", "Deno", "Error", "Headers", "Infinity", "JSON",
	"Map", "Math", "NaN", "Number", "Object", "Promise", "RegExp", "Request",
	"Response", "Set", "String", "Symbol", "TextDecoder", "TextEncoder", "URL",
	"URLSearchParams", "Uint8Array", "atob", "btoa", "clearTimeout", "console",
	"decodeURIComponent", "encodeURIComponent", "exports", "fetch", "globalThis",
	"module", "performance", "process", "queueMicrotask", "require",
	"setTimeout",
)

func setOf(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

var (
	preludeOnce  sync.Once
	preludeNames map[string]bool
	topLevelDecl = regexp.MustCompile(`(?m)^(?:export\s+)?(?:declare\s+)?(?:async\s+)?(?:function\*?|const|let|var|class|type|interface|enum|namespace)\s+([A-Za-z_$][\w$]*)`)
)

// PreludeNames are the identifiers the generated module declares at its top
// level. Read from the module itself, so a name added to the runtime is
// covered without anyone remembering to list it.
func PreludeNames() map[string]bool {
	preludeOnce.Do(func() {
		preludeNames = map[string]bool{}
		for _, m := range topLevelDecl.FindAllStringSubmatch(Module(nil, "", ""), -1) {
			preludeNames[m[1]] = true
		}
	})
	return preludeNames
}

// ReservedNamespace reports whether name cannot be a namespace: a reserved
// word, a name the module already declares (`log`, `tools`, `call`), or a
// global the runtime depends on. `export const log = {...}` beside the
// runtime's own `log` is a redeclaration, which broke every script.
func ReservedNamespace(name string) bool {
	return jsReserved[name] || runtimeGlobals[name] || PreludeNames()[name]
}

// SafeNamespace suffixes a name until it is not reserved.
func SafeNamespace(name string) string {
	for ReservedNamespace(name) {
		name += "_"
	}
	return name
}
