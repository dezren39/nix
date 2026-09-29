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

	// DirMode and FileMode are what this package creates configuration
	// directories and files with. 0644 rather than 0600 because a
	// configuration file is meant to be readable -- secrets belong in the
	// environment it names, not in it.
	DirMode  = defaults.DirMode
	FileMode = defaults.PublicMode
)
