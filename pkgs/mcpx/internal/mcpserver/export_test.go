package mcpserver

import (
	"context"
	"encoding/json"
)

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

// ConnForTest is a connection with a given identity, standing in for a
// second Streamable HTTP session.
func (s *Server) ConnForTest(id string) *Conn { return s.newConn(id, nil) }

// ConnWithSend is a connection whose frames go to send, as stdio's do.
func (s *Server) ConnWithSend(id string, send func(any) error) *Conn { return s.newConn(id, send) }

func (s *Server) SurfaceForTest(ctx context.Context) ([]Tool, error) { return s.surface(ctx) }

func (s *Server) InvokeForTest(ctx context.Context, name string, args json.RawMessage) (string, error) {
	return s.invoke(ctx, name, args)
}

func (s *Server) SetDefaultConnForTest(c *Conn) {
	s.mu.Lock()
	s.def = c
	s.mu.Unlock()
}

// ParamsFor renders a question for a client's revision.
func (q Question) ParamsFor(p Peer) (json.RawMessage, error) { return q.paramsFor(p) }

// DeliverForTest hands a connection the client's reply to a request mcpx
// sent it, as the transport's read loop does.
func (c *Conn) DeliverForTest(id int64, result json.RawMessage) bool {
	return c.deliver(id, result, nil)
}

// StopListenForTest ends every stream a connection holds, as its transport
// going away does.
func (c *Conn) StopListenForTest() { c.stopListen() }

// WireKey and AnswersByID are the inputRequests key mapping.
func WireKey(q Question, qs []Question) string { return wireKey(q, qs) }
func AnswersByID(a map[string]json.RawMessage, qs []Question) map[string]json.RawMessage {
	return answersByID(a, qs)
}
