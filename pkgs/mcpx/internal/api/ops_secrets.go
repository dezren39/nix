package api

// secretsOps are the operations that give a server the secrets it needs, and
// ask a person for them when it cannot start without one.
//
// Values are the one thing here that must not go through an agent. A tool
// call's arguments are in the model's context and in its transcript, so the
// operation that carries a value has no MCP tool (NoTool says why). Everything
// else is offered like any other operation: names, requests and the preflight
// are what an agent needs to explain what is missing.
func secretsOps() []Op {
	return []Op{
		{
			Name: "secrets_list", Method: "GET", Path: "/v1/secrets",
			Command: "secrets list",
			Summary: "Which secrets servers need, and which of them have a value",
			Description: "Names only, never values. Lists every name a configured server " +
				"requires and every name given a value at runtime, with whether it is " +
				"set and whether the value came from the environment or from runtime.",
		},
		{
			Name: "secrets_set", Method: "PUT", Path: "/v1/secrets/{name}",
			Command:    "secrets set",
			Idempotent: true,
			NoTool: "a value sent as a tool argument would sit in the model's context " +
				"and transcript; the CLI reads it from standard input and OpenChamber " +
				"from a password field",
			Summary: "Give a secret a value, in memory only",
			Description: "The value is kept in the running daemon's memory and nowhere else: " +
				"it is not written to disk, not logged, and not returned. Servers that " +
				"require the name are restarted so that the next call starts them with " +
				"it, and a server that was waiting for it becomes callable without a " +
				"restart of the daemon. A secret request for it is resolved when every " +
				"name it asks for has a value.",
			Admin: true, Mutating: true,
			Params: []Param{
				{Name: "name", In: InPath, Type: "string", Required: true,
					Desc: "the environment variable name the server reads, such as GITHUB_TOKEN"},
				{Name: "value", In: InBody, Type: "string", Required: true,
					Desc: "the value. Empty is refused."},
			},
		},
		{
			Name: "secrets_unset", Method: "DELETE", Path: "/v1/secrets/{name}",
			Command:    "secrets unset",
			Idempotent: true,
			Summary:    "Forget a runtime value for a secret",
			Description: "Removes the value given at runtime and nothing else: a value in the " +
				"environment is untouched. Servers that require the name stop and are " +
				"inactive until it has a value again, so their calls are refused with " +
				"secret_required rather than failing part way through.",
			Admin: true, Mutating: true, Destructive: true,
			Params: []Param{
				{Name: "name", In: InPath, Type: "string", Required: true,
					Desc: "the environment variable name whose runtime value is to be dropped"},
			},
		},
		{
			Name: "secret_requests_list", Method: "GET", Path: "/v1/secret-requests",
			Command: "secrets requests",
			Summary: "Requests waiting for a person to give secrets",
			Description: "Pending requests first, oldest first, then the resolved and declined " +
				"ones from the last ten minutes. A request names a server and the secrets " +
				"it needs, never their values.",
		},
		{
			Name: "secret_request_get", Method: "GET", Path: "/v1/secret-requests/{id}",
			Command: "secrets request",
			Summary: "One secret request, and whether it is still waiting",
			Description: "Poll this while a script waits: a request becomes resolved once every " +
				"secret it names has a value, and declined when somebody says no.",
			Params: []Param{
				{Name: "id", In: InPath, Type: "string", Required: true, Desc: "the request id"},
			},
		},
		{
			Name: "secret_request_decline", Method: "DELETE", Path: "/v1/secret-requests/{id}",
			Command:    "secrets decline",
			Idempotent: true,
			Summary:    "Decline a secret request",
			Description: "Ends a pending request without an answer, so a script waiting on it " +
				"fails before it runs. Declining on a person's behalf is a privileged act, " +
				"like answering a question a server asked.",
			Admin: true, Mutating: true,
			Params: []Param{
				{Name: "id", In: InPath, Type: "string", Required: true, Desc: "the request id"},
			},
		},
		{
			Name: "preflight", Method: "POST", Path: "/v1/preflight",
			Idempotent: true,
			Summary:    "Check a script's servers are ready before running it",
			Description: "Reads a script without running anything and reports which servers it " +
				"reaches through tools.<namespace>.<tool>, which of them are not ready, " +
				"and which secret requests cover what is missing, creating those requests " +
				"if none is pending. The script is never executed.",
			Mutating: true,
			Params: []Param{
				{Name: "source", In: InBody, Type: "string", Required: true,
					Desc: "the script's source text"},
			},
		},
	}
}
