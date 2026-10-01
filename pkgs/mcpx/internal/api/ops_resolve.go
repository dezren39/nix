package api

// resolveOps declares the discovery endpoint.
//
// It exists so that a client with no mcpx binary -- the opencode plugin, a
// container sidecar, any harness that only has a socket -- can find out which
// daemon serves a directory by asking a daemon it has already found. Any
// live daemon answers for any directory, including ones it does not serve
// itself, because the answer is a function of the configuration search path
// and not of what this daemon happens to have loaded.
//
// Without it the only ways to answer are spawning `mcpx status` in the
// directory, which needs the binary, or guessing from the nearest
// .mcpx.json, which is wrong exactly when configuration is inherited from a
// parent -- the case the fingerprint exists to handle.
func resolveOps() []Op {
	return []Op{
		{
			Name: "resolve", Method: "GET", Path: "/v1/resolve",
			Summary: "Which daemon serves a directory",
			Description: "Resolves mcpx's configuration search path as if the caller " +
				"were in the named directory, fingerprints every file that " +
				"contributed, and reports the socket, endpoint and config that " +
				"a daemon for that directory would use -- and whether one is " +
				"currently listening. The answer does not depend on which " +
				"daemon is asked.",
			Params: []Param{
				{Name: "dir", In: InQuery, Type: "string", Required: true, DefaultCwd: true,
					Desc: "absolute path of the directory to resolve for"},
			},
		},
	}
}
