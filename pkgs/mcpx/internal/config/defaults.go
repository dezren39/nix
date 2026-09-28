package config

import "github.com/dezren39/mcpx/internal/defaults"

// Typed views of the embedded default layer. The values live in
// internal/defaults/defaults.json; these only attach this package's types.
var (
	DefaultMax          = defaults.Max
	DefaultMin          = defaults.Min
	DefaultIdleTimeout  = defaults.IdleTimeout
	DefaultCallTimeout  = defaults.CallTimeout
	DefaultStartTimeout = defaults.StartTimeout
	DefaultSharing      = Sharing(defaults.Sharing)
	DefaultScope        = Scope(defaults.Scope)
)
