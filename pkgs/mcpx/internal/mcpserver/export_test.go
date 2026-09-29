package mcpserver

// Exported for the tests in this package's external test file. They exercise
// the two things that have no other way in: the downgrade at the edge, which
// is applied inside Handle, and the requestState signer, whose key is
// deliberately unreachable.

// Downgrade rewrites a result into the shapes a revision defines.
func (s *Server) Downgrade(result any, version string) any {
	return downgrade(result, version)
}

// MintState issues a requestState bound to a connection identity.
func (s *Server) MintState(callID, binding string) (string, error) {
	return s.states().mint(callID, binding)
}

// VerifyState checks one and returns the call it names.
func (s *Server) VerifyState(token, binding string) (string, error) {
	return s.states().verify(token, binding)
}
