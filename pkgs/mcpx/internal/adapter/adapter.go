// Package adapter turns things that are not MCP servers into MCP servers.
//
// An enormous amount of capability already exists as command-line programs.
// Writing an MCP server to wrap one is a day's work that produces a process
// whose only job is to shell out, and there are hundreds of such programs.
//
// So mcpx wraps them from configuration. A declaration names a binary, the
// subcommands worth exposing and what each takes, and mcpx presents it as a
// server like any other -- appearing in `mcpx ls`, callable from a script,
// reachable over MCP.
//
// This is deliberately not a general shell escape. A tool here is a named
// subcommand with declared parameters, so a model cannot invent a command
// line, and what is reachable is exactly what somebody wrote down.
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Spec declares a command-line program as a server.
type Spec struct {
	// Name is the namespace the tools appear under.
	Name string `json:"name"`
	// Command is the binary.
	Command string `json:"command"`
	// Args are prepended to every invocation, for a program reached through
	// a wrapper: {"command":"docker","args":["run","--rm","thing"]}.
	Args []string `json:"args,omitempty"`
	// Env is added to the environment of every invocation.
	Env map[string]string `json:"env,omitempty"`
	// Dir is the working directory. Empty means the caller's.
	Dir string `json:"dir,omitempty"`
	// Description is shown beside the namespace.
	Description string `json:"description,omitempty"`
	// Instructions explain conventions the schemas cannot, exactly as a real
	// server's initialize does.
	Instructions string `json:"instructions,omitempty"`
	// Timeout bounds every invocation. Empty means 60s.
	Timeout string `json:"timeout,omitempty"`
	// Tools are the subcommands worth exposing.
	Tools []ToolSpec `json:"tools"`
}

// ToolSpec is one subcommand.
type ToolSpec struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Args are the fixed arguments that select this subcommand.
	Args []string `json:"args,omitempty"`
	// Params are the arguments a caller supplies.
	Params []Param `json:"params,omitempty"`
	// Stdin names a parameter whose value is written to stdin instead of
	// appearing on the command line. Some programs only take input that way,
	// and everything else about them is worth exposing.
	Stdin string `json:"stdin,omitempty"`
	// JSON parses the output as JSON before returning it, so a caller gets
	// structure rather than a string containing structure.
	JSON bool `json:"json,omitempty"`
	// Timeout overrides the server's for this tool.
	Timeout string `json:"timeout,omitempty"`
}

// Param is one argument.
type Param struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Type is string, number, integer, boolean or array. Defaults to string.
	Type     string `json:"type,omitempty"`
	Required bool   `json:"required,omitempty"`
	// Flag is the option this parameter becomes: "--path". Empty makes it
	// positional, in declaration order.
	Flag string `json:"flag,omitempty"`
	// Repeat sends an array as several occurrences of the flag rather than
	// one comma-joined value, which is what most programs actually accept.
	Repeat bool `json:"repeat,omitempty"`
	// Default is used when the caller omits it.
	Default string `json:"default,omitempty"`
	// Enum restricts the value.
	Enum []string `json:"enum,omitempty"`
}

// Validate checks a declaration before anything runs it.
//
// The failures here are all of the kind that would otherwise surface as a
// confusing command line: a tool with no name, a parameter that is required
// and also has a default, a stdin parameter that does not exist.
func (s Spec) Validate() error {
	var problems []string
	if strings.TrimSpace(s.Name) == "" {
		problems = append(problems, "the server needs a name")
	}
	if strings.TrimSpace(s.Command) == "" {
		problems = append(problems, s.Name+": no command")
	}
	if _, err := parseDuration(s.Timeout, time.Minute); err != nil {
		problems = append(problems, s.Name+": timeout "+err.Error())
	}
	seen := map[string]bool{}
	for _, t := range s.Tools {
		if strings.TrimSpace(t.Name) == "" {
			problems = append(problems, s.Name+": a tool has no name")
			continue
		}
		if seen[t.Name] {
			problems = append(problems, s.Name+": two tools named "+t.Name)
		}
		seen[t.Name] = true

		params := map[string]bool{}
		for _, p := range t.Params {
			if p.Name == "" {
				problems = append(problems, s.Name+"."+t.Name+": a parameter has no name")
				continue
			}
			params[p.Name] = true
			if p.Required && p.Default != "" {
				problems = append(problems, fmt.Sprintf(
					"%s.%s: %s is required and also has a default; one of those is wrong",
					s.Name, t.Name, p.Name))
			}
		}
		if t.Stdin != "" && !params[t.Stdin] {
			problems = append(problems, fmt.Sprintf(
				"%s.%s: stdin names %q, which is not a parameter",
				s.Name, t.Name, t.Stdin))
		}
		if _, err := parseDuration(t.Timeout, 0); err != nil {
			problems = append(problems, s.Name+"."+t.Name+": timeout "+err.Error())
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("adapter is not usable:\n  %s", strings.Join(problems, "\n  "))
	}
	return nil
}

func parseDuration(v string, fallback time.Duration) (time.Duration, error) {
	if strings.TrimSpace(v) == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("is not a duration: %q", v)
	}
	return d, nil
}

// Schema renders one tool's parameters as JSON Schema, so an adapted program
// is indistinguishable from a real server to anything reading schemas.
func (t ToolSpec) Schema() json.RawMessage {
	props := map[string]any{}
	var required []string
	for _, p := range t.Params {
		typ := p.Type
		if typ == "" {
			typ = "string"
		}
		entry := map[string]any{"type": typ}
		if p.Description != "" {
			entry["description"] = p.Description
		}
		if len(p.Enum) > 0 {
			entry["enum"] = p.Enum
		}
		if typ == "array" {
			entry["items"] = map[string]any{"type": "string"}
		}
		props[p.Name] = entry
		if p.Required {
			required = append(required, p.Name)
		}
	}
	doc := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		sort.Strings(required)
		doc["required"] = required
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	return b
}

// Result is what an invocation produced.
type Result struct {
	Stdout   string          `json:"stdout,omitempty"`
	Stderr   string          `json:"stderr,omitempty"`
	JSON     json.RawMessage `json:"json,omitempty"`
	ExitCode int             `json:"exitCode"`
	Command  string          `json:"command"`
}

// Call runs one tool.
func (s Spec) Call(ctx context.Context, tool string, args map[string]any) (*Result, error) {
	var spec *ToolSpec
	for i := range s.Tools {
		if s.Tools[i].Name == tool {
			spec = &s.Tools[i]
			break
		}
	}
	if spec == nil {
		names := make([]string, 0, len(s.Tools))
		for _, t := range s.Tools {
			names = append(names, t.Name)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("%s has no tool %q; it has %s",
			s.Name, tool, strings.Join(names, ", "))
	}

	argv, stdin, err := spec.buildArgs(args)
	if err != nil {
		return nil, err
	}
	full := append(append([]string{}, s.Args...), argv...)

	timeout, _ := parseDuration(spec.Timeout, 0)
	if timeout == 0 {
		timeout, _ = parseDuration(s.Timeout, time.Minute)
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, s.Command, full...)
	cmd.Dir = s.Dir
	cmd.Env = os.Environ()
	for k, v := range s.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	runErr := cmd.Run()

	res := &Result{
		Stdout:  out.String(),
		Stderr:  errb.String(),
		Command: s.Command + " " + strings.Join(full, " "),
	}
	if ee, ok := runErr.(*exec.ExitError); ok {
		res.ExitCode = ee.ExitCode()
	} else if runErr != nil {
		if cctx.Err() != nil {
			return res, fmt.Errorf("%s.%s did not finish within %s", s.Name, tool, timeout)
		}
		return res, runErr
	}
	if spec.JSON && out.Len() > 0 {
		var probe any
		if json.Unmarshal(out.Bytes(), &probe) == nil {
			res.JSON = json.RawMessage(out.Bytes())
			// Kept as well as the parse: a caller that wanted text should not
			// be forced to re-encode it.
		}
	}
	// A non-zero exit is a result, not an error. The program ran and said
	// something; deciding that is a failure belongs to whoever asked.
	return res, nil
}

// buildArgs turns named arguments into a command line.
func (t ToolSpec) buildArgs(args map[string]any) ([]string, string, error) {
	out := append([]string{}, t.Args...)
	stdin := ""

	for _, p := range t.Params {
		raw, given := args[p.Name]
		if !given || raw == nil {
			if p.Required {
				return nil, "", fmt.Errorf("%s is required", p.Name)
			}
			if p.Default == "" {
				continue
			}
			raw = p.Default
		}
		if p.Name == t.Stdin {
			stdin = toString(raw)
			continue
		}
		values, err := toValues(raw)
		if err != nil {
			return nil, "", fmt.Errorf("%s: %w", p.Name, err)
		}
		if len(p.Enum) > 0 {
			for _, v := range values {
				if !contains(p.Enum, v) {
					return nil, "", fmt.Errorf("%s must be one of %s, got %q",
						p.Name, strings.Join(p.Enum, ", "), v)
				}
			}
		}
		// A boolean flag is its presence, not its value: --verbose true is
		// wrong for almost every program ever written.
		if p.Type == "boolean" {
			if len(values) == 1 && values[0] == "true" && p.Flag != "" {
				out = append(out, p.Flag)
			}
			continue
		}
		if p.Flag == "" {
			out = append(out, values...)
			continue
		}
		if p.Repeat {
			for _, v := range values {
				out = append(out, p.Flag, v)
			}
			continue
		}
		out = append(out, p.Flag, strings.Join(values, ","))
	}
	return out, stdin, nil
}

func toValues(v any) ([]string, error) {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			out = append(out, toString(e))
		}
		return out, nil
	case []string:
		return t, nil
	}
	return []string{toString(v)}, nil
}

func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case nil:
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func contains(list []string, v string) bool {
	for _, e := range list {
		if e == v {
			return true
		}
	}
	return false
}

// Probe checks the binary exists before anything tries to call it.
func (s Spec) Probe() error {
	if _, err := exec.LookPath(s.Command); err != nil {
		return fmt.Errorf("%s: %s is not on PATH", s.Name, s.Command)
	}
	return nil
}
