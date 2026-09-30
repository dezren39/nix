package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/dezren39/mcpx/internal/defaults"
	"os"

	"github.com/dezren39/mcpx/internal/mcpserver"
	"github.com/dezren39/mcpx/internal/settings"
)

// OpenAPI describes mcpx's HTTP surface.
//
// Generated from the same declarations everything else is -- the command
// table, the setting registry, the MCP tool list -- so it cannot describe an
// endpoint that does not exist or omit a parameter that does. A specification
// maintained by hand is wrong within two releases, and a wrong specification
// is worse than none because people trust it.
func OpenAPI(version string) map[string]any {
	paths := map[string]any{
		"/mcp": map[string]any{
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
		},
		"/health": map[string]any{
			"get": map[string]any{
				"summary":     "Liveness",
				"operationId": "health",
				"responses":   map[string]any{"200": jsonResponse("the server is up")},
			},
		},
		"/openapi.json": map[string]any{
			"get": map[string]any{
				"summary":     "This document",
				"operationId": "openapi",
				"responses":   map[string]any{"200": jsonResponse("the specification")},
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

	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   "mcpx",
			"version": version,
			"summary": "Run TypeScript against your MCP servers.",
			"description": "mcpx keeps a local daemon that owns every MCP server " +
				"process and exposes them through a few tools rather than many, so " +
				"tool schemas never enter a model's context.\n\n" +
				"Every endpoint here is unauthenticated. mcpx binds to loopback by " +
				"default; exposing it further is a deliberate act and the " +
				"responsibility of whatever does it.",
			"license": map[string]any{"name": "MIT"},
		},
		"servers": []any{
			map[string]any{"url": "http://127.0.0.1:8787", "description": "mcpx serve --transport http"},
		},
		"paths": paths,
		"components": map[string]any{
			"schemas": map[string]any{
				"Setting": settingSchema(),
			},
		},
		"x-mcpx-settings": settingsIndex(),
	}
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
