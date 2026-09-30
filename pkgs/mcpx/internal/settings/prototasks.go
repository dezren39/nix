package settings

import "github.com/dezren39/mcpx/internal/defaults"

// protoTasksSettings govern the tasks mcpx hands out, over MCP and /v1 alike.
func protoTasksSettings() []Setting {
	return []Setting{
		{
			Path: "protoTasks.pollInterval", Scope: ScopeDaemon, Kind: KindDuration,
			Default:  defaults.Str(defaults.TaskPollInterval),
			Commands: []string{"serve", "daemon"},
			Name:     "Task poll interval",
			Short:    "the pollInterval every task carries",
			Long: "How often a client is told it may usefully ask after a task. The " +
				"specification says a client SHOULD respect it, so it is the rate " +
				"mcpx is asking to be polled at. It was a literal one second in the " +
				"task store.",
		},
	}
}
