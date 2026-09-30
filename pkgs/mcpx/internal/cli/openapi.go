package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/dezren39/mcpx/internal/api"
	"github.com/dezren39/mcpx/internal/defaults"

	"github.com/dezren39/mcpx/internal/mcpserver"
	"github.com/dezren39/mcpx/internal/settings"
)

// OpenAPI describes mcpx's HTTP surface.
//
// Generated from the same declarations everything else is -- the /v1
// operation table, the setting registry, the MCP tool list -- so it cannot
// describe an endpoint that does not exist or omit a parameter that does. A
// specification maintained by hand is wrong within two releases, and a wrong
// specification is worse than none because people trust it.
//
// This function used to prove that. It was written by hand beside the
// generated one and described the surface as it had been before #61: a
// `servers` block naming `mcpx serve --transport http`, which now refuses to
// run, and bare /health and /openapi.json paths that 404. It listed none of
// the /v1 operations. So `mcpx openapi` and the daemon's
// /v1/openapi.json were two different documents under one name, and the one
// a person reaches for from the command line was the stale one.
//
// Now there is one document. The /v1 half is api.OpenAPI, the same bytes the
// daemon serves; this adds the two things that are not /v1 operations -- the
// MCP endpoint and a REST path per MCP tool -- and the settings index.
//
// It is the publishable form: every path comes from a declaration compiled
// into the binary, so the document is the same on every machine. Nothing here
// reads a running daemon's configured servers. The per-tool paths below are
// mcpx's own tools; an upstream tool is covered by the declared
// /v1/call/{server}/{tool} template, which is a template for exactly that
// reason.
//
// The daemon's /v1/openapi.json is not this document. It starts from the same
// api.OpenAPI, and every path the two share is identical, but handleOpenAPI
// then adds one path per tool in the live catalogue -- deliberately, so a
// server that appears after startup is described without a restart. Driven
// against a live daemon, configuring one server with eight tools takes it
// from 53 paths to 61:
//
//	/v1/call/demo/echo, /v1/call/demo/boom, ... (8 paths)
//
// So the daemon's document describes this machine and this one describes the
// product. That is the difference worth keeping, and it is why `mcpx openapi`
// does not simply proxy the daemon.
func OpenAPI(version string) map[string]any {
	doc := api.OpenAPI(version)
	paths, _ := doc["paths"].(map[string]any)
	if paths == nil {
		paths = map[string]any{}
		doc["paths"] = paths
	}

	paths[defaults.ProtoMCPPath] = map[string]any{
		"post": map[string]any{
			"summary": "Model Context Protocol endpoint",
			"description": "JSON-RPC 2.0. Supports initialize, tools/list, " +
				"tools/call and ping. Point any MCP host at this URL.",
			"operationId": "mcp",
			"requestBody": jsonBody(map[string]any{
				"type":     "object",
				"required": []string{"jsonrpc", "method"},
				"properties": map[string]any{
					"jsonrpc": map[string]any{"type": "string", "enum": []string{"2.0"}},
					"id":      map[string]any{"description": "absent for a notification"},
					"method":  map[string]any{"type": "string"},
					"params":  map[string]any{"type": "object"},
				},
			}),
			"responses": map[string]any{
				"200": jsonResponse("a JSON-RPC result or error"),
				"202": map[string]any{"description": "a notification was accepted"},
			},
		},
	}

	// One path per MCP tool as well, so a caller that would rather speak REST
	// than JSON-RPC can. The two are the same code underneath; offering only
	// the protocol form would make mcpx reachable from MCP hosts and from
	// nothing else, which is the opposite of the point.
	srv := mcpserver.New(nil, "mcpx", version)
	for _, tool := range srv.Tools() {
		var schema any
		_ = json.Unmarshal(tool.InputSchema, &schema)
		paths["/v1/tools/"+tool.Name] = map[string]any{
			"post": map[string]any{
				"summary":     tool.Description,
				"operationId": tool.Name,
				"tags":        []string{"tools"},
				"requestBody": jsonBody(schema),
				"responses": map[string]any{
					"200": jsonResponse("the tool's text result"),
					"400": jsonResponse("the arguments were rejected"),
				},
			},
		}
	}

	doc["components"] = map[string]any{
		"schemas": map[string]any{"Setting": settingSchema()},
	}
	doc["x-mcpx-settings"] = settingsIndex()
	return doc
}

func jsonBody(schema any) map[string]any {
	return map[string]any{
		"required": true,
		"content": map[string]any{
			"application/json": map[string]any{"schema": schema},
		},
	}
}

func jsonResponse(desc string) map[string]any {
	return map[string]any{
		"description": desc,
		"content": map[string]any{
			"application/json": map[string]any{"schema": map[string]any{"type": "object"}},
		},
	}
}

func settingSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string", "description": "dotted location in a config file"},
			"kind":    map[string]any{"type": "string"},
			"default": map[string]any{"type": "string"},
			"flag":    map[string]any{"type": "string"},
			"env":     map[string]any{"type": "string"},
			"short":   map[string]any{"type": "string"},
		},
	}
}

// settingsIndex publishes every setting as an extension.
//
// Not a path, because settings are not an endpoint. In the document because a
// client generated from this should be able to tell a user what a knob is
// called without a second source.
func settingsIndex() []any {
	sch, err := settings.New(settings.Registry())
	if err != nil {
		return nil
	}
	var out []any
	for _, s := range sch.All() {
		if s.Plumbing {
			continue
		}
		out = append(out, map[string]any{
			"path": s.Path, "kind": s.Kind.String(), "default": s.Default,
			"flag": "--" + s.FlagName(), "env": s.EnvName(), "short": s.Short,
		})
	}
	return out
}

// CmdOpenAPI prints the specification.
func (a *App) CmdOpenAPI(_ context.Context, args []string) error {
	fs := newFlagSet("openapi")
	out := fs.String("o", "", "write to this file instead of stdout")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	doc, err := json.MarshalIndent(OpenAPI(a.Version), "", "  ")
	if err != nil {
		return err
	}
	doc = append(doc, '\n')
	if *out == "" {
		_, err = os.Stdout.Write(doc)
		return err
	}
	if err := os.WriteFile(*out, doc, defaults.PublicMode); err != nil {
		return err
	}
	fmt.Println(*out)
	return nil
}
