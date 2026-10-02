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

// Scope is which process a setting governs.
//
// It exists because a flag on a CLI command and a value inside an
// already-running daemon are not the same thing, and pretending they are
// produces the worst configuration bug there is: a flag that is accepted,
// validated, printed back, and then silently ignored because the process that
// would act on it was started an hour ago with a different environment.
//
// Knowing the scope lets each surface do the right thing. A daemon-scoped
// setting is read by the daemon and a client that sets it must either restart
// the daemon or ask it to change at runtime. A call-scoped setting travels
// with the request, so a flag on one command reaches the daemon serving it.
type Scope int

const (
	// ScopeUnset is the zero value, and is not a legal scope. A setting that
	// does not say who reads it is a setting nobody can reason about from
	// the outside, so a test refuses one rather than guessing on its behalf.
	ScopeUnset Scope = iota
	// ScopeDaemon is daemon-wide: the pooled servers, the listener, the log
	// the daemon writes.
	ScopeDaemon
	// ScopeClient is the CLI process itself: output rendering, how to reach
	// a daemon, whether to start one.
	ScopeClient
	// ScopeCall is meaningful per request. The CLI sends its effective value
	// with the request and the daemon honours it for that request only.
	ScopeCall
	// ScopePlugin is read by the opencode plugin, which is neither.
	ScopePlugin
)

func (s Scope) String() string {
	switch s {
	case ScopeUnset:
		return "unset"
	case ScopeDaemon:
		return "daemon"
	case ScopeClient:
		return "client"
	case ScopeCall:
		return "call"
	case ScopePlugin:
		return "plugin"
	}
	return "unknown"
}

// ParseScope reads a scope name.
func ParseScope(v string) (Scope, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "daemon":
		return ScopeDaemon, true
	case "client":
		return ScopeClient, true
	case "call":
		return ScopeCall, true
	case "plugin":
		return ScopePlugin, true
	}
	return ScopeUnset, false
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

	// EnumAliases maps an Enum value to older or alternative spellings that
	// are accepted and normalised to it. A value's name can go stale -- a
	// relative word like "modern" stops describing anything once something
	// newer ships -- and renaming it must not break a config file that uses
	// the old one. Every reader sees only the canonical value.
	EnumAliases map[string][]string

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

	// Scope says which process reads this. See Scope.
	Scope Scope

	// Hot marks a setting that takes effect the moment it changes, because
	// whoever reads it reads it afresh on every use. A setting that is not
	// hot was consumed once at startup -- a listener address, a log file --
	// and changing it at runtime would be recorded and then do nothing, so
	// the runtime API refuses it and says a restart is needed instead.
	Hot bool

	// Bootstrap marks a setting that is needed before there is anything to
	// resolve it from: which configuration file to read cannot come from a
	// configuration file. Only the environment and an explicit Flag reach
	// one. A configuration file naming it is refused rather than obeyed or
	// ignored, no per-command flag is generated for it, and the settings API
	// will not change it -- each of those would be accepted and then do
	// nothing, which is the failure this package exists to prevent.
	Bootstrap bool

	// ClampedBy names the setting whose value is the most this one may
	// resolve to. The ceiling is daemon-scoped and not hot, this setting is
	// call-scoped, and both are KindEnum; this one's Enum is an ordered
	// subset of the ceiling's, and the ceiling's order is what "most" means.
	//
	// A value above the ceiling is lowered to it, from any layer, and the
	// lowering is recorded on the Value. It is never refused: the lower
	// value is always a safe answer to the request
	// (docs/decisions/0002-autonomy-dial.md).
	ClampedBy string
}

// Writable reports whether a configuration file or the runtime API may set
// this, and says why not when they may not.
func (s Setting) Writable() error {
	if !s.Bootstrap {
		return nil
	}
	how := s.EnvName()
	if s.Flag != "" {
		how += " or --" + s.Flag
	}
	return fmt.Errorf("%s is read before any configuration exists, so neither a "+
		"configuration file nor a running daemon can change it; set %s", s.Path, how)
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

// dashed splits a dotted camelCase path into lower-case dash-separated words.
//
// A run of capitals is one word, because it is an acronym: `askTTL` is
// ask-ttl, not ask-t-t-l. The one exception is the last capital of a run that
// is followed by a lower-case letter, which starts the next word -- the P in
// `HTTPTimeout` ends HTTP, the T begins Timeout. Treating every capital as a
// word of its own derived MCPX_PROTO_ASK_T_T_L, which nobody would ever type.
func dashed(path string) string {
	rs := []rune(path)
	var b strings.Builder
	for i, r := range rs {
		if r == '.' {
			b.WriteByte('-')
			continue
		}
		if !isUpper(r) {
			b.WriteRune(r)
			continue
		}
		if i > 0 && rs[i-1] != '.' {
			prev := rs[i-1]
			nextLower := i+1 < len(rs) && isLower(rs[i+1])
			if !isUpper(prev) || nextLower {
				b.WriteByte('-')
			}
		}
		b.WriteRune(r - 'A' + 'a')
	}
	return b.String()
}

func isUpper(r rune) bool { return r >= 'A' && r <= 'Z' }
func isLower(r rune) bool { return r >= 'a' && r <= 'z' }

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
	for i := range s.settings {
		if err := checkEnumAliases(&s.settings[i]); err != nil {
			return nil, err
		}
		if err := s.checkClamp(&s.settings[i]); err != nil {
			return nil, err
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
//
// A bootstrap setting is never one of them. By the time a subcommand parses
// its flags the configuration has been read, so a flag generated for one
// would be accepted and inert.
func (s *Schema) ForCommand(cmd string) []Setting {
	var out []Setting
	for _, set := range s.settings {
		if set.Bootstrap {
			continue
		}
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

// checkEnumAliases refuses an alias table that could resolve ambiguously: an
// alias for a value that is not declared, or one spelling that names two
// values (or is itself a canonical value).
func checkEnumAliases(set *Setting) error {
	if len(set.EnumAliases) == 0 {
		return nil
	}
	if set.Kind != KindEnum {
		return fmt.Errorf("settings: %q has enum aliases but is not an enum", set.Path)
	}
	seen := map[string]string{}
	for _, e := range set.Enum {
		seen[strings.ToLower(e)] = e
	}
	for canon, aliases := range set.EnumAliases {
		if _, ok := seen[strings.ToLower(canon)]; !ok || seen[strings.ToLower(canon)] != canon {
			return fmt.Errorf("settings: %q aliases %q, which is not one of %v", set.Path, canon, set.Enum)
		}
		for _, a := range aliases {
			if prev, dup := seen[strings.ToLower(a)]; dup {
				return fmt.Errorf("settings: %q value spelling %q claimed by both %q and %q",
					set.Path, a, prev, canon)
			}
			seen[strings.ToLower(a)] = canon
		}
	}
	return nil
}

// Canonical resolves a raw enum value, or one of its aliases, to the
// declared spelling. ok is false for a value the setting does not accept.
// Non-enum settings return raw unchanged.
func (s Setting) Canonical(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if s.Kind != KindEnum || raw == "" {
		return raw, true
	}
	for _, e := range s.Enum {
		if strings.EqualFold(raw, e) {
			return e, true
		}
		for _, a := range s.EnumAliases[e] {
			if strings.EqualFold(raw, a) {
				return e, true
			}
		}
	}
	return raw, false
}

// EnumWords renders the accepted values with their aliases, for help text and
// error messages: "a (also x, y), b, c".
func (s Setting) EnumWords() string {
	parts := make([]string, 0, len(s.Enum))
	for _, e := range s.Enum {
		if al := s.EnumAliases[e]; len(al) > 0 {
			e += " (also " + strings.Join(al, ", ") + ")"
		}
		parts = append(parts, e)
	}
	return strings.Join(parts, ", ")
}
