package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/dezren39/mcpx/internal/defaults"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/dezren39/mcpx/internal/logging"
	"github.com/dezren39/mcpx/internal/logstore"
)

// logDir resolves where the JSONL logs live, in the same order the daemon
// resolves it, so the CLI reads exactly what the daemon wrote.
func (a *App) logDir(override string) string {
	paths, cfg := a.resolvePathsForConfig()
	cfgDir := ""
	if cfg != nil {
		cfgDir = cfg.Logging.Dir
	}
	return firstSet(override, a.Settings().String("logging.dir"), cfgDir,
		filepath.Join(paths.State, "logs"))
}

// openStore opens the index and brings it up to date.
//
// Ingest runs here, on the way into every query, rather than in a background
// thread in the daemon. A lazy index is either correct or visibly slow; a
// background one can be quietly stale, and a log tool nobody trusts is worse
// than no log tool.
func (a *App) openStore(override string) (*logstore.Store, error) {
	dir := a.logDir(override)
	if err := os.MkdirAll(dir, defaults.DirMode); err != nil {
		return nil, err
	}
	st, err := logstore.Open(dir)
	if err != nil {
		return nil, err
	}
	// plumbing.indexOnQuery is the switch this paragraph describes. Off, the
	// index answers from whatever it already holds, which is what somebody
	// querying a very large log repeatedly wants.
	if a.Settings().Bool("plumbing.indexOnQuery") {
		if _, err := st.Ingest(); err != nil {
			st.Close()
			return nil, err
		}
	}
	return st, nil
}

// CmdLog queries the durable log.
func (a *App) CmdLog(ctx context.Context, args []string) error {
	if len(args) > 0 && args[0] == "sql" {
		return a.cmdLogSQL(ctx, args[1:])
	}
	if len(args) > 0 && args[0] == "record" {
		return a.CmdLogRecord(ctx, args[1:])
	}

	fs := newFlagSet("log")
	since := fs.String("since", "", "start of the window: a duration (15m, 2h) or an RFC3339 time")
	until := fs.String("until", "", "end of the window: a duration before now, or an RFC3339 time")
	level := fs.String("level", "", "minimum level: debug, info, warn, error")
	event := fs.String("event", "", "event glob: server.*, mcp.call")
	server := fs.String("server", "", "only records from this server")
	tool := fs.String("tool", "", "only records for this tool")
	session := fs.String("session", "", "only records from this session")
	trace := fs.String("trace", "", "only records carrying this trace id")
	chain := fs.String("chain", "", "this trace and every ancestor, oldest first, as a tree")
	grep := fs.String("grep", "", "regular expression over the message and the attributes")
	limit := fs.Int("limit", 100, "maximum records")
	reverse := fs.Bool("reverse", false, "newest first")
	follow := fs.Bool("follow", false, "keep printing as new records arrive")
	followShort := fs.Bool("f", false, "alias for --follow")
	format := fs.String("format", "", "text, json, json-pretty, logfmt, compact, bare")
	fields := fs.String("fields", "", "print only these columns, comma separated")
	logDir := fs.String("log-dir", "", "log directory (default: the daemon's)")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}

	q := logstore.Query{
		Level: *level, Event: *event, Server: *server, Tool: *tool,
		Session: *session, Trace: *trace, Grep: *grep,
		Limit: *limit, Reverse: *reverse,
	}
	now := time.Now()
	var err error
	if q.Since, err = logstore.ParseWhen(*since, now); err != nil {
		return err
	}
	if q.Until, err = logstore.ParseWhen(*until, now); err != nil {
		return err
	}
	f, err := logging.ParseFormat(*format)
	if err != nil {
		return err
	}
	if a.JSON && *format == "" {
		f = logging.FormatJSON
	}

	st, err := a.openStore(*logDir)
	if err != nil {
		return err
	}
	defer st.Close()

	p := newRecordPrinter(f, splitFields(*fields))

	// fs.Visit reports only the flags actually given, which is the sole way
	// to tell `--chain ""` from no --chain at all. Left as a plain empty
	// check, an empty substitution silently printed the entire log, which
	// reads as "that trace touched everything" rather than as the mistake it
	// is.
	chainGiven := false
	fs.Visit(func(fl *flag.Flag) {
		if fl.Name == "chain" {
			chainGiven = true
		}
	})
	if chainGiven && strings.TrimSpace(*chain) == "" {
		return errors.New("--chain needs a trace id; it was given an empty value, " +
			"which usually means a shell substitution came back empty")
	}
	if *chain != "" {
		// The same refusal the daemon makes. printChain takes an id and a
		// limit; every other flag was folded into q above and then dropped,
		// so `--chain X --level error` printed the whole tree and looked
		// like it had filtered it.
		var named []string
		fs.Visit(func(fl *flag.Flag) {
			switch fl.Name {
			case "level", "event", "server", "tool", "session", "trace", "grep",
				"since", "until", "reverse":
				named = append(named, "--"+fl.Name)
			}
		})
		if len(named) > 0 {
			sort.Strings(named)
			return fmt.Errorf("--chain returns a whole trace tree and cannot be "+
				"filtered; drop %s, or drop --chain", strings.Join(named, ", "))
		}
		return a.printChain(st, *chain, *limit, p)
	}

	recs, err := st.Records(q)
	if err != nil {
		return err
	}
	for _, r := range recs {
		fmt.Println(p.line(r))
	}
	if !*follow && !*followShort {
		return nil
	}
	return a.followLog(ctx, st, q, recs, p)
}

// followLog polls the JSONL files and prints what ingest adds.
//
// The poll is on the files, not on the database: SQLite has no way to block
// until a row appears, so tailing the index would mean busy-querying it, and
// the files are the thing that actually changes. Ingest's unchanged-file fast
// path makes each tick a stat call per file when nothing has happened.
func (a *App) followLog(ctx context.Context, st *logstore.Store, q logstore.Query,
	seen []logstore.Record, p *recordPrinter) error {
	if len(seen) > 0 {
		for _, r := range seen {
			if r.ID > q.AfterID {
				q.AfterID = r.ID
			}
		}
	} else {
		// With nothing printed there is no floor, and a first tick would
		// replay the whole window that was just shown as empty.
		var max int64
		if err := st.DB().QueryRow(`SELECT ifnull(max(id),0) FROM records`).Scan(&max); err == nil {
			q.AfterID = max
		}
	}
	q.Reverse = false
	q.Since = time.Time{}
	q.Until = time.Time{}
	q.Limit = a.Settings().Int("logstore.followBacklog")

	tick := time.NewTicker(defaults.FollowPollInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
		// Follow always ingests: the whole point of it is to see what has
		// just been written, and indexOnQuery is about the cost of a query
		// rather than about a tail that has nothing to show.
		if _, err := st.Ingest(); err != nil {
			return err
		}
		recs, err := st.Records(q)
		if err != nil {
			return err
		}
		for _, r := range recs {
			fmt.Println(p.line(r))
			if r.ID > q.AfterID {
				q.AfterID = r.ID
			}
		}
	}
}

func (a *App) printChain(st *logstore.Store, id string, limit int, p *recordPrinter) error {
	levels, err := st.Chain(id, limit)
	if err != nil {
		return err
	}
	if len(levels) == 0 {
		return fmt.Errorf("no records for trace %q", id)
	}
	if a.JSON {
		type out struct {
			Trace   string   `json:"trace"`
			Parent  string   `json:"parent,omitempty"`
			Depth   int      `json:"depth"`
			Records []string `json:"records"`
		}
		var doc []out
		for _, l := range levels {
			o := out{Trace: l.Trace, Parent: l.Parent, Depth: l.Depth}
			for _, r := range l.Records {
				o.Records = append(o.Records, p.line(r))
			}
			doc = append(doc, o)
		}
		return a.out(doc)
	}
	for _, l := range levels {
		indent := strings.Repeat("  ", l.Depth)
		fmt.Printf("%s%s\n", indent, l.Trace)
		for _, r := range l.Records {
			fmt.Printf("%s  %s\n", indent, p.line(r))
		}
	}
	return nil
}

// recordPrinter renders one record, either through the logging package's own
// formatters or as a projection of named columns.
type recordPrinter struct {
	w      *logging.Writer
	buf    *bytes.Buffer
	fields []string
}

func newRecordPrinter(f logging.Format, fields []string) *recordPrinter {
	buf := &bytes.Buffer{}
	return &recordPrinter{
		w:      logging.NewWriter(buf, f, slog.LevelDebug),
		buf:    buf,
		fields: fields,
	}
}

func (p *recordPrinter) line(r logstore.Record) string {
	if len(p.fields) > 0 {
		cols := make([]string, 0, len(p.fields))
		for _, f := range p.fields {
			cols = append(cols, fieldValue(r, f))
		}
		return strings.Join(cols, "\t")
	}
	p.buf.Reset()
	p.w.Write(r.Logging())
	return strings.TrimRight(p.buf.String(), "\n")
}

func fieldValue(r logstore.Record, name string) string {
	switch name {
	case "ts", "time":
		return r.Time.Format(time.RFC3339Nano)
	case "level":
		return logging.LevelName(r.Level)
	case "msg", "message":
		return r.Msg
	case "template":
		return r.Template
	}
	v, ok := r.Attrs[name]
	if !ok {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case float64:
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%g", x)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func splitFields(s string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' }) {
		out = append(out, f)
	}
	return out
}

// cmdLogSQL is raw access to the index.
func (a *App) cmdLogSQL(_ context.Context, args []string) error {
	fs := newFlagSet("log sql")
	schema := fs.Bool("schema", false, "print the schema and exit")
	path := fs.Bool("path", false, "print the database path and exit")
	logDir := fs.String("log-dir", "", "log directory (default: the daemon's)")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if *schema {
		fmt.Println(strings.TrimSpace(logstore.Schema()))
		return nil
	}
	if *path {
		fmt.Println(filepath.Join(a.logDir(*logDir), "index.db"))
		return nil
	}
	query := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if query == "" {
		return fmt.Errorf("usage: mcpx log sql '<select ...>' (--schema for the DDL, --path for the file)")
	}
	st, err := a.openStore(*logDir)
	if err != nil {
		return err
	}
	defer st.Close()
	table, err := st.RunSQL(query)
	if err != nil {
		return err
	}
	if a.JSON {
		return a.out(table)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(table.Columns, "\t"))
	for _, row := range table.Rows {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	return tw.Flush()
}

// CmdLogRecord appends one record to the durable log.
//
// The point is that mcpx's log should be able to hold what other things know.
// The opencode plugin records tool timings through it, so one `mcpx stats`
// covers the harness as well as mcpx; anything else that can run a command
// can do the same.
//
// Deliberately forgiving about shape. A caller that can produce JSON should
// not also have to learn a schema, so anything not recognised becomes an
// attribute, and a record with only a message is valid.
func (a *App) CmdLogRecord(_ context.Context, args []string) error {
	fs := newFlagSet("log record")
	level := fs.String("level", "info", "debug, info, warn or error")
	logDir := fs.String("log-dir", "", "log directory (default: the daemon's)")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	payload := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if payload == "" {
		// Reading stdin means a caller can pipe rather than quote, which
		// matters as soon as the JSON contains anything a shell would eat.
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		payload = strings.TrimSpace(string(b))
	}
	if payload == "" {
		return errors.New("usage: mcpx log record '<json>'  (or pipe it on stdin)")
	}

	rec, err := logging.ExternalRecord([]byte(payload), *level)
	if err != nil {
		return err
	}
	// The harness ids come from this process's environment, which the plugin
	// populates. Over POST /v1/log they are in the body instead, because the
	// daemon's environment belongs to whoever started it.
	for k, v := range logging.HarnessIDs() {
		if _, taken := rec.Attrs[k]; !taken {
			rec.Attrs[k] = v
		}
	}

	dir := firstNonEmpty(*logDir, a.Settings().String("logging.dir"),
		filepath.Join(a.Paths.State, "logs"))
	sink, err := logging.NewFileSink(logging.FileOptions{Dir: dir})
	if err != nil {
		return err
	}
	defer sink.Close()
	sink.Write(rec, nil)
	return nil
}

// CmdStats aggregates the log.
func (a *App) CmdStats(_ context.Context, args []string) error {
	// The dimension is hoisted before parsing because Go's flag package stops
	// at the first non-flag argument, so `stats slowest --top 3` would
	// otherwise silently ignore the flag.
	dim := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		dim, args = args[0], args[1:]
	}
	fs := newFlagSet("stats")
	by := fs.String("by", "", "dimension: "+strings.Join(logstore.Dimensions, ", "))
	since := fs.String("since", "", "start of the window: a duration (15m, 2h) or an RFC3339 time")
	until := fs.String("until", "", "end of the window")
	server := fs.String("server", "", "restrict to this server")
	tool := fs.String("tool", "", "restrict to this tool")
	session := fs.String("session", "", "restrict to this session")
	top := fs.Int("top", 20, "rows to show where the dimension is a ranking")
	logDir := fs.String("log-dir", "", "log directory (default: the daemon's)")
	fromOpencode := fs.Bool("opencode", false, "report on the opencode database instead of mcpx's log")
	dbPath := fs.String("db", "", "database to read with --opencode (default: opencode's own)")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	// `stats opencode ...` and `stats --opencode ...` are the same request.
	// Both spellings exist because the first reads better as a subject and
	// the second composes with the other dimensions.
	if dim == "opencode" {
		*fromOpencode = true
		dim = ""
		if len(fs.Args()) > 0 {
			dim = fs.Arg(0)
		}
	}
	if *fromOpencode {
		return a.statsOpencode(dim, *dbPath, *since, *until, *top)
	}
	dim = firstSet(dim, *by, "calls")
	q := logstore.Query{Server: *server, Tool: *tool, Session: *session, Limit: -1}
	now := time.Now()
	var err error
	if q.Since, err = logstore.ParseWhen(*since, now); err != nil {
		return err
	}
	if q.Until, err = logstore.ParseWhen(*until, now); err != nil {
		return err
	}

	st, err := a.openStore(*logDir)
	if err != nil {
		return err
	}
	defer st.Close()

	switch dim {
	case "calls":
		return a.statsCalls(st, q)
	case "servers":
		return a.statsServers(st, q)
	case "instances":
		return a.statsInstances(st, q)
	case "errors":
		return a.statsErrors(st, q, *top)
	case "sessions":
		return a.statsSessions(st, q, *top)
	case "volume":
		return a.statsVolume(st, q)
	case "slowest":
		return a.statsSlowest(st, q, *top)
	}
	return fmt.Errorf("unknown dimension %q; want one of %s", dim,
		strings.Join(logstore.Dimensions, ", "))
}

func table(header string) *tabwriter.Writer {
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, header)
	return tw
}

func ms(v float64) string { return fmt.Sprintf("%.1f", v) }

func (a *App) statsCalls(st *logstore.Store, q logstore.Query) error {
	rows, err := st.Calls(q)
	if err != nil {
		return err
	}
	if a.JSON {
		return a.out(rows)
	}
	if len(rows) == 0 {
		return a.noData("tool calls")
	}
	tw := table("SERVER\tTOOL\tCALLS\tERRORS\tERR%\tP50\tP95\tP99\tMAX\tTOTAL")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%.1f\t%s\t%s\t%s\t%s\t%s\n",
			r.Server, r.Tool, r.Calls, r.Errors, r.ErrRate*100,
			ms(r.P50), ms(r.P95), ms(r.P99), ms(r.Max), ms(r.TotalMs))
	}
	return tw.Flush()
}

func (a *App) statsServers(st *logstore.Store, q logstore.Query) error {
	rows, err := st.Servers(q)
	if err != nil {
		return err
	}
	if a.JSON {
		return a.out(rows)
	}
	if len(rows) == 0 {
		return a.noData("server lifecycle events")
	}
	tw := table("SERVER\tSTARTS\tSTOPS\tRESTARTS\tUNCLEAN\tRUNNING\tUPTIME\tREADY P50\tREADY MAX\tCALLS")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%d\t%s\t%s\t%s\t%d\n",
			r.Server, r.Starts, r.Stops, r.Restarts, r.Unclean, r.Running,
			(time.Duration(r.UptimeSec) * time.Second).String(),
			ms(r.ReadyP50), ms(r.ReadyMax), r.Calls)
	}
	return tw.Flush()
}

func (a *App) statsInstances(st *logstore.Store, q logstore.Query) error {
	rows, err := st.Instances(q)
	if err != nil {
		return err
	}
	if a.JSON {
		return a.out(rows)
	}
	if len(rows) == 0 {
		return a.noData("server instances")
	}
	tw := table("INSTANCE\tPID\tSTARTED\tREADY\tUPTIME\tCALLS\tSTOPPED\tREASON")
	for _, r := range rows {
		stopped := "-"
		if !r.Stopped.IsZero() {
			stopped = r.Stopped.Local().Format("15:04:05")
		}
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%d\t%s\t%s\n",
			r.Instance, r.PID, r.Started.Local().Format("2006-01-02 15:04:05"), ms(r.ReadyMs),
			(time.Duration(r.UptimeSec) * time.Second).String(), r.Calls, stopped,
			firstSet(r.Reason, "-"))
	}
	return tw.Flush()
}

func (a *App) statsErrors(st *logstore.Store, q logstore.Query, top int) error {
	rows, err := st.Errors(q)
	if err != nil {
		return err
	}
	if len(rows) > top {
		rows = rows[:top]
	}
	if a.JSON {
		return a.out(rows)
	}
	if len(rows) == 0 {
		return a.noData("errors")
	}
	tw := table("COUNT\tLAST SEEN\tSERVERS\tMESSAGE")
	for _, r := range rows {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", r.Count,
			r.LastSeen.Local().Format("2006-01-02 15:04:05"), firstSet(r.Servers, "-"),
			truncate(oneLine(r.Message), 80))
	}
	return tw.Flush()
}

func (a *App) statsSessions(st *logstore.Store, q logstore.Query, top int) error {
	rows, err := st.Sessions(q)
	if err != nil {
		return err
	}
	if len(rows) > top {
		rows = rows[:top]
	}
	if a.JSON {
		return a.out(rows)
	}
	if len(rows) == 0 {
		return a.noData("sessions")
	}
	tw := table("SESSION\tRECORDS\tCALLS\tERRORS\tSPAN\tBUSY\tSERVERS")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%s\t%s\t%s\n", r.Session, r.Records,
			r.Calls, r.Errors,
			(time.Duration(r.SpanSec) * time.Second).Round(time.Second).String(),
			ms(r.BusyMs), firstSet(r.Servers, "-"))
	}
	return tw.Flush()
}

func (a *App) statsVolume(st *logstore.Store, q logstore.Query) error {
	buckets, files, err := st.Volume(q)
	if err != nil {
		return err
	}
	if a.JSON {
		return a.out(struct {
			Hours []logstore.VolumeBucket `json:"hours"`
			Files []logstore.FileStat     `json:"files"`
		}{buckets, files})
	}
	if len(buckets) == 0 && len(files) == 0 {
		return a.noData("records")
	}
	tw := table("HOUR\tDEBUG\tINFO\tWARN\tERROR\tTOTAL")
	for _, b := range buckets {
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%d\n", b.Hour, b.Debug, b.Info,
			b.Warn, b.Error, b.Total)
	}
	tw.Flush()
	if len(files) == 0 {
		return nil
	}
	fmt.Println()
	ft := table("FILE\tBYTES\tRECORDS\tSKIPPED")
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	for _, f := range files {
		fmt.Fprintf(ft, "%s\t%d\t%d\t%d\n", filepath.Base(f.Path), f.Bytes,
			f.Records, f.Skipped)
	}
	return ft.Flush()
}

func (a *App) statsSlowest(st *logstore.Store, q logstore.Query, top int) error {
	rows, err := st.Slowest(q, top)
	if err != nil {
		return err
	}
	if a.JSON {
		return a.out(rows)
	}
	if len(rows) == 0 {
		return a.noData("tool calls")
	}
	tw := table("MS\tWHEN\tSERVER\tTOOL\tOK\tSESSION\tTRACE")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%t\t%s\t%s\n", ms(r.Duration),
			r.Time.Local().Format("2006-01-02 15:04:05"), r.Server, r.Tool, r.OK,
			firstSet(r.Session, "-"), r.Trace)
	}
	tw.Flush()
	fmt.Println("\nNext: `mcpx log --chain <trace>` for the call and everything that led to it.")
	return nil
}

// noData says which records were missing rather than printing an empty table,
// because an empty table reads as "nothing happened" when the usual cause is
// that the daemon has not run since the log directory was created.
func (a *App) noData(what string) error {
	fmt.Printf("No %s in the log yet.\n", what)
	return nil
}
