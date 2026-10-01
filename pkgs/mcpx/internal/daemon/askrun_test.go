package daemon

import (
	"context"
	"testing"
)

// One attribution rule for single interruptible calls and scripts: a call
// owns a question on (server, key) when every table entry on the key is its
// own and their number equals the pool's in-flight count. See askFor. #77, #229.
func TestAskTableAttribution(t *testing.T) {
	t.Run("exec/correlation/run-member-question-belongs-to-run", func(t *testing.T) {
		tb := newAskTable()
		tb.beginRun("call-1", "run-1", "s")
		defer tb.join("run-1", "ask", "k")()
		if a, ok := tb.sole("ask", "k", 1); !ok || a.ID != "call-1" {
			t.Fatalf("sole = %v, %v; want call-1", a, ok)
		}
	})

	t.Run("exec/correlation/parallel-calls-of-one-run-stay-attributed", func(t *testing.T) {
		tb := newAskTable()
		tb.beginRun("call-1", "run-1", "s")
		l1 := tb.join("run-1", "ask", "k")
		l2 := tb.join("run-1", "ask", "k")
		if a, ok := tb.sole("ask", "k", 2); !ok || a.ID != "call-1" {
			t.Fatalf("two calls from one run must stay that run's, got %v %v", a, ok)
		}
		l1()
		if a, ok := tb.sole("ask", "k", 1); !ok || a.ID != "call-1" {
			t.Fatalf("leave must remove one entry only, got %v %v", a, ok)
		}
		l2()
		if _, ok := tb.sole("ask", "k", 0); ok {
			t.Fatal("no calls in flight, nothing to attribute")
		}
	})

	t.Run("exec/correlation/two-runs-on-a-shared-key-are-ambiguous", func(t *testing.T) {
		tb := newAskTable()
		tb.beginRun("call-1", "run-1", "s1")
		tb.beginRun("call-2", "run-2", "s2")
		defer tb.join("run-1", "ask", "shared")()
		defer tb.join("run-2", "ask", "shared")()
		if a, ok := tb.sole("ask", "shared", 2); ok {
			t.Fatalf("two runs share the key; attributed to %s", a.ID)
		}
	})

	t.Run("exec/correlation/an-unregistered-caller-makes-the-key-ambiguous", func(t *testing.T) {
		// A plain /v1/call registers nothing; the pool counts it.
		tb := newAskTable()
		tb.begin("call-1", "ask", "shared", "s1")
		if a, ok := tb.sole("ask", "shared", 2); ok {
			t.Fatalf("a plain caller shares the key with an ask call; attributed to %s", a.ID)
		}
		tb2 := newAskTable()
		tb2.beginRun("call-1", "run-1", "s1")
		defer tb2.join("run-1", "ask", "shared")()
		defer tb2.join("", "ask", "shared")()
		if a, ok := tb2.sole("ask", "shared", 2); ok {
			t.Fatalf("a plain caller shares the key with a run; attributed to %s", a.ID)
		}
	})

	t.Run("exec/correlation/unknown-run-registers-nothing", func(t *testing.T) {
		tb := newAskTable()
		defer tb.join("not-a-live-run", "ask", "k")()
		if _, _, ok := tb.owner("ask", "k"); ok {
			t.Fatal("a run nobody registered can own nothing")
		}
	})

	t.Run("exec/correlation/ended-run-owns-nothing-still-in-flight", func(t *testing.T) {
		tb := newAskTable()
		tb.beginRun("call-1", "run-1", "s")
		leave := tb.join("run-1", "ask", "k")
		tb.end("call-1")
		if a, ok := tb.sole("ask", "k", 1); ok {
			t.Fatalf("the run is over; its straggler's question went to %s", a.ID)
		}
		if _, ok := tb.byRun["run-1"]; ok {
			t.Fatal("run index not cleared")
		}
		leave() // must not panic or remove anything else
	})

	t.Run("exec/correlation/run-id-travels-on-the-context", func(t *testing.T) {
		if got := runFrom(withRun(context.Background(), "r")); got != "r" {
			t.Fatalf("runFrom = %q", got)
		}
		if got := runFrom(withRun(context.Background(), "")); got != "" {
			t.Fatalf("runFrom = %q", got)
		}
	})
}

// A question raised on a call's own context belongs to that call, however
// many others share the instance. Inferred from the instance alone, a second
// call -- one an earlier client never resumed was enough -- left every later
// question with the broker, and the client that could answer never saw it.
func TestAQuestionOnACallsContextIsThatCalls(t *testing.T) {
	r := &Registry{asks: newAskTable()}
	r.asks.begin("call-1", "srv", "shared", "s1")
	r.asks.begin("call-2", "srv", "shared", "s2")
	a, ok := r.askFor(withAskCall(context.Background(), "call-2"), "srv", "shared")
	if !ok || a.ID != "call-2" {
		t.Fatalf("askFor = %v, %v; want call-2", a, ok)
	}
	if _, ok := r.askFor(withAskCall(context.Background(), "call-2"), "other", "shared"); ok {
		t.Fatal("a call is only the asker for its own server")
	}
}
