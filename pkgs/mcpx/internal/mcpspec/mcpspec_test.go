package mcpspec

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRevisionsEmbedded(t *testing.T) {
	want := []string{"2024-11-05", "2025-03-26", "2025-06-18", "2025-11-25", "2026-07-28"}
	got := Revisions()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("revisions = %v, want %v", got, want)
	}
	for _, r := range got {
		s, err := Get(r)
		if err != nil || len(s.Defs) < 50 {
			t.Fatalf("%s: %v, %d defs", r, err, len(s.Defs))
		}
	}
}

// Every official example must validate against the definition it is filed under; this is the validator's
// ground truth (a validator that rejects the spec's own examples is wrong, not the examples).
func TestOfficialExamplesValidate(t *testing.T) {
	total := 0
	for _, rev := range Revisions() {
		ex, err := Examples(rev)
		if err != nil {
			t.Fatal(err)
		}
		for name, raw := range ex {
			def := strings.SplitN(name, "/", 2)[0]
			total++
			t.Run(rev+"/"+name, func(t *testing.T) {
				if err := Validate(rev, def, raw); err != nil {
					t.Fatalf("%s: %v", def, err)
				}
				// Strict mode must accept the spec's own examples too, or it flags things the spec does.
				s, _ := Get(rev)
				doc, _ := decode(raw)
				err := s.validateAt(def, doc, "$", true)
				// A known-bad example is asserted bad rather than skipped: the
				// day the spec fixes it, this fails and says to drop the entry,
				// instead of a skip quietly hiding a check that now passes.
				if why, ok := knownBadExamples[rev+"/"+name]; ok {
					if err == nil {
						t.Fatalf("knownBadExamples[%q] (%s) now validates in strict mode; remove the entry", rev+"/"+name, why)
					}
					return
				}
				if err != nil {
					t.Fatalf("strict %s: %v", def, err)
				}
			})
		}
	}
	if total < 100 {
		t.Fatalf("only %d examples found; embed is missing them", total)
	}
}

// Examples the schema itself rejects once the result union is discriminated. Recorded, not hidden: each is
// a spec-repo inconsistency, and TestKnownBadExamplesAreStillBad fails when upstream fixes one.
var knownBadExamples = map[string]string{
	// ReadResourceResult requires ttlMs and cacheScope (2026-07-28 caching); this example has neither. It
	// passes the published ReadResourceResultResponse only because that envelope is
	// anyOf(InputRequiredResult, ReadResourceResult) and InputRequiredResult requires nothing but resultType.
	"2026-07-28/ReadResourceResultResponse/read-resource-result-response.json": "spec example omits required ttlMs/cacheScope",
}

func TestKnownBadExamplesAreStillBad(t *testing.T) {
	for key := range knownBadExamples {
		parts := strings.SplitN(key, "/", 3)
		ex, _ := Examples(parts[0])
		raw, ok := ex[parts[1]+"/"+parts[2]]
		if !ok {
			t.Errorf("%s: example gone; drop it from knownBadExamples", key)
			continue
		}
		if err := ValidateServerMessage(parts[0], raw, "resources/read"); err == nil {
			t.Errorf("%s now validates; drop it from knownBadExamples", key)
		}
	}
}

// Mutating an example must break it: proves the example test is not vacuous.
func TestMutatedExamplesFail(t *testing.T) {
	cases := []struct {
		rev, def, file, drop, wantPath string
	}{
		{"2026-07-28", "CallToolResult", "result-with-unstructured-text.json", "content", "$"},
		{"2026-07-28", "DiscoverResult", "", "supportedVersions", "$"},
	}
	for _, c := range cases {
		ex, _ := Examples(c.rev)
		for name, raw := range ex {
			if !strings.HasPrefix(name, c.def+"/") || (c.file != "" && !strings.HasSuffix(name, c.file)) {
				continue
			}
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatal(err)
			}
			if _, ok := m[c.drop]; !ok {
				continue
			}
			delete(m, c.drop)
			b, _ := json.Marshal(m)
			err := Validate(c.rev, c.def, b)
			if err == nil || !strings.Contains(err.Error(), c.drop) {
				t.Fatalf("%s without %s: err = %v", name, c.drop, err)
			}
			break
		}
	}
}

func TestInvalidFramesNamePath(t *testing.T) {
	cases := []struct {
		name, rev, frame, method, wantPath, wantMsg string
		client                                      bool
	}{
		{"tools-list-missing-tools", "2025-11-25", `{"jsonrpc":"2.0","id":1,"result":{}}`, "tools/list", "$.result", `"tools"`, false},
		{"tool-without-inputSchema", "2025-06-18", `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"a"}]}}`, "tools/list", "$.result.tools[0]", "inputSchema", false},
		{"text-content-text-not-string", "2025-11-25", `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":5}]}}`, "tools/call", "$.result.content[0].text", "string", false},
		{"initialize-missing-serverInfo", "2025-03-26", `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26","capabilities":{}}}`, "initialize", "$.result", "serverInfo", false},
		{"discover-legacy-shape", "2026-07-28", `{"jsonrpc":"2.0","id":1,"result":{"resultType":"complete","protocolVersions":["2026-07-28"],"capabilities":{},"serverInfo":{"name":"x","version":"1"}}}`, "server/discover", "$.result", "missing required property", false},
		{"result-missing-resultType", "2026-07-28", `{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`, "tools/list", "$.result", "resultType", false},
		{"jsonrpc-wrong", "2025-11-25", `{"jsonrpc":"1.0","id":1,"result":{}}`, "", "$.jsonrpc", `"2.0"`, false},
		{"unknown-method", "2025-11-25", `{"jsonrpc":"2.0","id":1,"method":"nope/nope"}`, "", "$.method", "nope/nope", true},
		{"progress-token-bool", "2025-11-25", `{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":true,"progress":1}}`, "", "$.params.progressToken", "string|integer", false},
		{"id-is-float", "2025-11-25", `{"jsonrpc":"2.0","id":1.5,"method":"ping"}`, "", "$.id", "string|integer", true},
		{"error-missing-code", "2025-06-18", `{"jsonrpc":"2.0","id":1,"error":{"message":"x"}}`, "", "$.error", "code", false},
		{"image-data-not-base64", "2025-11-25", `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"image","mimeType":"image/png","data":"!!"}]}}`, "tools/call", "$.result.content[0].data", "base64", false},
		{"log-level-enum", "2025-11-25", `{"jsonrpc":"2.0","method":"notifications/message","params":{"level":"loud","data":1}}`, "", "$.params.level", "not one of", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var err error
			if c.client {
				err = ValidateClientMessage(c.rev, []byte(c.frame))
			} else {
				err = ValidateServerMessage(c.rev, []byte(c.frame), c.method)
			}
			if err == nil {
				t.Fatal("frame validated; want failure")
			}
			e, ok := err.(*Error)
			if !ok {
				t.Fatalf("error %T %v, want *Error", err, err)
			}
			if e.Path != c.wantPath || !strings.Contains(e.Msg, c.wantMsg) {
				t.Fatalf("got %q; want path %q containing %q", err, c.wantPath, c.wantMsg)
			}
		})
	}
}

func TestValidFrames(t *testing.T) {
	cases := []struct{ rev, frame, method string }{
		{"2024-11-05", `{"jsonrpc":"2.0","id":"a","result":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"m","version":"1"}}}`, "initialize"},
		{"2025-06-18", `{"jsonrpc":"2.0","id":1,"result":{}}`, "ping"},
		{"2025-11-25", `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"a","inputSchema":{"type":"object"}}]}}`, "tools/list"},
		{"2026-07-28", `{"jsonrpc":"2.0","id":1,"result":{"resultType":"complete","content":[{"type":"text","text":"hi"}]}}`, "tools/call"},
		{"2026-07-28", `{"jsonrpc":"2.0","method":"notifications/tools/list_changed"}`, ""},
		{"2025-11-25", `{"jsonrpc":"2.0","id":7,"method":"elicitation/create","params":{"message":"m","requestedSchema":{"type":"object","properties":{}}}}`, ""},
	}
	for _, c := range cases {
		if err := ValidateServerMessage(c.rev, []byte(c.frame), c.method); err != nil {
			t.Errorf("%s %s: %v", c.rev, c.frame, err)
		}
	}
}

// Every request method in every revision must map to a result definition, so ValidateServerMessage never
// silently skips a response.
func TestEveryRequestHasResultDef(t *testing.T) {
	for _, rev := range Revisions() {
		s, _ := Get(rev)
		for name, d := range s.Defs {
			if !strings.HasSuffix(name, "Request") || strings.HasPrefix(name, "Client") || strings.HasPrefix(name, "Server") || name == "JSONRPCRequest" {
				continue
			}
			m, _ := d.(map[string]any)
			props, _ := m["properties"].(map[string]any)
			mp, _ := props["method"].(map[string]any)
			method, _ := mp["const"].(string)
			if method == "" {
				continue
			}
			def, _, err := s.ResultDef(method)
			if err != nil || def == "Result" && !strings.Contains(rev, "2026") {
				t.Errorf("%s %s -> %q %v", rev, method, def, err)
			}
		}
	}
}

// The validator ignores keywords it does not know; a refreshed schema that adds one must be noticed.
func TestSchemasUseOnlyKnownKeywords(t *testing.T) {
	known := map[string]bool{"$ref": true, "$schema": true, "$defs": true, "definitions": true, "type": true,
		"properties": true, "required": true, "items": true, "additionalProperties": true, "anyOf": true,
		"oneOf": true, "allOf": true, "enum": true, "const": true, "minimum": true, "maximum": true,
		"maxItems": true, "format": true, "description": true}
	for _, rev := range Revisions() {
		raw, _ := schemaFS.ReadFile("schema/" + rev + "/schema.json")
		v, _ := decode(raw)
		var walk func(x any, keysAreNames bool)
		walk = func(x any, keysAreNames bool) {
			switch t2 := x.(type) {
			case map[string]any:
				for k, sub := range t2 {
					if !keysAreNames && !known[k] {
						t.Errorf("%s uses unsupported keyword %q", rev, k)
					}
					walk(sub, !keysAreNames && (k == "properties" || k == "definitions" || k == "$defs"))
				}
			case []any:
				for _, sub := range t2 {
					walk(sub, false)
				}
			}
		}
		walk(v, false)
	}
}

// The package embeds ~1 MB of schema; it must stay out of the binary.
func TestNotImportedByBinary(t *testing.T) {
	root, _ := filepath.Abs("../..")
	const path = `"github.com/dezren39/mcpx/internal/mcpspec"`
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
		if err != nil {
			return nil
		}
		for _, im := range f.Imports {
			if im.Path.Value == path {
				t.Errorf("%s imports mcpspec from non-test code", p)
			}
		}
		return nil
	})
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root not found at %s", root)
	}
}

// The 2026-07-28 CallToolResultResponse is anyOf(InputRequiredResult, CallToolResult), and
// InputRequiredResult requires only resultType -- so validating the union accepts a "complete" tool
// result with no content at all. Dispatching on resultType is what catches it.
func TestResultTypeDiscriminates(t *testing.T) {
	const rev = "2026-07-28"
	bad := `{"jsonrpc":"2.0","id":1,"result":{"resultType":"complete"}}`
	if err := Validate(rev, "CallToolResultResponse", []byte(bad)); err != nil {
		t.Fatalf("premise: the union itself should admit this, got %v", err)
	}
	err := ValidateServerMessage(rev, []byte(bad), "tools/call")
	if err == nil || !strings.Contains(err.Error(), `"content"`) {
		t.Fatalf("complete tools/call without content: %v", err)
	}
	ir := `{"jsonrpc":"2.0","id":1,"result":{"resultType":"input_required","requestState":"s"}}`
	if err := ValidateServerMessage(rev, []byte(ir), "tools/call"); err != nil {
		t.Fatalf("input_required: %v", err)
	}
	task := `{"jsonrpc":"2.0","id":1,"result":{"resultType":"task","task":{}}}`
	if _, ok := ValidateServerMessage(rev, []byte(task), "tools/call").(*ErrExtension); !ok {
		t.Fatal("resultType task should be reported as an extension frame")
	}
	legacyTask := `{"jsonrpc":"2.0","id":1,"result":{"task":{"taskId":"t","status":"working","createdAt":"x","lastUpdatedAt":"x","ttl":1}}}`
	if err := ValidateServerMessage("2025-11-25", []byte(legacyTask), "tools/call"); err != nil {
		t.Fatalf("2025-11-25 CreateTaskResult: %v", err)
	}
}

func TestStrictRejectsUndefinedProperties(t *testing.T) {
	// "tasks" is a 2025-11-25 server capability; 2026-07-28 moved tasks to an extension.
	f := `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","capabilities":{"tasks":{}},"serverInfo":{"name":"a","version":"1"}}}`
	if err := ValidateServerMessage("2025-06-18", []byte(f), "initialize"); err != nil {
		t.Fatalf("lenient: %v", err)
	}
	err := ValidateServerMessageStrict("2025-06-18", []byte(f), "initialize")
	if e, ok := err.(*Error); !ok || e.Path != "$.result.capabilities.tasks" {
		t.Fatalf("strict: %v", err)
	}
	// Open bags stay open: a tool's inputSchema is a JSON Schema, not protocol structure.
	ok := `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"a","inputSchema":{"type":"object","additionalProperties":false}}],"_meta":{"x":1}}}`
	if err := ValidateServerMessageStrict("2025-06-18", []byte(ok), "tools/list"); err != nil {
		t.Fatalf("strict open bag: %v", err)
	}
}
