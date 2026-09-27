package logging_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/logging"
)

func render(t *testing.T, f logging.Format, r logging.Record) string {
	t.Helper()
	var buf bytes.Buffer
	logging.NewWriter(&buf, f, slog.LevelDebug).Write(r)
	return strings.TrimRight(buf.String(), "\n")
}

func rec() logging.Record {
	msg, _ := logging.Interpolate("fetched {count} pages", map[string]any{"count": 3})
	return logging.Record{
		Level:    slog.LevelInfo,
		Msg:      msg,
		Template: "fetched {count} pages",
		Attrs:    map[string]any{"count": 3, "url": "x"},
	}
}

func TestInterpolateSubstitutesNamedPlaceholders(t *testing.T) {
	got, used := logging.Interpolate("a {x} b {y}", map[string]any{"x": 1, "y": "two"})
	if got != "a 1 b two" {
		t.Fatalf("got %q", got)
	}
	if !used["x"] || !used["y"] {
		t.Fatalf("both keys should be marked used: %v", used)
	}
}

func TestMissingPlaceholderIsLeftVisible(t *testing.T) {
	// Blanking it would turn a bug into prose that reads as finished.
	got, used := logging.Interpolate("status: {status}", map[string]any{})
	if got != "status: {status}" {
		t.Fatalf("got %q", got)
	}
	if len(used) != 0 {
		t.Fatalf("nothing should be marked used: %v", used)
	}
}

func TestDoubledBracesEscape(t *testing.T) {
	got, _ := logging.Interpolate("literal {{x}} and {x}", map[string]any{"x": 9})
	if got != "literal {x} and 9" {
		t.Fatalf("got %q", got)
	}
}

func TestTextFormatDoesNotRepeatConsumedAttributes(t *testing.T) {
	out := render(t, logging.FormatText, rec())
	if !strings.Contains(out, "fetched 3 pages") {
		t.Fatalf("message not interpolated: %s", out)
	}
	if strings.Contains(out, "count=3") {
		t.Errorf("an attribute used by the template must not repeat: %s", out)
	}
	if !strings.Contains(out, "url=x") {
		t.Errorf("an unused attribute should be appended: %s", out)
	}
}

func TestJSONKeepsBothMessageAndTemplate(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal([]byte(render(t, logging.FormatJSON, rec())), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["msg"] != "fetched 3 pages" {
		t.Errorf("msg should be interpolated: %v", doc["msg"])
	}
	if doc["template"] != "fetched {count} pages" {
		t.Errorf("template should be preserved: %v", doc["template"])
	}
	if doc["count"] != float64(3) {
		t.Errorf("attributes should be top level: %v", doc)
	}
}

func TestBareIsMessageOnly(t *testing.T) {
	if got := render(t, logging.FormatBare, rec()); got != "fetched 3 pages" {
		t.Fatalf("got %q", got)
	}
}

func TestCompactDropsTheTimestamp(t *testing.T) {
	out := render(t, logging.FormatCompact, rec())
	if !strings.HasPrefix(out, "INFO ") {
		t.Fatalf("got %q", out)
	}
	if strings.Contains(out, ":") && strings.Count(out, ":") > 0 && strings.Contains(out, "T") {
		t.Errorf("compact should carry no timestamp: %s", out)
	}
}

func TestLogfmtQuotesValuesNeedingIt(t *testing.T) {
	out := render(t, logging.FormatLogfmt, logging.Record{
		Level: slog.LevelWarn, Msg: "two words", Attrs: map[string]any{"k": "a b"},
	})
	if !strings.Contains(out, `msg="two words"`) || !strings.Contains(out, `k="a b"`) {
		t.Fatalf("got %s", out)
	}
}

func TestLevelThresholdDropsQuieterRecords(t *testing.T) {
	var buf bytes.Buffer
	w := logging.NewWriter(&buf, logging.FormatBare, slog.LevelWarn)
	w.Write(logging.Record{Level: slog.LevelInfo, Msg: "hidden"})
	w.Write(logging.Record{Level: slog.LevelError, Msg: "shown"})
	if strings.Contains(buf.String(), "hidden") {
		t.Error("below-threshold record was emitted")
	}
	if !strings.Contains(buf.String(), "shown") {
		t.Error("at-threshold record was dropped")
	}
}

func TestParseFormatRejectsUnknownAndListsOptions(t *testing.T) {
	if _, err := logging.ParseFormat("nope"); err == nil {
		t.Fatal("expected an error")
	} else if !strings.Contains(err.Error(), "logfmt") {
		t.Errorf("the error should list valid formats: %v", err)
	}
	if f, err := logging.ParseFormat(""); err != nil || f != logging.FormatText {
		t.Errorf("empty should default to text, got %v %v", f, err)
	}
}

func TestWireRoundTripInterpolatesOnce(t *testing.T) {
	line := logging.Sentinel + `{"level":"info","msg":"a {x}","template":"a {x}","attrs":{"x":7}}`
	r, ok := logging.ParseLine(line)
	if !ok {
		t.Fatal("line should parse as a record")
	}
	if r.Msg != "a 7" {
		t.Errorf("parse should interpolate: %q", r.Msg)
	}
	if r.Template != "a {x}" {
		t.Errorf("template should survive: %q", r.Template)
	}
}

func TestNonRecordLinesAreNotRecords(t *testing.T) {
	if _, ok := logging.ParseLine("just some output"); ok {
		t.Fatal("ordinary output must not be mistaken for a record")
	}
}

func TestMalformedRecordSurfacesRatherThanDisappearing(t *testing.T) {
	r, ok := logging.ParseLine(logging.Sentinel + "{not json")
	if !ok {
		t.Fatal("a malformed record should still be reported")
	}
	if r.Level != slog.LevelError {
		t.Errorf("it should be an error, got %v", r.Level)
	}
}

func TestStreamSeparatesRecordsFromOrdinaryOutput(t *testing.T) {
	src := strings.NewReader(strings.Join([]string{
		"plain line one",
		logging.Sentinel + `{"level":"warn","msg":"structured"}`,
		"plain line two",
	}, "\n"))

	var rendered, passthrough bytes.Buffer
	var collected []logging.Record
	err := logging.Stream(src, logging.NewWriter(&rendered, logging.FormatBare, slog.LevelDebug),
		logging.StreamOptions{
			Enrich:      map[string]any{"session": "s1"},
			Passthrough: &passthrough,
			Collect:     func(r logging.Record) { collected = append(collected, r) },
		})
	if err != nil {
		t.Fatal(err)
	}
	if got := passthrough.String(); !strings.Contains(got, "plain line one") ||
		!strings.Contains(got, "plain line two") {
		t.Errorf("ordinary output should pass through verbatim: %q", got)
	}
	if strings.Contains(passthrough.String(), "structured") {
		t.Error("a structured record leaked into passthrough")
	}
	if !strings.Contains(rendered.String(), "structured") {
		t.Errorf("the record was not rendered: %q", rendered.String())
	}
	if len(collected) != 1 || collected[0].Attrs["session"] != "s1" {
		t.Errorf("enrichment should be applied: %+v", collected)
	}
}

func TestSlogHandlerRendersThroughTheSameWriter(t *testing.T) {
	var buf bytes.Buffer
	l := slog.New(logging.NewSlogHandler(
		logging.NewWriter(&buf, logging.FormatLogfmt, slog.LevelDebug)))
	l.Info("server {name} started", "name", "demo", "pid", 42)

	out := buf.String()
	if !strings.Contains(out, "server demo started") {
		t.Errorf("slog messages should interpolate too: %s", out)
	}
	if !strings.Contains(out, "pid=42") {
		t.Errorf("slog attributes should render: %s", out)
	}
}

func TestSlogHandlerWithAttrsAreInherited(t *testing.T) {
	var buf bytes.Buffer
	l := slog.New(logging.NewSlogHandler(
		logging.NewWriter(&buf, logging.FormatLogfmt, slog.LevelDebug))).With("server", "fff")
	l.Warn("slow")
	if !strings.Contains(buf.String(), "server=fff") {
		t.Errorf("With attributes should persist: %s", buf.String())
	}
}

func TestStreamedResultsAreSeparatedFromLogs(t *testing.T) {
	src := strings.NewReader(strings.Join([]string{
		logging.Sentinel + `{"kind":"result","value":{"step":1}}`,
		logging.Sentinel + `{"kind":"log","level":"info","msg":"between"}`,
		logging.Sentinel + `{"kind":"result","value":{"step":2}}`,
	}, "\n"))

	var rendered bytes.Buffer
	var logs []logging.Record
	var results []logging.Streamed
	err := logging.Stream(src, logging.NewWriter(&rendered, logging.FormatBare, slog.LevelDebug),
		logging.StreamOptions{
			Collect: func(r logging.Record) { logs = append(logs, r) },
			Result:  func(s logging.Streamed) { results = append(results, s) },
		})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected two streamed values, got %d", len(results))
	}
	if string(results[0].Value) != `{"step":1}` || string(results[1].Value) != `{"step":2}` {
		t.Errorf("streamed values are wrong or out of order: %s, %s", results[0].Value, results[1].Value)
	}
	if len(logs) != 1 || logs[0].Msg != "between" {
		t.Errorf("a result must not be mistaken for a log record: %+v", logs)
	}
	if strings.Contains(rendered.String(), "step") {
		t.Error("streamed results must not be rendered as log lines")
	}
}

func TestResultAndLogKindsDoNotCrossParse(t *testing.T) {
	if _, ok := logging.ParseLine(logging.Sentinel + `{"kind":"result","value":1}`); ok {
		t.Error("a result should not parse as a log record")
	}
	if _, ok := logging.ParseResult(logging.Sentinel + `{"kind":"log","level":"info","msg":"x"}`); ok {
		t.Error("a log record should not parse as a result")
	}
}

func TestSlogSourceIsOptIn(t *testing.T) {
	var off, on bytes.Buffer
	slog.New(logging.NewSlogHandler(
		logging.NewWriter(&off, logging.FormatLogfmt, slog.LevelDebug))).Info("x")
	slog.New(logging.NewSlogHandler(
		logging.NewWriter(&on, logging.FormatLogfmt, slog.LevelDebug)).
		WithSourceLevel(slog.LevelDebug)).Info("x")

	if strings.Contains(off.String(), "source.file") {
		t.Errorf("source should be off by default: %s", off.String())
	}
	if !strings.Contains(on.String(), "source.file") {
		t.Errorf("WithSourceLevel should record the call site: %s", on.String())
	}
}

func TestUserAttributesNeverOverwriteBuiltins(t *testing.T) {
	out := render(t, logging.FormatJSON, logging.Record{
		Level: slog.LevelInfo,
		Msg:   "the real message",
		Attrs: map[string]any{"msg": "mine", "level": "mine", "ts": "mine", "keep": 1},
	})
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["msg"] != "the real message" || doc["level"] != "info" {
		t.Errorf("builtins must win the plain name: %v", doc)
	}
	// ...but the user's values must still be present somewhere. Dropping data
	// silently is worse than an awkward key.
	if doc["attr.msg"] != "mine" || doc["attr.level"] != "mine" || doc["attr.ts"] != "mine" {
		t.Errorf("colliding attributes should be renamed, not dropped: %v", doc)
	}
	if doc["keep"] != float64(1) {
		t.Errorf("non-colliding attributes should be untouched: %v", doc)
	}
}

func TestSourceLevelDefaultsToWarn(t *testing.T) {
	// The default exists because capture costs ~5us in a script: worth paying
	// where something went wrong, wasteful on routine progress.
	if got := logging.SourceLevel(""); got != slog.LevelWarn {
		t.Fatalf("default should be warn, got %v", got)
	}
}

func TestSourceLevelAcceptsNamesAndBooleans(t *testing.T) {
	cases := map[string]slog.Level{
		"debug": slog.LevelDebug,
		"error": slog.LevelError,
		"true":  slog.LevelDebug,
		"1":     slog.LevelDebug,
		"all":   slog.LevelDebug,
	}
	for in, want := range cases {
		if got := logging.SourceLevel(in); got != want {
			t.Errorf("SourceLevel(%q) = %v, want %v", in, got, want)
		}
	}
	for _, off := range []string{"false", "0", "none", "never"} {
		if logging.SourceLevel(off) <= slog.LevelError {
			t.Errorf("SourceLevel(%q) should disable capture entirely", off)
		}
	}
}

func TestSourceSpecRoundTrips(t *testing.T) {
	for _, in := range []string{"debug", "warn", "error", "false"} {
		if got := logging.SourceLevel(logging.SourceSpec(logging.SourceLevel(in))); got != logging.SourceLevel(in) {
			t.Errorf("%q did not round trip", in)
		}
	}
}

func TestSlogSourceAppliesOnlyAtOrAboveTheLevel(t *testing.T) {
	var buf bytes.Buffer
	h := logging.NewSlogHandler(
		logging.NewWriter(&buf, logging.FormatLogfmt, slog.LevelDebug)).
		WithSourceLevel(slog.LevelWarn)
	l := slog.New(h)

	l.Info("routine")
	if strings.Contains(buf.String(), "source.file") {
		t.Errorf("info should not be traced at a warn threshold: %s", buf.String())
	}
	buf.Reset()

	l.Warn("something off")
	if !strings.Contains(buf.String(), "source.file") {
		t.Errorf("warn should be traced at a warn threshold: %s", buf.String())
	}
}
