package scripting

import (
	"context"
	"fmt"
	"strings"

	"github.com/dezren39/mcpx/internal/diagnose"
)

// Synthesize turns a natural language user prompt into a valid, executable TypeScript script.
func Synthesize(ctx context.Context, prompt string, cat *diagnose.Catalog, provider Provider) (string, error) {
	if provider == nil {
		return "", fmt.Errorf("no model provider configured for script synthesis")
	}

	var toolSlice strings.Builder
	if cat != nil && len(cat.Tools) > 0 {
		toolSlice.WriteString("Available tools in mcpx:\n")
		for _, t := range cat.Tools {
			reqStr := strings.Join(t.Shape.Required, ", ")
			toolSlice.WriteString(fmt.Sprintf("- await tools.%s.%s({ %s })\n", t.Namespace, t.Func, reqStr))
		}
	}

	systemPrompt := "You write short TypeScript programs for mcpx. " +
		"Every tool is already bound as an async function on `tools.<namespace>.<tool>(...)` or `tools.<tool>(...)`. " +
		"Import nothing, call tools directly using await, log or emit results, and handle errors cleanly. " +
		"Answer with the executable TypeScript code only, no prose and no code fences.\n\n" + toolSlice.String()

	userPrompt := "Task to implement:\n" + prompt

	candidate, err := provider.Complete(ctx, CompletionRequest{
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
		MaxTokens:    2048,
		Temperature:  0.2,
	})
	if err != nil {
		return "", fmt.Errorf("synthesis provider failed: %w", err)
	}

	candidate = StripFences(candidate)
	if strings.TrimSpace(candidate) == "" {
		return "", fmt.Errorf("provider produced an empty script")
	}

	// Validate synthesized candidate
	val := Validate(candidate, cat)
	if val.HasFatal() {
		// Attempt 1 automatic repair pass to recover
		repaired, rerr := Repair(ctx, candidate, nil, val.Diagnostics, cat, provider)
		if rerr == nil && repaired != nil && !repaired.Validation.HasFatal() {
			return repaired.RepairedSource, nil
		}
		return candidate, fmt.Errorf("synthesized script contains fatal errors: %s", val.Summary())
	}

	return candidate, nil
}
