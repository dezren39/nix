package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/scripting"
)

func TestHandleDiagnose_Validation(t *testing.T) {
	reg := &Registry{
		testTools: []ToolInfo{
			{
				Namespace: "calc",
				Tool:      "add",
				Function:  "calc.add",
				InputSchema: []byte(`{
					"type": "object",
					"required": ["a", "b"],
					"properties": {
						"a": {"type": "number"},
						"b": {"type": "number"}
					}
				}`),
			},
		},
	}
	s := &Server{reg: reg}

	// Case 1: valid script
	validReq := []byte(`{"source": "const res = await tools.calc.add({a: 1, b: 2});"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/diagnose", bytes.NewReader(validReq))
	w := httptest.NewRecorder()
	s.handleDiagnose(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if valid, _ := resp["valid"].(bool); !valid {
		t.Errorf("expected script to be valid")
	}

	// Case 2: syntax error (unclosed bracket)
	invalidReq := []byte(`{"source": "const res = await tools.calc.add({a: 1, b: 2};"}`)
	req2 := httptest.NewRequest(http.MethodPost, "/v1/diagnose", bytes.NewReader(invalidReq))
	w2 := httptest.NewRecorder()
	s.handleDiagnose(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var resp2 map[string]any
	if err := json.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
		t.Fatal(err)
	}
	if valid, _ := resp2["valid"].(bool); valid {
		t.Errorf("expected script to be invalid")
	}
}

func TestHandleDiagnose_RepairSafety(t *testing.T) {
	reg := &Registry{
		testTools: []ToolInfo{
			{
				Namespace: "calc",
				Tool:      "add",
				Function:  "calc.add",
				InputSchema: []byte(`{"type":"object","required":["a","b"]}`),
			},
		},
	}

	// Provider that fixes the syntax error
	fixingProvider := scripting.NewCallbackProvider("fixer", func(ctx context.Context, req scripting.CompletionRequest) (string, error) {
		return "const res = await tools.calc.add({a: 1, b: 2});", nil
	})

	brokenScript := "const res = await tools.calc.add({a: 1, b: 2};"

	cat := reg.DiagnoseCatalog()
	val := scripting.Validate(brokenScript, &cat)
	if val.Valid {
		t.Fatalf("expected syntax error in source")
	}

	res, err := scripting.Repair(context.Background(), brokenScript, nil, val.Diagnostics, &cat, fixingProvider)
	if err != nil {
		t.Fatalf("repair failed: %v", err)
	}
	if !res.Validation.Valid {
		t.Errorf("repaired source should be valid")
	}

	// Provider that returns another broken script
	brokenProvider := scripting.NewCallbackProvider("broken", func(ctx context.Context, req scripting.CompletionRequest) (string, error) {
		return "const res = ({ broken;", nil
	})
	resBroken, err := scripting.Repair(context.Background(), brokenScript, nil, val.Diagnostics, &cat, brokenProvider)
	if err == nil && resBroken != nil {
		t.Errorf("expected broken repair to be rejected by safety validator")
	}
}

func TestHandleIntent_ProposeWithProvider(t *testing.T) {
	reg := &Registry{
		testTools: []ToolInfo{
			{
				Namespace:   "weather",
				Tool:        "forecast",
				Function:    "weather.forecast",
				Description: "Get weather forecast for a city",
				InputSchema: []byte(`{
					"type": "object",
					"required": ["city"],
					"properties": {"city": {"type": "string"}}
				}`),
			},
		},
	}

	// Callback provider that synthesizes a valid script
	prov := scripting.NewCallbackProvider("mock-ai", func(ctx context.Context, req scripting.CompletionRequest) (string, error) {
		return "const data = await tools.weather.forecast({city: 'Chicago'});\nreturn data;", nil
	})

	cat := reg.DiagnoseCatalog()
	source, err := scripting.Synthesize(context.Background(), "get chicago weather", &cat, prov)
	if err != nil {
		t.Fatalf("synthesize failed: %v", err)
	}
	val := scripting.Validate(source, &cat)
	if !val.Valid {
		t.Fatalf("synthesized script should be valid: %+v", val.Diagnostics)
	}
	if !strings.Contains(source, "tools.weather.forecast") {
		t.Errorf("expected tools.weather.forecast in synthesized script: %s", source)
	}

	// Verify server handleIntent behavior with empty prompt
	s := &Server{reg: reg}
	emptyReq := httptest.NewRequest(http.MethodPost, "/v1/intent", bytes.NewReader([]byte(`{"prompt": ""}`)))
	w := httptest.NewRecorder()
	s.handleIntent(w, emptyReq)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty prompt, got %d", w.Code)
	}
}
