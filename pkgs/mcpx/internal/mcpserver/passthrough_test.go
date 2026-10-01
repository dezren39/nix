package mcpserver_test

import (
	"encoding/json"
	"testing"

	"github.com/dezren39/mcpx/internal/mcpserver"
)

// Two rounds of one call must not share a requestState: SEP-2322's
// multi-round flow tells rounds apart by it.
func TestRequestStateDiffersPerRound(t *testing.T) {
	s := mcpserver.New(nil, "mcpx", "test")
	a, err := s.MintState("tsk-1", "tools/call:x")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.MintState("tsk-1", "tools/call:x")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatalf("two rounds minted the same requestState %q", a)
	}
	for _, tok := range []string{a, b} {
		if id, err := s.VerifyState(tok, "tools/call:x"); err != nil || id != "tsk-1" {
			t.Fatalf("verify(%q) = %q, %v", tok, id, err)
		}
	}
}

// An upstream's inputRequests keys reach the client unrenamed, and the
// client's answers under those keys reach the right question.
func TestInputRequestKeysKeepTheUpstreamsNames(t *testing.T) {
	qs := []mcpserver.Question{
		{ID: "elc-1", Method: "elicitation/create", Key: "user_name"},
		{ID: "elc-2", Method: "sampling/createMessage", Key: "greeting"},
		{ID: "elc-3", Method: "elicitation/create"},
	}
	if k := mcpserver.WireKey(qs[0], qs); k != "user_name" {
		t.Errorf("wire key = %q, want user_name", k)
	}
	if k := mcpserver.WireKey(qs[2], qs); k != "elc-3" {
		t.Errorf("a question with no upstream key keeps its id, got %q", k)
	}
	dup := []mcpserver.Question{{ID: "a", Key: "k"}, {ID: "b", Key: "k"}}
	if mcpserver.WireKey(dup[0], dup) != "a" || mcpserver.WireKey(dup[1], dup) != "b" {
		t.Error("a key two questions share is ambiguous and must fall back to ids")
	}
	got := mcpserver.AnswersByID(map[string]json.RawMessage{
		"user_name": json.RawMessage(`1`), "greeting": json.RawMessage(`2`), "elc-3": json.RawMessage(`3`),
	}, qs)
	if string(got["elc-1"]) != "1" || string(got["elc-2"]) != "2" || string(got["elc-3"]) != "3" || len(got) != 3 {
		t.Errorf("answers mapped to %v", got)
	}
}
