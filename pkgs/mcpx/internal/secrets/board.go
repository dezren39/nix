package secrets

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Status is where a request stands.
type Status string

// The three states a request moves through. Pending is the only one that can
// change: a request ends as resolved, when its secrets all have values, or as
// declined, when somebody says no.
const (
	Pending  Status = "pending"
	Resolved Status = "resolved"
	Declined Status = "declined"
)

// Request says that a server needs one or more named secrets before it can
// run. It carries names only.
type Request struct {
	ID        string    `json:"id"`
	Server    string    `json:"server"`
	Secrets   []string  `json:"secrets"`
	Status    Status    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	Message   string    `json:"message"`

	// settled is when the request was resolved or declined. Zero while it is
	// pending, and not part of the wire shape.
	settled time.Time
}

// Board is the set of requests the daemon has made. It is memory only, like
// the values it is waiting for.
type Board struct {
	mu  sync.Mutex
	now func() time.Time
	// retention is how long a request that was resolved or declined stays
	// listed. A caller that polls needs to see how its request ended, and a
	// request from an hour ago is no longer an answer to anything.
	retention time.Duration
	requests  map[string]*Request
}

// NewBoard returns an empty board that keeps ended requests for retention.
func NewBoard(retention time.Duration) *Board {
	return &Board{now: time.Now, retention: retention, requests: map[string]*Request{}}
}

// Require returns the pending request for server and secrets, creating one
// when none is pending. Two requests for the same server and the same secrets
// are one request: the script that is waiting on it and the person who
// answers it must be looking at the same thing.
func (b *Board) Require(server string, secrets []string) Request {
	names := normalise(secrets)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneLocked()
	for _, r := range b.requests {
		if r.Status == Pending && r.Server == server && equal(r.Secrets, names) {
			return *r
		}
	}
	r := &Request{
		ID:        newID(),
		Server:    server,
		Secrets:   names,
		Status:    Pending,
		CreatedAt: b.now().UTC().Truncate(time.Second),
		Message:   message(server, names),
	}
	b.requests[r.ID] = r
	return *r
}

// Settle resolves every pending request whose secrets all have values. has
// reports whether a server has a value for a name, which is the server's own
// judgement: the same name can be set for one server and not another.
func (b *Board) Settle(has func(server, name string) bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, r := range b.requests {
		if r.Status != Pending {
			continue
		}
		done := true
		for _, name := range r.Secrets {
			if !has(r.Server, name) {
				done = false
				break
			}
		}
		if done {
			r.Status = Resolved
			r.settled = b.now()
		}
	}
}

// Get returns the request with id, and whether there is one.
func (b *Board) Get(id string) (Request, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneLocked()
	r, ok := b.requests[id]
	if !ok {
		return Request{}, false
	}
	return *r, true
}

// List returns the pending requests, oldest first, followed by the resolved
// and declined ones still inside the retention window, most recent first. It
// never returns nil, so an empty answer serialises as an empty list.
func (b *Board) List() []Request {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneLocked()
	out := make([]Request, 0, len(b.requests))
	for _, r := range b.requests {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		pi, pj := out[i].Status == Pending, out[j].Status == Pending
		if pi != pj {
			return pi
		}
		if pi {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

// Decline ends a pending request without an answer, and returns it. A request
// that has already ended keeps its status: a request that was resolved stays
// resolved, and declining twice changes nothing. False means there is no such
// request.
func (b *Board) Decline(id string) (Request, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pruneLocked()
	r, ok := b.requests[id]
	if !ok {
		return Request{}, false
	}
	if r.Status == Pending {
		r.Status = Declined
		r.settled = b.now()
	}
	return *r, true
}

// pruneLocked forgets requests that ended more than the retention window ago.
// Pending requests are never forgotten: they are waiting on someone.
func (b *Board) pruneLocked() {
	cutoff := b.now().Add(-b.retention)
	for id, r := range b.requests {
		if r.Status != Pending && r.settled.Before(cutoff) {
			delete(b.requests, id)
		}
	}
}

// normalise returns names sorted and without repeats, which is the form two
// requests are compared in.
func normalise(names []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, n := range names {
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func message(server string, names []string) string {
	return fmt.Sprintf("%s needs %s before it can run. Give the value in OpenChamber, "+
		"or run `mcpx secrets set <NAME>` for each one.", server, strings.Join(names, ", "))
}

func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail on a working system; if it ever does, a
		// fixed-width id from the clock is still unique enough for a board
		// that holds a handful of requests.
		return fmt.Sprintf("sr-%x", time.Now().UnixNano())
	}
	return "sr-" + hex.EncodeToString(b[:])
}
