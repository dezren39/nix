package settings

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Layer is where a value came from. Higher wins.
type Layer int

const (
	LayerDefault Layer = iota // internal/defaults/defaults.json
	LayerFile                 // a configuration file, furthest first
	LayerEnv                  // the environment
	// LayerPreset is a flag that came from a preset (--preset, MCPX_PRESET).
	// Above the environment because selecting a preset is an explicit
	// request for those flags; below the command line so a flag typed out
	// beats a bundle.
	LayerPreset
	LayerFlag // the command line
	// LayerRuntime is an override set through the API after the process
	// started. It is highest because it is the most recent statement of
	// intent: somebody changed their mind while the thing was running, and
	// having a flag from ten minutes ago win would be inexplicable.
	LayerRuntime
)

func (l Layer) String() string {
	switch l {
	case LayerDefault:
		return "default"
	case LayerFile:
		return "file"
	case LayerEnv:
		return "env"
	case LayerPreset:
		return "preset"
	case LayerFlag:
		return "flag"
	case LayerRuntime:
		return "runtime"
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
	// Requested is the value the layers asked for when a ceiling lowered it
	// (Setting.ClampedBy); Raw is then the ceiling. Empty when nothing was
	// lowered.
	Requested string `json:"requested,omitempty"`
	// ClampedBy is the ceiling that lowered Requested to Raw, with its
	// origin: "autonomy.max (file:/etc/mcpx.json)".
	ClampedBy string `json:"clampedBy,omitempty"`
}

// Set is a resolved configuration.
//
// A Set is read from many goroutines at once inside the daemon while another
// is applying a runtime override, so the map is guarded. Apply itself is
// still expected to run during start-up on one goroutine; the lock is there
// for the reads, which happen forever.
type Set struct {
	schema  *Schema
	mu      sync.RWMutex
	values  map[string]*Value
	unknown []UnknownKeys

	// base is the set this one layers on. A per-request view of the daemon's
	// configuration is a Set with a handful of overrides and no copy of the
	// eighty values underneath it, because a copy per request would be eighty
	// allocations to change one number.
	base *Set
	// override are values applied above every declared layer: a runtime
	// change on the root set, or the call-scoped values a client sent with
	// one request on a view.
	override map[string]string
	// overrideDetail describes where the overrides came from, for provenance.
	overrideDetail string
}

// NewSet starts from the schema's declared defaults.
func NewSet(schema *Schema) *Set {
	s := &Set{schema: schema, values: map[string]*Value{},
		override: map[string]string{}, overrideDetail: "api"}
	for _, set := range schema.All() {
		s.values[set.Path] = &Value{
			Path:   set.Path,
			Raw:    set.Default,
			Origin: Origin{Layer: LayerDefault},
		}
	}
	return s
}

// WithOverrides returns a view of this set with some values replaced.
//
// The view does not copy anything: it holds the overrides and defers
// everything else. This is what carries a call-scoped setting from a client's
// command line into the daemon serving that one request, without the value
// leaking into the next request or into another client.
func (s *Set) WithOverrides(ov map[string]string, detail string) *Set {
	if len(ov) == 0 {
		return s
	}
	copied := make(map[string]string, len(ov))
	for k, v := range ov {
		copied[k] = v
	}
	return &Set{schema: s.schema, base: s, override: copied, overrideDetail: detail}
}

// SetRuntime applies an override above every declared layer, live.
//
// Validated exactly as a value from a file would be, because a runtime change
// that skips the parser is the one way to get an invalid value into a running
// process.
func (s *Set) SetRuntime(path, raw string) error {
	set, ok := s.schema.Lookup(path)
	if !ok {
		return fmt.Errorf("unknown setting %q", path)
	}
	raw, err := Normalize(*set, raw)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.override == nil {
		s.override = map[string]string{}
	}
	s.override[path] = raw
	return nil
}

// ClearRuntime drops a runtime override, so the value falls back to whatever
// the flags, environment and files said. Reports whether one was there.
func (s *Set) ClearRuntime(path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, had := s.override[path]
	delete(s.override, path)
	return had
}

// RuntimeOverrides returns the overrides currently in force.
func (s *Set) RuntimeOverrides() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.override))
	for k, v := range s.override {
		out[k] = v
	}
	return out
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
	raw, err := Normalize(*set, raw)
	if err != nil {
		return fmt.Errorf("%s (from %s): %w", path, origin, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
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

// Value returns the resolved value for a path, overrides included, lowered
// to its ceiling when it has one (Setting.ClampedBy), so no reader can
// observe a value above it.
func (s *Set) Value(path string) (*Value, bool) {
	v, ok := s.unclamped(path)
	if !ok {
		return v, ok
	}
	return s.clampValue(v), true
}

func (s *Set) unclamped(path string) (*Value, bool) {
	s.mu.RLock()
	raw, overridden := s.override[path]
	detail := s.overrideDetail
	s.mu.RUnlock()

	var under *Value
	if s.base != nil {
		under, _ = s.base.unclamped(path)
	} else {
		s.mu.RLock()
		if v, ok := s.values[path]; ok {
			c := *v
			under = &c
		}
		s.mu.RUnlock()
	}
	if !overridden {
		return under, under != nil
	}
	out := &Value{Path: path, Raw: raw,
		Origin: Origin{Layer: LayerRuntime, Detail: detail}}
	if under != nil {
		out.Shadowed = append([]Origin{under.Origin}, under.Shadowed...)
	}
	return out, true
}

// All returns every resolved value, path-ordered.
func (s *Set) All() []Value {
	out := make([]Value, 0, len(s.schema.settings))
	for _, set := range s.schema.All() {
		if v, ok := s.Value(set.Path); ok {
			out = append(out, *v)
		}
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
	v, ok := s.Value(path)
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

// Given reports whether anybody actually said this, as opposed to the
// built-in default standing in. It is the question to ask before copying a
// resolved value into a structure that has its own fallback, where writing
// the default in would turn "unset" into "set to the same thing" -- a
// difference that matters when a narrower scope is allowed to override.
func (s *Set) Given(path string) bool {
	v, ok := s.Value(path)
	return ok && v.Origin.Layer != LayerDefault
}

// AboveFile returns the raw value only when it came from the environment, a
// flag or a runtime override.
//
// It exists for the few settings whose configuration-file layer is consumed
// somewhere other than the Set. The script phases are the case: they reach
// the runner through config.ScriptPhase, which folds every contributing file
// with its own inheritance marker, and taking the resolved value as well
// would run a line from the file twice. What is genuinely missing there is
// the two layers a file reader cannot see, so that is what this hands back.
//
// Anything whose file layer is not already read elsewhere should use the
// ordinary accessors instead. This is a narrowing, and a narrowing applied
// where it is not needed is how a setting becomes half-read.
func (s *Set) AboveFile(path string) (string, bool) {
	v, ok := s.Value(path)
	if !ok || v.Origin.Layer < LayerEnv {
		return "", false
	}
	return v.Raw, true
}

// ListAboveFile is AboveFile for a repeatable setting.
func (s *Set) ListAboveFile(path string) []string {
	raw, ok := s.AboveFile(path)
	if !ok {
		return nil
	}
	return splitList(raw)
}

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
		if _, ok := set.Canonical(raw); !ok {
			return fmt.Errorf("want one of %s, got %q", set.EnumWords(), raw)
		}
	}
	return nil
}

// Normalize validates a raw value and returns the spelling every reader
// should see: for an enum, the declared value an alias or a differently-cased
// spelling stands for. It is the one place a value is parsed, so nothing
// downstream compares against an alias.
func Normalize(set Setting, raw string) (string, error) {
	if err := Validate(set, raw); err != nil {
		return raw, err
	}
	v, _ := set.Canonical(raw)
	if set.Kind != KindEnum {
		return raw, nil
	}
	return v, nil
}

// NormalizePath is Normalize for a setting named by path in Registry(), for a
// value read outside a Set -- a per-server key that overrides a setting.
func NormalizePath(path, raw string) (string, error) {
	registryOnce.Do(func() { registrySchema, registryErr = New(Registry()) })
	if registryErr != nil {
		return raw, registryErr
	}
	set, ok := registrySchema.Lookup(path)
	if !ok {
		return raw, fmt.Errorf("unknown setting %q", path)
	}
	return Normalize(*set, raw)
}

var (
	registryOnce   sync.Once
	registrySchema *Schema
	registryErr    error
)
