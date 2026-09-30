package api

import (
	"encoding/json"
	"strings"

	"github.com/dezren39/mcpx/internal/settings"
)

// OpenAPI describes the daemon's /v1 API.
//
// Generated from the table rather than written, for the reason every
// generated document is: a specification maintained by hand is wrong within
// two releases, and a wrong specification is worse than none because people
// trust it.
//
// This is the /v1 half of the document `mcpx openapi` prints. That one adds
// the MCP endpoint, a REST path per mcpx tool and the settings index; the
// paths here are the same bytes in both, because there is one table.
func OpenAPI(version string) map[string]any {
	paths := map[string]any{}
	for _, op := range Ops() {
		item, _ := paths[op.Path].(map[string]any)
		if item == nil {
			item = map[string]any{}
			paths[op.Path] = item
		}
		item[strings.ToLower(op.Method)] = operation(op)
	}
	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   "mcpx daemon",
			"version": version,
			"summary": "The local API that owns every MCP server process.",
			"description": "One daemon per configuration, listening on a unix socket and " +
				"on loopback TCP. Everything here is also reachable as an MCP tool " +
				"named mcpx_<operationId>, except the event stream, which MCP " +
				"carries as subscriptions/listen instead.\n\n" +
				"Every endpoint is unauthenticated. The socket's file permissions " +
				"are the access control; binding the TCP listener wider than " +
				"loopback is a deliberate act and the responsibility of whatever " +
				"does it. Operations marked x-mcpx-admin are the ones a scoped " +
				"token would have to grant separately.",
			"license": map[string]any{"name": "MIT"},
		},
		"servers": []any{
			map[string]any{"url": "http://mcpx", "description": "over the daemon's unix socket"},
			// A server variable rather than an absolute URL. daemon.port
			// defaults to 0, which means "choose one at start-up", so the
			// literal this replaced -- http://127.0.0.1:0 -- was an address
			// nothing can ever be listening on. Offering it as a `servers`
			// entry is the same defect as the entry it replaced, which named
			// a command that refuses to run: a destination a client cannot
			// send to. The variable says the port is chosen and where to
			// read it, and keeps the document identical on every machine.
			map[string]any{
				"url":         "http://127.0.0.1:{port}",
				"description": "the loopback endpoint; GET /v1/health reports the port this daemon chose",
				"variables": map[string]any{
					"port": map[string]any{
						"default":     defaultPort(),
						"description": "the daemon.port setting; its default of 0 means the daemon picks a free port and reports it from GET /v1/health",
					},
				},
			},
		},
		"paths": paths,
	}
}

// defaultPort is the declared default of daemon.port, read from the registry
// so the document cannot disagree with `mcpx settings` about it.
func defaultPort() string {
	for _, s := range settings.Registry() {
		if s.Path == "daemon.port" {
			return s.Default
		}
	}
	// Unreachable while the setting exists; a guard test holds that.
	return "0"
}

func operation(op Op) map[string]any {
	out := map[string]any{
		"operationId":   op.Name,
		"summary":       op.Summary,
		"description":   op.Description,
		"x-mcpx-admin":  op.Admin,
		"x-mcpx-stream": op.Streams,
	}
	if !op.Streams {
		out["x-mcpx-tool"] = op.ToolName()
	}
	var params []any
	body := map[string]any{}
	var required []string
	for _, p := range op.Params {
		if p.Raw {
			continue
		}
		if p.In == InBody {
			var schema any
			_ = json.Unmarshal([]byte(p.schema()), &schema)
			body[p.Name] = schema
			if p.Required {
				required = append(required, p.Name)
			}
			continue
		}
		var schema any
		_ = json.Unmarshal([]byte(p.schema()), &schema)
		params = append(params, map[string]any{
			"name": p.Name, "in": string(p.In),
			"required": p.Required || p.In == InPath,
			"schema":   schema,
			// The description lives on the schema too, but a reader of a
			// rendered document looks for it on the parameter.
			"description": p.Desc,
		})
	}
	if len(params) > 0 {
		out["parameters"] = params
	}
	if raw, ok := op.RawParam(); ok {
		out["requestBody"] = map[string]any{
			"required":    raw.Required,
			"description": raw.Desc,
			"content": map[string]any{
				"application/octet-stream": map[string]any{
					"schema": map[string]any{"type": "string", "format": "binary"}},
			},
		}
	} else if op.Method != "GET" {
		schema := map[string]any{"type": "object", "properties": body}
		if len(required) > 0 {
			schema["required"] = required
		}
		if wrapper, ok := op.bodyIsOneObject(); ok {
			// These routes take the document itself, not an object with the
			// document inside it.
			if inner, found := body[wrapper]; found {
				schema = map[string]any{"allOf": []any{inner}}
			}
		}
		out["requestBody"] = map[string]any{
			"required": len(required) > 0,
			"content": map[string]any{
				"application/json": map[string]any{"schema": schema},
			},
		}
	}
	content := map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "object"}}}
	switch {
	case op.Streams:
		content = map[string]any{"text/event-stream": map[string]any{"schema": map[string]any{"type": "string"}}}
	case op.Text:
		content = map[string]any{"text/plain": map[string]any{"schema": map[string]any{"type": "string"}}}
	}
	out["responses"] = map[string]any{
		"200": map[string]any{"description": op.Summary, "content": content},
	}
	return out
}
