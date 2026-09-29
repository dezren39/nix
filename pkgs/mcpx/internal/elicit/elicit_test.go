package elicit_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/elicit"
)

func open(t *testing.T) *elicit.Broker {
	t.Helper()
	b, err := elicit.Open(filepath.Join(t.TempDir(), "elicit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	return b
}

func ask(t *testing.T, b *elicit.Broker, r elicit.Request) elicit.Request {
	t.Helper()
	if r.Message == "" {
		r.Message = "which one?"
	}
	out, err := b.OpenRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestAnAnswerCanComeFromAnotherProcess(t *testing.T) {
	// The whole point: the asker and the answerer need not be the same
	// process, so CI can raise a question a person answers later.
	b := open(t)
	r := ask(t, b, elicit.Request{Session: "s1"})

	done := make(chan elicit.Answer, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		a, err := b.Await(ctx, r.ID)
		if err != nil {
			t.Error(err)
		}
		done <- a
	}()

	time.Sleep(50 * time.Millisecond)
	if err := b.Respond(elicit.Answer{
		ID: r.ID, Action: elicit.Accept,
		Content: json.RawMessage(`{"repo":"me/thing"}`), By: "cli",
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case a := <-done:
		if a.Action != elicit.Accept || !strings.Contains(string(a.Content), "me/thing") {
			t.Errorf("got %+v", a)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiter was never woken")
	}
}

func TestAnExpiredQuestionBecomesCancelNotDecline(t *testing.T) {
	// Expiry means dismissed without choosing. Telling a server the user
	// declined would say something different and untrue.
	b := open(t)
	r := ask(t, b, elicit.Request{ExpiresAt: time.Now().Add(-time.Second)})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, err := b.Await(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a.Action != elicit.Cancel {
		t.Errorf("expiry should cancel, got %q", a.Action)
	}
	if a.By != "expiry" {
		t.Errorf("it should say who answered: %q", a.By)
	}
}

func TestAnsweringAfterTheProcessDiedStillWorks(t *testing.T) {
	// The reattach case: nothing is in memory, and a fresh handle on the
	// same file finds the question and can answer it.
	dir := t.TempDir()
	path := filepath.Join(dir, "e.db")

	first, err := elicit.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	r := ask(t, first, elicit.Request{Session: "s1"})
	first.Close() // the asking process goes away

	second, err := elicit.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	pending, err := second.Pending(elicit.Filter{Session: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != r.ID {
		t.Fatalf("the question should have survived: %+v", pending)
	}
	if err := second.Respond(elicit.Answer{ID: r.ID, Action: elicit.Decline}); err != nil {
		t.Fatal(err)
	}
	a, ok, _ := second.Lookup(r.ID)
	if !ok || a.Action != elicit.Decline {
		t.Errorf("got %+v", a)
	}
}

func TestAnsweringTwiceIsRefusedAndTheFirstStands(t *testing.T) {
	b := open(t)
	r := ask(t, b, elicit.Request{})
	if err := b.Respond(elicit.Answer{ID: r.ID, Action: elicit.Decline}); err != nil {
		t.Fatal(err)
	}
	err := b.Respond(elicit.Answer{ID: r.ID, Action: elicit.Accept,
		Content: json.RawMessage(`{"a":1}`)})
	if err == nil {
		t.Fatal("a second answer should be refused")
	}
	a, _, _ := b.Lookup(r.ID)
	if a.Action != elicit.Decline {
		t.Errorf("the first answer should stand: %q", a.Action)
	}
}

func TestAcceptingAFormWithoutContentIsRefused(t *testing.T) {
	// An accepted form elicitation whose content is missing is a protocol
	// violation the server cannot recover from.
	b := open(t)
	r := ask(t, b, elicit.Request{Mode: elicit.Form})
	if err := b.Respond(elicit.Answer{ID: r.ID, Action: elicit.Accept}); err == nil {
		t.Fatal("expected a refusal")
	}
}

func TestPendingExpiresOverdueQuestionsRatherThanListingThem(t *testing.T) {
	// Expiring on read means no sweeper to go wrong, and nothing is ever
	// reported as pending when it is not.
	b := open(t)
	ask(t, b, elicit.Request{Session: "s1", ExpiresAt: time.Now().Add(-time.Second)})
	fresh := ask(t, b, elicit.Request{Session: "s1", ExpiresAt: time.Now().Add(time.Hour)})

	pending, err := b.Pending(elicit.Filter{Session: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != fresh.ID {
		t.Errorf("only the live question should be pending: %+v", pending)
	}
}

func TestRoutingSendsCredentialsToAHumanAndTheRestToTheAgent(t *testing.T) {
	// The default is the agent: it asked for something, the question is part
	// of that request, and it has the context. A human is pulled in only for
	// what an agent cannot know or should not hold.
	cases := []struct {
		name string
		req  elicit.Request
		want elicit.Audience
	}{
		{"plain choice",
			elicit.Request{Schema: json.RawMessage(`{"properties":{"repo":{"type":"string"}}}`)},
			elicit.ToAgent},
		{"a token field",
			elicit.Request{Schema: json.RawMessage(`{"properties":{"api_token":{"type":"string"}}}`)},
			elicit.ToHuman},
		{"a password format",
			elicit.Request{Schema: json.RawMessage(`{"properties":{"pw":{"type":"string","format":"password"}}}`)},
			elicit.ToHuman},
		{"a bare confirmation",
			elicit.Request{Schema: json.RawMessage(`{"properties":{"confirm":{"type":"boolean"}}}`)},
			elicit.ToHuman},
		{"url mode", elicit.Request{Mode: elicit.URL, URL: "https://x"}, elicit.ToHuman},
	}
	for _, c := range cases {
		got, why := elicit.Route(c.req)
		if got != c.want {
			t.Errorf("%s: routed to %q, want %q (%s)", c.name, got, c.want, why)
		}
		if why == "" {
			t.Errorf("%s: a routing decision nobody can inspect is one nobody can correct", c.name)
		}
	}
}

func TestTTLRemainingNeverGoesNegative(t *testing.T) {
	r := elicit.Request{ExpiresAt: time.Now().Add(-time.Hour)}
	if r.TTLRemaining() != 0 {
		t.Errorf("got %v", r.TTLRemaining())
	}
}
