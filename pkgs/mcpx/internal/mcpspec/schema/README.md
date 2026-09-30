# Vendored MCP schemas

Copied verbatim from github.com/modelcontextprotocol/modelcontextprotocol at commit
`046fa30efd374370afb87ef830bd788eac5f217e`: `schema/<rev>/schema.json` and, where the revision ships them,
`schema/<rev>/examples/**`.

Licence: the spec repository's `LICENSE` is copied beside this file. At that commit the project is moving from MIT
to Apache-2.0; schema contributions are under Apache-2.0 or, where relicensing consent was not given, MIT. Both
permit vendoring with the notice kept.

These files exist only for tests (`internal/mcpspec` is imported from `_test.go` files, never from the binary). To
refresh, copy the same paths from a newer commit and update the hash above.
