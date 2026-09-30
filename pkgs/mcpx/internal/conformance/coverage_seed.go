package conformance

// Covers written with the harness itself.
func init() {
	const jr = "conformance.TestJSONRPCFramesEveryRevision"
	register(
		SR("messages-all-messages-follow-jsonrpc-2", jr, "messages/all-messages-follow-jsonrpc-2-server-stdio"),
		SR("messages-all-messages-follow-jsonrpc-2", jr, "messages/all-messages-follow-jsonrpc-2-server-http"),
		CR("messages-all-messages-follow-jsonrpc-2", jr, "messages/all-messages-follow-jsonrpc-2-client"),
	)
}
