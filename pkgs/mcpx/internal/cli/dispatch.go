package cli

import (
	"context"
	"sort"
)

// Handler runs one subcommand with the arguments that follow its name.
type Handler func(context.Context, []string) error

// handWritten is every command with an implementation of its own, under
// every name it answers to.
//
// It lived in main as a map literal, where nothing could compare it with
// Commands(), and the two had drifted by seven commands: serve, openapi,
// adapter, registry, api, man and completion all ran without being declared,
// so none of them appeared in `mcpx help <cmd>`, the man page or completion.
// Here, a test holds the two together.
func (a *App) handWritten() map[string]Handler {
	return map[string]Handler{
		"ls":         a.CmdLs,
		"list":       a.CmdLs,
		"namespaces": a.CmdLs,
		"types":      a.CmdTypes,
		"catalog":    a.CmdCatalog,
		"search":     a.CmdSearch,
		"call":       a.CmdCall,
		"batch":      a.CmdBatch,
		"run":        a.CmdRun,
		"exec":       a.CmdExec,
		"client":     a.CmdClient,
		"status":     a.CmdStatus,
		"refresh":    a.CmdRefresh,
		"restart":    a.CmdRestart,
		"stop":       a.CmdStop,
		"daemons":    a.CmdDaemons,
		"scripts":    a.CmdScripts,
		"log":        a.CmdLog,
		"logs":       a.CmdLog,
		"stats":      a.CmdStats,
		"daemon":     a.CmdDaemon,
		"config":     a.CmdConfig,
		"init":       a.CmdInit,
		"help":       a.CmdHelp,
		"man":        a.CmdMan,
		"completion": a.CmdCompletion,
		"explore":    a.CmdExplore,
		"tui":        a.CmdTUI,
		"serve":      a.CmdServe,
		"openapi":    a.CmdOpenAPI,
		"adapter":    a.CmdAdapter,
		"registry":   a.CmdRegistry,
		"api":        a.CmdAPI,
		"prompts":    a.CmdPrompts,
		"resources":  a.CmdResources,
		"doctor":     a.CmdDoctor,
		"schema":     a.CmdSchema,
		"elicit":     a.CmdElicit,
		"diagnose":   a.CmdDiagnose,
		"recipes":    a.CmdRecipes,
		"prompt":     a.CmdPrompt,
		"settings":   a.CmdSettings,
		"servers":    a.CmdServers,
		"dashboard":    a.CmdDashboard,
		"proxy":        a.CmdProxy,
		"embeddings":   a.CmdEmbeddings,
		"feedback":     a.CmdFeedback,
		"interactions": a.CmdInteractions,
	}
}

// Handlers is the whole dispatch table: every hand-written command and
// alias, then one command per family of /v1 operations that no hand-written
// command covers.
//
// The generated half is why an operation declared in internal/api is
// reachable from the shell the moment it exists. The CLI used to be the one
// surface that had to be extended by hand, and it was the one that fell
// behind: resolve, protocol, artifacts, tasks and ask were reachable from
// /v1, MCP and the plugin and not from the command line (#76).
func (a *App) Handlers() map[string]Handler {
	h := a.handWritten()
	for _, g := range opGroups() {
		g := g
		for _, name := range append([]string{g.Name}, g.Aliases...) {
			// A hand-written command always wins. A collision is a
			// declaration mistake, which TestGeneratedCommandsDoNotShadow
			// reports; at run time the older, deliberate command is the
			// safer answer.
			if _, taken := h[name]; !taken {
				h[name] = func(ctx context.Context, args []string) error {
					return a.runOpGroup(ctx, g, args)
				}
			}
		}
	}
	return h
}

// HandlerNames lists every name the dispatch table answers to, sorted.
func (a *App) HandlerNames() []string {
	h := a.Handlers()
	out := make([]string, 0, len(h))
	for name := range h {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
