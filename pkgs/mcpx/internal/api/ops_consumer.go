package api

// consumerOps are the operations behind the things mcpx asks for itself:
// a deterministic explanation of a failing script, the recipes that make a
// repeated request free, and the one endpoint that turns a request in words
// into a script.
//
// They are declared here, in their own file, for the same reason every other
// operation is declared in this package: a route, an MCP tool and an OpenAPI
// entry that are written separately drift, and the drift is invisible until
// somebody asks for the thing that was forgotten.
func consumerOps() []Op {
	return []Op{
		{
			Name: "diagnose", Method: "POST", Path: "/v1/diagnose",
			Command: "diagnose",
			Summary: "Explain what is wrong with a script, deterministically",
			Description: "Compares every tool call in the source against the schemas the " +
				"configured servers publish now, and against what those schemas " +
				"used to be. The answer names the tool, the argument at fault, " +
				"what changed in that tool's schema and when, the line as written " +
				"and the minimum that works. No model is involved: the facts are " +
				"ones mcpx already holds.",
			Params: []Param{
				{Name: "source", In: InBody, Type: "string", Required: true,
					Desc: "the script to check"},
				{Name: "session", In: InBody, Type: "string",
					Desc: "the caller's session id, for the record"},
				{Name: "repair", In: InBody, Type: "boolean",
					Desc: "attempt automatic script repair if errors are detected"},
			},
		},
		{
			Name: "catalog_history", Method: "GET", Path: "/v1/catalog/history",
			CLI:     "history",
			Summary: "What changed in the tool schemas, and when",
			Description: "mcpx records every tool's schema each time it reads one, so a " +
				"diagnostic can say `options became required on the twentieth` " +
				"rather than `the arguments are wrong`. This is that record.",
			Params: []Param{
				{Name: "tool", In: InQuery, Type: "string",
					Desc: "restrict to one namespace.tool"},
			},
		},
		{
			Name: "recipes_list", Method: "GET", Path: "/v1/recipes",
			Command: "recipes list",
			Summary: "Saved scripts, with the placeholders each takes",
			Description: "A recipe is a saved script that declares its own holes with " +
				"`// @param name:type = default`. With q set, the list is ranked " +
				"against the request by a deterministic score over the name, the " +
				"description, the placeholder names and the tools it calls.",
			Params: []Param{
				{Name: "q", In: InQuery, Type: "string",
					Desc: "rank against this request instead of listing everything"},
				{Name: "limit", In: InQuery, Type: "integer", Desc: "how many ranked candidates"},
			},
		},
		{
			Name: "recipe_get", Method: "GET", Path: "/v1/recipes/{name}",
			Command:     "recipes show",
			Summary:     "One recipe, with its source",
			Description: "Everything the listing carries, plus the script itself.",
			Params: []Param{
				{Name: "name", In: InPath, Type: "string", Required: true, Desc: "the recipe's name"},
			},
		},
		{
			Name: "recipe_save", Method: "POST", Path: "/v1/recipes/{name}",
			Command: "recipes save",
			Summary: "Save a script as a recipe",
			Description: "Writes the source into the nearest project scripts directory, " +
				"where `mcpx run <name>` and every recipe route will find it. This " +
				"is how a generated script that worked stops being generated.",
			Mutating: true,
			Params: []Param{
				{Name: "name", In: InPath, Type: "string", Required: true, Desc: "the recipe's name; saving over an existing one replaces it"},
				{Name: "source", In: InBody, Type: "string", Required: true, Desc: "the recipe's source, as TypeScript"},
				{Name: "overwrite", In: InBody, Type: "boolean",
					Desc: "replace an existing recipe of this name"},
			},
		},
		{
			Name: "recipe_run", Method: "POST", Path: "/v1/recipes/{name}/run",
			Command: "recipes run",
			Summary: "Run a recipe with its placeholders filled in",
			Description: "Placeholders that are not supplied and have no default are " +
				"asked for as one form through the question broker, with a " +
				"deadline. With autonomy=propose the rendered source comes back " +
				"without running, which is the safer thing to do first.",
			Mutating: true,
			Params: []Param{
				{Name: "name", In: InPath, Type: "string", Required: true, Desc: "the recipe to run"},
				{Name: "placeholders", In: InBody, Type: "object",
					Desc:   "values by placeholder name",
					Schema: `{"type":"object","additionalProperties":true}`},
				{Name: "autonomy", In: InBody, Type: "string", Enum: []string{"propose", "run"},
					Desc: "propose renders and returns the script; run executes it (the default here)"},
				{Name: "session", In: InBody, Type: "string", Desc: "the session to run it in, so it reaches that session's servers"},
			},
		},
		{
			Name: "intent", Method: "POST", Path: "/v1/intent",
			Command: "prompt",
			Summary: "A request in words, a script back",
			Description: "Matches a recipe first, deterministically and for nothing. Only " +
				"when nothing matches does it ask for a script to be written, and " +
				"it has no model of its own to ask: the request goes out as MCP " +
				"sampling, through the broker, carrying only the slice of the " +
				"catalog the search ranked for this request. With no answerer, it " +
				"returns the ranked recipes and says plainly that no model is " +
				"available. By default it proposes -- returns the script rather than running " +
				"it, because generated code runs with your credentials. " +
				"`mcpx prompt` is this operation; the name differs because " +
				"POST /v1/prompt already renders an upstream server's prompt.",
			Mutating: true,
			Params: []Param{
				{Name: "prompt", In: InBody, Type: "string", Required: true,
					Desc: "what you want done, in words"},
				{Name: "autonomy", In: InBody, Type: "string", Enum: []string{"propose", "run"},
					Desc: "propose returns the program; run executes it; absent means the caller's prompt.autonomy"},
				{Name: "placeholders", In: InBody, Type: "object",
					Desc:   "values for a matched recipe's placeholders",
					Schema: `{"type":"object","additionalProperties":true}`},
				{Name: "session", In: InBody, Type: "string", Desc: "the session the intent belongs to, so its servers and history are the session's"},
			},
		},
	}
}
