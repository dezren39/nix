package mcpserver_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/mcpserver"
)

// stragglerAsker opens one question at once and the rest after a delay, which
// is how a relayed 2026-07-28 round arrives: the gateway answers the
// upstream's inputRequests concurrently, so they reach the ask table one at a
// time and the later ones can be slow.
type stragglerAsker struct {
	mu      sync.Mutex
	first   mcpserver.Question
	rest    []mcpserver.Question
	at      time.Time
	delay   time.Duration
	answers map[string]json.RawMessage
	polls   int
}

func (a *stragglerAsker) Begin(context.Context, string, json.RawMessage) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.answers = map[string]json.RawMessage{}
	a.at = time.Now()
	return "call-1", nil
}

func (a *stragglerAsker) Poll(_ context.Context, _ string, wait time.Duration) (mcpserver.Outcome, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.polls++
	all := []mcpserver.Question{a.first}
	if time.Since(a.at) >= a.delay {
		all = append(all, a.rest...)
	}
	var open []mcpserver.Question
	for _, q := range all {
		if _, done := a.answers[q.ID]; !done {
			open = append(open, q)
		}
	}
	if len(open) == 0 {
		return mcpserver.Outcome{Done: true, Text: "done"}, nil
	}
	return mcpserver.Outcome{Questions: open}, nil
}

func (a *stragglerAsker) Reply(_ context.Context, _ string, answers map[string]json.RawMessage) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for k, v := range answers {
		a.answers[k] = v
	}
	return nil
}

func (a *stragglerAsker) Abandon(string) {}

func roundOf(n int, delay time.Duration) *stragglerAsker {
	q := func(i int) mcpserver.Question {
		return mcpserver.Question{
			ID: "elc-" + string(rune('0'+i)), Method: "elicitation/create", Mode: "form",
			Server: "github", Round: n,
			Params: json.RawMessage(`{"message":"?","requestedSchema":{"type":"object"}}`),
		}
	}
	a := &stragglerAsker{first: q(1), delay: delay}
	for i := 2; i <= n; i++ {
		a.rest = append(a.rest, q(i))
	}
	return a
}

// A round is sent whole, however long its later questions take. Waiting a
// fixed moment instead sent whatever had arrived by then: on a loaded machine
// the official suite's sep-2322-multiple-inputs-incomplete got 2 of 3.
func TestAnInputRequiredRoundIsSentWhole(t *testing.T) {
	for _, delay := range []time.Duration{0, 150 * time.Millisecond, 400 * time.Millisecond} {
		t.Run(delay.String(), func(t *testing.T) {
			s := mcpserver.New(newBackend(), "mcpx", "test")
			asker := roundOf(3, delay)
			s.Ask = asker
			m := handle(t, s, "tools/call", modernCall(1, nil))
			r := resultOf(t, m)
			if r["resultType"] != "input_required" {
				t.Fatalf("resultType = %v, want input_required: %v", r["resultType"], m)
			}
			reqs, _ := r["inputRequests"].(map[string]any)
			if len(reqs) != 3 {
				t.Fatalf("sent %d of the round's 3 questions: %v", len(reqs), reqs)
			}
		})
	}
}

// A round whose later questions never arrive still answers, with what it has.
func TestARoundThatNeverCompletesStillAnswers(t *testing.T) {
	s := mcpserver.New(newBackend(), "mcpx", "test")
	asker := roundOf(3, time.Hour)
	s.Ask = asker
	start := time.Now()
	m := handle(t, s, "tools/call", modernCall(1, nil))
	r := resultOf(t, m)
	if r["resultType"] != "input_required" {
		t.Fatalf("resultType = %v: %v", r["resultType"], m)
	}
	if reqs, _ := r["inputRequests"].(map[string]any); len(reqs) != 1 {
		t.Fatalf("want the one question that arrived, got %d", len(reqs))
	}
	if d := time.Since(start); d > 30*time.Second {
		t.Fatalf("waited %v for a round that cannot complete", d)
	}
}
