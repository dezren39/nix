package scripting_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/diagnose"
	"github.com/dezren39/mcpx/internal/scripting"
)

func sampleCatalog() *diagnose.Catalog {
	return &diagnose.Catalog{
		Tools: []diagnose.Tool{
			{
				Namespace: "github",
				Name:      "list_issues",
				Func:      "listIssues",
				Shape: diagnose.ShapeOf(json.RawMessage(`{
					"type": "object",
					"properties": {
						"owner": {"type": "string"},
						"repo": {"type": "string"},
						"state": {"type": "string"}
					},
					"required": ["owner", "repo"]
				}`)),
			},
			{
				Namespace: "fs",
				Name:      "read_file",
				Func:      "readFile",
				Shape: diagnose.ShapeOf(json.RawMessage(`{
					"type": "object",
					"properties": {
						"path": {"type": "string"}
					},
					"required": ["path"]
				}`)),
			},
		},
	}
}

func TestValidationOnValidScript(t *testing.T) {
	src := `
const issues = await tools.github.listIssues({ owner: "dezren39", repo: "mcpx" });
console.log("Found issues:", issues.length);
`
	res := scripting.Validate(src, sampleCatalog())
	if !res.Valid {
		t.Fatalf("expected valid script, got: %s", res.Summary())
	}
	if len(res.Questions) > 0 {
		t.Fatalf("unexpected questions for valid script: %+v", res.Questions)
	}
}

func TestValidationDetectsSyntaxAndUnbalancedDelimiters(t *testing.T) {
	cases := []struct {
		name string
		src  string
		err  string
	}{
		{
			name: "unclosed paren",
			src:  `const x = (1 + 2;`,
			err:  "unclosed delimiter '('",
		},
		{
			name: "mismatched delimiter",
			src:  `const obj = { a: [1, 2} ];`,
			err:  "mismatched delimiter",
		},
		{
			name: "unclosed string",
			src:  "const s = \"hello world\nconst y = 2;",
			err:  "unclosed string literal",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := scripting.Validate(tc.src, nil)
			if res.Valid {
				t.Fatalf("expected invalid result for %s", tc.name)
			}
			found := false
			for _, d := range res.Diagnostics {
				if strings.Contains(d.Message, tc.err) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected diagnostic containing %q, got: %s", tc.err, res.Summary())
			}
		})
	}
}

func TestValidationDetectsMissingToolArgumentsAndAsksQuestions(t *testing.T) {
	src := `
const res = await tools.github.listIssues();
`
	cat := sampleCatalog()
	res := scripting.Validate(src, cat)
	if res.Valid {
		t.Fatalf("expected invalid result for missing required arguments")
	}
	if len(res.Questions) == 0 {
		t.Fatalf("expected clarification questions for missing required argument")
	}
	q := res.Questions[0]
	if q.Field != "owner" && q.Field != "repo" {
		t.Fatalf("expected question for owner or repo, got: %s", q.Field)
	}
}

type mockProvider struct {
	response string
	err      error
}

func (m *mockProvider) Name() string { return "mock" }
func (m *mockProvider) Complete(ctx context.Context, req scripting.CompletionRequest) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.response, nil
}

func TestRepairSucceedsOnValidFix(t *testing.T) {
	brokenSrc := `const x = (1 + 2;`
	validFixed := `const x = (1 + 2);`

	mock := &mockProvider{response: validFixed}
	ctx := context.Background()

	result, err := scripting.Repair(ctx, brokenSrc, nil, nil, nil, mock)
	if err != nil {
		t.Fatalf("repair should succeed: %v", err)
	}
	if result.RepairedSource != validFixed {
		t.Fatalf("expected repaired source %q, got %q", validFixed, result.RepairedSource)
	}
}

func TestRepairRejectsStillBrokenCode(t *testing.T) {
	brokenSrc := `const x = (1 + 2;`
	stillBroken := `const x = [1 + 2;`

	mock := &mockProvider{response: stillBroken}
	ctx := context.Background()

	_, err := scripting.Repair(ctx, brokenSrc, nil, nil, nil, mock)
	if err == nil {
		t.Fatalf("repair must reject candidate that fails validation")
	}
	if !strings.Contains(err.Error(), "candidate repair failed validation") {
		t.Fatalf("expected error mentioning validation failure, got: %v", err)
	}
}

func TestSynthesizeProducesScript(t *testing.T) {
	synthCode := `
const data = await tools.fs.readFile({ path: "/tmp/test.txt" });
console.log(data);
`
	mock := &mockProvider{response: synthCode}
	ctx := context.Background()

	res, err := scripting.Synthesize(ctx, "read /tmp/test.txt", sampleCatalog(), mock)
	if err != nil {
		t.Fatalf("synthesize failed: %v", err)
	}
	if !strings.Contains(res, "readFile") {
		t.Fatalf("expected readFile in synthesized script, got: %s", res)
	}
}
