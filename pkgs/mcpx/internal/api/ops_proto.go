package api

// protoOps are the operations that exist because a question can interrupt a
// call.
//
// They are the /v1 shape of the same thing mcpx's MCP server offers its own
// clients natively: begin a call that may be interrupted, see what it is
// waiting on, answer, give up. A curl user and a 2026-07-28 agent reach the
// identical machinery, which is the only way the two can be relied on to
// behave the same.
func protoOps() []Op {
	return []Op{
		{
			Name: "ask_begin", Method: "POST", Path: "/v1/ask",
			Summary: "Start a call that a server may interrupt with a question",
			Description: "Runs tools/call, prompts/get or resources/read as a task and " +
				"returns its handle at once. Questions the upstream server asks are " +
				"correlated to this call, so whoever started it can answer them -- " +
				"which an ordinary /v1/call cannot offer, because the answer may " +
				"arrive after that request had to return. Collect the result from " +
				"/v1/ask/{id}, or from /v1/tasks/{id}: it is the same task.",
			Mutating: true,
			Params: append([]Param{
				{Name: "kind", In: InBody, Type: "string", Required: true,
					Enum: []string{"tools/call", "prompts/get", "resources/read"},
					Desc: "which MCP method to perform"},
				{Name: "server", In: InBody, Type: "string", Required: true, Desc: "server name or namespace"},
				{Name: "tool", In: InBody, Type: "string", Desc: "with tools/call"},
				{Name: "name", In: InBody, Type: "string", Desc: "with prompts/get"},
				{Name: "uri", In: InBody, Type: "string", Desc: "with resources/read"},
				{Name: "args", In: InBody, Type: "object", Desc: "the tool's arguments",
					Schema: `{"type":"object","additionalProperties":true}`},
				{Name: "arguments", In: InBody, Type: "object", Desc: "a prompt's arguments, which are strings",
					Schema: `{"type":"object","additionalProperties":{"type":"string"}}`},
				{Name: "ttl", In: InBody, Type: "integer", Desc: "milliseconds the call and its result are kept"},
			}, callContextParams()...),
		},
		{
			Name: "ask_poll", Method: "GET", Path: "/v1/ask/{id}",
			Summary: "What a call is waiting on, or its result",
			Description: "Long-polls: it returns as soon as a question appears or the " +
				"call finishes, rather than on a fixed interval. The thing being " +
				"waited for is a person answering, so a poll loop would be either a " +
				"busy one or a delay somebody notices. Questions come back in MCP's " +
				"own shape -- an elicitation/create or sampling/createMessage request " +
				"apiece -- so a client that already implements either can answer " +
				"without learning anything new.",
			Params: []Param{
				{Name: "id", In: InPath, Type: "string", Required: true, Desc: "the call id"},
				{Name: "waitMs", In: InQuery, Type: "integer", Desc: "how long to wait for something to happen"},
			},
		},
		{
			Name: "ask_answers", Method: "POST", Path: "/v1/ask/{id}/answers",
			Summary: "Answer the questions a call is waiting on",
			Description: "The body maps question id to the MCP result -- an ElicitResult " +
				"or a CreateMessageResult -- exactly as a client would have produced " +
				"it on the wire. Answering a question this call did not ask is " +
				"refused rather than ignored: it is either a confused client or one " +
				"reaching for somebody else's question. Answering on a user's behalf " +
				"is privileged, because the server is told a person chose.",
			Admin: true, Mutating: true,
			Params: []Param{
				{Name: "id", In: InPath, Type: "string", Required: true, Desc: "the call id"},
				{Name: "answers", In: InBody, Type: "object", Required: true,
					Desc:   "question id to its MCP result",
					Schema: `{"type":"object","additionalProperties":true}`},
			},
		},
		{
			Name: "ask_abandon", Method: "POST", Path: "/v1/ask/{id}/abandon",
			Summary: "Give up on a call nobody is going to answer",
			Description: "Cancels the task. The questions it raised stay in the broker " +
				"until their own deadlines, where they expire as cancellations -- " +
				"which is what they are, and not the same thing as a refusal.",
			Admin: true, Mutating: true, Destructive: true,
			Params: []Param{
				{Name: "id", In: InPath, Type: "string", Required: true},
			},
		},
		{
			Name: "protocol", Method: "GET", Path: "/v1/protocol",
			Summary: "Which MCP revisions mcpx speaks, in both directions",
			Description: "The revisions mcpx serves and the feature each one defines, " +
				"plus the era and capabilities every configured server actually " +
				"settled on. Read from the same table the code consults, so a claim " +
				"here cannot drift from what the wire does.",
		},
	}
}
