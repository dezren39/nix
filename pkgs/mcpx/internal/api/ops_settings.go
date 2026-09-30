package api

// settingsOps are the operations that change mcpx itself rather than asking
// it to do something.
//
// Two groups, declared together because they answer the same question. "Can
// I change this without restarting" has been, until now, a different answer
// for each surface: a configuration file needed an editor, a setting needed
// the right environment on the right process, and adding a server needed a
// restart because nothing re-read the file. Making all of it reachable here
// means it is reachable from the CLI, from /v1 and from MCP at once, because
// those three are generated from this table.
func settingsOps() []Op {
	return []Op{
		{
			Name: "settings_list", Method: "GET", Path: "/v1/settings",
			Command: "settings list",
			Summary: "Every setting, its effective value and where that value came from",
			Description: "The whole registry: path, kind, effective value, default, " +
				"the file or variable or flag that decided it, the environment " +
				"variable and flag that would change it, its scope, and whether " +
				"it can be changed without a restart. This is the answer to 'what " +
				"is this set to and why', which otherwise takes reading four " +
				"places and guessing.",
			Params: []Param{
				{Name: "scope", In: InQuery, Type: "string",
					Enum: []string{"daemon", "client", "call", "plugin"},
					Desc: "only settings read by this kind of process"},
				{Name: "plumbing", In: InQuery, Type: "string", Enum: []string{"0", "1"},
					Desc: "1 to include internal settings"},
				{Name: "changed", In: InQuery, Type: "string", Enum: []string{"0", "1"},
					Desc: "1 for only the settings something has overridden"},
			},
		},
		{
			Name: "settings_get", Method: "GET", Path: "/v1/settings/{path}",
			Command:     "settings get",
			Summary:     "One setting",
			Description: "The same record the listing carries, for one dotted path.",
			Params: []Param{
				{Name: "path", In: InPath, Type: "string", Required: true,
					Desc: "the dotted setting path, such as pool.idleTimeout"},
			},
		},
		{
			Name: "settings_set", Method: "PUT", Path: "/v1/settings/{path}",
			Idempotent: true,
			Command:    "settings set",
			Summary:    "Change a setting",
			Description: "persist=runtime changes the running daemon and nothing on " +
				"disk, which is what you want for an experiment. project and user " +
				"write the corresponding configuration file, and also apply at " +
				"once when the setting allows it. A setting that was consumed at " +
				"startup cannot be changed in place: the answer says so and names " +
				"the restart, rather than accepting a value that would do nothing.",
			Admin: true, Mutating: true,
			Params: []Param{
				{Name: "path", In: InPath, Type: "string", Required: true, Desc: "the setting's dotted path, such as pool.max"},
				{Name: "value", In: InBody, Type: "string", Required: true,
					Desc: "the new value, in the syntax a user would type"},
				{Name: "persist", In: InBody, Type: "string",
					Enum: []string{"runtime", "project", "user"},
					Desc: "where the change is kept; runtime is the default and survives nothing"},
			},
		},
		{
			Name: "settings_unset", Method: "DELETE", Path: "/v1/settings/{path}",
			Idempotent: true,
			Command:    "settings unset",
			Summary:    "Drop a runtime override",
			Description: "The value falls back to whatever the flags, the environment " +
				"and the configuration files say. It does not remove anything from " +
				"a file; use settings_set for that.",
			Admin: true, Mutating: true,
			Params: []Param{
				{Name: "path", In: InPath, Type: "string", Required: true, Desc: "the setting's dotted path, such as pool.max"},
			},
		},
		{
			Name: "servers_list", Method: "GET", Path: "/v1/servers",
			Command: "servers list",
			Summary: "Every configured server as it is written in the configuration",
			Description: "The entries themselves -- command, arguments, environment " +
				"keys, URL -- and which file each came from. /v1/namespaces is the " +
				"same servers seen from the other side, with tool counts and live " +
				"instances.",
		},
		{
			Name: "servers_add", Method: "POST", Path: "/v1/servers",
			Command: "servers add",
			Summary: "Add a server, live",
			Description: "Writes the entry to a configuration file and reloads, so the " +
				"server is callable immediately and is still there after a " +
				"restart. Existing instances of other servers are untouched.",
			Admin: true, Mutating: true,
			Params: []Param{
				{Name: "name", In: InBody, Type: "string", Required: true,
					Desc: "the server's name, which is also its namespace unless mcpx.namespace says otherwise"},
				{Name: "command", In: InBody, Type: "string", Desc: "the executable, for a stdio server"},
				{Name: "args", In: InBody, Type: "array", Desc: "its arguments",
					Schema: `{"type":"array","items":{"type":"string"}}`},
				{Name: "env", In: InBody, Type: "object", Desc: "environment for the child process",
					Schema: `{"type":"object","additionalProperties":{"type":"string"}}`},
				{Name: "url", In: InBody, Type: "string", Desc: "the endpoint, for a remote server"},
				{Name: "headers", In: InBody, Type: "object", Desc: "headers sent to a remote server",
					Schema: `{"type":"object","additionalProperties":{"type":"string"}}`},
				{Name: "transport", In: InBody, Type: "string",
					Enum: []string{"stdio", "http", "sse"}, Desc: "defaults to stdio with a command, http with a url"},
				{Name: "mcpx", In: InBody, Type: "object",
					Desc:   "mcpx's own per-server options: namespace, sharing, scope, max, description",
					Schema: `{"type":"object","additionalProperties":true}`},
				{Name: "scope", In: InBody, Type: "string", Enum: []string{"project", "user"},
					Desc: "which configuration file to write; project is the default"},
				{Name: "replace", In: InBody, Type: "boolean",
					Desc: "overwrite an entry of the same name instead of refusing"},
			},
		},
		{
			Name: "servers_remove", Method: "DELETE", Path: "/v1/servers/{name}",
			Idempotent: true,
			Command:    "servers remove",
			Summary:    "Remove a server, live",
			Description: "Deletes the entry from whichever file defines it and reloads. " +
				"Instances of that server stop; a caller mid-call against it fails.",
			Admin: true, Mutating: true, Destructive: true,
			Params: []Param{
				{Name: "name", In: InPath, Type: "string", Required: true, Desc: "the server to remove, by its name in the configuration"},
				{Name: "scope", In: InQuery, Type: "string", Enum: []string{"project", "user"},
					Desc: "which file to edit; by default, the one that defines it"},
			},
		},
	}
}
