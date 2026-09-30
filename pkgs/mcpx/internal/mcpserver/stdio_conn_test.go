package mcpserver_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/mcpserver"
)

// Two stdio loops on one Server used to share its default connection, so a
// question for one client's call went out on whichever loop had installed
// its writer last.
func TestTwoStdioLoopsDoNotShareAConnection(t *testing.T) {
	s := mcpserver.New(newBackend(), "mcpx", "test")
	s.Ask = &scriptedAsker{questions: oneQuestion(), answers: map[string]json.RawMessage{}, text: "done"}
	s.Timing = mcpserver.Timing{AskTimeout: 500 * time.Millisecond, AskPoll: 20 * time.Millisecond}
	init := frame(1, "initialize", map[string]any{"protocolVersion": "2025-11-25",
		"capabilities": map[string]any{"elicitation": map[string]any{}},
		"clientInfo":   map[string]any{"name": "t", "version": "1"}})
	a := startStdio(t, s)
	a.send(t, init)
	a.next(t)
	b := startStdio(t, s)
	b.send(t, init)
	b.next(t)
	a.send(t, frame(9, "tools/call", map[string]any{"name": "mcpx_call",
		"arguments": map[string]any{"namespace": "x", "tool": "y"}}))
	if m := a.next(t); m["method"] != "elicitation/create" {
		t.Fatalf("the asking loop was not asked: %v", m)
	}
	select {
	case f := <-b.frames:
		t.Errorf("the other loop was asked: %v", f)
	case <-time.After(200 * time.Millisecond):
	}
}
