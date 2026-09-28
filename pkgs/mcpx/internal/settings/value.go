package settings

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Layer is where a value came from. Higher wins.
type Layer int

const (
	LayerDefault Layer = iota // internal/defaults/defaults.json
	LayerFile                 // a configuration file, furthest first
	LayerEnv                  // the environment
	LayerFlag                 // the command line
)

func (l Layer) String() string {
	switch l {
	case LayerDefault:
		return "default"
	case LayerFile:
		return "file"
	case LayerEnv:
		return "env"
	case LayerFlag:
		return "flag"
	}
	return "?"
}

// Origin records where a value came from precisely enough to put in an error
// message. "logging.level is debug" is not useful on its own; "logging.level
// is debug, from MCPX_LOG_LEVEL" ends the investigation.
type Origin struct {
	Layer Layer `json:"layer"`
	// Detail is the file path, variable name, or flag spelling.
	Detail string `json:"detail,omitempty"`
	// Rank orders layers of the same kind: config files are ranked by
	// distance, nearest highest.
	Rank int `json:"-"`
}

func (o Origin) String() string {
	if o.Detail == "" {
		return o.Layer.String()
	}
	return o.Layer.String() + ":" + o.Detail
}

// Value is a setting's resolved value with its provenance.
type Value struct {
	Path   string `json:"path"`
	Raw    string `json:"raw"`
	Origin Origin `json:"origin"`
	// Shadowed lists the values this one overrode, nearest first. Kept
	// because "why is this not what my config says" is the single most
	// common configuration question, and the answer is always in this list.
	Shadowed []Origin `json:"shadowed,omitempty"`
}

// Set is a resolved configuration.
type Set struct {
	schema  *Schema
	values  map[string]*Value
	unknown []UnknownKeys
}

// NewSet starts from the schema's declared defaults.
func NewSet(schema *Schema) *Set {
	s := &Set{schema: schema, values: map[string]*Value{}}
	for _, set := range schema.All() {
		s.values[set.Path] = &Value{
			Path:   set.Path,
			Raw:    set.Default,
			Origin: Origin{Layer: LayerDefault},
		}
	}
	return s
}

// Apply records a value at a layer. Later calls at a higher-or-equal layer
// win; the displaced value is remembered.
//
// Two settings at the *same* layer that mean the same thing is the conflict
// worth catching -- `--log-level` and its alias both given, or a setting named
// twice in one file. Across layers there is no conflict, only precedence, and
// treating that as an error would make configuration files useless.
func (s *Set) Apply(path, raw string, origin Origin) error {
	set, ok := s.schema.Lookup(path)
	if !ok {
		return fmt.Errorf("unknown setting %q", path)
	}
	if err := Validate(*set, raw); err != nil {
		return fmt.Errorf("%s (from %s): %w", path, origin, err)
	}
	cur, seen := s.values[path]
	if !seen {
		s.values[path] = &Value{Path: path, Raw: raw, Origin: origin}
		return nil
	}
	if origin.Layer == cur.Origin.Layer && origin.Rank == cur.Origin.Rank &&
		cur.Origin.Layer != LayerDefault && cur.Raw != raw {
		return fmt.Errorf("%s set twice at the same level: %s says %q, %s says %q; "+
			"they are different spellings of one setting, so there is no order to pick",
			path, cur.Origin, cur.Raw, origin, raw)
	}
	if origin.Layer < cur.Origin.Layer ||
		(origin.Layer == cur.Origin.Layer && origin.Rank < cur.Origin.Rank) {
		cur.Shadowed = append(cur.Shadowed, origin)
		return nil
	}
	prev := cur.Origin
	shadowed := append([]Origin{prev}, cur.Shadowed...)
	s.values[path] = &Value{Path: path, Raw: raw, Origin: origin, Shadowed: shadowed}
	return nil
}

// Value returns the resolved value for a path.
func (s *Set) Value(path string) (*Value, bool) {
	v, ok := s.values[path]
	return v, ok
}

// All returns every resolved value, path-ordered.
func (s *Set) All() []Value {
	out := make([]Value, 0, len(s.values))
	for _, v := range s.values {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// Schema returns the schema this set was built from.
func (s *Set) Schema() *Schema { return s.schema }

// ---- typed readers ----
//
// These panic on an unknown path, deliberately. A typo in a path is a
// programming error that every run would hit, so failing loudly at the first
// call is better than returning a zero value that looks like a user's choice.

func (s *Set) raw(path string) string {
	v, ok := s.values[path]
	if !ok {
		panic("settings: no such setting " + path)
	}
	return v.Raw
}

func (s *Set) Bool(path string) bool {
	b, _ := strconv.ParseBool(strings.TrimSpace(s.raw(path)))
	return b
}

func (s *Set) String(path string) string { return s.raw(path) }

func (s *Set) Int(path string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s.raw(path)))
	return n
}

func (s *Set) Duration(path string) time.Duration {
	d, _ := time.ParseDuration(strings.TrimSpace(s.raw(path)))
	return d
}

func (s *Set) Bytes(path string) int64 {
	n, _ := ParseBytes(s.raw(path))
	return n
}

func (s *Set) List(path string) []string { return splitList(s.raw(path)) }

func splitList(v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	// A JSON array is accepted because that is what a config file naturally
	// holds, and round-tripping it through a comma-joined string and back
	// would corrupt any entry containing a comma.
	if strings.HasPrefix(v, "[") {
		var arr []any
		if json.Unmarshal([]byte(v), &arr) == nil {
			out := make([]string, 0, len(arr))
			for _, e := range arr {
				if e == nil {
					out = append(out, NullMarker)
					continue
				}
				out = append(out, fmt.Sprint(e))
			}
			return out
		}
	}
	fields := strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == '\n' })
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// NullMarker is the splice point in a list.
//
// A user who wants "my directory, then whatever was already there" has no way
// to say it with plain replacement, and no way to say "before" versus "after"
// with plain appending. A null entry is the join: it stands for the list this
// layer inherited.
const NullMarker = "\x00null"

// Splice expands null markers in a list against what the lower layers gave.
// A list with no marker replaces outright, which is what most people mean
// most of the time.
func Splice(list, inherited []string) []string {
	hasMarker := false
	for _, e := range list {
		if e == NullMarker {
			hasMarker = true
			break
		}
	}
	if !hasMarker {
		return list
	}
	out := make([]string, 0, len(list)+len(inherited))
	for _, e := range list {
		if e == NullMarker {
			out = append(out, inherited...)
			continue
		}
		out = append(out, e)
	}
	return dedupe(out)
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, e := range in {
		if seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	return out
}

// ParseBytes reads 16MB, 1GiB, 1024. Decimal and binary suffixes both work
// because both appear in the wild and arguing about which is correct helps
// nobody configure a log file.
func ParseBytes(v string) (int64, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, nil
	}
	mult := int64(1)
	upper := strings.ToUpper(v)
	for _, suf := range []struct {
		s string
		m int64
	}{
		{"KIB", 1 << 10}, {"MIB", 1 << 20}, {"GIB", 1 << 30}, {"TIB", 1 << 40},
		{"KB", 1000}, {"MB", 1000 * 1000}, {"GB", 1000 * 1000 * 1000},
		{"TB", 1000 * 1000 * 1000 * 1000},
		{"K", 1 << 10}, {"M", 1 << 20}, {"G", 1 << 30}, {"T", 1 << 40},
		{"B", 1},
	} {
		if strings.HasSuffix(upper, suf.s) {
			mult = suf.m
			v = strings.TrimSpace(v[:len(v)-len(suf.s)])
			break
		}
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("not a size: %q", v)
	}
	return int64(f * float64(mult)), nil
}

// Validate checks one raw value against its declaration.
func Validate(set Setting, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	switch set.Kind {
	case KindBool:
		if _, err := strconv.ParseBool(raw); err != nil {
			return fmt.Errorf("want true or false, got %q", raw)
		}
	case KindInt:
		if _, err := strconv.Atoi(raw); err != nil {
			return fmt.Errorf("want a whole number, got %q", raw)
		}
	case KindDuration:
		if _, err := time.ParseDuration(raw); err != nil {
			return fmt.Errorf("want a duration like 30s or 5m, got %q", raw)
		}
	case KindBytes:
		if _, err := ParseBytes(raw); err != nil {
			return err
		}
	case KindEnum:
		for _, ok := range set.Enum {
			if strings.EqualFold(raw, ok) {
				return nil
			}
		}
		return fmt.Errorf("want one of %s, got %q", strings.Join(set.Enum, ", "), raw)
	}
	return nil
}
