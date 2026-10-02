package settings

import (
	"strings"

	"github.com/dezren39/mcpx/internal/spec"
)

// specSettings choose between MCP revisions where they genuinely conflict and
// the peer's stated version does not settle it (#307, internal/spec).
func specSettings() []Setting {
	return []Setting{
		{
			Path: "spec.precedence", Kind: KindList, Scope: ScopeDaemon,
			Default: strings.Join(spec.Revisions, ","),
			Name:    "Revision precedence",
			Short:   "MCP revisions in the order their rules win a conflict, first wins",
			Long: "Where two MCP revisions require incompatible behaviour and nothing " +
				"the peer sent says which it expects, the first revision here decides. " +
				"Revisions left out follow, newest first; an unknown revision is " +
				"refused. See docs/spec/revision-conflicts.md for which conflicts " +
				"consult it.",
		},
		{
			Path: "spec.first", Kind: KindString, Scope: ScopeDaemon, Default: "",
			Flag: "mcp-spec", Env: "MCPX_MCP_SPEC",
			Name:  "Preferred revision",
			Short: "move one MCP revision to the front of spec.precedence",
			Long: "Shorthand for reordering spec.precedence: `--mcp-spec 2024-11-05` " +
				"makes that revision's rules win every conflict and leaves the rest " +
				"in their order.",
		},
		{
			Path: "spec.lenient", Kind: KindList, Scope: ScopeDaemon, Default: "",
			Name:  "Lenient revisions",
			Short: "MCP revisions not held strictly; every revision not listed is strict",
			Long: "A strict revision's MUST NOTs are enforced even where relaxing them " +
				"would interoperate with a peer that ignores them. Listing " +
				"2026-07-28 here makes mcpx answer requests a 2026-07-28 server " +
				"sends, which that revision forbids.",
		},
	}
}
