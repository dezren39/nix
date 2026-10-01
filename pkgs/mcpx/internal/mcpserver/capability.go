package mcpserver

import (
	"context"
	"encoding/json"
)

type peerCapsKey struct{}

type peerVersionKey struct{}

func withPeerVersion(ctx context.Context, p Peer) context.Context {
	if p.Version == "" {
		return ctx
	}
	return context.WithValue(ctx, peerVersionKey{}, p.Version)
}

// PeerVersion is the protocol revision this request's client speaks, or ""
// outside a request. A pass-through Backend relays it so a pool configured
// protocol: follow can serve a legacy caller from a legacy upstream session.
func PeerVersion(ctx context.Context) string {
	v, _ := ctx.Value(peerVersionKey{}).(string)
	return v
}

// withPeerCaps records, for the rest of a 2026-07-28 request, the
// clientCapabilities it declared. A legacy request declared its capabilities
// once at initialize, for the whole session, and has nothing to record here.
func withPeerCaps(ctx context.Context, p Peer) context.Context {
	if !p.Modern || p.Caps == nil {
		return ctx
	}
	b, err := json.Marshal(p.Caps)
	if err != nil {
		return ctx
	}
	return context.WithValue(ctx, peerCapsKey{}, json.RawMessage(b))
}

// DeclaredCapabilities is the clientCapabilities this request's client
// declared on it, when it is a 2026-07-28 request. A pass-through Backend
// relays them, so the upstream judges what the client can answer rather than
// what mcpx can.
func DeclaredCapabilities(ctx context.Context) json.RawMessage {
	caps, _ := ctx.Value(peerCapsKey{}).(json.RawMessage)
	return caps
}

// UpstreamError is a pass-through upstream's JSON-RPC error, to be answered
// as that error. A Backend returns it so the protocol layer relays the code,
// message and data -- a -32021 with its requiredCapabilities, a -32602 for a
// name the upstream does not know -- instead of a tool result with isError,
// which would tell the client the tool ran and failed.
type UpstreamError struct {
	Code    int
	Message string
	Data    json.RawMessage
}

func (e *UpstreamError) Error() string { return e.Message }

// relay renders e as the JSON-RPC error it stands for.
func (e *UpstreamError) relay(id json.RawMessage) *response {
	var data any
	if len(e.Data) > 0 {
		data = e.Data
	}
	return &response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: e.Code, Message: e.Message, Data: data}}
}
