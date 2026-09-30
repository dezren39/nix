package conformance

// Covers written with the harness itself.
func init() {
	const jr = "conformance.TestJSONRPCFramesEveryRevision"
	register(
		S("messages-all-messages-follow-jsonrpc-2",
			append(PerRev(jr, "messages/all-messages-follow-jsonrpc-2-server-stdio", All...),
				PerRev(jr, "messages/all-messages-follow-jsonrpc-2-server-http", All...)...)...),
		C("messages-all-messages-follow-jsonrpc-2",
			PerRev(jr, "messages/all-messages-follow-jsonrpc-2-client", All...)...),
	)
}
