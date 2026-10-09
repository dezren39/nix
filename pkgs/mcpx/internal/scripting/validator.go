package scripting

import (
	"fmt"
	"strings"

	"github.com/dezren39/mcpx/internal/diagnose"
)

// ClarificationQuestion represents a prompt to ask a user or agent when a script
// has ambiguous or missing parameters.
type ClarificationQuestion struct {
	Tool     string   `json:"tool,omitempty"`
	Field    string   `json:"field,omitempty"`
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
}

// ValidationResult summarizes the static and schema checks on a script.
type ValidationResult struct {
	Valid          bool                    `json:"valid"`
	Diagnostics    []diagnose.Diagnostic   `json:"diagnostics,omitempty"`
	Questions      []ClarificationQuestion `json:"questions,omitempty"`
	SuggestedFixes []string                `json:"suggestedFixes,omitempty"`
}

// HasFatal reports whether any diagnostic is marked fatal.
func (v ValidationResult) HasFatal() bool {
	for _, d := range v.Diagnostics {
		if d.Fatal {
			return true
		}
	}
	return false
}

// Summary returns a human-readable synopsis of the validation result.
func (v ValidationResult) Summary() string {
	if v.Valid && len(v.Diagnostics) == 0 {
		return "script is valid with no diagnostics"
	}
	var sb strings.Builder
	if v.Valid {
		sb.WriteString("script is valid with non-fatal warnings:\n")
	} else {
		sb.WriteString("script validation failed:\n")
	}
	for _, d := range v.Diagnostics {
		sb.WriteString("  - ")
		sb.WriteString(d.String())
		sb.WriteString("\n")
	}
	if len(v.Questions) > 0 {
		sb.WriteString("\nClarifying questions:\n")
		for _, q := range v.Questions {
			sb.WriteString(fmt.Sprintf("  ? [%s] %s\n", q.Field, q.Question))
		}
	}
	return strings.TrimSpace(sb.String())
}

// Validate checks a script for obvious syntax/delimiter errors and compares
// all tool calls against the available catalog schemas.
// Crucial guarantee: It never marks a valid script as invalid.
func Validate(src string, cat *diagnose.Catalog) ValidationResult {
	res := ValidationResult{Valid: true}

	// 1. Delimiter & Syntax pre-flight check
	synDiags := checkSyntaxAndDelimiters(src)
	res.Diagnostics = append(res.Diagnostics, synDiags...)

	// 2. Catalog and tool schema checks
	if cat != nil {
		toolDiags := diagnose.Script(src, *cat)
		res.Diagnostics = append(res.Diagnostics, toolDiags...)
	}

	// 3. Process diagnostics into questions & suggested fixes
	for _, d := range res.Diagnostics {
		if d.Fatal {
			res.Valid = false
		}
		if d.Kind == diagnose.KindMissingArgument {
			res.Questions = append(res.Questions, ClarificationQuestion{
				Tool:     d.Tool,
				Field:    d.Field,
				Question: fmt.Sprintf("Missing required parameter %q for tool %s: what value should be used?", d.Field, d.Tool),
			})
		}
		if d.Fix != "" {
			res.SuggestedFixes = append(res.SuggestedFixes, d.Fix)
		}
	}

	return res
}

type delimToken struct {
	char rune
	line int
}

func checkSyntaxAndDelimiters(src string) []diagnose.Diagnostic {
	var diags []diagnose.Diagnostic
	var stack []delimToken

	line := 1
	inSingleLineComment := false
	inMultiLineComment := false
	inString := rune(0)
	isEscaped := false

	chars := []rune(src)
	n := len(chars)

	for i := 0; i < n; i++ {
		ch := chars[i]

		if ch == '\n' {
			line++
			inSingleLineComment = false
			if inString == '\'' || inString == '"' {
				// unclosed single-line string literal
				diags = append(diags, diagnose.Diagnostic{
					Kind:    diagnose.Kind("syntax-error"),
					Message: fmt.Sprintf("unclosed string literal (%c) before newline", inString),
					Fatal:   true,
					Source:  &diagnose.Site{Line: line - 1},
				})
				inString = rune(0)
			}
			continue
		}

		if inSingleLineComment {
			continue
		}

		if inMultiLineComment {
			if ch == '*' && i+1 < n && chars[i+1] == '/' {
				inMultiLineComment = false
				i++
			}
			continue
		}

		if inString != 0 {
			if isEscaped {
				isEscaped = false
				continue
			}
			if ch == '\\' {
				isEscaped = true
				continue
			}
			if ch == inString {
				inString = rune(0)
			}
			continue
		}

		// Comment starts
		if ch == '/' && i+1 < n {
			if chars[i+1] == '/' {
				inSingleLineComment = true
				i++
				continue
			}
			if chars[i+1] == '*' {
				inMultiLineComment = true
				i++
				continue
			}
		}

		// String starts
		if ch == '\'' || ch == '"' || ch == '`' {
			inString = ch
			continue
		}

		// Delimiters
		switch ch {
		case '(', '{', '[':
			stack = append(stack, delimToken{char: ch, line: line})
		case ')', '}', ']':
			if len(stack) == 0 {
				diags = append(diags, diagnose.Diagnostic{
					Kind:    diagnose.Kind("syntax-error"),
					Message: fmt.Sprintf("unmatched closing delimiter %q", ch),
					Fatal:   true,
					Source:  &diagnose.Site{Line: line},
				})
				continue
			}
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if !matchesDelim(top.char, ch) {
				diags = append(diags, diagnose.Diagnostic{
					Kind:    diagnose.Kind("syntax-error"),
					Message: fmt.Sprintf("mismatched delimiter: opened %q on line %d, closed with %q on line %d", top.char, top.line, ch, line),
					Fatal:   true,
					Source:  &diagnose.Site{Line: line},
				})
			}
		}
	}

	if inMultiLineComment {
		diags = append(diags, diagnose.Diagnostic{
			Kind:    diagnose.Kind("syntax-error"),
			Message: "unclosed multi-line comment (/* ... */)",
			Fatal:   true,
			Source:  &diagnose.Site{Line: line},
		})
	}
	if inString == '`' {
		diags = append(diags, diagnose.Diagnostic{
			Kind:    diagnose.Kind("syntax-error"),
			Message: "unclosed template literal (`)",
			Fatal:   true,
			Source:  &diagnose.Site{Line: line},
		})
	}

	for _, unclosed := range stack {
		diags = append(diags, diagnose.Diagnostic{
			Kind:    diagnose.Kind("syntax-error"),
			Message: fmt.Sprintf("unclosed delimiter %q opened on line %d", unclosed.char, unclosed.line),
			Fatal:   true,
			Source:  &diagnose.Site{Line: unclosed.line},
		})
	}

	return diags
}

func matchesDelim(open, close rune) bool {
	return (open == '(' && close == ')') ||
		(open == '{' && close == '}') ||
		(open == '[' && close == ']')
}
