package conformance

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var issueRe = regexp.MustCompile(`^#[0-9]+$`)

// Status of one requirement, in one revision, for one side.
type Status struct {
	Kind   string   // "tested", "n/a", "gap", "missing"
	Tests  []string // for tested
	Detail string   // reason for n/a, issue for gap
}

// AreaGroup is the first word of a catalogue area ("resources / errors" is
// "resources"), which is how the work on the matrix is divided.
func AreaGroup(area string) string {
	a := strings.TrimSpace(area)
	if i := strings.IndexAny(a, " /("); i >= 0 {
		a = a[:i]
	}
	if a == "" {
		return "security"
	}
	return a
}

// Cell keys a requirement in a revision on a side.
type Cell struct{ ID, Rev, Side string }

// Evaluate joins the catalogue with coverage and gaps.
func Evaluate(reqs []Requirement, cs []Cover, gs []Gap) (map[Cell]Status, error) {
	byID := map[string]*Requirement{}
	for i := range reqs {
		byID[reqs[i].ID] = &reqs[i]
	}
	tests := map[Cell][]string{}
	for _, c := range cs {
		r, ok := byID[c.ID]
		if !ok {
			return nil, fmt.Errorf("cover names unknown requirement %q (%s)", c.ID, c.Test)
		}
		ref, err := ParseRef(c.Test)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", c.ID, err)
		}
		hit := false
		for _, rev := range r.Revs {
			if ref.CoversRev(rev) {
				hit = true
				k := Cell{c.ID, rev, c.Side}
				tests[k] = append(tests[k], Expand(c.Test, rev, *r))
			}
		}
		if !hit {
			return nil, fmt.Errorf("%s: %s covers no revision the requirement has (%v)", c.ID, c.Test, r.Revs)
		}
	}
	gapAt := map[Cell]Gap{}
	for _, g := range gs {
		r, ok := byID[g.ID]
		if !ok {
			return nil, fmt.Errorf("gap names unknown requirement %q", g.ID)
		}
		if !issueRe.MatchString(g.Issue) {
			return nil, fmt.Errorf("gap %s names %q, not a filed issue (#123)", g.ID, g.Issue)
		}
		revs := g.Revs
		if revs == nil {
			revs = r.Revs
		}
		for _, rev := range revs {
			gapAt[Cell{g.ID, rev, g.Side}] = g
		}
	}
	out := map[Cell]Status{}
	var contradictions []string
	for _, r := range reqs {
		for _, rev := range r.Revs {
			for _, side := range []string{Server, Client} {
				k := Cell{r.ID, rev, side}
				a := r.Applicability[side]
				switch {
				case strings.HasPrefix(a, NAPrefix):
					out[k] = Status{Kind: "n/a", Detail: strings.TrimPrefix(a, NAPrefix)}
				case strings.HasPrefix(a, NotImplPrefix):
					out[k] = Status{Kind: "gap", Detail: "not implemented: " + strings.TrimPrefix(a, NotImplPrefix)}
				// A recorded gap wins over a cover: the covering test
				// skips there.
				case gapAt[k].Issue != "":
					g := gapAt[k]
					out[k] = Status{Kind: "gap", Detail: g.Issue + ": " + g.Why}
				case len(tests[k]) > 0:
					t := append([]string(nil), tests[k]...)
					sort.Strings(t)
					out[k] = Status{Kind: "tested", Tests: t}
					// A note that says gap and a test that says otherwise
					// cannot both be published; one of them is stale.
					if r.Claims[side] == ClaimGap {
						contradictions = append(contradictions, fmt.Sprintf("%s %s %s: note says %q, tested by %s",
							rev, side, r.ID, r.Notes[side], strings.Join(t, ", ")))
					}
				default:
					out[k] = Status{Kind: "missing"}
				}
			}
		}
	}
	if len(contradictions) > 0 {
		return nil, fmt.Errorf("%d cells are tested while their catalogue note claims a gap; "+
			"correct the note in %s or the cover:\n%s", len(contradictions), MarkdownPath, strings.Join(contradictions, "\n"))
	}
	return out, nil
}

// LevelClass reduces a catalogue level to its strongest keyword.
func LevelClass(level string) string {
	u := strings.ToUpper(level)
	best, at := "other", len(u)+1
	for _, k := range []struct{ word, class string }{
		{"MUST NOT", "MUST NOT"}, {"SHALL NOT", "MUST NOT"}, {"MUST", "MUST"}, {"REQUIRED", "MUST"},
		{"SHOULD NOT", "SHOULD NOT"}, {"SHOULD", "SHOULD"}, {"RECOMMENDED", "SHOULD"},
		{"MAY", "MAY"}, {"OPTIONAL", "MAY"},
	} {
		if i := strings.Index(u, k.word); i >= 0 && i < at {
			best, at = k.class, i
		}
	}
	return best
}

var levelOrder = []string{"MUST", "MUST NOT", "SHOULD", "SHOULD NOT", "MAY", "other"}

// Counts is applies/tested/n-a/gap for one bucket.
type Counts struct{ Applies, Tested, NA, Gap, Missing int }

// Summary counts per revision, side and level class.
func Summary(reqs []Requirement, st map[Cell]Status) map[string]map[string]map[string]*Counts {
	out := map[string]map[string]map[string]*Counts{}
	for _, r := range reqs {
		lc := LevelClass(r.Level)
		for _, rev := range r.Revs {
			for _, side := range []string{Server, Client} {
				if out[rev] == nil {
					out[rev] = map[string]map[string]*Counts{}
				}
				if out[rev][side] == nil {
					out[rev][side] = map[string]*Counts{}
				}
				c := out[rev][side][lc]
				if c == nil {
					c = &Counts{}
					out[rev][side][lc] = c
				}
				s := st[Cell{r.ID, rev, side}]
				switch s.Kind {
				case "n/a":
					c.NA++
				case "tested":
					c.Applies++
					c.Tested++
				case "gap":
					c.Applies++
					c.Gap++
				case "missing":
					c.Applies++
					c.Missing++
				}
			}
		}
	}
	return out
}

func mdEscape(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.ReplaceAll(strings.ReplaceAll(s, `\|`, "|"), "|", `\|`)
}

func cellText(s Status) string {
	switch s.Kind {
	case "tested":
		t := make([]string, len(s.Tests))
		for i, x := range s.Tests {
			t[i] = "`" + x + "`"
		}
		return "tested: " + strings.Join(t, "<br>")
	case "n/a":
		return "n/a: " + mdEscape(s.Detail)
	case "gap":
		return "**gap** " + mdEscape(s.Detail)
	}
	return "**MISSING**"
}

// Render is docs/spec/conformance-matrix.md.
//
// inputs is InputDigest of the tree it was generated from: a file cannot
// name the commit that contains it, but it can name its inputs, and
// TestMatrix fails when they no longer hash to what the file says.
func Render(reqs []Requirement, st map[Cell]Status, inputs string) string {
	var b strings.Builder
	b.WriteString("# MCP conformance matrix\n\n")
	b.WriteString("Generated by `internal/conformance` (`MCPX_UPDATE_MATRIX=1 go test ./internal/conformance -run TestMatrix`); do not edit.\n\n")
	fmt.Fprintf(&b, "Generated from inputs `%s` (sha256 of %s). `TestMatrix` regenerates this file and fails when it differs, so it describes the tree it is committed in.\n\n",
		inputs, strings.Join(InputGlobs, ", "))
	b.WriteString("Every normative requirement of every revision (catalogue: `docs/spec/requirements.md`), for mcpx as a server and as a client. ")
	b.WriteString("A requirement is *tested* when a named test verifies it in that revision, *n/a* when it does not bind mcpx in that role, ")
	b.WriteString("and a *gap* when it binds mcpx and mcpx does not meet it yet, with the issue that tracks it. ")
	b.WriteString("Run exactly the covering tests with `go run ./internal/conformance/cmd/covering`.\n\n")

	sum := Summary(reqs, st)
	b.WriteString("## Summary\n\n| revision | side | level | applies | tested | gap | missing | n/a |\n|---|---|---|---|---|---|---|---|\n")
	for _, rev := range Revisions {
		for _, side := range []string{Server, Client} {
			for _, lc := range levelOrder {
				c := sum[rev][side][lc]
				if c == nil {
					continue
				}
				fmt.Fprintf(&b, "| %s | %s | %s | %d | %d | %d | %d | %d |\n", rev, side, lc, c.Applies, c.Tested, c.Gap, c.Missing, c.NA)
			}
		}
	}
	for _, rev := range Revisions {
		fmt.Fprintf(&b, "\n## %s\n", rev)
		byArea := map[string][]Requirement{}
		var areas []string
		for _, r := range reqs {
			for _, v := range r.Revs {
				if v == rev {
					if byArea[r.Area] == nil {
						areas = append(areas, r.Area)
					}
					byArea[r.Area] = append(byArea[r.Area], r)
				}
			}
		}
		sort.Strings(areas)
		for _, a := range areas {
			fmt.Fprintf(&b, "\n### %s\n\n| requirement | level | side | mcpx as server | mcpx as client |\n|---|---|---|---|---|\n", a)
			for _, r := range byArea[a] {
				fmt.Fprintf(&b, "| [%s](%s) | %s | %s | %s | %s |\n", r.ID, r.URL, mdEscape(r.Level), mdEscape(r.Side),
					cellText(st[Cell{r.ID, rev, Server}]), cellText(st[Cell{r.ID, rev, Client}]))
			}
		}
	}
	return b.String()
}
