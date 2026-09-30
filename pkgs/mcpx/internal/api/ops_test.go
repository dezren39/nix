package api_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/api"
)

func op(t *testing.T, name string) api.Op {
	t.Helper()
	o, ok := api.ByName(name)
	if !ok {
		t.Fatalf("no operation %q", name)
	}
	return o
}

func TestArgumentsLandWhereTheRouteExpectsThem(t *testing.T) {
	// The whole proxy is this function. A caller filling in a tool call
	// should not have to know which of its arguments the daemon reads from
	// the URL and which from the body.
	o := op(t, "task_result")
	path, body, err := o.Request(map[string]any{"id": "tsk-1", "waitMs": 250})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/v1/tasks/tsk-1/result?waitMs=250" {
		t.Errorf("path = %q", path)
	}
	if len(body) != 0 {
		t.Errorf("a GET should carry no body, got %s", body)
	}
}

func TestBodyParametersBecomeAJSONObject(t *testing.T) {
	o := op(t, "complete")
	_, body, err := o.Request(map[string]any{
		"server":   "demo",
		"ref":      map[string]any{"type": "ref/prompt", "name": "summarise"},
		"argument": map[string]any{"name": "style", "value": "in"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["server"] != "demo" {
		t.Errorf("body = %s", body)
	}
	if _, ok := doc["ref"].(map[string]any); !ok {
		t.Errorf("ref should pass through as an object: %s", body)
	}
}

func TestSomeRoutesTakeTheDocumentItself(t *testing.T) {
	// POST /v1/log takes a record, not an object with a record inside it.
	// Getting this wrong produces a log entry whose every field is nested
	// one level too deep, which parses fine and is useless.
	o := op(t, "log_record")
	path, body, err := o.Request(map[string]any{
		"level":  "warn",
		"record": map[string]any{"msg": "hello", "event": "plugin.tool"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/v1/log?level=warn" {
		t.Errorf("path = %q", path)
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["msg"] != "hello" {
		t.Errorf("the record should be the body: %s", body)
	}
}

func TestAMissingPathParameterIsRefusedRatherThanSentAsALiteralBrace(t *testing.T) {
	o := op(t, "task_get")
	if _, _, err := o.Request(map[string]any{}); err == nil {
		t.Fatal("a request with no id should be refused")
	}
	if _, _, err := o.Request(map[string]any{"nope": "x"}); err == nil {
		t.Fatal("an unknown argument should be refused, not silently dropped")
	}
}

func TestEverySchemaIsValidJSON(t *testing.T) {
	for _, o := range api.Ops() {
		var doc map[string]any
		if err := json.Unmarshal(o.InputSchema(), &doc); err != nil {
			t.Errorf("%s: %v\n%s", o.Name, err, o.InputSchema())
			continue
		}
		props, _ := doc["properties"].(map[string]any)
		for _, p := range o.Params {
			if _, ok := props[p.Name]; !ok {
				t.Errorf("%s: %s is declared but not in the schema", o.Name, p.Name)
			}
		}
	}
}

func TestOpNamesAreSnakeCase(t *testing.T) {
	for _, o := range api.Ops() {
		if o.Name != strings.ToLower(o.Name) || strings.ContainsAny(o.Name, "- /.") {
			t.Errorf("%q is not snake_case", o.Name)
		}
	}
}

func TestOpenAPIDescribesEveryOperation(t *testing.T) {
	doc := api.OpenAPI("1.2.3")
	paths, _ := doc["paths"].(map[string]any)
	if len(paths) == 0 {
		t.Fatal("no paths")
	}
	for _, o := range api.Ops() {
		item, _ := paths[o.Path].(map[string]any)
		if item == nil {
			t.Errorf("%s is not in the document", o.Path)
			continue
		}
		if _, ok := item[strings.ToLower(o.Method)]; !ok {
			t.Errorf("%s %s is not in the document", o.Method, o.Path)
		}
	}
	// Two operations share /v1/log, one reading and one appending. A path
	// item that kept only the last would silently lose one of them.
	logItem, _ := paths["/v1/log"].(map[string]any)
	if len(logItem) != 2 {
		t.Errorf("/v1/log should carry both get and post, got %v", logItem)
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "1.2.3") {
		t.Error("the version should reach the document")
	}
}

// Every parameter says what it is.
//
// A generated CLI command prints one usage line per parameter, so an empty
// Desc is a flag followed by a blank line -- and the same blank reaches the
// MCP tool's input schema, the OpenAPI document and the generated TypeScript,
// because all four are built from this table. 32 of them shipped that way
// before the table generated a command for each.
func TestEveryParameterIsDescribed(t *testing.T) {
	n := 0
	for _, op := range api.Ops() {
		for _, p := range op.Params {
			n++
			if strings.TrimSpace(p.Desc) == "" {
				t.Errorf("%s: parameter %q has no description; it would print as a "+
					"usage line with nothing after it", op.Name, p.Name)
			}
		}
	}
	if n == 0 {
		t.Fatal("no parameters found; the table changed shape and this test checks nothing")
	}
}

// TestIdempotentHintIsTrue: a client may retry an idempotent tool without
// asking, so the hint has to be true where it is given. It was derived as
// "destructive implies idempotent", which made restart claim it -- and a
// second restart kills the calls the first one let start.
func TestIdempotentHintIsTrue(t *testing.T) {
	want := map[string]bool{
		"restart": false, "refresh": false, "exec": false, "call": false,
		"artifact_delete": true, "shutdown": true, "task_cancel": true,
		"settings_set": true, "status": true,
	}
	for _, op := range api.Ops() {
		w, ok := want[op.Name]
		if !ok {
			continue
		}
		if got, _ := op.Annotations()["idempotentHint"].(bool); got != w {
			t.Errorf("%s: idempotentHint = %v, want %v", op.Name, got, w)
		}
	}
}
