package logstore_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/logstore"
)

// line builds one JSONL record. Tests spell out the wire form rather than
// going through the writer, so a change in either one shows up as a failure
// here instead of cancelling out.
func line(ts time.Time, level, msg string, kv map[string]any) string {
	obj := map[string]any{
		"ts":    ts.Format(time.RFC3339Nano),
		"level": level,
		"msg":   msg,
	}
	for k, v := range kv {
		obj[k] = v
	}
	b, err := json.Marshal(obj)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func writeLog(t *testing.T, dir, name string, lines ...string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	body := strings.Join(lines, "\n")
	if body != "" {
		body += "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func appendLog(t *testing.T, path string, lines ...string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, l := range lines {
		if _, err := f.WriteString(l + "\n"); err != nil {
			t.Fatal(err)
		}
	}
}

func open(t *testing.T, dir string) *logstore.Store {
	t.Helper()
	st, err := logstore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func ingest(t *testing.T, st *logstore.Store) logstore.IngestResult {
	t.Helper()
	res, err := st.Ingest()
	if err != nil {
		t.Fatal(err)
	}
	return res
}

var base = time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)

// chainFixture is a daemon that started a server that served a call, which is
// the shape every chain question is really about.
func chainFixture(t *testing.T, dir string) string {
	t.Helper()
	return writeLog(t, dir, "mcpx-2026-09-27.jsonl",
		line(base, "info", "daemon starting", map[string]any{
			"event": "daemon.start", "trace": "dmn-1", "servers": 2}),
		line(base.Add(time.Second), "debug", "server.start", map[string]any{
			"event": "server.start", "trace": "srv-1", "trace.parent": "dmn-1",
			"server": "fff", "instance": "fff#1", "pid": 4242, "readyMs": 120.5,
			"reason": ""}),
		line(base.Add(2*time.Second), "debug", "mcp.call", map[string]any{
			"event": "mcp.call", "trace": "cal-1", "trace.parent": "srv-1",
			"server": "fff", "tool": "search", "session": "s1",
			"durationMs": 30.0, "ok": true}),
	)
}

func TestIngestIndexesEveryRecordInAJSONLFile(t *testing.T) {
	dir := t.TempDir()
	chainFixture(t, dir)
	st := open(t, dir)
	res := ingest(t, st)
	if res.Inserted != 3 {
		t.Fatalf("inserted %d records, want 3", res.Inserted)
	}
	recs, err := st.Records(logstore.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 {
		t.Fatalf("queried %d records, want 3", len(recs))
	}
	if recs[0].Msg != "daemon starting" {
		t.Errorf("records come back newest first; want oldest first, got %q", recs[0].Msg)
	}
}

func TestReIngestingUnchangedFilesInsertsNothing(t *testing.T) {
	dir := t.TempDir()
	chainFixture(t, dir)
	st := open(t, dir)
	ingest(t, st)

	second := ingest(t, st)
	if second.Inserted != 0 {
		t.Errorf("second pass inserted %d records, want 0", second.Inserted)
	}
	if second.Scanned != 0 {
		t.Errorf("second pass read %d lines; an unchanged file should not be opened", second.Scanned)
	}
	var n int
	if err := st.DB().QueryRow(`SELECT count(*) FROM records`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("table holds %d records after two passes, want 3", n)
	}
}

func TestACorruptLineIsSkippedAndCountedRatherThanFatal(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, dir, "mcpx-2026-09-27.jsonl",
		line(base, "info", "before", nil),
		`{"ts":"2026-09-27T10:00:01Z","level":"info","msg":`,
		`not json at all`,
		`{"level":"info","msg":"no timestamp"}`,
		line(base.Add(2*time.Second), "info", "after", nil),
	)
	st := open(t, dir)
	res := ingest(t, st)
	if res.Inserted != 2 {
		t.Fatalf("inserted %d records, want the 2 valid ones", res.Inserted)
	}
	if res.Skipped != 3 {
		t.Errorf("skipped %d lines, want 3", res.Skipped)
	}
	recs, err := st.Records(logstore.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 || recs[0].Msg != "before" || recs[1].Msg != "after" {
		t.Errorf("the good records did not survive the bad ones: %+v", recs)
	}
}

func TestAppendedRecordsAreIngestedWithoutRereadingTheWholeFile(t *testing.T) {
	dir := t.TempDir()
	path := chainFixture(t, dir)
	st := open(t, dir)
	ingest(t, st)

	appendLog(t, path, line(base.Add(time.Minute), "warn", "later", nil))
	res := ingest(t, st)
	if res.Inserted != 1 {
		t.Fatalf("inserted %d records, want 1", res.Inserted)
	}
	if res.Scanned != 1 {
		t.Errorf("read %d lines; only the appended one should have been read", res.Scanned)
	}
}

func TestAPartialTrailingLineIsPickedUpOnceItIsComplete(t *testing.T) {
	dir := t.TempDir()
	path := writeLog(t, dir, "mcpx-2026-09-27.jsonl", line(base, "info", "first", nil))
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	half := line(base.Add(time.Second), "info", "second", nil)
	if _, err := f.WriteString(half[:20]); err != nil {
		t.Fatal(err)
	}
	f.Close()

	st := open(t, dir)
	if res := ingest(t, st); res.Inserted != 1 {
		t.Fatalf("inserted %d records while a line was half written, want 1", res.Inserted)
	}
	appendLog(t, path, half[20:])
	if res := ingest(t, st); res.Inserted != 1 {
		t.Fatalf("inserted %d records once the line completed, want 1", res.Inserted)
	}
	var n int
	if err := st.DB().QueryRow(`SELECT count(*) FROM records`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("table holds %d records, want 2", n)
	}
}

func TestRotatingAFileAsideDoesNotDuplicateItsRecords(t *testing.T) {
	dir := t.TempDir()
	live := chainFixture(t, dir)
	st := open(t, dir)
	ingest(t, st)

	rotated := filepath.Join(dir, "mcpx-2026-09-27-100500.jsonl")
	if err := os.Rename(live, rotated); err != nil {
		t.Fatal(err)
	}
	writeLog(t, dir, "mcpx-2026-09-27.jsonl", line(base.Add(time.Hour), "info", "fresh", nil))

	res := ingest(t, st)
	if res.Renamed != 1 {
		t.Errorf("rename went unnoticed: %+v", res)
	}
	var n int
	if err := st.DB().QueryRow(`SELECT count(*) FROM records`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("table holds %d records after rotation, want the 3 old plus 1 new", n)
	}
	var file string
	if err := st.DB().QueryRow(
		`SELECT file FROM records WHERE trace = 'dmn-1'`).Scan(&file); err != nil {
		t.Fatal(err)
	}
	if file != rotated {
		t.Errorf("old records still point at %q, want %q", file, rotated)
	}
}

func TestChainWalksFromACallBackToTheDaemonThatStartedIt(t *testing.T) {
	dir := t.TempDir()
	chainFixture(t, dir)
	st := open(t, dir)
	ingest(t, st)

	levels, err := st.Chain("cal-1", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(levels) != 3 {
		t.Fatalf("chain has %d levels, want daemon, server, call", len(levels))
	}
	want := []string{"dmn-1", "srv-1", "cal-1"}
	for i, l := range levels {
		if l.Trace != want[i] {
			t.Errorf("level %d is %q, want %q (oldest first)", i, l.Trace, want[i])
		}
		if l.Depth != i {
			t.Errorf("level %d reports depth %d", i, l.Depth)
		}
		if len(l.Records) == 0 {
			t.Errorf("level %q has no records", l.Trace)
		}
	}
	if levels[2].Parent != "srv-1" {
		t.Errorf("the call names %q as its parent, want srv-1", levels[2].Parent)
	}
}

func TestChainOfATraceWithNoParentIsJustThatTrace(t *testing.T) {
	dir := t.TempDir()
	chainFixture(t, dir)
	st := open(t, dir)
	ingest(t, st)

	levels, err := st.Chain("dmn-1", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(levels) != 1 || levels[0].Trace != "dmn-1" {
		t.Fatalf("chain of a root is %+v, want one level", levels)
	}
}

func TestChainStopsRatherThanLoopingOnACycle(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, dir, "mcpx-2026-09-27.jsonl",
		line(base, "info", "a", map[string]any{"trace": "a", "trace.parent": "b"}),
		line(base.Add(time.Second), "info", "b", map[string]any{"trace": "b", "trace.parent": "a"}),
	)
	st := open(t, dir)
	ingest(t, st)
	levels, err := st.Chain("a", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(levels) != 2 {
		t.Fatalf("a cycle produced %d levels, want the 2 distinct traces", len(levels))
	}
}

func TestLevelFilterMatchesAtOrAboveTheThreshold(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, dir, "mcpx-2026-09-27.jsonl",
		line(base, "debug", "d", nil),
		line(base.Add(time.Second), "info", "i", nil),
		line(base.Add(2*time.Second), "warn", "w", nil),
		line(base.Add(3*time.Second), "error", "e", nil),
	)
	st := open(t, dir)
	ingest(t, st)
	recs, err := st.Records(logstore.Query{Level: "warn"})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("level=warn returned %d records, want warn and error", len(recs))
	}
}

func TestEventGlobSelectsAFamilyOfEvents(t *testing.T) {
	dir := t.TempDir()
	chainFixture(t, dir)
	st := open(t, dir)
	ingest(t, st)
	recs, err := st.Records(logstore.Query{Event: "server.*"})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("server.* matched %d records, want 1", len(recs))
	}
	exact, err := st.Records(logstore.Query{Event: "mcp.call"})
	if err != nil {
		t.Fatal(err)
	}
	if len(exact) != 1 {
		t.Fatalf("mcp.call matched %d records, want 1", len(exact))
	}
}

func TestGrepMatchesTheMessageAndTheAttributes(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, dir, "mcpx-2026-09-27.jsonl",
		line(base, "info", "nothing interesting", nil),
		line(base.Add(time.Second), "info", "connection refused", nil),
		line(base.Add(2*time.Second), "info", "plain", map[string]any{"detail": "connection reset"}),
	)
	st := open(t, dir)
	ingest(t, st)
	recs, err := st.Records(logstore.Query{Grep: `connection (refused|reset)`})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("grep matched %d records, want the message and the attribute", len(recs))
	}
}

func TestGrepAlsoMatchesTheEventName(t *testing.T) {
	// event is promoted out of attrs into its own column, so a grep over
	// msg and attrs alone could never match it: `--grep server.start` found
	// nothing while `--event server.start` found everything.
	dir := t.TempDir()
	writeLog(t, dir, "mcpx-2026-09-27.jsonl",
		line(base, "info", "a server started", map[string]any{"event": "server.start"}),
		line(base.Add(time.Second), "info", "unrelated", nil),
	)
	st := open(t, dir)
	ingest(t, st)
	recs, err := st.Records(logstore.Query{Grep: `server\.start`})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("grep on an event name matched %d records, want 1", len(recs))
	}
}

func TestTheTimeWindowExcludesRecordsOutsideIt(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, dir, "mcpx-2026-09-27.jsonl",
		line(base, "info", "old", nil),
		line(base.Add(time.Hour), "info", "new", nil),
	)
	st := open(t, dir)
	ingest(t, st)
	recs, err := st.Records(logstore.Query{Since: base.Add(30 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].Msg != "new" {
		t.Fatalf("since returned %+v, want only the newer record", recs)
	}
}

func TestCallStatsReportExactPercentiles(t *testing.T) {
	dir := t.TempDir()
	var lines []string
	// 1..100ms, so the exact nearest-rank percentiles are known by hand.
	for i := 1; i <= 100; i++ {
		lines = append(lines, line(base.Add(time.Duration(i)*time.Second), "debug", "mcp.call",
			map[string]any{"event": "mcp.call", "server": "fff", "tool": "search",
				"durationMs": float64(i), "ok": i%10 != 0, "trace": "cal"}))
	}
	writeLog(t, dir, "mcpx-2026-09-27.jsonl", lines...)
	st := open(t, dir)
	ingest(t, st)

	stats, err := st.Calls(logstore.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 {
		t.Fatalf("grouped into %d rows, want one server+tool pair", len(stats))
	}
	s := stats[0]
	if s.Calls != 100 {
		t.Errorf("counted %d calls, want 100", s.Calls)
	}
	if s.Errors != 10 {
		t.Errorf("counted %d errors, want 10", s.Errors)
	}
	if s.P50 != 50 || s.P95 != 95 || s.P99 != 99 || s.Max != 100 {
		t.Errorf("percentiles are p50=%v p95=%v p99=%v max=%v, want 50/95/99/100",
			s.P50, s.P95, s.P99, s.Max)
	}
}

func TestServerStatsCountAnUncleanStopSeparatelyFromAnOrderlyOne(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, dir, "mcpx-2026-09-27.jsonl",
		line(base, "debug", "server.start", map[string]any{
			"event": "server.start", "server": "fff", "instance": "fff#1",
			"trace": "srv-1", "readyMs": 100.0}),
		line(base.Add(time.Minute), "debug", "server.stop", map[string]any{
			"event": "server.stop", "server": "fff", "instance": "fff#1",
			"reason": "idle", "uptimeSec": 60, "calls": 3}),
		line(base.Add(2*time.Minute), "debug", "server.start", map[string]any{
			"event": "server.start", "server": "fff", "instance": "fff#2",
			"trace": "srv-2", "readyMs": 300.0}),
		line(base.Add(3*time.Minute), "debug", "server.stop", map[string]any{
			"event": "server.stop", "server": "fff", "instance": "fff#2",
			"reason": "exited", "uptimeSec": 60, "calls": 1}),
	)
	st := open(t, dir)
	ingest(t, st)
	stats, err := st.Servers(logstore.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 {
		t.Fatalf("grouped into %d rows, want one server", len(stats))
	}
	s := stats[0]
	if s.Starts != 2 || s.Stops != 2 {
		t.Errorf("counted %d starts and %d stops, want 2 and 2", s.Starts, s.Stops)
	}
	if s.Unclean != 1 {
		t.Errorf("counted %d unclean stops, want only the one that exited", s.Unclean)
	}
	if s.UptimeSec != 120 || s.Calls != 4 {
		t.Errorf("uptime %ds over %d calls, want 120s and 4", s.UptimeSec, s.Calls)
	}
	if s.ReadyMax != 300 {
		t.Errorf("slowest start was %vms, want 300", s.ReadyMax)
	}
}

func TestInstanceStatsNameTheProcessThatWentAway(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, dir, "mcpx-2026-09-27.jsonl",
		line(base, "debug", "server.start", map[string]any{
			"event": "server.start", "server": "fff", "instance": "fff#1",
			"pid": 111, "trace": "srv-1"}),
		line(base.Add(time.Minute), "debug", "server.stop", map[string]any{
			"event": "server.stop", "server": "fff", "instance": "fff#1",
			"pid": 111, "reason": "exited", "uptimeSec": 60}),
		line(base.Add(2*time.Minute), "debug", "server.start", map[string]any{
			"event": "server.start", "server": "fff", "instance": "fff#2",
			"pid": 222, "trace": "srv-2"}),
	)
	st := open(t, dir)
	ingest(t, st)
	rows, err := st.Instances(logstore.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("listed %d instances, want 2", len(rows))
	}
	if rows[0].Reason != "exited" || rows[0].PID != 111 {
		t.Errorf("first instance is %+v, want pid 111 stopped because it exited", rows[0])
	}
	if !rows[1].Stopped.IsZero() {
		t.Errorf("the second instance is still running but reports a stop time")
	}
}

func TestErrorsGroupOnTheTemplateRatherThanTheInterpolatedMessage(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, dir, "mcpx-2026-09-27.jsonl",
		`{"ts":"2026-09-27T10:00:00Z","level":"error","msg":"call timed out on fff","template":"call timed out on {server}","server":"fff"}`,
		`{"ts":"2026-09-27T10:00:01Z","level":"error","msg":"call timed out on codedb","template":"call timed out on {server}","server":"codedb"}`,
		`{"ts":"2026-09-27T10:00:02Z","level":"error","msg":"something else","server":"fff"}`,
	)
	st := open(t, dir)
	ingest(t, st)
	rows, err := st.Errors(logstore.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("grouped into %d rows, want the template once and the one-off once", len(rows))
	}
	if rows[0].Count != 2 || rows[0].Message != "call timed out on {server}" {
		t.Errorf("most frequent error is %+v, want the template with a count of 2", rows[0])
	}
	if !strings.Contains(rows[0].Servers, "fff") || !strings.Contains(rows[0].Servers, "codedb") {
		t.Errorf("the grouped row lost the servers it happened on: %q", rows[0].Servers)
	}
}

func TestSessionsReportWhichServersEachOneTouched(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, dir, "mcpx-2026-09-27.jsonl",
		line(base, "debug", "mcp.call", map[string]any{"event": "mcp.call",
			"session": "s1", "server": "fff", "tool": "search", "durationMs": 10.0, "ok": true}),
		line(base.Add(time.Second), "debug", "mcp.call", map[string]any{"event": "mcp.call",
			"session": "s1", "server": "codedb", "tool": "defs", "durationMs": 20.0, "ok": false}),
		line(base.Add(2*time.Second), "debug", "mcp.call", map[string]any{"event": "mcp.call",
			"session": "s2", "server": "fff", "tool": "search", "durationMs": 5.0, "ok": true}),
	)
	st := open(t, dir)
	ingest(t, st)
	rows, err := st.Sessions(logstore.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("reported %d sessions, want 2", len(rows))
	}
	var s1 *logstore.SessionStat
	for i := range rows {
		if rows[i].Session == "s1" {
			s1 = &rows[i]
		}
	}
	if s1 == nil {
		t.Fatal("session s1 is missing")
	}
	if s1.Calls != 2 || s1.Errors != 1 || s1.Distinct != 2 || s1.BusyMs != 30 {
		t.Errorf("s1 is %+v, want 2 calls, 1 error, 2 servers, 30ms busy", *s1)
	}
}

func TestVolumeCountsRecordsPerLevelPerHour(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, dir, "mcpx-2026-09-27.jsonl",
		line(base, "info", "a", nil),
		line(base.Add(time.Minute), "error", "b", nil),
		line(base.Add(time.Hour), "info", "c", nil),
	)
	st := open(t, dir)
	ingest(t, st)
	buckets, files, err := st.Volume(logstore.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(buckets) != 2 {
		t.Fatalf("bucketed into %d hours, want 2", len(buckets))
	}
	if buckets[0].Info != 1 || buckets[0].Error != 1 || buckets[0].Total != 2 {
		t.Errorf("first hour is %+v, want one info and one error", buckets[0])
	}
	if len(files) != 1 || files[0].Records != 3 {
		t.Errorf("file accounting is %+v, want one file holding 3 records", files)
	}
}

func TestSlowestCallsCarryTheTraceIdNeededToWalkTheChain(t *testing.T) {
	dir := t.TempDir()
	chainFixture(t, dir)
	st := open(t, dir)
	ingest(t, st)
	rows, err := st.Slowest(logstore.Query{}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("returned %d calls, want 1", len(rows))
	}
	if rows[0].Trace != "cal-1" {
		t.Errorf("slowest call reports trace %q, want cal-1", rows[0].Trace)
	}
	if levels, err := st.Chain(rows[0].Trace, 10); err != nil || len(levels) != 3 {
		t.Errorf("the reported trace does not lead anywhere: %v %v", levels, err)
	}
}

func TestReadOnlyGuardRejectsAnythingThatWrites(t *testing.T) {
	ok := []string{
		`SELECT count(*) FROM records`,
		`  select 1;`,
		`WITH t AS (SELECT 1) SELECT * FROM t`,
		`PRAGMA table_info(records)`,
		`EXPLAIN SELECT 1`,
	}
	for _, q := range ok {
		if err := logstore.CheckReadOnly(q); err != nil {
			t.Errorf("rejected a read: %q: %v", q, err)
		}
	}
	bad := []string{
		`DELETE FROM records`,
		`drop table records`,
		`UPDATE records SET msg = 'x'`,
		`SELECT 1; DELETE FROM records`,
		`-- select
		DELETE FROM records`,
		`WITH t AS (SELECT 1) DELETE FROM records`,
		``,
	}
	for _, q := range bad {
		if err := logstore.CheckReadOnly(q); err == nil {
			t.Errorf("allowed a write: %q", q)
		}
	}
}

func TestRawSQLReturnsColumnsAndRows(t *testing.T) {
	dir := t.TempDir()
	chainFixture(t, dir)
	st := open(t, dir)
	ingest(t, st)
	table, err := st.RunSQL(`SELECT event, trace FROM records ORDER BY ts_unix_ms`)
	if err != nil {
		t.Fatal(err)
	}
	if len(table.Columns) != 2 || table.Columns[0] != "event" {
		t.Fatalf("columns are %v, want event and trace", table.Columns)
	}
	if len(table.Rows) != 3 || table.Rows[0][1] != "dmn-1" {
		t.Errorf("rows are %v, want the daemon first", table.Rows)
	}
}

func TestSinceAcceptsBothADurationAndAnAbsoluteTime(t *testing.T) {
	now := base
	got, err := logstore.ParseWhen("15m", now)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(now.Add(-15 * time.Minute)) {
		t.Errorf("15m resolved to %s, want 15 minutes before now", got)
	}
	got, err = logstore.ParseWhen("2026-09-27T10:00:00Z", now)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(base) {
		t.Errorf("an RFC3339 time resolved to %s, want %s", got, base)
	}
	if _, err := logstore.ParseWhen("yesterdayish", now); err == nil {
		t.Error("nonsense was accepted as a time")
	}
}

func TestAnEmptyLogDirectoryQueriesCleanly(t *testing.T) {
	st := open(t, t.TempDir())
	if res := ingest(t, st); res.Files != 0 {
		t.Errorf("found %d files in an empty directory", res.Files)
	}
	recs, err := st.Records(logstore.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Errorf("returned %d records from an empty log", len(recs))
	}
}
