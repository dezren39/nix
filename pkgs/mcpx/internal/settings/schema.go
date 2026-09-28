// Package settings is the single declaration of every knob mcpx has.
//
// A setting is described once -- its type, its default, what it is called, what
// it means -- and from that one description it becomes readable from a
// configuration file, an environment variable and a command-line flag. Nothing
// is restated. Adding a knob means adding one entry here; forgetting to wire it
// into one of the three surfaces is not possible, because there is no wiring.
//
// The alternative, and what this replaces, is a flag declared in one file, a
// default in another, an environment lookup in a third, and a struct field in a
// fourth. That arrangement has exactly one failure mode and it happens every
// time: the four drift, and the answer to "what is this set to" becomes "read
// all four and guess".
package settings

import (
	"fmt"
	"sort"
	"strings"
)

// Kind is the type of a setting's value, which determines how a string from
// the environment or the command line is parsed.
type Kind int

const (
	KindBool Kind = iota
	KindString
	KindInt
	KindDuration
	KindBytes  // 16MB, 1GiB
	KindList   // comma or space separated
	KindSource // inline text, a file path, or a directory of files
	KindEnum
	KindPathList // like KindList, but entries are paths and null splices
)

func (k Kind) String() string {
	switch k {
	case KindBool:
		return "bool"
	case KindString:
		return "string"
	case KindInt:
		return "int"
	case KindDuration:
		return "duration"
	case KindBytes:
		return "bytes"
	case KindList:
		return "list"
	case KindSource:
		return "source"
	case KindEnum:
		return "enum"
	case KindPathList:
		return "paths"
	}
	return "unknown"
}

// Setting is one knob.
type Setting struct {
	// Path is the dotted location in the configuration file, and the basis
	// for the generated flag and environment variable names.
	Path string

	Kind Kind

	// Default is the value when nothing sets it. It is stated here as a
	// string in the same syntax a user would write, so that the default and
	// a user's override go through identical parsing -- a default that skips
	// the parser is a default that can be invalid.
	Default string

	// Name is the human title, for help output.
	Name string

	// Short is one line. Long is the paragraph shown by `--help <setting>`.
	Short string
	Long  string

	// Flag overrides the derived flag name. FlagAliases are additional
	// spellings, including single letters.
	Flag        string
	FlagAliases []string

	// Env overrides the derived variable. EnvAliases are additional
	// spellings, given in full.
	Env        string
	EnvAliases []string

	// Enum lists the permitted values when Kind is KindEnum.
	Enum []string

	// Bare is the value implied when a flag is given with no argument. An
	// empty Bare means the flag requires one.
	Bare string

	// Repeatable allows the flag to appear several times, accumulating. A
	// bare "-" entry splices in whatever the lower layers provided.
	Repeatable bool

	// Commands restricts the flag to those subcommands. Empty means every
	// command accepts it.
	Commands []string

	// Plumbing marks a setting as an internal detail. These are real and
	// they work, but they are hidden from ordinary help because changing one
	// without knowing why is how a working installation stops working.
	Plumbing bool

	// Requires are conditions that must hold when this setting is given a
	// non-default value. They are checked before any work begins.
	Requires []Requirement

	// SourceKind constrains a KindSource setting: whether a directory is a
	// legal value for it, and whether that directory is read recursively.
	AllowDir  bool
	Recursive bool
}

// Requirement is a cross-field condition.
//
// The case that motivates this is a setting that is real but inert: naming a
// log directory while file logging is off is not a syntax error and not a
// runtime error, it simply does nothing, and the user finds out by noticing
// the absence of something. Checking it up front turns a silent no-op into a
// sentence.
type Requirement struct {
	// Path is the other setting this one depends on.
	Path string
	// Equals is the value that other setting must hold. Empty means it only
	// has to be set to something other than its default.
	Equals string
	// Because explains the dependency in the error message.
	Because string
}

// FlagName is the command-line spelling: dots become dashes, camelCase
// becomes dash-separated, and the leading section is kept because
// `--log-level` and `--script-level` want to stay distinguishable.
func (s Setting) FlagName() string {
	if s.Flag != "" {
		return s.Flag
	}
	return dashed(s.Path)
}

// EnvName is the environment spelling: MCPX_ plus the dotted path uppercased
// with dots and camel humps becoming underscores.
func (s Setting) EnvName() string {
	if s.Env != "" {
		return s.Env
	}
	return "MCPX_" + strings.ToUpper(strings.ReplaceAll(dashed(s.Path), "-", "_"))
}

func dashed(path string) string {
	var b strings.Builder
	for i, r := range path {
		switch {
		case r == '.':
			b.WriteByte('-')
		case r >= 'A' && r <= 'Z':
			if i > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r - 'A' + 'a')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Schema is the whole set.
type Schema struct {
	settings []Setting
	byPath   map[string]*Setting
	byFlag   map[string]*Setting
	byEnv    map[string]*Setting
}

// New builds a schema, rejecting collisions. A duplicated flag or variable is
// a programming error, and it is far better found at startup than by a user
// discovering that one of two settings silently wins.
func New(list []Setting) (*Schema, error) {
	s := &Schema{
		byPath: map[string]*Setting{},
		byFlag: map[string]*Setting{},
		byEnv:  map[string]*Setting{},
	}
	// Sort before taking any pointers. Sorting afterwards moves the elements
	// out from under every pointer already handed to the lookup maps, which
	// presents as a setting resolving to a neighbour's declaration -- a
	// failure that looks like anything except what it is.
	s.settings = append([]Setting(nil), list...)
	sort.Slice(s.settings, func(i, j int) bool { return s.settings[i].Path < s.settings[j].Path })

	for i := range s.settings {
		set := s.settings[i]
		if _, dup := s.byPath[set.Path]; dup {
			return nil, fmt.Errorf("settings: %q declared twice", set.Path)
		}
		p := &s.settings[i]
		s.byPath[set.Path] = p

		for _, name := range append([]string{set.FlagName()}, set.FlagAliases...) {
			if prev, dup := s.byFlag[name]; dup {
				return nil, fmt.Errorf("settings: flag --%s claimed by both %q and %q",
					name, prev.Path, set.Path)
			}
			s.byFlag[name] = p
		}
		for _, name := range append([]string{set.EnvName()}, set.EnvAliases...) {
			if prev, dup := s.byEnv[name]; dup {
				return nil, fmt.Errorf("settings: %s claimed by both %q and %q",
					name, prev.Path, set.Path)
			}
			s.byEnv[name] = p
		}
	}
	return s, nil
}

// All returns every setting, path-ordered.
func (s *Schema) All() []Setting { return append([]Setting(nil), s.settings...) }

// Lookup finds a setting by its dotted path.
func (s *Schema) Lookup(path string) (*Setting, bool) {
	set, ok := s.byPath[path]
	return set, ok
}

// ByFlag finds a setting by any of its flag spellings.
func (s *Schema) ByFlag(name string) (*Setting, bool) {
	set, ok := s.byFlag[strings.TrimLeft(name, "-")]
	return set, ok
}

// ByEnv finds a setting by any of its variable spellings.
func (s *Schema) ByEnv(name string) (*Setting, bool) {
	set, ok := s.byEnv[name]
	return set, ok
}

// ForCommand returns the settings a given subcommand accepts.
func (s *Schema) ForCommand(cmd string) []Setting {
	var out []Setting
	for _, set := range s.settings {
		if len(set.Commands) == 0 {
			out = append(out, set)
			continue
		}
		for _, c := range set.Commands {
			if c == cmd {
				out = append(out, set)
				break
			}
		}
	}
	return out
}
