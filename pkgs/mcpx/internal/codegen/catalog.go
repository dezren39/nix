package codegen

import (
	"fmt"
	"github.com/dezren39/mcpx/internal/defaults"
	"sort"
	"strings"
)

// CatalogOptions control how a budgeted catalogue is fitted.
type CatalogOptions struct {
	// Budget is the approximate token ceiling for the whole block. Zero means
	// the default.
	Budget int
	// Bias, when set, promotes signatures matching these terms so a query can
	// influence which tools win their pass.
	Bias []string
}

// DefaultCatalogBudget is the token ceiling used when none is given. It comes
// from the embedded defaults layer, not from a const here, so that every
// default the program has is readable in one file.
var DefaultCatalogBudget = defaults.CatalogBudget

// charsPerToken is the estimate used for budgeting. It is deliberately crude:
// the budget is a guard rail, not an accounting system, and a real tokeniser
// would tie the output to one model family.
const charsPerToken = 4

// descriptionLimit truncates a tool's first description line.
const descriptionLimit = 120

func cost(s string) int { return (len(s) + charsPerToken - 1) / charsPerToken }

type listing struct {
	path  string
	line  string
	cost  int
	bias  int
	shown bool
}

type nsFit struct {
	ns       Namespace
	listings []*listing
	order    []*listing
	next     int
	shown    int
}

// Catalog renders every namespace, with as many full signatures as fit inside
// a token budget.
//
// The allocation is round-robin rather than global: namespaces are visited in
// turn, each contributing its cheapest remaining signature, until the budget
// is spent. Ranking globally by cost would let one namespace's short
// signatures crowd out every other namespace; taking namespaces whole would
// let a large one consume the entire budget before the rest are reached.
// Rotation gives a three-tool namespace all three and a twenty-seven-tool
// namespace as many as fit.
//
// Namespace headers are always emitted, and are charged against the budget
// before any signature is. A namespace is therefore never invisible, however
// small the budget: the worst case is a list of names and counts, which is
// still enough to know what to ask for next.
func Catalog(nss []Namespace, opts CatalogOptions) string {
	budget := opts.Budget
	if budget <= 0 {
		budget = DefaultCatalogBudget
	}

	sorted := append([]Namespace(nil), nss...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	fits := make([]*nsFit, 0, len(sorted))
	for _, ns := range sorted {
		f := &nsFit{ns: ns}
		tools := append([]Tool(nil), ns.Tools...)
		sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
		for _, t := range tools {
			l := &listing{path: ns.Name + "." + t.Name, line: catalogLine(ns, t)}
			l.cost = cost(l.line)
			l.bias = biasScore(ns, t, opts.Bias)
			f.listings = append(f.listings, l)
		}
		f.order = append([]*listing(nil), f.listings...)
		// Cheapest first, but anything matching the bias is considered before
		// anything that does not, so a query shapes what survives the budget.
		sort.SliceStable(f.order, func(i, j int) bool {
			a, b := f.order[i], f.order[j]
			if (a.bias > 0) != (b.bias > 0) {
				return a.bias > b.bias
			}
			if a.bias != b.bias {
				return a.bias > b.bias
			}
			if a.cost != b.cost {
				return a.cost < b.cost
			}
			return a.path < b.path
		})
		fits = append(fits, f)
	}

	// Headers are charged first so they can never be squeezed out.
	remaining := budget
	for _, f := range fits {
		remaining -= cost(namespaceHeader(f, len(f.listings)))
	}

	active := make([]*nsFit, len(fits))
	copy(active, fits)
	for len(active) > 0 {
		next := active[:0]
		for _, f := range active {
			if f.next >= len(f.order) {
				continue
			}
			cand := f.order[f.next]
			if cand.cost > remaining {
				continue // this namespace is done; others may still fit
			}
			cand.shown = true
			f.next++
			f.shown++
			remaining -= cand.cost
			if f.next < len(f.order) {
				next = append(next, f)
			}
		}
		if len(next) == len(active) && len(next) > 0 && remaining <= 0 {
			break
		}
		if len(next) == 0 {
			break
		}
		active = next
	}

	var b strings.Builder
	b.WriteString("// mcpx catalog. Namespaces are complete; signatures are fitted to a budget.\n")
	b.WriteString("// Use `mcpx types <namespace>` for everything in one, or `mcpx search <query>`.\n\n")
	for _, f := range fits {
		b.WriteString(namespaceHeader(f, len(f.listings)))
		b.WriteString("\n")
		for _, l := range f.listings {
			if l.shown {
				b.WriteString(l.line)
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

func namespaceHeader(f *nsFit, total int) string {
	unit := "tools"
	if total == 1 {
		unit = "tool"
	}
	label := fmt.Sprintf("%d %s", total, unit)
	switch {
	case f.shown == total && total > 0:
	case f.shown == 0:
		label += ", none shown"
	default:
		label = fmt.Sprintf("%d %s, %d shown", total, unit, f.shown)
	}
	line := fmt.Sprintf("- %s (%s)", f.ns.Name, label)
	if d := strings.TrimSpace(f.ns.Description); d != "" {
		line += " // " + oneLine(d)
	}
	return line
}

func catalogLine(ns Namespace, t Tool) string {
	args, optional := ArgsTypeFor(t.InputSchema, "")
	opt := ""
	if optional {
		opt = "?"
	}
	line := fmt.Sprintf("  - %s.%s(args%s: %s)", ns.Name, ToolFuncName(t.Name), opt, compact(args))
	if d := firstLine(t.Description); d != "" {
		line += " // " + d
	}
	return line
}

// compact collapses a multi-line object type onto one line, since a catalogue
// entry is a reminder rather than a definition.
func compact(t string) string {
	if !strings.Contains(t, "\n") {
		return t
	}
	var out []string
	for _, line := range strings.Split(t, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "/*") || strings.HasPrefix(line, "*") {
			continue
		}
		out = append(out, line)
	}
	joined := strings.Join(out, " ")
	joined = strings.ReplaceAll(joined, "{ ", "{")
	joined = strings.ReplaceAll(joined, "; }", " }")
	return strings.TrimSpace(joined)
}

func firstLine(s string) string {
	line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(s), "\n", 2)[0])
	if len(line) > descriptionLimit {
		return line[:descriptionLimit-3] + "..."
	}
	return line
}

func oneLine(s string) string {
	return firstLine(s)
}

func biasScore(ns Namespace, t Tool, terms []string) int {
	if len(terms) == 0 {
		return 0
	}
	hay := strings.ToLower(ns.Name + "." + t.Name + " " + t.Description)
	score := 0
	for _, term := range terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" {
			continue
		}
		switch {
		case strings.Contains(strings.ToLower(t.Name), term):
			score += 10
		case strings.Contains(hay, term):
			score += 3
		}
	}
	return score
}
