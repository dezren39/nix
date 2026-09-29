package logging

import (
	"bufio"
	"context"
	"encoding/json"
	"github.com/dezren39/mcpx/internal/defaults"
	"io"
	"log/slog"
	"runtime"
	"strings"
	"time"
)

// Sentinel prefixes a structured record on a script's stderr.
//
// Scripts log to stderr rather than to the daemon so that logging works with
// no daemon reachable and needs no round trip. A record is one line beginning
// with this marker; anything else on stderr is the script's own output and is
// passed through untouched. U+001E is the ASCII record separator: it is not
// something a human types, and it survives being written by every runtime.
const Sentinel = "\x1emcpx\x1e"

// wireRecord is the JSON a script emits. Three kinds travel the same channel:
// "log" is diagnostics, "result" is a streamed value, and "artifact" is a
// file the script registered with the daemon. Sharing the channel keeps them
// ordered relative to one another, which matters when a log line explains the
// result that follows it, and when a consumer wants to know an artifact
// exists before the run has finished.
type wireRecord struct {
	Kind     string          `json:"kind,omitempty"`
	Level    string          `json:"level"`
	Msg      string          `json:"msg"`
	Template string          `json:"template,omitempty"`
	Time     string          `json:"ts,omitempty"`
	Attrs    map[string]any  `json:"attrs,omitempty"`
	Value    json.RawMessage `json:"value,omitempty"`
	FileOnly bool            `json:"fileOnly,omitempty"`
	ID       string          `json:"id,omitempty"`
	Name     string          `json:"name,omitempty"`
}

// Streamed is a result a script produced before finishing.
type Streamed struct {
	Time  time.Time
	Value json.RawMessage
}

// Artifacted is a file a script registered while it ran.
//
// Only the identity travels here. The metadata of record is the daemon's,
// read back from the index: what the script says it stored is a claim, and
// what the store holds is a fact.
type Artifacted struct {
	Time time.Time
	ID   string
	Name string
}

// ParseArtifact returns an artifact announcement if the line carries one.
func ParseArtifact(line string) (Artifacted, bool) {
	rest, ok := strings.CutPrefix(line, Sentinel)
	if !ok {
		return Artifacted{}, false
	}
	var w wireRecord
	if err := json.Unmarshal([]byte(rest), &w); err != nil || w.Kind != "artifact" {
		return Artifacted{}, false
	}
	ts := time.Now()
	if w.Time != "" {
		if parsed, perr := time.Parse(time.RFC3339Nano, w.Time); perr == nil {
			ts = parsed
		}
	}
	return Artifacted{Time: ts, ID: w.ID, Name: w.Name}, true
}

// ParseResult returns a streamed value if the line carries one.
func ParseResult(line string) (Streamed, bool) {
	rest, ok := strings.CutPrefix(line, Sentinel)
	if !ok {
		return Streamed{}, false
	}
	var w wireRecord
	if err := json.Unmarshal([]byte(rest), &w); err != nil || w.Kind != "result" {
		return Streamed{}, false
	}
	ts := time.Now()
	if w.Time != "" {
		if parsed, perr := time.Parse(time.RFC3339Nano, w.Time); perr == nil {
			ts = parsed
		}
	}
	value := w.Value
	if len(value) == 0 {
		value = json.RawMessage("null")
	}
	return Streamed{Time: ts, Value: value}, true
}

// ParseLine turns one line of script stderr into a log record. The second
// return reports whether the line was a log record at all; when false the line
// is either a streamed result or ordinary output.
func ParseLine(line string) (Record, bool) {
	rest, ok := strings.CutPrefix(line, Sentinel)
	if !ok {
		return Record{}, false
	}
	var w wireRecord
	if err := json.Unmarshal([]byte(rest), &w); err == nil && (w.Kind == "result" || w.Kind == "artifact") {
		return Record{}, false
	} else if err != nil {
		// A malformed record is still more useful surfaced than dropped.
		return Record{
			Time:  time.Now(),
			Level: slog.LevelError,
			Msg:   "malformed log record from script: " + strings.TrimSpace(rest),
		}, true
	}
	level, err := ParseLevel(w.Level)
	if err != nil {
		level = slog.LevelInfo
	}
	ts := time.Now()
	if w.Time != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, w.Time); err == nil {
			ts = parsed
		}
	}
	attrs := w.Attrs
	if attrs == nil {
		attrs = map[string]any{}
	}
	msg := w.Msg
	// Interpolate once, here, so every consumer -- renderer, --json envelope,
	// future log store -- sees the same finished message and the same raw
	// template, rather than each having to remember to do it.
	if w.Template != "" {
		msg, _ = Interpolate(w.Template, attrs)
	}
	return Record{Time: ts, Level: level, Msg: msg, Template: w.Template,
		Attrs: attrs, FileOnly: w.FileOnly}, true
}

// StreamOptions configure Stream.
type StreamOptions struct {
	// Enrich adds ambient context to every structured record. The values a
	// script cannot know about itself -- which pool served it, which scope it
	// resolved to -- are added here rather than being asked of the script.
	Enrich map[string]any
	// Passthrough receives lines that were not structured records.
	Passthrough io.Writer
	// Collect, when non-nil, receives every parsed record as well as the
	// writer, so `run --json` can return them.
	Collect func(Record)
	// Result receives each streamed value, in the order the script produced
	// them.
	Result func(Streamed)
	// Artifact receives each file the script registered, in order.
	Artifact func(Artifacted)
}

// Stream reads a script's stderr, rendering structured records through w and
// forwarding everything else verbatim.
func Stream(src io.Reader, w *Writer, opts StreamOptions) error {
	sc := bufio.NewScanner(src)
	// Script output can be long; a snapshot or a stack trace easily exceeds
	// the default 64 KiB line limit.
	sc.Buffer(make([]byte, 0, int(defaults.HTTPStreamBufferInit)), int(defaults.HTTPStreamBufferMax))
	for sc.Scan() {
		line := sc.Text()
		if streamed, ok := ParseResult(line); ok {
			if opts.Result != nil {
				opts.Result(streamed)
			}
			continue
		}
		if art, ok := ParseArtifact(line); ok {
			if opts.Artifact != nil {
				opts.Artifact(art)
			}
			continue
		}
		rec, ok := ParseLine(line)
		if !ok {
			if opts.Passthrough != nil {
				io.WriteString(opts.Passthrough, line+"\n")
			}
			continue
		}
		if rec.Attrs == nil {
			rec.Attrs = map[string]any{}
		}
		for k, v := range opts.Enrich {
			if _, taken := rec.Attrs[k]; !taken {
				rec.Attrs[k] = v
			}
		}
		if opts.Collect != nil {
			opts.Collect(rec)
		}
		w.Write(rec)
	}
	return sc.Err()
}

// SlogHandler adapts this package's rendering to a slog.Handler, so the daemon
// can use an ordinary *slog.Logger and still honour --format.
type SlogHandler struct {
	w         *Writer
	attrs     map[string]any
	group     string
	sourceAt  slog.Level
	hasSource bool
}

// NewSlogHandler wraps a Writer.
func NewSlogHandler(w *Writer) *SlogHandler {
	return &SlogHandler{w: w, attrs: map[string]any{}}
}

// WithSourceLevel records the file, line and function for records at or above
// this level. slog already carries the program counter, so this only costs a
// symbol lookup when a record actually qualifies.
func (h *SlogHandler) WithSourceLevel(l slog.Level) *SlogHandler {
	next := *h
	next.sourceAt = l
	next.hasSource = true
	return &next
}

// Enabled implements slog.Handler.
func (h *SlogHandler) Enabled(_ context.Context, l slog.Level) bool { return h.w.Enabled(l) }

// Handle implements slog.Handler.
func (h *SlogHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := make(map[string]any, len(h.attrs)+r.NumAttrs())
	for k, v := range h.attrs {
		attrs[k] = v
	}
	r.Attrs(func(a slog.Attr) bool {
		attrs[h.key(a.Key)] = a.Value.Any()
		return true
	})
	if h.hasSource && r.Level >= h.sourceAt && r.PC != 0 {
		f, _ := runtime.CallersFrames([]uintptr{r.PC}).Next()
		if f.File != "" {
			attrs["source.file"] = f.File
			attrs["source.line"] = f.Line
			if f.Function != "" {
				attrs["source.function"] = f.Function
			}
		}
	}
	// slog has no notion of a template, so a message containing {name} is
	// treated as one. That makes the two sources render identically.
	template := ""
	msg := r.Message
	if strings.Contains(r.Message, "{") {
		template = r.Message
		msg, _ = Interpolate(template, attrs)
	}
	h.w.Write(Record{Time: r.Time, Level: r.Level, Msg: msg, Template: template, Attrs: attrs})
	return nil
}

func (h *SlogHandler) key(k string) string {
	if h.group == "" {
		return k
	}
	return h.group + "." + k
}

// WithAttrs implements slog.Handler.
func (h *SlogHandler) WithAttrs(as []slog.Attr) slog.Handler {
	next := &SlogHandler{w: h.w, group: h.group, sourceAt: h.sourceAt, hasSource: h.hasSource, attrs: map[string]any{}}
	for k, v := range h.attrs {
		next.attrs[k] = v
	}
	for _, a := range as {
		next.attrs[h.key(a.Key)] = a.Value.Any()
	}
	return next
}

// WithGroup implements slog.Handler.
func (h *SlogHandler) WithGroup(name string) slog.Handler {
	next := &SlogHandler{w: h.w, attrs: h.attrs, sourceAt: h.sourceAt, hasSource: h.hasSource, group: name}
	if h.group != "" && name != "" {
		next.group = h.group + "." + name
	}
	return next
}
