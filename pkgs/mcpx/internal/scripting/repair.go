package scripting

import (
	"context"
	"fmt"
	"strings"

	"github.com/dezren39/mcpx/internal/diagnose"
)

// RepairResult represents the outcome of an automated script repair attempt.
type RepairResult struct {
	RepairedSource string
	OriginalSource string
	Validation     ValidationResult
}

// Repair attempts to fix a failing script using the configured model provider.
// Crucial Safety Invariant: The repaired script is strictly validated before
// acceptance. If the repair does not pass validation or introduces regressions,
// it is rejected so that mcpx never provides or executes a broken script.
func Repair(ctx context.Context, src string, failureErr error, diags []diagnose.Diagnostic, cat *diagnose.Catalog, provider Provider) (*RepairResult, error) {
	if provider == nil {
		return nil, fmt.Errorf("no repair provider available")
	}

	// 1. Gather diagnostic context
	var errDesc strings.Builder
	if failureErr != nil {
		errDesc.WriteString("Runtime Error: " + failureErr.Error() + "\n")
	}
	if len(diags) > 0 {
		errDesc.WriteString("Diagnostics:\n")
		for _, d := range diags {
			errDesc.WriteString("  - " + d.String() + "\n")
		}
	}

	// 2. Gather catalog context
	var catSummary strings.Builder
	if cat != nil && len(cat.Tools) > 0 {
		catSummary.WriteString("Available Tools:\n")
		for _, t := range cat.Tools {
			reqStr := strings.Join(t.Shape.Required, ", ")
			catSummary.WriteString(fmt.Sprintf("  - %s.%s(args: {%s})\n", t.Namespace, t.Func, reqStr))
		}
	}

	// 3. Build Repair Prompt
	systemPrompt := "You are a precise TypeScript engineer for mcpx. " +
		"Your task is to fix the provided failing script. " +
		"Ensure all delimiters, parentheses, and strings are properly closed, and that all tool calls match the tool signatures. " +
		"Respond ONLY with the complete, corrected TypeScript program. No explanations, no markdown fences."

	userPrompt := fmt.Sprintf(`The following mcpx script failed:
---
%s
---

Error details:
%s

%s

Please output the complete repaired TypeScript script that resolves these errors without altering the intended logic.`,
		src, errDesc.String(), catSummary.String())

	// 4. Request repair from model provider
	candidate, err := provider.Complete(ctx, CompletionRequest{
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
		MaxTokens:    3000,
		Temperature:  0.0,
	})
	if err != nil {
		return nil, fmt.Errorf("repair provider completion failed: %w", err)
	}

	candidate = StripFences(candidate)
	if strings.TrimSpace(candidate) == "" {
		return nil, fmt.Errorf("repair provider returned empty script")
	}

	// 5. Verification Gate: Validate candidate script!
	val := Validate(candidate, cat)
	if val.HasFatal() {
		return nil, fmt.Errorf("candidate repair failed validation: %s", val.Summary())
	}

	return &RepairResult{
		RepairedSource: candidate,
		OriginalSource: src,
		Validation:     val,
	}, nil
}
