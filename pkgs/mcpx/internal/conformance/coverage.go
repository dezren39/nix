package conformance

import (
	"fmt"
	"sort"
	"strings"
)

// Cover says a test verifies a requirement for one side of the wire.
//
// Test is "<pkg>.<TestName>[/<subtest>...][@rev,rev|@all]" where <pkg> is a
// directory under internal/. The revisions a reference covers are the
// revision its subtest path begins with (the "<rev>/<area>/<id>"
// convention), or, for a test not named per revision, the explicit @ list.
type Cover struct {
	ID   string
	Side string
	Test string
}

// Gap is a requirement that applies to mcpx and that mcpx does not yet
// meet, named with the issue that tracks it. Test, when set, is the failing
// test that is kept but skipped with t.Skip("gap: <issue>").
type Gap struct {
	ID    string
	Side  string
	Revs  []string // nil: every revision the requirement has
	Issue string   // "#123": a filed GitHub issue
	Why   string
	Test  string
}

var (
	covers []Cover
	gaps   []Gap
)

// S and C build server- and client-side covers; several tests may be named.
func S(id string, tests ...string) []Cover { return mk(id, Server, tests) }
func C(id string, tests ...string) []Cover { return mk(id, Client, tests) }

func mk(id, side string, tests []string) []Cover {
	out := make([]Cover, 0, len(tests))
	for _, t := range tests {
		out = append(out, Cover{ID: id, Side: side, Test: t})
	}
	return out
}

func register(cs ...[]Cover) {
	for _, c := range cs {
		covers = append(covers, c...)
	}
}

func registerGaps(gs ...Gap) { gaps = append(gaps, gs...) }

// Covers and Gaps return the registries, sorted for stable output.
func Covers() []Cover {
	out := append([]Cover(nil), covers...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		if out[i].Side != out[j].Side {
			return out[i].Side < out[j].Side
		}
		return out[i].Test < out[j].Test
	})
	return out
}

func Gaps() []Gap {
	out := append([]Gap(nil), gaps...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return out[i].Side < out[j].Side
	})
	return out
}

// Ref is a parsed test reference.
type Ref struct {
	Pkg, Func, Sub string
	Revs           []string // revisions covered; nil with All
	All            bool
	// Template is set when the subtest contains "{rev}": the reference
	// stands for one subtest per revision of the requirement.
	Template bool
}

// RevPlaceholder in a reference is replaced by each revision the
// requirement has.
const RevPlaceholder = "{rev}"

// AreaPlaceholder and IDPlaceholder are replaced by the requirement's area
// group and id: the subtest names forReq in the conformance tests produces.
const (
	AreaPlaceholder = "{area}"
	IDPlaceholder   = "{id}"
)

// Expand is the concrete reference for one revision of a requirement.
func Expand(test, rev string, r Requirement) string {
	return strings.NewReplacer(RevPlaceholder, rev, AreaPlaceholder, AreaGroup(r.Area), IDPlaceholder, r.ID).Replace(test)
}

// ParseRef reads a Cover.Test.
func ParseRef(s string) (Ref, error) {
	var r Ref
	body, at, hasAt := strings.Cut(s, "@")
	dot := strings.Index(body, ".")
	if dot <= 0 {
		return r, fmt.Errorf("%q: want <pkg>.<TestName>", s)
	}
	r.Pkg = body[:dot]
	r.Func, r.Sub, _ = strings.Cut(body[dot+1:], "/")
	if !strings.HasPrefix(r.Func, "Test") {
		return r, fmt.Errorf("%q: %q is not a test function", s, r.Func)
	}
	if hasAt {
		if at == "all" {
			r.All = true
			return r, nil
		}
		for _, v := range strings.Split(at, ",") {
			if !isRev(v) {
				return r, fmt.Errorf("%q: unknown revision %q", s, v)
			}
			r.Revs = append(r.Revs, v)
		}
		return r, nil
	}
	if strings.Contains(r.Sub, RevPlaceholder) {
		r.Template, r.All = true, true
		return r, nil
	}
	first, _, _ := strings.Cut(r.Sub, "/")
	if !isRev(first) {
		return r, fmt.Errorf("%q: the subtest does not start with a revision; add @rev,... or @all", s)
	}
	r.Revs = []string{first}
	return r, nil
}

// CoversRev reports whether the reference covers rev.
func (r Ref) CoversRev(rev string) bool {
	if r.All {
		return true
	}
	for _, v := range r.Revs {
		if v == rev {
			return true
		}
	}
	return false
}

// All is every revision, for PerRev.
var All = Revisions

// PerRev names the subtest "<fn>/<rev>/<sub>" for each revision: the form a
// table-driven test over revisions produces.
func PerRev(fn, sub string, revs ...string) []string {
	out := make([]string, len(revs))
	for i, r := range revs {
		out[i] = fn + "/" + r + "/" + sub
	}
	return out
}

// SR and CR cover a requirement with a table-driven test over revisions:
// the subtest "<fn>/<rev>/<sub>" for every revision the requirement has.
func SR(id, fn, sub string) []Cover { return S(id, fn+"/"+RevPlaceholder+"/"+sub) }
func CR(id, fn, sub string) []Cover { return C(id, fn+"/"+RevPlaceholder+"/"+sub) }

// SReq and CReq cover requirements with forReq subtests of fn: one subtest
// "<rev>/<area>/<id>" per revision the requirement has, which is exactly
// what forReq runs, since it reads the revisions from the catalogue.
func SReq(fn string, ids ...string) []Cover { return reqs(Server, fn, ids) }
func CReq(fn string, ids ...string) []Cover { return reqs(Client, fn, ids) }

func reqs(side, fn string, ids []string) []Cover {
	out := make([]Cover, 0, len(ids))
	for _, id := range ids {
		out = append(out, Cover{ID: id, Side: side, Test: fn + "/" + RevPlaceholder + "/" + AreaPlaceholder + "/" + IDPlaceholder})
	}
	return out
}

// GapAt is the gap recorded for a requirement in one revision on one side,
// if any. The conformance tests skip there, so the failing check is kept
// and runs again the moment the gap entry is removed.
func GapAt(id, side, rev string) *Gap {
	for i := range gaps {
		g := &gaps[i]
		if g.ID != id || g.Side != side {
			continue
		}
		if g.Revs == nil {
			return g
		}
		for _, r := range g.Revs {
			if r == rev {
				return g
			}
		}
	}
	return nil
}
