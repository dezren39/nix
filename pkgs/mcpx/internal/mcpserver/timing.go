package mcpserver

import (
	"time"

	"github.com/dezren39/mcpx/internal/defaults"
)

// Timing is how long mcpx's own MCP server waits, and how many times it
// relays a question, before it gives up.
//
// Every one of these was a constant read straight out of internal/defaults at
// the point of use, which made proto.askTimeout, proto.askPoll,
// proto.askRounds, proto.stateTTL and proto.sessionIdle five settings that
// `mcpx settings` reported, `mcpx config --schema` documented and nothing
// consulted. They are a struct handed in rather than a settings set read
// here, because this package serves a protocol and should not know that a
// configuration file exists; the caller that builds the server knows both.
//
// A zero field means the built-in default, so a test that cares about one of
// them sets one of them.
type Timing struct {
	// AskTimeout bounds how long one request may be held while a question
	// goes unanswered.
	AskTimeout time.Duration
	// AskPoll is how long one wait for a question may block.
	AskPoll time.Duration
	// AskRounds bounds how many times one request may come back asking.
	AskRounds int
	// StateTTL is how long a client may resume an interrupted request.
	StateTTL time.Duration
	// SessionIdle is how long an unused Streamable HTTP session is kept.
	SessionIdle time.Duration
	// TaskAfter is how long a tools/call from a tasks-extension client runs
	// in line before it is handed a task instead.
	TaskAfter time.Duration
	// SSEKeepAlive is how often a quiet GET event stream carries a comment.
	SSEKeepAlive time.Duration
	// StdioDrain is how long in-flight stdio requests may finish after the
	// input closes, before they are cancelled.
	StdioDrain time.Duration
}

func (t Timing) resolved() Timing {
	if t.AskTimeout <= 0 {
		t.AskTimeout = defaults.ProtoAskTimeout
	}
	if t.AskPoll <= 0 {
		t.AskPoll = defaults.ProtoAskPoll
	}
	if t.AskRounds <= 0 {
		t.AskRounds = defaults.ProtoAskRounds
	}
	if t.StateTTL <= 0 {
		t.StateTTL = defaults.ProtoStateTTL
	}
	if t.SessionIdle <= 0 {
		t.SessionIdle = defaults.ProtoSessionIdle
	}
	if t.TaskAfter <= 0 {
		t.TaskAfter = defaults.ProtoTaskAfter
	}
	if t.SSEKeepAlive <= 0 {
		t.SSEKeepAlive = defaults.TransportSSEKeepAlive
	}
	if t.StdioDrain <= 0 {
		t.StdioDrain = defaults.TransportStdioDrain
	}
	return t
}
