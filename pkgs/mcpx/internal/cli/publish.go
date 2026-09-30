package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dezren39/mcpx/internal/defaults"
	"os"
	"sort"
	"strings"

	"github.com/dezren39/mcpx/internal/daemon"
)

// toolPaths renders every upstream tool as an OpenAPI path.
//
// Without this the document described mcpx's own ten tools and nothing else,
// so the three hundred tools it actually fronts were reachable from MCP and
// from a script and from nowhere a generated client could see. A
// specification that describes the wrapper but not what it wraps is a
// specification of the wrong thing.
func toolPaths(tools []daemon.ToolInfo) map[string]any {
	out := map[string]any{}
	for _, t := range tools {
		var schema any
		if len(t.InputSchema) > 0 {
			_ = json.Unmarshal(t.InputSchema, &schema)
		}
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}
		desc := t.Description
		if desc == "" {
			desc = t.Namespace + "." + t.Tool
		}
		out["/v1/call/"+t.Namespace+"/"+t.Tool] = map[string]any{
			"post": map[string]any{
				"summary":     firstLine(desc),
				"description": desc,
				"operationId": t.Namespace + "_" + t.Tool,
				"tags":        []string{t.Namespace},
				"requestBody": jsonBody(schema),
				"responses": map[string]any{
					"200": jsonResponse("the tool's result"),
					"400": jsonResponse("the arguments were rejected"),
				},
			},
		}
	}
	return out
}

// CmdSchema publishes tool types in whichever form the caller wants.
//
// Three forms, because three different things consume them. TypeScript is
// what a script imports. JSON Schema is what a validator and most MCP
// tooling reads. OpenAPI is what a client generator reads. They are the same
// information, and a caller should not have to reshape one into another.
func (a *App) CmdSchema(ctx context.Context, args []string) error {
	args = hoistFlags(args, map[string]bool{"ns": true, "format": true, "o": true})
	fs := newFlagSet("schema")
	format := fs.String("format", "json-schema",
		"json-schema, openapi, typescript or mcp")
	nsFlag := fs.String("ns", "", "restrict to these namespaces")
	out := fs.String("o", "", "write to this file instead of stdout")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	ns := splitCSV(*nsFlag)
	if len(ns) == 0 {
		ns = splitAll(fs.Args())
	}

	c, err := a.ensure(ctx)
	if err != nil {
		return err
	}
	if len(ns) > 0 {
		if err := a.ensureSchemas(ctx, c, ns); err != nil {
			return err
		}
	} else if err := a.ensureAnySchemas(ctx, c); err != nil {
		return err
	}

	var text string
	switch *format {
	case "typescript", "ts":
		text, err = c.Types(ctx, ns, true, a.Profile)
		if err != nil {
			return err
		}

	case "json-schema", "jsonschema", "json":
		tools, terr := c.Tools(ctx, ns)
		if terr != nil {
			return terr
		}
		text, err = renderJSONSchema(tools)
		if err != nil {
			return err
		}

	case "mcp":
		// The shape an MCP tools/list reply carries, for anything that
		// already knows how to read one.
		tools, terr := c.Tools(ctx, ns)
		if terr != nil {
			return terr
		}
		list := make([]map[string]any, 0, len(tools))
		for _, t := range tools {
			list = append(list, map[string]any{
				"name":        t.Namespace + "_" + t.Tool,
				"description": t.Description,
				"inputSchema": json.RawMessage(t.InputSchema),
			})
		}
		b, merr := json.MarshalIndent(map[string]any{"tools": list}, "", "  ")
		if merr != nil {
			return merr
		}
		text = string(b)

	case "openapi":
		tools, terr := c.Tools(ctx, ns)
		if terr != nil {
			return terr
		}
		doc := OpenAPI(a.Version)
		paths, _ := doc["paths"].(map[string]any)
		for k, v := range toolPaths(tools) {
			paths[k] = v
		}
		b, merr := json.MarshalIndent(doc, "", "  ")
		if merr != nil {
			return merr
		}
		text = string(b)

	default:
		return fmt.Errorf("no format %q; json-schema, openapi, typescript or mcp", *format)
	}

	if *out != "" {
		if err := os.WriteFile(*out, []byte(text+"\n"), defaults.PublicMode); err != nil {
			return err
		}
		fmt.Println(*out)
		return nil
	}
	fmt.Println(strings.TrimRight(text, "\n"))
	return nil
}

// renderJSONSchema emits one document with every tool under $defs.
//
// $defs rather than a bare map because that is where a JSON Schema reader
// looks for named subschemas, and it means the result is itself a valid
// schema document rather than a bag of them.
func renderJSONSchema(tools []daemon.ToolInfo) (string, error) {
	defs := map[string]any{}
	byNS := map[string][]string{}
	for _, t := range tools {
		var schema any
		if len(t.InputSchema) > 0 {
			_ = json.Unmarshal(t.InputSchema, &schema)
		}
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}
		if m, ok := schema.(map[string]any); ok && t.Description != "" {
			if _, has := m["description"]; !has {
				m["description"] = t.Description
			}
		}
		name := t.Namespace + "." + t.Tool
		defs[name] = schema
		byNS[t.Namespace] = append(byNS[t.Namespace], name)
	}
	namespaces := map[string]any{}
	for ns, names := range byNS {
		sort.Strings(names)
		namespaces[ns] = names
	}
	b, err := json.MarshalIndent(map[string]any{
		"$schema":           "https://json-schema.org/draft/2020-12/schema",
		"title":             "mcpx tool arguments",
		"description":       "One subschema per tool, named <namespace>.<tool>.",
		"$defs":             defs,
		"x-mcpx-namespaces": namespaces,
	}, "", "  ")
	return string(b), err
}

// The per-tool POST routes this file used to serve live on the daemon now,
// as POST /v1/call/{server}/{tool}. They were reachable only while a second
// HTTP server was running, which is the problem they were part of.
