// Package spec decides between MCP revisions where they genuinely conflict.
//
// mcpx serves every revision at once, and most differences are settled by
// the revision the peer states. A few are not: two revisions require
// incompatible behaviour and nothing on the wire says which the peer
// expects (docs/spec/revision-conflicts.md). Those are decided here, by two
// settings rather than by whichever rule the code happened to be written
// against (#307):
//
//   - spec.precedence, an ordered list of revisions. The first is the one
//     whose rule wins. Default: every revision mcpx supports, newest first.
//     --mcp-spec / MCPX_MCP_SPEC (spec.first) moves one revision to the front.
//   - spec.lenient, the revisions mcpx does not hold strictly. Every revision
//     is strict by default.
//
// Code asks the active Policy -- spec.Current() -- through First, Strict and
// the predicates, never by comparing date strings in place.
package spec

import (
	"fmt"
	"strings"
	"sync/atomic"
)

// Revisions is every MCP revision mcpx serves, newest first. It is the one
// list: mcpserver.Supported is this slice, and the default precedence is it.
var Revisions = []string{"2026-07-28", "2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

// Known reports whether mcpx serves a revision.
func Known(rev string) bool {
	for _, r := range Revisions {
		if r == rev {
			return true
		}
	}
	return false
}

// Policy is a resolved precedence order and strictness table.
type Policy struct {
	order   []string
	lenient map[string]bool
}

// Default is every revision newest first, every revision strict.
func Default() Policy {
	return Policy{order: append([]string(nil), Revisions...)}
}

// New builds a policy. precedence may be partial: revisions it does not name
// follow it, newest first. first, when non-empty, is moved to the front.
// An unknown or repeated revision is refused, naming the valid ones.
func New(precedence []string, first string, lenient []string) (Policy, error) {
	seen := map[string]bool{}
	var order []string
	add := func(r string) { seen[r] = true; order = append(order, r) }
	if first != "" {
		if err := check("spec.first (--mcp-spec)", first); err != nil {
			return Policy{}, err
		}
		add(first)
	}
	named := map[string]bool{}
	for _, r := range precedence {
		if err := check("spec.precedence", r); err != nil {
			return Policy{}, err
		}
		if named[r] {
			return Policy{}, fmt.Errorf("spec.precedence names %s twice", r)
		}
		named[r] = true
		if !seen[r] {
			add(r)
		}
	}
	for _, r := range Revisions {
		if !seen[r] {
			add(r)
		}
	}
	p := Policy{order: order}
	for _, r := range lenient {
		if err := check("spec.lenient", r); err != nil {
			return Policy{}, err
		}
		if p.lenient == nil {
			p.lenient = map[string]bool{}
		}
		p.lenient[r] = true
	}
	return p, nil
}

func check(what, rev string) error {
	if Known(rev) {
		return nil
	}
	return fmt.Errorf("%s: unknown MCP revision %q; valid: %s", what, rev, strings.Join(Revisions, ", "))
}

// Order is the full precedence, first wins.
func (p Policy) Order() []string {
	if len(p.order) == 0 {
		return Default().order
	}
	return append([]string(nil), p.order...)
}

// First is the revision whose rule wins a conflict.
func (p Policy) First() string { return p.Order()[0] }

// FirstIs reports whether the winning revision satisfies a predicate.
func (p Policy) FirstIs(pred Pred) bool { return pred(p.First()) }

// Strict reports whether mcpx holds a revision's rules strictly.
func (p Policy) Strict(rev string) bool { return !p.lenient[rev] }

// Winner is the highest-ranked of the revisions given, or "" if none is
// known. The revisions given are the ones a particular conflict is between.
func (p Policy) Winner(revs ...string) string {
	for _, r := range p.Order() {
		for _, c := range revs {
			if c == r {
				return r
			}
		}
	}
	return ""
}

// Governs reports whether rev's rule decides a conflict between the
// revisions given, and is held strictly.
//
// This is the default way to settle a conflict two revisions answer
// differently: whichever of them the operator ranked higher wins, and only
// that one's strictness is consulted -- the others' settings say nothing
// about a conflict they lost. With no revisions given the conflict is taken
// to be between all of them, which is the common case: one revision does
// something the rest do not.
//
// A rule that needs to decide differently is free to: Order, First, FirstIs,
// Winner and Strict are all available, and `docs/protocol.md` asks that such
// a rule say in a comment why the usual answer is wrong for it.
func (p Policy) Governs(rev string, among ...string) bool {
	if len(among) == 0 {
		among = p.Order()
	} else {
		among = append(append([]string(nil), among...), rev)
	}
	return p.Winner(among...) == rev && p.Strict(rev)
}

// StrictTable is Strict for every revision, for reporting.
func (p Policy) StrictTable() map[string]bool {
	out := make(map[string]bool, len(Revisions))
	for _, r := range Revisions {
		out[r] = p.Strict(r)
	}
	return out
}

// Pred is a test on a revision. Revisions are ISO dates, so date order is
// lexical order.
type Pred func(rev string) bool

// AtOrAfter is rev >= d.
func AtOrAfter(d string) Pred { return func(r string) bool { return r != "" && r >= d } }

// Before is rev < d.
func Before(d string) Pred { return func(r string) bool { return r != "" && r < d } }

// Only is rev in revs.
func Only(revs ...string) Pred {
	return func(r string) bool {
		for _, v := range revs {
			if r == v {
				return true
			}
		}
		return false
	}
}

// Between is every revision from a to b inclusive, in either order.
func Between(a, b string) Pred {
	if a > b {
		a, b = b, a
	}
	return func(r string) bool { return r != "" && r >= a && r <= b }
}

var current atomic.Pointer[Policy]

// Current is the process's active policy.
func Current() Policy {
	if p := current.Load(); p != nil {
		return *p
	}
	return Default()
}

// Set installs a policy and returns a function that restores the previous
// one, so a test can write t.Cleanup(spec.Set(p)).
func Set(p Policy) (restore func()) {
	prev := current.Swap(&p)
	return func() { current.Store(prev) }
}

// Must is New for tests and constants: it panics on an invalid revision.
func Must(precedence []string, first string, lenient []string) Policy {
	p, err := New(precedence, first, lenient)
	if err != nil {
		panic(err)
	}
	return p
}
