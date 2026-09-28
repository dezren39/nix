package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dezren39/mcpx/internal/logstore"
	"github.com/dezren39/mcpx/internal/opencode"
	"github.com/dezren39/mcpx/internal/tui"
)

// Stats renders one dimension of the log as a grid.
func (s tuiSource) Stats(_ context.Context, dimension string) (tui.Table, error) {
	st, err := s.app.openStore("")
	if err != nil {
		return tui.Table{}, err
	}
	defer st.Close()
	q := logstore.Query{Limit: -1}

	switch dimension {
	case "calls":
		rows, err := st.Calls(q)
		if err != nil {
			return tui.Table{}, err
		}
		t := tui.Table{
			Title:   "calls",
			Columns: []string{"SERVER", "TOOL", "CALLS", "ERRORS", "P50", "P95", "MAX", "TOTAL"},
			Note:    "ordered by total time spent, not by count: the question is what is costing you",
		}
		for _, r := range rows {
			t.Rows = append(t.Rows, []string{
				r.Server, r.Tool, itoa(r.Calls), itoa(r.Errors),
				msOf(r.P50), msOf(r.P95), msOf(r.Max), msOf(r.TotalMs),
			})
			t.Detail = append(t.Detail, fmt.Sprintf(
				"%s.%s\n\ncalls      %d\nerrors     %d (%.1f%%)\np50        %s\np95        %s\np99        %s\nmax        %s\ntotal      %s",
				r.Server, r.Tool, r.Calls, r.Errors, r.ErrRate*100,
				msOf(r.P50), msOf(r.P95), msOf(r.P99), msOf(r.Max), msOf(r.TotalMs)))
		}
		return withEmpty(t, "No tool calls recorded yet."), nil

	case "servers":
		rows, err := st.Servers(q)
		if err != nil {
			return tui.Table{}, err
		}
		t := tui.Table{
			Title:   "servers",
			Columns: []string{"SERVER", "STARTS", "STOPS", "UNCLEAN", "RUNNING", "UPTIME", "READY P50", "CALLS"},
		}
		for _, r := range rows {
			t.Rows = append(t.Rows, []string{
				r.Server, itoa(r.Starts), itoa(r.Stops), itoa(r.Unclean),
				itoa(r.Running), dur(r.UptimeSec), msOf(r.ReadyP50), itoa(r.Calls),
			})
			t.Detail = append(t.Detail, fmt.Sprintf(
				"%s\n\nstarts     %d\nstops      %d\nrestarts   %d\nunclean    %d\nrunning    %d\nuptime     %s\nready p50  %s\nready max  %s\ncalls      %d",
				r.Server, r.Starts, r.Stops, r.Restarts, r.Unclean, r.Running,
				dur(r.UptimeSec), msOf(r.ReadyP50), msOf(r.ReadyMax), r.Calls))
		}
		return withEmpty(t, "No server lifecycle recorded yet."), nil

	case "errors":
		rows, err := st.Errors(q)
		if err != nil {
			return tui.Table{}, err
		}
		t := tui.Table{
			Title:   "errors",
			Columns: []string{"COUNT", "LAST SEEN", "SERVERS", "MESSAGE"},
			Note:    "grouped by template, so one message with six servers in it is one row",
		}
		for _, r := range rows {
			t.Rows = append(t.Rows, []string{
				itoa(r.Count), r.LastSeen.Format("01-02 15:04"), r.Servers, r.Message,
			})
			t.Detail = append(t.Detail, fmt.Sprintf(
				"%s\n\ncount      %d\nlast seen  %s\nservers    %s",
				r.Message, r.Count, r.LastSeen.Format(time.RFC3339), orDash(r.Servers)))
		}
		return withEmpty(t, "No errors recorded. "), nil

	case "sessions":
		rows, err := st.Sessions(q)
		if err != nil {
			return tui.Table{}, err
		}
		t := tui.Table{
			Title:   "sessions",
			Columns: []string{"SESSION", "CALLS", "ERRORS", "SPAN", "BUSY", "SERVERS"},
		}
		for _, r := range rows {
			t.Rows = append(t.Rows, []string{
				r.Session, itoa(r.Calls), itoa(r.Errors),
				dur(int64(r.SpanSec)), msOf(r.BusyMs), r.Servers,
			})
			t.Detail = append(t.Detail, fmt.Sprintf(
				"%s\n\ncalls      %d\nerrors     %d\nrecords    %d\nfirst      %s\nlast       %s\nspan       %s\nbusy       %s\nservers    %s (%d distinct)",
				r.Session, r.Calls, r.Errors, r.Records,
				r.First.Format(time.RFC3339), r.Last.Format(time.RFC3339),
				dur(int64(r.SpanSec)), msOf(r.BusyMs), r.Servers, r.Distinct))
		}
		return withEmpty(t, "No sessions recorded yet."), nil

	case "slowest":
		rows, err := st.Slowest(q, 50)
		if err != nil {
			return tui.Table{}, err
		}
		t := tui.Table{
			Title:   "slowest",
			Columns: []string{"WHEN", "MS", "OK", "SERVER.TOOL", "TRACE"},
			Note:    "enter opens the record; the trace is what `mcpx log --chain` takes",
		}
		for _, r := range rows {
			t.Rows = append(t.Rows, []string{
				r.Time.Format("01-02 15:04:05"), msOf(r.Duration), yesno(r.OK),
				r.Server + "." + r.Tool, r.Trace,
			})
			t.Detail = append(t.Detail, fmt.Sprintf(
				"%s.%s\n\nwhen       %s\nduration   %s\nok         %v\nsession    %s\ntrace      %s\n\nmcpx log --chain %s",
				r.Server, r.Tool, r.Time.Format(time.RFC3339), msOf(r.Duration),
				r.OK, orDash(r.Session), r.Trace, r.Trace))
		}
		return withEmpty(t, "No calls recorded yet."), nil

	case "volume":
		buckets, _, err := st.Volume(q)
		if err != nil {
			return tui.Table{}, err
		}
		t := tui.Table{
			Title:   "volume",
			Columns: []string{"HOUR", "DEBUG", "INFO", "WARN", "ERROR", "TOTAL", ""},
		}
		peak := 0
		for _, b := range buckets {
			if b.Total > peak {
				peak = b.Total
			}
		}
		for _, b := range buckets {
			t.Rows = append(t.Rows, []string{
				b.Hour, itoa(b.Debug), itoa(b.Info), itoa(b.Warn), itoa(b.Error),
				itoa(b.Total), sparkbar(b.Total, peak, 20),
			})
			t.Detail = append(t.Detail, fmt.Sprintf(
				"%s\n\ndebug      %d\ninfo       %d\nwarn       %d\nerror      %d\ntotal      %d",
				b.Hour, b.Debug, b.Info, b.Warn, b.Error, b.Total))
		}
		return withEmpty(t, "Nothing logged yet."), nil
	}
	return tui.Table{}, fmt.Errorf("no statistic named %q", dimension)
}

// Instances lists server processes, which is the row somebody acts on: a
// server restarting six times is a number, but only an instance says which
// process died and when.
func (s tuiSource) Instances(_ context.Context) (tui.Table, error) {
	st, err := s.app.openStore("")
	if err != nil {
		return tui.Table{}, err
	}
	defer st.Close()
	rows, err := st.Instances(logstore.Query{Limit: -1})
	if err != nil {
		return tui.Table{}, err
	}
	t := tui.Table{
		Title:   "instances",
		Columns: []string{"SERVER", "INSTANCE", "PID", "STARTED", "UPTIME", "READY", "CALLS", "STATE"},
	}
	for _, r := range rows {
		state := "running"
		if !r.Stopped.IsZero() {
			state = orDash(r.Reason)
		}
		t.Rows = append(t.Rows, []string{
			r.Server, r.Instance, itoa64(r.PID), r.Started.Format("01-02 15:04"),
			dur(r.UptimeSec), msOf(r.ReadyMs), itoa(r.Calls), state,
		})
		stopped := "still running"
		if !r.Stopped.IsZero() {
			stopped = r.Stopped.Format(time.RFC3339)
		}
		t.Detail = append(t.Detail, fmt.Sprintf(
			"%s  %s\n\npid        %d\nstarted    %s\nstopped    %s\nreason     %s\nuptime     %s\nready in   %s\ncalls      %d",
			r.Server, r.Instance, r.PID, r.Started.Format(time.RFC3339),
			stopped, orDash(r.Reason), dur(r.UptimeSec), msOf(r.ReadyMs), r.Calls))
	}
	return withEmpty(t, "No server processes recorded yet."), nil
}

// Sessions merges what mcpx saw with what opencode recorded.
//
// They answer different halves of the same question -- mcpx knows which
// servers a session used, opencode knows what it cost -- and neither alone is
// the row somebody wants.
func (s tuiSource) Sessions(_ context.Context) (tui.Table, error) {
	st, err := s.app.openStore("")
	if err != nil {
		return tui.Table{}, err
	}
	defer st.Close()
	mine, err := st.Sessions(logstore.Query{Limit: -1})
	if err != nil {
		return tui.Table{}, err
	}

	type row struct {
		id      string
		calls   int
		errors  int
		servers string
		last    time.Time
		title   string
		cost    float64
		msgs    int64
		agent   string
	}
	byID := map[string]*row{}
	order := []string{}
	for _, r := range mine {
		byID[r.Session] = &row{
			id: r.Session, calls: r.Calls, errors: r.Errors,
			servers: r.Servers, last: r.Last,
		}
		order = append(order, r.Session)
	}

	// Opencode is optional. A machine without it still gets mcpx's half
	// rather than an error, because the view is useful either way.
	note := ""
	if path, ferr := opencode.Find(""); ferr == nil {
		if db, derr := opencode.Open(path); derr == nil {
			defer db.Close()
			if sessions, serr := db.Busiest(time.Time{}, time.Time{}, 200); serr == nil {
				for _, os := range sessions {
					r, ok := byID[os.ID]
					if !ok {
						r = &row{id: os.ID, last: os.Updated}
						byID[os.ID] = r
						order = append(order, os.ID)
					}
					r.title, r.cost, r.msgs, r.agent = os.Title, os.Cost, os.Messages, os.Agent
				}
			}
			note = "merged with " + filepath.Base(path)
		}
	} else {
		note = "no opencode database found; showing what mcpx saw"
	}

	sort.SliceStable(order, func(i, j int) bool {
		return byID[order[i]].last.After(byID[order[j]].last)
	})

	t := tui.Table{
		Title:   "sessions",
		Columns: []string{"SESSION", "CALLS", "ERR", "MSGS", "COST", "AGENT", "SERVERS", "TITLE"},
		Note:    note,
	}
	for _, id := range order {
		r := byID[id]
		t.Rows = append(t.Rows, []string{
			r.id, itoa(r.calls), itoa(r.errors), itoa64(r.msgs),
			money(r.cost), orDash(r.agent), r.servers, r.title,
		})
		t.Detail = append(t.Detail, fmt.Sprintf(
			"%s\n\ntitle      %s\nagent      %s\nmcpx calls %d (%d errors)\nservers    %s\nmessages   %d\ncost       %s\nlast seen  %s",
			r.id, orDash(r.title), orDash(r.agent), r.calls, r.errors,
			orDash(r.servers), r.msgs, money(r.cost), r.last.Format(time.RFC3339)))
	}
	return withEmpty(t, "No sessions recorded yet."), nil
}

// Storage is what all of this costs on disk.
//
// Worth a view of its own because it is the question that arrives suddenly --
// something is large and it is not obvious what -- and answering it otherwise
// means knowing where four different things live.
func (s tuiSource) Storage(_ context.Context) (tui.Table, error) {
	st, err := s.app.openStore("")
	if err != nil {
		return tui.Table{}, err
	}
	defer st.Close()
	_, files, err := st.Volume(logstore.Query{Limit: -1})
	if err != nil {
		return tui.Table{}, err
	}

	t := tui.Table{
		Title:   "storage",
		Columns: []string{"WHAT", "SIZE", "RECORDS", "PATH"},
	}
	var total int64
	add := func(what, path string, size, records int64, detail string) {
		total += size
		t.Rows = append(t.Rows, []string{what, humanBytes(size), itoa64(records), path})
		t.Detail = append(t.Detail, detail)
	}

	// The index first, because it is the one people are surprised by.
	if fi, serr := os.Stat(st.Path()); serr == nil {
		add("index", st.Path(), fi.Size(), 0, fmt.Sprintf(
			"%s\n\nThe SQLite index over the JSONL logs.\n\nsize       %s\nmodified   %s\n\nIt can be deleted at any time: the JSONL files are the\nsource of truth and the index is rebuilt on the next query.",
			st.Path(), humanBytes(fi.Size()), fi.ModTime().Format(time.RFC3339)))
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path > files[j].Path })
	for _, f := range files {
		skipped := ""
		if f.Skipped > 0 {
			skipped = fmt.Sprintf("\nskipped    %d unparseable lines", f.Skipped)
		}
		add("log", f.Path, f.Bytes, f.Records, fmt.Sprintf(
			"%s\n\nsize       %s\nrecords    %d%s",
			f.Path, humanBytes(f.Bytes), f.Records, skipped))
	}
	if path, ferr := opencode.Find(""); ferr == nil {
		if fi, serr := os.Stat(path); serr == nil {
			add("opencode", path, fi.Size(), 0, fmt.Sprintf(
				"%s\n\nopencode's own database. mcpx only ever reads it.\n\nsize       %s\nmodified   %s",
				path, humanBytes(fi.Size()), fi.ModTime().Format(time.RFC3339)))
		}
	}
	t.Note = "total " + humanBytes(total)
	return withEmpty(t, "Nothing on disk yet."), nil
}

func withEmpty(t tui.Table, note string) tui.Table {
	if t.Empty() && t.Note == "" {
		t.Note = note
	}
	return t
}

func itoa(n int) string     { return fmt.Sprintf("%d", n) }
func itoa64(n int64) string { return fmt.Sprintf("%d", n) }
func yesno(b bool) string {
	if b {
		return "yes"
	}
	return "NO"
}
func money(v float64) string {
	if v == 0 {
		return "-"
	}
	return fmt.Sprintf("$%.2f", v)
}

// msOf renders a duration in milliseconds at a readable scale.
func msOf(v float64) string {
	switch {
	case v == 0:
		return "-"
	case v >= 10000:
		return fmt.Sprintf("%.1fs", v/1000)
	case v >= 100:
		return fmt.Sprintf("%.0fms", v)
	}
	return fmt.Sprintf("%.1fms", v)
}

func dur(sec int64) string {
	if sec <= 0 {
		return "-"
	}
	d := time.Duration(sec) * time.Second
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
	case d >= time.Hour:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

func sparkbar(v, max, width int) string {
	if max <= 0 || v <= 0 {
		return ""
	}
	n := v * width / max
	if n == 0 {
		n = 1
	}
	return strings.Repeat("\u2588", n)
}
