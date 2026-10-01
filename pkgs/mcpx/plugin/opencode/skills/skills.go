// Package skills carries the skill directories beside this file into the mcpx
// binary.
//
// They are the same files a user copies into ~/.config/opencode/skills/; mcpx
// also serves them over MCP through the io.modelcontextprotocol/skills
// extension (SEP-2640), so any MCP host connected to mcpx -- not only opencode
// with the plugin installed -- can find out how to use it. One copy, two
// routes: an edit here reaches both.
package skills

import "embed"

// FS holds every file of every mcpx-* skill directory, rooted at this
// directory: FS's top-level entries are the skill names.
//
//go:embed mcpx-*
var FS embed.FS
