package settings

import "github.com/dezren39/mcpx/internal/defaults"

// upstreamSettings govern how mcpx connects to the servers it fronts.
//
// The question they answer is which protocol era a server speaks. The
// 2026-07-28 revision prescribes a dual-era client that asks the modern way
// first and falls back, and makes the answer a property of the server worth
// remembering; these are the knobs on that.
func upstreamSettings() []Setting {
	return []Setting{
		{
			Path: "upstream.protocol", Scope: ScopeDaemon, Kind: KindEnum, Default: defaults.UpstreamProtocol,
			// Named for the first request sent, not for an era's relative
			// age: "modern" and "legacy" go stale the moment a newer
			// revision ships. The old names stay accepted.
			Enum: []string{"prefer-discover", "prefer-initialize", "force-discover", "force-initialize", "follow"},
			EnumAliases: map[string][]string{
				"prefer-discover":   {"modern", "prefer-modern", "prefer-stateless", "prefer-newest"},
				"prefer-initialize": {"legacy", "prefer-legacy", "prefer-session", "prefer-oldest"},
				"force-discover":    {"force-modern", "force-stateless"},
				"force-initialize":  {"force-legacy", "force-session"},
			},
			Commands: []string{"daemon"},
			Name:     "Upstream protocol",
			Short:    "which request to send first to a server that names no protocol of its own",
			Long: "prefer-discover sends server/discover first and falls back to initialize, " +
				"which is what the 2026-07-28 transport pages prescribe. prefer-initialize sends " +
				"initialize first and probes only if it fails. force-discover and " +
				"force-initialize send only that request, with no fallback. follow is " +
				"prefer-discover, plus a second, initialize-only session " +
				"for callers that reach mcpx in a legacy revision, so a dual-era " +
				"server can still send them elicitation and sampling requests; a " +
				"server that refuses initialize serves them from the modern one. " +
				"A server's own protocol key overrides this.",
		},
		{
			Path: "upstream.probeTimeout", Scope: ScopeDaemon, Kind: KindDuration, Default: "2s",
			Commands: []string{"daemon"},
			Name:     "Probe timeout",
			Short:    "how long a stdio server/discover may go unanswered before initialize is also sent",
			Long: "Some legacy servers ignore methods they do not know, so the probe " +
				"has to give up waiting. It does not give up on the answer: a modern " +
				"server that is merely slow to start is still recognised if its " +
				"discover reply arrives before initialize is answered.",
		},
		{
			Path: "upstream.eraCache", Scope: ScopeDaemon, Kind: KindBool, Default: "true",
			Commands: []string{"daemon"},
			Name:     "Era cache",
			Short:    "remember which era each server configuration speaks, across restarts",
			Long: "On, the era found for a server is kept in the state directory and " +
				"tried first next time, so a legacy server costs no probe after its " +
				"first start. A wrong guess is noticed and re-probed. Off, every " +
				"start probes.",
		},
	}
}
