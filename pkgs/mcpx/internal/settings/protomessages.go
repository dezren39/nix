package settings

import "github.com/dezren39/mcpx/internal/defaults"

// protoMessagesSettings govern fields mcpx's own MCP server attaches to the
// messages it sends, rather than anything about who answers a question.
//
// Separate from protoSettings because these are about the wire shape of a
// 2026-07-28 reply -- how long a client may cache it, how often an idle
// stream says it is still alive -- and nothing about the ask machinery.
func protoMessagesSettings() []Setting {
	return []Setting{
		{
			Path: "protoMessages.listMaxAge", Scope: ScopeDaemon, Kind: KindDuration,
			Default:  defaults.Str(defaults.ProtoListMaxAge),
			Commands: []string{"serve", "daemon"},
			Name:     "List cache TTL",
			Short:    "ttlMs on server/discover and every list result sent to a 2026-07-28 client",
			Long: "The specification makes a cache hint mandatory on these results. " +
				"The lists follow configuration and the servers behind it, and a " +
				"list_changed notification only reaches a client that is listening, " +
				"so this is how stale a client that is not listening may let them get.",
		},
		{
			Path: "protoMessages.readMaxAge", Scope: ScopeDaemon, Kind: KindDuration,
			Default:  defaults.Str(defaults.ProtoReadMaxAge),
			Commands: []string{"serve", "daemon"},
			Name:     "Read cache TTL",
			Short:    "ttlMs on resources/read results sent to a 2026-07-28 client",
			Long: "Zero by default: a resource is whatever the upstream server says it " +
				"is now, and mcpx has no way to know how long that stays true.",
		},
		{
			Path: "protoMessages.taskAfter", Scope: ScopeDaemon, Kind: KindDuration,
			Default:  defaults.Str(defaults.ProtoTaskAfter),
			Commands: []string{"serve", "daemon"},
			Name:     "Task threshold",
			Short:    "how long a tools/call runs in line before a tasks-extension client is handed a task",
			Long: "The tasks extension leaves the choice to the server. A call that " +
				"finishes within this is answered directly, as if tasks did not " +
				"exist; one that does not becomes a task the client polls. Only for " +
				"a 2026-07-28 client that declared io.modelcontextprotocol/tasks.",
		},
	}
}
