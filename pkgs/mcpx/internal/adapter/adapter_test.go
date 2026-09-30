package adapter_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/adapter"
)

func echoSpec() adapter.Spec {
	return adapter.Spec{
		Name: "demo", Command: "echo",
		Tools: []adapter.ToolSpec{
			{
				Name: "say", Args: []string{"said:"},
				Params: []adapter.Param{
					{Name: "text", Required: true},
					{Name: "loud", Type: "boolean", Flag: "--loud"},
					{Name: "tag", Flag: "--tag"},
					{Name: "many", Type: "array", Flag: "-m", Repeat: true},
				},
			},
		},
	}
}

func TestAPositionalParameterBecomesAnArgument(t *testing.T) {
	res, err := echoSpec().Call(context.Background(), "say", map[string]any{"text": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Stdout, "said: hello") {
		t.Errorf("got %q", res.Stdout)
	}
}

func TestABooleanIsItsPresenceNotItsValue(t *testing.T) {
	// --verbose true is wrong for almost every program ever written.
	res, _ := echoSpec().Call(context.Background(), "say",
		map[string]any{"text": "x", "loud": true})
	if !strings.Contains(res.Stdout, "--loud") {
		t.Errorf("a true boolean should appear as the flag: %q", res.Stdout)
	}
	if strings.Contains(res.Stdout, "--loud true") {
		t.Errorf("and only as the flag: %q", res.Stdout)
	}
	res, _ = echoSpec().Call(context.Background(), "say",
		map[string]any{"text": "x", "loud": false})
	if strings.Contains(res.Stdout, "--loud") {
		t.Errorf("a false boolean should not appear at all: %q", res.Stdout)
	}
}

func TestARepeatedFlagIsSentOncePerValue(t *testing.T) {
	// Which is what most programs actually accept, unlike a comma-joined one.
	res, _ := echoSpec().Call(context.Background(), "say",
		map[string]any{"text": "x", "many": []any{"a", "b"}})
	if strings.Count(res.Stdout, "-m") != 2 {
		t.Errorf("expected the flag twice: %q", res.Stdout)
	}
}

func TestAMissingRequiredParameterIsRefusedBeforeRunning(t *testing.T) {
	_, err := echoSpec().Call(context.Background(), "say", map[string]any{})
	if err == nil {
		t.Fatal("a missing required parameter should be refused")
	}
	if !strings.Contains(err.Error(), "text is required") {
		t.Errorf("the message should name it: %v", err)
	}
}

func TestAnUnknownToolNamesWhatExists(t *testing.T) {
	_, err := echoSpec().Call(context.Background(), "nope", nil)
	if err == nil || !strings.Contains(err.Error(), "say") {
		t.Errorf("the error should list the real tools: %v", err)
	}
}

func TestStdinCarriesAParameterInstead(t *testing.T) {
	// Some programs only take input that way, and everything else about them
	// is still worth exposing.
	spec := adapter.Spec{
		Name: "c", Command: "cat",
		Tools: []adapter.ToolSpec{{
			Name: "through", Stdin: "body",
			Params: []adapter.Param{{Name: "body", Required: true}},
		}},
	}
	res, err := spec.Call(context.Background(), "through", map[string]any{"body": "piped"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(res.Stdout) != "piped" {
		t.Errorf("got %q", res.Stdout)
	}
	if strings.Contains(res.Command, "piped") {
		t.Errorf("a stdin parameter must not also reach the command line: %q", res.Command)
	}
}

func TestANonZeroExitIsAResultNotAnError(t *testing.T) {
	// The program ran and said something; deciding that is a failure belongs
	// to whoever asked.
	spec := adapter.Spec{
		Name: "f", Command: "false",
		Tools: []adapter.ToolSpec{{Name: "fail"}},
	}
	res, err := spec.Call(context.Background(), "fail", nil)
	if err != nil {
		t.Fatalf("running it is not an error: %v", err)
	}
	if res.ExitCode == 0 {
		t.Error("the exit code should be reported")
	}
}

func TestJSONOutputIsParsedWhenAsked(t *testing.T) {
	spec := adapter.Spec{
		Name: "j", Command: "echo",
		Tools: []adapter.ToolSpec{{
			Name: "doc", JSON: true,
			Params: []adapter.Param{{Name: "body"}},
		}},
	}
	res, err := spec.Call(context.Background(), "doc", map[string]any{"body": `{"a":1}`})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.JSON) == 0 {
		t.Fatalf("should have parsed: %+v", res)
	}
	var doc map[string]any
	if err := json.Unmarshal(res.JSON, &doc); err != nil || doc["a"] != 1.0 {
		t.Errorf("got %s", res.JSON)
	}
}

func TestSchemaDescribesTheParameters(t *testing.T) {
	// An adapted program should be indistinguishable from a real server to
	// anything reading schemas.
	s := echoSpec().Tools[0].Schema()
	var doc struct {
		Type       string `json:"type"`
		Required   []string
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(s, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Type != "object" {
		t.Errorf("got %q", doc.Type)
	}
	if doc.Properties["loud"].Type != "boolean" {
		t.Errorf("types should survive: %+v", doc.Properties)
	}
}

func TestValidateCatchesContradictions(t *testing.T) {
	bad := adapter.Spec{
		Name: "x", Command: "true",
		Tools: []adapter.ToolSpec{
			{Name: "a", Stdin: "nothere", Params: []adapter.Param{
				{Name: "p", Required: true, Default: "d"},
			}},
			{Name: "a"},
		},
	}
	err := bad.Validate()
	if err == nil {
		t.Fatal("expected problems")
	}
	for _, want := range []string{"two tools named a", "not a parameter", "also has a default"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q from:\n%v", want, err)
		}
	}
}

func TestEnumIsEnforced(t *testing.T) {
	spec := adapter.Spec{
		Name: "e", Command: "echo",
		Tools: []adapter.ToolSpec{{
			Name: "pick", Params: []adapter.Param{
				{Name: "mode", Enum: []string{"fast", "slow"}},
			},
		}},
	}
	if _, err := spec.Call(context.Background(), "pick", map[string]any{"mode": "sideways"}); err == nil {
		t.Fatal("a value outside the enum should be refused")
	}
}
