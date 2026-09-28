package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dezren39/mcpx/internal/daemon"
)

// Source is everything the view reads.
//
// An interface rather than the concrete client, so a fake drives it in tests.
// A full-screen program that cannot be tested is one that breaks quietly, and
// the failures worth catching are not visual.
type Source interface {
	Namespaces(ctx context.Context) ([]daemon.NamespaceInfo, error)
	Tools(ctx context.Context, ns string) ([]daemon.ToolInfo, error)
	Signature(ctx context.Context, ns, tool string) (string, error)
	Records(ctx context.Context, limit int) ([]Record, error)
	Stats(ctx context.Context, dimension string) (Table, error)
	Instances(ctx context.Context) (Table, error)
	Sessions(ctx context.Context) (Table, error)
	Storage(ctx context.Context) (Table, error)
}

// Record is one log line.
//
// Attrs is kept as a map rather than a rendered string because the detail
// view needs the values, and re-parsing a rendered line to get them back
// would be both slower and lossy.
type Record struct {
	Time  time.Time
	Level string
	Msg   string
	Attrs map[string]any
}

// Line renders the record as one row.
func (r Record) Line() string {
	return fmt.Sprintf("%s %-5s %s %s",
		r.Time.Format("15:04:05.000"), r.Level, r.Msg, r.flatAttrs())
}

func (r Record) flatAttrs() string {
	if len(r.Attrs) == 0 {
		return ""
	}
	keys := make([]string, 0, len(r.Attrs))
	for k := range r.Attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%v ", k, compact(r.Attrs[k]))
	}
	return strings.TrimSpace(b.String())
}

// Detail renders the record expanded, one attribute per line.
//
// This is what pressing enter is for. A log line is truncated to fit a
// terminal, and the part that gets cut is usually the part being looked for;
// a stack, a full path, a nested result.
func (r Record) Detail() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s\n%s\n\n",
		r.Time.Format("2006-01-02 15:04:05.000"), r.Level, r.Msg)

	keys := make([]string, 0, len(r.Attrs))
	width := 0
	for k := range r.Attrs {
		keys = append(keys, k)
		if len(k) > width {
			width = len(k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "%-*s  %s\n", width, k, expand(r.Attrs[k]))
	}
	return b.String()
}

// compact renders a value on one line.
func compact(v any) string {
	switch t := v.(type) {
	case string:
		if strings.ContainsAny(t, " \t\n") {
			return fmt.Sprintf("%q", t)
		}
		return t
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%.3f", t)
	case nil:
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// expand renders a value across lines when it has structure, because the
// reason to open a record is usually that something in it is nested.
func expand(v any) string {
	switch t := v.(type) {
	case string:
		// A multi-line string -- a stack, usually -- is indented rather than
		// escaped, since escaping it is what made it unreadable in the row.
		if strings.Contains(t, "\n") {
			return "\n      " + strings.ReplaceAll(t, "\n", "\n      ")
		}
		return t
	case map[string]any, []any:
		b, err := json.MarshalIndent(v, "      ", "  ")
		if err == nil {
			return "\n      " + string(b)
		}
	}
	return compact(v)
}

// Table is a simple grid, which is what most of these views are.
//
// Rendering them through one type means a new view is a query rather than a
// new widget, and every view gets sorting and selection without restating it.
type Table struct {
	Title   string
	Columns []string
	Rows    [][]string
	// Note is shown under the table: a total, or why it is empty.
	Note string
	// Detail is optional expanded text per row, for enter.
	Detail []string
}

// Empty reports whether there is anything to show.
func (t Table) Empty() bool { return len(t.Rows) == 0 }

// Render draws the table at a width, truncating the widest columns first.
//
// Truncating the widest first keeps short identifying columns -- a server
// name, a count -- intact, which are the ones that make a row findable.
func (t Table) Render(width int, selected int, sel, dim func(string) string) string {
	if len(t.Columns) == 0 {
		return t.Note
	}
	widths := make([]int, len(t.Columns))
	for i, c := range t.Columns {
		widths[i] = len(c)
	}
	for _, row := range t.Rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}
	shrink(widths, width-2*(len(widths)-1))

	var b strings.Builder
	head := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		head[i] = pad(c, widths[i])
	}
	b.WriteString(dim(strings.Join(head, "  ")))
	b.WriteString("\n")

	for r, row := range t.Rows {
		cells := make([]string, len(widths))
		for i := range widths {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			cells[i] = pad(truncate(cell, widths[i]), widths[i])
		}
		line := strings.Join(cells, "  ")
		if r == selected {
			line = sel(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	if t.Note != "" {
		b.WriteString("\n")
		b.WriteString(dim(t.Note))
	}
	return b.String()
}

// shrink reduces the widest columns until the total fits.
func shrink(widths []int, budget int) {
	if budget <= 0 {
		return
	}
	for {
		total := 0
		widest, at := 0, -1
		for i, w := range widths {
			total += w
			if w > widest {
				widest, at = w, i
			}
		}
		if total <= budget || at < 0 || widths[at] <= 6 {
			return
		}
		widths[at]--
	}
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "\u2026"
}
