package secrets

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// clock is a board whose time a test sets, so retention can be tested without
// sleeping.
// testRetention is the window the board tests use.
const testRetention = 10 * time.Minute

func clock(t *testing.T) (*Board, *time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	b := NewBoard(testRetention)
	b.now = func() time.Time { return now }
	return b, &now
}

func TestRequireCreatesOnePendingRequestPerServerAndSecretSet(t *testing.T) {
	b, _ := clock(t)
	first := b.Require("chrome", []string{"TOKEN", "KEY"})
	if first.Status != Pending || first.Server != "chrome" {
		t.Fatalf("a new request is pending for its server: %+v", first)
	}
	if !strings.HasPrefix(first.ID, "sr-") || len(first.ID) != len("sr-")+16 {
		t.Errorf("id %q should be sr- and sixteen hex digits", first.ID)
	}
	// Same server, same names in a different order: the same request.
	again := b.Require("chrome", []string{"KEY", "TOKEN"})
	if again.ID != first.ID {
		t.Fatalf("a pending request is reused, got %s then %s", first.ID, again.ID)
	}
	if got := len(b.List()); got != 1 {
		t.Fatalf("reuse must not duplicate: %d requests", got)
	}
	// A different set of names, or a different server, is a different request.
	if other := b.Require("chrome", []string{"TOKEN"}); other.ID == first.ID {
		t.Error("a request for a subset of the names is its own request")
	}
	if other := b.Require("fs", []string{"TOKEN", "KEY"}); other.ID == first.ID {
		t.Error("the same names for another server are their own request")
	}
}

func TestRequireDoesNotReuseAnEndedRequest(t *testing.T) {
	b, _ := clock(t)
	first := b.Require("chrome", []string{"TOKEN"})
	b.Decline(first.ID)
	next := b.Require("chrome", []string{"TOKEN"})
	if next.ID == first.ID || next.Status != Pending {
		t.Fatalf("after a decline, the next ask is a new pending request: %+v", next)
	}
}

func TestMessageNamesSecretsAndNeverCarriesAValue(t *testing.T) {
	b, _ := clock(t)
	r := b.Require("chrome", []string{"GITHUB_TOKEN"})
	if !strings.Contains(r.Message, "chrome") || !strings.Contains(r.Message, "GITHUB_TOKEN") {
		t.Errorf("the message should name the server and the secret: %q", r.Message)
	}
	if !strings.Contains(r.Message, "mcpx secrets set") {
		t.Errorf("the message should say how to answer: %q", r.Message)
	}
}

func TestSettleResolvesOnlyWhenEverySecretHasAValue(t *testing.T) {
	b, _ := clock(t)
	r := b.Require("chrome", []string{"A", "B"})
	have := map[string]bool{"A": true}
	has := func(server, name string) bool { return server == "chrome" && have[name] }
	b.Settle(has)
	if got, _ := b.Get(r.ID); got.Status != Pending {
		t.Fatalf("with one of two secrets set, the request waits: %s", got.Status)
	}
	have["B"] = true
	b.Settle(has)
	if got, _ := b.Get(r.ID); got.Status != Resolved {
		t.Fatalf("with both set, the request is resolved: %s", got.Status)
	}
}

func TestSettleJudgesEachServerItself(t *testing.T) {
	b, _ := clock(t)
	r := b.Require("chrome", []string{"TOKEN"})
	// The same name is set, but for another server.
	b.Settle(func(server, name string) bool { return server == "fs" && name == "TOKEN" })
	if got, _ := b.Get(r.ID); got.Status != Pending {
		t.Fatal("a value for another server does not resolve this server's request")
	}
}

func TestDeclineEndsAPendingRequestAndKeepsItsStatus(t *testing.T) {
	b, _ := clock(t)
	r := b.Require("chrome", []string{"TOKEN"})
	got, ok := b.Decline(r.ID)
	if !ok || got.Status != Declined {
		t.Fatalf("decline ends a pending request: %+v, %v", got, ok)
	}
	// Declining again changes nothing, and neither does declining a request
	// that was already resolved.
	if again, _ := b.Decline(r.ID); again.Status != Declined {
		t.Errorf("a second decline keeps declined: %s", again.Status)
	}
	done := b.Require("fs", []string{"X"})
	b.Settle(func(string, string) bool { return true })
	if after, _ := b.Decline(done.ID); after.Status != Resolved {
		t.Errorf("a resolved request stays resolved: %s", after.Status)
	}
	if _, ok := b.Decline("sr-nothing"); ok {
		t.Error("declining an unknown id reports false")
	}
}

func TestListPendingFirstThenRecentEndedAndForgetsOldOnes(t *testing.T) {
	b, now := clock(t)
	old := b.Require("a", []string{"X"})
	b.Decline(old.ID)
	*now = now.Add(time.Minute)
	pending := b.Require("b", []string{"Y"})
	*now = now.Add(time.Minute)
	recent := b.Require("c", []string{"Z"})
	b.Decline(recent.ID)

	list := b.List()
	if len(list) != 3 {
		t.Fatalf("three requests inside retention: %d", len(list))
	}
	if list[0].ID != pending.ID || list[0].Status != Pending {
		t.Fatalf("pending requests come first: %+v", list[0])
	}
	if list[1].ID != recent.ID {
		t.Errorf("then ended requests, most recent first: got %s", list[1].ID)
	}

	// Past the retention window, an ended request is forgotten and a pending
	// one is not, however old it is: someone is still waiting on it.
	*now = now.Add(testRetention + time.Second)
	list = b.List()
	if len(list) != 1 || list[0].ID != pending.ID {
		t.Fatalf("after retention only the pending request remains: %+v", list)
	}
	if _, ok := b.Get(recent.ID); ok {
		t.Error("an ended request past retention is gone")
	}
}

func TestListIsNeverNil(t *testing.T) {
	b, _ := clock(t)
	if b.List() == nil {
		t.Fatal("an empty board must list an empty slice, which serialises as []")
	}
}

func TestRequestWireShapeIsTheContract(t *testing.T) {
	b, _ := clock(t)
	r := b.Require("chrome", []string{"TOKEN"})
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(raw)
	for _, key := range []string{`"id":`, `"server":`, `"secrets":`, `"status":`, `"createdAt":`, `"message":`} {
		if !strings.Contains(wire, key) {
			t.Errorf("request JSON lacks %s: %s", key, wire)
		}
	}
	if strings.Contains(wire, "settled") {
		t.Errorf("the settle time is internal and must not be on the wire: %s", wire)
	}
}
