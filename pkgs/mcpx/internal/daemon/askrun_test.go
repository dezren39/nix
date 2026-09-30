package daemon

import (
	"context"
	"testing"
)

// Correlating questions raised inside a script (mcpx_exec) to the script's
// ask-call through its run id. See askTable. Issue #77.
func TestAskTableAttributesByRun(t *testing.T) {
	t.Run("exec/correlation/run-member-question-belongs-to-run", func(t *testing.T) {
		tb := newAskTable()
		tb.beginRun("call-1", "run-1", "s")
		leave := tb.join("run-1", "ask", "k")
		defer leave()
		a, ok := tb.forKey("ask", "k")
		if !ok || a.ID != "call-1" {
			t.Fatalf("forKey = %v, %v; want call-1", a, ok)
		}
	})

	t.Run("exec/correlation/parallel-calls-of-one-run-stay-attributed", func(t *testing.T) {
		tb := newAskTable()
		tb.beginRun("call-1", "run-1", "s")
		l1 := tb.join("run-1", "ask", "k")
		l2 := tb.join("run-1", "ask", "k")
		if a, ok := tb.forKey("ask", "k"); !ok || a.ID != "call-1" {
			t.Fatalf("two calls from one run must stay that run's, got %v %v", a, ok)
		}
		l1()
		if a, ok := tb.forKey("ask", "k"); !ok || a.ID != "call-1" {
			t.Fatalf("leave must remove one entry only, got %v %v", a, ok)
		}
		l2()
		if _, ok := tb.forKey("ask", "k"); ok {
			t.Fatal("no calls in flight, nothing to attribute")
		}
	})

	t.Run("exec/correlation/two-runs-on-a-shared-key-are-ambiguous", func(t *testing.T) {
		tb := newAskTable()
		tb.beginRun("call-1", "run-1", "s1")
		tb.beginRun("call-2", "run-2", "s2")
		defer tb.join("run-1", "ask", "shared")()
		defer tb.join("run-2", "ask", "shared")()
		if a, ok := tb.forKey("ask", "shared"); ok {
			t.Fatalf("two runs share the key; attributed to %s", a.ID)
		}
	})

	t.Run("exec/correlation/an-unregistered-caller-makes-the-key-ambiguous", func(t *testing.T) {
		// A plain /v1/call from someone else sharing the instance: before
		// runs, such calls were invisible and the ask-call got their question.
		tb := newAskTable()
		tb.begin("call-1", "ask", "shared", "s1")
		defer tb.join("", "ask", "shared")()
		if a, ok := tb.forKey("ask", "shared"); ok {
			t.Fatalf("an anonymous caller shares the key; attributed to %s", a.ID)
		}
		tb2 := newAskTable()
		tb2.beginRun("call-1", "run-1", "s1")
		defer tb2.join("run-1", "ask", "shared")()
		defer tb2.join("", "ask", "shared")()
		if a, ok := tb2.forKey("ask", "shared"); ok {
			t.Fatalf("an anonymous caller shares the key with a run; attributed to %s", a.ID)
		}
	})

	t.Run("exec/correlation/unknown-run-joins-anonymously", func(t *testing.T) {
		tb := newAskTable()
		defer tb.join("not-a-live-run", "ask", "k")()
		if _, ok := tb.forKey("ask", "k"); ok {
			t.Fatal("a run nobody registered can own nothing")
		}
	})

	t.Run("exec/correlation/ended-run-owns-nothing-still-in-flight", func(t *testing.T) {
		tb := newAskTable()
		tb.beginRun("call-1", "run-1", "s")
		leave := tb.join("run-1", "ask", "k")
		tb.end("call-1")
		if a, ok := tb.forKey("ask", "k"); ok {
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
