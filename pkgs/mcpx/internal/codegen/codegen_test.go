package codegen_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/codegen"
)

func typeOf(t *testing.T, schemaJSON string) string {
	t.Helper()
	return strings.TrimSpace(codegen.TypeFor(json.RawMessage(schemaJSON), ""))
}

func TestPrimitiveTypes(t *testing.T) {
	cases := map[string]string{
		`{"type":"string"}`:  "string",
		`{"type":"integer"}`: "number",
		`{"type":"number"}`:  "number",
		`{"type":"boolean"}`: "boolean",
		`{"type":"null"}`:    "null",
	}
	for in, want := range cases {
		if got := typeOf(t, in); got != want {
			t.Errorf("%s => %q, want %q", in, got, want)
		}
	}
}

func TestNullableTypeArrayBecomesUnion(t *testing.T) {
	if got := typeOf(t, `{"type":["integer","null"]}`); got != "number | null" {
		t.Fatalf("got %q", got)
	}
}

func TestEnumBecomesLiteralUnion(t *testing.T) {
	got := typeOf(t, `{"type":"string","enum":["a","b","c"]}`)
	if got != `"a" | "b" | "c"` {
		t.Fatalf("got %q", got)
	}
}

func TestConstBecomesLiteral(t *testing.T) {
	if got := typeOf(t, `{"const":"fixed"}`); got != `"fixed"` {
		t.Fatalf("got %q", got)
	}
}

func TestArrayOfUnionGetsParentheses(t *testing.T) {
	got := typeOf(t, `{"type":"array","items":{"type":["string","null"]}}`)
	if got != "(string | null)[]" {
		t.Fatalf("got %q", got)
	}
}

func TestTupleViaPrefixItems(t *testing.T) {
	got := typeOf(t, `{"type":"array","prefixItems":[{"type":"string"},{"type":"number"}]}`)
	if got != "[string, number]" {
		t.Fatalf("got %q", got)
	}
}

func TestObjectRequiredAndOptional(t *testing.T) {
	got := typeOf(t, `{
	  "type":"object",
	  "properties":{"a":{"type":"string"},"b":{"type":"number"}},
	  "required":["a"]
	}`)
	if !strings.Contains(got, "a: string;") {
		t.Errorf("required prop should not be optional: %s", got)
	}
	if !strings.Contains(got, "b?: number;") {
		t.Errorf("unlisted prop should be optional: %s", got)
	}
}

func TestNonIdentifierPropertyIsQuoted(t *testing.T) {
	got := typeOf(t, `{"type":"object","properties":{"a-b":{"type":"string"}},"required":["a-b"]}`)
	if !strings.Contains(got, `"a-b": string;`) {
		t.Fatalf("got %s", got)
	}
}

func TestRefResolution(t *testing.T) {
	got := typeOf(t, `{
	  "type":"object",
	  "properties":{"p":{"$ref":"#/$defs/Point"}},
	  "required":["p"],
	  "$defs":{"Point":{"type":"object","properties":{"x":{"type":"number"}},"required":["x"]}}
	}`)
	if !strings.Contains(got, "x: number;") {
		t.Fatalf("$ref was not resolved: %s", got)
	}
}

func TestRecursiveRefTerminates(t *testing.T) {
	got := typeOf(t, `{
	  "type":"object",
	  "properties":{"next":{"$ref":"#/$defs/Node"}},
	  "$defs":{"Node":{"type":"object","properties":{"next":{"$ref":"#/$defs/Node"}}}}
	}`)
	if got == "" {
		t.Fatal("recursive schema produced nothing")
	}
	if !strings.Contains(got, "unknown") {
		t.Fatalf("a cycle should bottom out in unknown: %s", got)
	}
}

func TestAnyOfBecomesUnion(t *testing.T) {
	got := typeOf(t, `{"anyOf":[{"type":"string"},{"type":"number"}]}`)
	if got != "string | number" {
		t.Fatalf("got %q", got)
	}
}

func TestAllOfBecomesIntersection(t *testing.T) {
	got := typeOf(t, `{"allOf":[{"type":"object","properties":{"a":{"type":"string"}},"required":["a"]},{"type":"object","properties":{"b":{"type":"number"}},"required":["b"]}]}`)
	if !strings.Contains(got, "&") {
		t.Fatalf("got %q", got)
	}
}

func TestAdditionalPropertiesBecomesIndexSignature(t *testing.T) {
	got := typeOf(t, `{"type":"object","properties":{"a":{"type":"string"}},"additionalProperties":{"type":"number"}}`)
	if !strings.Contains(got, "[key: string]: number;") {
		t.Fatalf("got %s", got)
	}
}

func TestEmptyObjectBecomesRecord(t *testing.T) {
	if got := typeOf(t, `{"type":"object"}`); got != "Record<string, unknown>" {
		t.Fatalf("got %q", got)
	}
}

func TestArgsTypeMarksNoRequiredAsOptional(t *testing.T) {
	_, optional := codegen.ArgsTypeFor(json.RawMessage(`{"type":"object","properties":{"a":{"type":"string"}}}`), "")
	if !optional {
		t.Fatal("a schema with no required props should allow omitting the argument")
	}
	_, optional = codegen.ArgsTypeFor(json.RawMessage(`{"type":"object","properties":{"a":{"type":"string"}},"required":["a"]}`), "")
	if optional {
		t.Fatal("a schema with a required prop must demand the argument")
	}
}

func TestToolFuncName(t *testing.T) {
	cases := map[string]string{
		"take_screenshot": "take_screenshot",
		"fancy-name":      "fancy_name",
		"9lives":          "_9lives",
		"a.b":             "a_b",
	}
	for in, want := range cases {
		if got := codegen.ToolFuncName(in); got != want {
			t.Errorf("ToolFuncName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDescriptionBecomesJSDoc(t *testing.T) {
	got := typeOf(t, `{"type":"object","properties":{"a":{"type":"string","description":"the thing"}},"required":["a"]}`)
	if !strings.Contains(got, "/** the thing */") {
		t.Fatalf("got %s", got)
	}
}

func TestCommentTerminatorIsEscaped(t *testing.T) {
	got := typeOf(t, `{"type":"object","properties":{"a":{"type":"string","description":"ends with */ oops"}},"required":["a"]}`)
	if strings.Contains(got, "*/ oops") {
		t.Fatalf("a description must not be able to close its own comment: %s", got)
	}
}

func sampleNamespace() codegen.Namespace {
	return codegen.Namespace{
		Name:        "demo",
		Server:      "demo-server",
		Description: "a demo",
		Tools: []codegen.Tool{
			{
				Name:        "fancy-name",
				Description: "does a thing",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}},"required":["x"]}`),
			},
			{
				Name:        "noargs",
				InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
			},
		},
	}
}

func TestDeclarationsShape(t *testing.T) {
	out := codegen.Declarations([]codegen.Namespace{sampleNamespace()})
	for _, want := range []string{
		"declare namespace demo {",
		"function fancy_name(args: {",
		"function noargs(args?: Record<string, unknown>): Promise<ToolResult>;",
		"/** a demo */",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("declarations missing %q:\n%s", want, out)
		}
	}
}

func TestModuleShape(t *testing.T) {
	out := codegen.Module([]codegen.Namespace{sampleNamespace()}, "http://127.0.0.1:1234", "sess-1")
	for _, want := range []string{
		`const DEFAULT_ENDPOINT = "http://127.0.0.1:1234";`,
		`const DEFAULT_SESSION = "sess-1";`,
		`export const demo = {`,
		`return __call("demo-server", "fancy-name", args);`,
		`export const tools = {`,
		`export default tools;`,
		`env("MCPX_SESSION")`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("module missing %q", want)
		}
	}
}

func TestModuleCallsUseServerNameNotNamespace(t *testing.T) {
	// The namespace is a TypeScript identifier; the wire call must use the
	// real server name so a renamed namespace still routes correctly.
	out := codegen.Module([]codegen.Namespace{sampleNamespace()}, "http://x", "")
	if strings.Contains(out, `__call("demo",`) {
		t.Fatal("call should use the server name, not the namespace")
	}
}

func TestModuleExposesPathHelpers(t *testing.T) {
	out := codegen.Module([]codegen.Namespace{sampleNamespace()}, "http://x", "")
	for _, want := range []string{
		"export interface PathPair",
		"export const paths = {",
		"export function here(meta: { url: string }): PathPair",
		"export function hereDir(meta: { url: string }): PathPair",
		`env("MCPX_CWD")`,
		`env("MCPX_ENTRY")`,
		`env("MCPX_SCRIPT_DIRS")`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("module missing %q", want)
		}
	}
	// here() must be a function, not a constant: a constant would resolve once
	// inside the client and report the client's path to every importer.
	if strings.Contains(out, "export const here") {
		t.Error("here must be a function so each importing file gets its own path")
	}
}
