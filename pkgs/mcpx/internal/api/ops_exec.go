package api

// execOps are running a script on the daemon and fetching what it produced.
//
// Declared here so the MCP tool, the OpenAPI document and the route all come
// from one statement. The parity test is what makes that mandatory rather
// than a habit: a route with no entry is an endpoint an agent cannot reach,
// and the failure is silent -- everything works except from MCP.
func execOps() []Op {
	return []Op{
		{
			Name: "exec", Method: "POST", Path: "/v1/exec",
			Command: "exec",
			Summary: "Run TypeScript on the daemon",
			Description: "The same execution `mcpx exec` performs, hosted by the daemon " +
				"instead of by the CLI. That is what lets a caller with no mcpx " +
				"binary run a script, and what makes a daemon on another machine " +
				"useful: the script runs where the servers are. Options carry the " +
				"caller's context -- cwd, env, stdin -- because a remote caller's " +
				"context is not the daemon's. Declare capabilities:[\"artifacts\"] " +
				"to receive files the script produced as references rather than as " +
				"base64 in the answer. With output:\"stream\" the response is SSE " +
				"(or NDJSON on Accept) and disconnecting stops the script.",
			Mutating: true,
			// A script can do anything the daemon's user can, so this is the
			// most privileged operation there is.
			Admin:     true,
			CoveredBy: "mcpx_exec",
			Params: []Param{
				{Name: "source", In: InBody, Type: "string", Desc: "TypeScript; top-level await is available"},
				{Name: "file", In: InBody, Type: "string", Desc: "a script on the daemon's filesystem, instead of source"},
				{Name: "options", In: InBody, Type: "object", Desc: "ExecOptions: how to run it and what the caller can receive",
					Schema: execOptionsSchema},
			},
		},
		{
			Name: "artifact_put", Method: "POST", Path: "/v1/artifacts",
			Summary: "Store a file a script produced",
			Description: "What artifact() in a generated client calls. The body is the " +
				"bytes; a caller on the daemon's own filesystem may instead send " +
				"X-Mcpx-Path and have the file hardlinked, which is why handing back " +
				"a large local file costs an inode rather than a copy. Content is " +
				"addressed by sha256 and served by a separate random id, because a " +
				"content hash is guessable by anyone who can guess the content.",
			Admin: true, Mutating: true,
			Params: []Param{
				{Name: "name", In: InQuery, Type: "string", Required: true, Desc: "the suggested filename; sanitised before use"},
				{Name: "mime", In: InQuery, Type: "string", Desc: "content type; guessed from the name when absent"},
				{Name: "run", In: InQuery, Type: "string", Desc: "the exec run this belongs to"},
				{Name: "session", In: InQuery, Type: "string", Desc: "the session this belongs to"},
				{Name: "ttl", In: InQuery, Type: "string", Desc: "how long to keep it, as a duration"},
				{Name: "content", In: InBody, Type: "string", Required: true, Raw: true,
					Desc: "the bytes, sent as the whole request body; on the command line, @path reads a file and - reads stdin"},
			},
		},
		{
			Name: "artifacts_list", Method: "GET", Path: "/v1/artifacts",
			CLI:         "artifact list",
			Summary:     "Files scripts produced, newest first",
			Description: "Filter by run to collect one execution's output, or by session.",
			Params: []Param{
				{Name: "run", In: InQuery, Type: "string", Desc: "only this run's artifacts"},
				{Name: "session", In: InQuery, Type: "string", Desc: "only this session's artifacts"},
				{Name: "limit", In: InQuery, Type: "integer", Desc: "how many to return, newest first"},
			},
		},
		{
			Name: "artifact_get", Method: "GET", Path: "/v1/artifacts/{id}",
			Summary: "Fetch one artifact's bytes",
			Description: "Range requests are supported, so reading the tail of a large " +
				"artifact is cheap. The response carries the recorded content type " +
				"and a Content-Disposition naming the sanitised filename.",
			Text: true,
			Params: []Param{
				{Name: "id", In: InPath, Type: "string", Required: true, Desc: "the artifact's id, as returned when it was stored"},
			},
		},
		{
			Name: "artifact_delete", Method: "DELETE", Path: "/v1/artifacts/{id}",
			Idempotent: true,
			Summary:    "Forget one artifact",
			Description: "The registration goes, and the body with it once no other " +
				"registration shares that content.",
			Admin: true, Mutating: true, Destructive: true,
			Params: []Param{
				{Name: "id", In: InPath, Type: "string", Required: true, Desc: "the artifact's id, as returned when it was stored"},
			},
		},
	}
}

// execOptionsSchema is the ExecOptions object, written out rather than
// generated from the Go struct.
//
// The description is the documentation an agent reads before calling this,
// and a generated schema would carry field names and no reasons.
const execOptionsSchema = `{"type":"object","properties":{
"runtime":{"type":"string","enum":["auto","deno","bun","node"]},
"timeout":{"type":"string","description":"a duration such as 30s; default from settings"},
"permissions":{"type":"string","description":"sandbox profile: all, net, read, read-net, strict, or raw runtime flags"},
"placeholders":{"type":"object","additionalProperties":true,"description":"values for @names a launcher template refers to"},
"session":{"type":"string","description":"which pooled instance serves the script's calls"},
"cwd":{"type":"string","description":"the caller's working directory, on the machine the script runs on"},
"env":{"type":"object","additionalProperties":{"type":"string"}},
"stdin":{"type":"string","description":"what the script reads on standard input"},
"output":{"type":"string","enum":["text","structured","stream"],"description":"structured by default over /v1"},
"capabilities":{"type":"array","items":{"type":"string"},"description":"what the caller can receive; artifacts is the one that exists"},
"artifacts":{"type":"object","properties":{
  "delivery":{"type":"string","enum":["reference","inline","stream"],"description":"reference hands back an id to fetch later; inline base64s the body into the result; stream sends bodies after the end frame"},
  "maxBytes":{"type":"integer","description":"the per-artifact ceiling the caller accepts for inline or streamed bodies"},
  "sharedFs":{"type":"string","description":"a directory the caller can read: place artifacts there instead of transferring them"},
  "dir":{"type":"string","description":"where to write this run's artifacts for the caller"}
},"additionalProperties":false},
"task":{"type":"object","properties":{"ttl":{"type":"integer","description":"milliseconds the result is kept"}},"additionalProperties":false},
"ns":{"type":"array","items":{"type":"string"},"description":"restrict the generated client to these namespaces"},
"args":{"type":"array","items":{"type":"string"},"description":"arguments handed to the script"},
"export":{"type":"string","description":"call this exported function instead of the default one"},
"typecheck":{"type":"string","enum":["off","on","strict"],"description":"check the program before running it; default from settings"},
"launcher":{"type":"string","description":"launcher template text replacing the generated one, or none for no launcher"},
"launcherName":{"type":"string","description":"what to call the launcher in messages"},
"allowRepeat":{"type":"array","items":{"type":"string"},"description":"launcher placeholders permitted to resolve more than once"},
"captureConsole":{"type":"boolean","description":"mirror console.* into the record stream; default true"},
"phases":{"type":"object","properties":{
  "before":{"type":"array","items":{"type":"string"}},
  "prefix":{"type":"array","items":{"type":"string"}},
  "onSuccess":{"type":"array","items":{"type":"string"}},
  "onError":{"type":"array","items":{"type":"string"}},
  "suffix":{"type":"array","items":{"type":"string"}}
},"additionalProperties":false,"description":"source lines run around the script, as the CLI's phase flags"}
},"additionalProperties":false}`
