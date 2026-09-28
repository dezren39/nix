package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/mcpserver"
	"github.com/dezren39/mcpx/internal/openapi"
)

// APISpec declares an OpenAPI document as a source of tools.
type APISpec struct {
	Name              string            `json:"name"`
	Spec              string            `json:"spec"`
	BaseURL           string            `json:"baseUrl,omitempty"`
	Include           []string          `json:"include,omitempty"`
	Exclude           []string          `json:"exclude,omitempty"`
	Methods           []string          `json:"methods,omitempty"`
	IncludeDeprecated bool              `json:"includeDeprecated,omitempty"`
	Headers           map[string]string `json:"headers,omitempty"`
	Description       string            `json:"description,omitempty"`
}

// APIFile is a document of them.
type APIFile struct {
	APIs []APISpec `json:"apis"`
}

// loadAPIs reads declarations from paths.apis.
func (a *App) loadAPIs() ([]APISpec, error) {
	paths := splitPathList(a.Settings().String("paths.apis"))
	var out []APISpec
	for _, p := range paths {
		if p == "" {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("reading API declarations from %s: %w", p, err)
		}
		var doc APIFile
		if err := json.Unmarshal(config.StripJSONC(b), &doc); err != nil {
			var bare []APISpec
			if json.Unmarshal(config.StripJSONC(b), &bare) != nil {
				return nil, fmt.Errorf("%s is not a valid API file: %w", p, err)
			}
			doc.APIs = bare
		}
		out = append(out, doc.APIs...)
	}
	return out, nil
}

// buildAPI turns one declaration into callable tools.
func (a *App) buildAPI(ctx context.Context, s APISpec) (*openapi.API, error) {
	spec, err := openapi.Load(ctx, s.Spec, 0)
	if err != nil {
		return nil, err
	}
	// Environment expansion in headers, so a declaration can be checked in
	// and a token cannot. ${VAR} rather than $VAR because a header value
	// legitimately contains dollar signs.
	headers := map[string]string{}
	for k, v := range s.Headers {
		headers[k] = os.Expand(v, func(name string) string { return os.Getenv(name) })
	}
	return openapi.Build(spec, openapi.Options{
		Name: s.Name, BaseURL: s.BaseURL,
		Include: s.Include, Exclude: s.Exclude, Methods: s.Methods,
		IncludeDeprecated: s.IncludeDeprecated, Headers: headers,
	})
}

// CmdAPI inspects and calls OpenAPI-described services.
func (a *App) CmdAPI(ctx context.Context, args []string) error {
	sub := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	args = hoistFlags(args, map[string]bool{
		"spec": true, "base-url": true, "name": true,
		"include": true, "exclude": true, "methods": true, "limit": true,
	})
	fs := newFlagSet("api")
	specFlag := fs.String("spec", "", "an OpenAPI document: a path or a URL")
	baseURL := fs.String("base-url", "", "override the server URL the document declares")
	name := fs.String("name", "", "namespace for the generated tools")
	include := fs.String("include", "", "only operations matching these words, comma separated")
	exclude := fs.String("exclude", "", "drop operations matching these words")
	methods := fs.String("methods", "", "HTTP methods to expose; 'all' for every one (default: read-only)")
	deprecated := fs.Bool("deprecated", false, "include operations the document marks deprecated")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}

	// An inline --spec is a declaration of one, so the two paths converge
	// immediately rather than being maintained separately.
	var specs []APISpec
	if *specFlag != "" {
		specs = []APISpec{{
			Name: *name, Spec: *specFlag, BaseURL: *baseURL,
			Include: splitCSV(*include), Exclude: splitCSV(*exclude),
			Methods: splitCSV(*methods), IncludeDeprecated: *deprecated,
		}}
	} else {
		declared, err := a.loadAPIs()
		if err != nil {
			return err
		}
		specs = declared
	}

	switch sub {
	case "", "list":
		if len(specs) == 0 {
			fmt.Println("No API specifications declared.\n" +
				"Point paths.apis at a file, or pass --spec <path-or-url>:\n" +
				"  mcpx api tools --spec https://example.com/openapi.json")
			return nil
		}
		for _, s := range specs {
			api, err := a.buildAPI(ctx, s)
			if err != nil {
				fmt.Printf("%-16s error: %v\n", s.Name, err)
				continue
			}
			fmt.Printf("%-16s %-44s %d operations\n", api.Name, api.BaseURL, len(api.Tools))
		}
		return nil

	case "tools":
		var out []mcpserver.Tool
		for _, s := range specs {
			api, err := a.buildAPI(ctx, s)
			if err != nil {
				return err
			}
			for _, t := range api.Tools {
				out = append(out, mcpserver.Tool{
					Name: t.Name, Description: t.Description, InputSchema: t.InputSchema,
				})
			}
		}
		if a.JSON {
			return a.out(out)
		}
		for _, t := range out {
			fmt.Printf("%s\n    %s\n", t.Name, t.Description)
		}
		fmt.Printf("\n%d operations.\n", len(out))
		return nil

	case "call":
		if fs.NArg() == 0 {
			return fmt.Errorf("usage: mcpx api call <tool> '<json>' [--spec <doc>]")
		}
		tool := fs.Arg(0)
		var payload map[string]any
		if fs.NArg() > 1 {
			if err := json.Unmarshal([]byte(fs.Arg(1)), &payload); err != nil {
				return fmt.Errorf("arguments must be a JSON object: %w", err)
			}
		}
		for _, s := range specs {
			api, err := a.buildAPI(ctx, s)
			if err != nil {
				continue
			}
			for _, t := range api.Tools {
				if t.Name != tool {
					continue
				}
				res, err := api.Call(ctx, tool, payload)
				if err != nil {
					return err
				}
				if a.JSON {
					return a.out(res)
				}
				fmt.Print(res.Body)
				if res.Status >= 400 {
					return fmt.Errorf("%s returned %d", tool, res.Status)
				}
				return nil
			}
		}
		return fmt.Errorf("no operation named %q; `mcpx api tools` lists them", tool)
	}
	return fmt.Errorf("no api subcommand %q; list, tools or call", sub)
}

// apiTools exposes declared APIs through the MCP server.
func (a *App) apiTools(ctx context.Context) []mcpserver.Extra {
	specs, err := a.loadAPIs()
	if err != nil || len(specs) == 0 {
		return nil
	}
	var out []mcpserver.Extra
	for _, s := range specs {
		api, err := a.buildAPI(ctx, s)
		if err != nil {
			continue
		}
		bound := api
		for _, t := range bound.Tools {
			t := t
			out = append(out, mcpserver.Extra{
				Tool: mcpserver.Tool{
					Name: t.Name, Description: t.Description, InputSchema: t.InputSchema,
				},
				Call: func(ctx context.Context, raw json.RawMessage) (string, error) {
					var args map[string]any
					if len(raw) > 0 {
						if err := json.Unmarshal(raw, &args); err != nil {
							return "", err
						}
					}
					res, err := bound.Call(ctx, t.Name, args)
					if err != nil {
						return "", err
					}
					if res.Status >= 400 {
						return fmt.Sprintf("HTTP %d\n\n%s", res.Status, res.Body), nil
					}
					return res.Body, nil
				},
			})
		}
	}
	return out
}

// splitCSV splits a comma separated flag, dropping blanks.
func splitCSV(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
