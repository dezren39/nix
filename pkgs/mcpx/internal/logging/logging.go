// Package logging renders structured records in several shapes.
//
// The model is Go's log/slog: a level, a message, and typed attributes. This
// package adds two things slog does not have, both needed because records
// arrive from scripts as well as from Go:
//
//   - message templates. A record keeps both the interpolated message and the
//     template it came from, so "how often did this line fire" stays
//     answerable when the values differ every time.
//   - a wire form. Scripts emit JSON on stderr; the runner parses it back into
//     the same Record a Go caller would have produced.
package logging

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Format selects a rendering.
type Format string

const (
	// FormatText is the default: a timestamp, a level, the message, then any
	// attributes the template did not consume.
	FormatText Format = "text"
	// FormatJSON is one object per line.
	FormatJSON Format = "json"
	// FormatJSONPretty is indented JSON, for reading rather than piping.
	FormatJSONPretty Format = "json-pretty"
	// FormatLogfmt is key=value pairs.
	FormatLogfmt Format = "logfmt"
	// FormatCompact drops the timestamp, keeping level and message.
	FormatCompact Format = "compact"
	// FormatBare is the message alone, for a script whose output is the point.
	FormatBare Format = "bare"
)

// Formats lists every accepted value, for flag help and errors.
var Formats = []Format{
	FormatText, FormatJSON, FormatJSONPretty, FormatLogfmt, FormatCompact, FormatBare,
}

// ParseFormat validates a format name.
func ParseFormat(s string) (Format, error) {
	if s == "" {
		return FormatText, nil
	}
	for _, f := range Formats {
		if string(f) == s {
			return f, nil
		}
	}
	names := make([]string, len(Formats))
	for i, f := range Formats {
		names[i] = string(f)
	}
	return "", fmt.Errorf("unknown format %q; want one of %s", s, strings.Join(names, ", "))
}

// reserved are the keys the rendered output owns. A user attribute with one
// of these names is renamed rather than dropped: losing a value silently is
// worse than an awkward key, and these names are exactly the ones someone
// logging about logging would reach for.
var reserved = map[string]bool{
	"ts": true, "level": true, "msg": true, "template": true,
}

// ReservedPrefix is applied to a user attribute whose name collides with a
// field the output format owns.
const ReservedPrefix = "attr."

// mergeAttrs folds attributes into an output object, renaming collisions.
func mergeAttrs(dst map[string]any, attrs map[string]any) {
	for k, v := range attrs {
		key := k
		if reserved[k] {
			key = ReservedPrefix + k
		}
		if _, taken := dst[key]; taken {
			key = ReservedPrefix + key
		}
		dst[key] = v
	}
}

// Record is one log line, from Go or from a script.
type Record struct {
	Time  time.Time  `json:"ts"`
	Level slog.Level `json:"-"`
	Msg   string     `json:"msg"`
	// Template is the uninterpolated message, present only when it differs
	// from Msg. Grouping on it is what makes counts meaningful.
	Template string         `json:"template,omitempty"`
	Attrs    map[string]any `json:"-"`
	// FileOnly keeps a record out of the terminal while still writing it to
	// the durable log. Mirrored console.log output uses this: the line has
	// already been printed to stdout, and printing it again as a log record
	// would double every result a script produces.
	FileOnly bool `json:"-"`
}

// LevelName is the lowercase name used on the wire and in output.
func LevelName(l slog.Level) string {
	switch {
	case l < slog.LevelInfo:
		return "debug"
	case l < slog.LevelWarn:
		return "info"
	case l < slog.LevelError:
		return "warn"
	default:
		return "error"
	}
}

// ParseLevel accepts the names LevelName produces, plus common aliases.
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug", "trace":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error", "fatal":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("unknown level %q; want debug, info, warn or error", s)
}

// token matches a doubled brace or a placeholder. Both are handled in one
// pass, because substituting first and unescaping afterwards would let a
// value's own braces be mistaken for an escape.
var token = regexp.MustCompile(`\{\{|\}\}|\{([a-zA-Z_][a-zA-Z0-9_.-]*)\}`)

// Interpolate substitutes {name} from attrs, returning the rendered message
// and the names that were consumed.
//
// A missing key is left as-is rather than blanked: a template that says
// {status} and gets no status should say so, not silently read as finished
// prose with a hole in it.
func Interpolate(template string, attrs map[string]any) (string, map[string]bool) {
	used := map[string]bool{}
	if template == "" || !strings.Contains(template, "{") {
		return template, used
	}
	out := token.ReplaceAllStringFunc(template, func(m string) string {
		switch m {
		case "{{":
			return "{"
		case "}}":
			return "}"
		}
		name := m[1 : len(m)-1]
		v, ok := attrs[name]
		if !ok {
			return m
		}
		used[name] = true
		return valueString(v)
	})
	return out, used
}

func valueString(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case time.Duration:
		return x.String()
	}
	if b, err := json.Marshal(v); err == nil {
		return string(b)
	}
	return fmt.Sprint(v)
}

// Writer renders records to a stream in one format, and optionally mirrors
// every record to a durable file.
//
// The two have different thresholds on purpose. A terminal is read by a person
// now and should stay quiet; a file is read by a tool later and should be
// generous, because a question asked tomorrow cannot be answered by a line
// that was never written.
type Writer struct {
	mu     sync.Mutex
	out    io.Writer
	format Format
	min    slog.Level

	file    *FileSink
	fileMin slog.Level
	base    map[string]any
}

// NewWriter builds a renderer.
func NewWriter(out io.Writer, format Format, min slog.Level) *Writer {
	return &Writer{out: out, format: format, min: min, fileMin: slog.LevelDebug}
}

// WithFile mirrors records at or above fileMin into a durable log.
func (w *Writer) WithFile(f *FileSink, fileMin slog.Level) *Writer {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.file = f
	w.fileMin = fileMin
	return w
}

// WithBase adds attributes to every record, terminal and file alike. This is
// where trace identity and ambient facts live once they are known.
func (w *Writer) WithBase(attrs map[string]any) *Writer {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.base == nil {
		w.base = map[string]any{}
	}
	for k, v := range attrs {
		w.base[k] = v
	}
	return w
}

// File exposes the durable sink, if any.
func (w *Writer) File() *FileSink {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file
}

// Enabled reports whether a level passes the threshold.
func (w *Writer) Enabled(l slog.Level) bool { return l >= w.min }

// Write renders one record to the terminal and mirrors it to the file.
func (w *Writer) Write(r Record) {
	w.mu.Lock()
	base := w.base
	file := w.file
	fileMin := w.fileMin
	w.mu.Unlock()

	if len(base) > 0 {
		merged := make(map[string]any, len(base)+len(r.Attrs))
		for k, v := range base {
			merged[k] = v
		}
		for k, v := range r.Attrs {
			merged[k] = v
		}
		r.Attrs = merged
	}

	if file != nil && r.Level >= fileMin {
		file.Write(r, nil)
	}
	if r.FileOnly || !w.Enabled(r.Level) {
		return
	}
	line := w.render(r)
	w.mu.Lock()
	fmt.Fprintln(w.out, line)
	w.mu.Unlock()
}

func (w *Writer) render(r Record) string {
	if r.Time.IsZero() {
		r.Time = time.Now()
	}
	// Msg is already interpolated by the time a record reaches a writer; the
	// template is kept only to learn which attributes it consumed, so they are
	// not repeated in the trailing key=value list.
	msg := r.Msg
	_, used := Interpolate(firstNonEmpty(r.Template, r.Msg), r.Attrs)
	if r.Template != "" && r.Msg == "" {
		msg, used = Interpolate(r.Template, r.Attrs)
	}

	switch w.format {
	case FormatJSON, FormatJSONPretty:
		obj := map[string]any{
			"ts":    r.Time.Format(time.RFC3339Nano),
			"level": LevelName(r.Level),
			"msg":   msg,
		}
		if r.Template != "" && r.Template != msg {
			obj["template"] = r.Template
		}
		mergeAttrs(obj, r.Attrs)
		var b []byte
		if w.format == FormatJSONPretty {
			b, _ = json.MarshalIndent(obj, "", "  ")
		} else {
			b, _ = json.Marshal(obj)
		}
		return string(b)

	case FormatLogfmt:
		parts := []string{
			"ts=" + quoteIfNeeded(r.Time.Format(time.RFC3339)),
			"level=" + LevelName(r.Level),
			"msg=" + quoteIfNeeded(msg),
		}
		for _, k := range sortedKeys(r.Attrs) {
			parts = append(parts, k+"="+quoteIfNeeded(valueString(r.Attrs[k])))
		}
		return strings.Join(parts, " ")

	case FormatBare:
		return msg

	case FormatCompact:
		return strings.ToUpper(LevelName(r.Level)) + " " + msg + trailing(r.Attrs, used)

	default: // FormatText
		return r.Time.Format("15:04:05.000") + " " +
			padLevel(LevelName(r.Level)) + " " + msg + trailing(r.Attrs, used)
	}
}

// trailing renders the attributes a template did not already consume, so a
// value never appears twice on one line.
func trailing(attrs map[string]any, used map[string]bool) string {
	var parts []string
	for _, k := range sortedKeys(attrs) {
		if used[k] {
			continue
		}
		parts = append(parts, k+"="+quoteIfNeeded(valueString(attrs[k])))
	}
	if len(parts) == 0 {
		return ""
	}
	return " " + strings.Join(parts, " ")
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func padLevel(l string) string {
	for len(l) < 5 {
		l += " "
	}
	return strings.ToUpper(l)
}

func quoteIfNeeded(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, " \t\"=\n") {
		return strconv.Quote(s)
	}
	return s
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// SourceLevel resolves the "which levels carry a call site" setting.
//
// It accepts a level name ("warn"), a boolean ("true"/"false"/"1"/"0"), or
// nothing. The default is warn: a call site is most wanted where something
// went wrong, and least wanted on routine progress, where it is pure cost.
func SourceLevel(spec string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(spec)) {
	case "":
		return slog.LevelWarn
	case "1", "true", "yes", "all", "always":
		return slog.LevelDebug
	case "0", "false", "no", "none", "never":
		// Above every real level, so nothing qualifies.
		return slog.LevelError + 1
	}
	if l, err := ParseLevel(spec); err == nil {
		return l
	}
	return slog.LevelWarn
}

// SourceSpec renders a resolved source level back into the form a script
// understands, so both ends agree without a second encoding.
func SourceSpec(l slog.Level) string {
	if l > slog.LevelError {
		return "0"
	}
	return LevelName(l)
}
