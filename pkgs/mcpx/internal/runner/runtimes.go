package runner

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dezren39/mcpx/internal/defaults"
)

// Runtime kinds. A kind is what mcpx knows how to build an argv for; a
// runtime is a binary of some kind, which may be the one on PATH or one
// somebody declared under script.runtimes.
const (
	KindDeno = "deno"
	KindBun  = "bun"
	KindNode = "node"
)

var kinds = []string{KindDeno, KindBun, KindNode}

func knownKind(k string) bool {
	for _, c := range kinds {
		if c == k {
			return true
		}
	}
	return false
}

// RawPrefix introduces flags passed to the runtime verbatim.
const RawPrefix = "raw:"

// Runtime is a resolved JavaScript runtime.
type Runtime struct {
	// Name is what was asked for: deno, a declared name, or a path.
	Name string
	// Kind decides how argv is built.
	Kind string
	Bin  string
	// Perms are the rendered permission flags.
	Perms []string
	Args  func(script string) []string
}

// RuntimeDef declares a runtime under script.runtimes.
type RuntimeDef struct {
	Kind string `json:"kind"`
	// Bin is a path or a name looked up on PATH; empty means the name it
	// was declared under.
	Bin string `json:"bin,omitempty"`
	// Args are runtime options placed ahead of the permission flags.
	Args []string `json:"args,omitempty"`
}

// Profile maps a runtime kind to the flags that enforce the profile there.
// A kind that is absent cannot enforce it. An empty list is a statement that
// the runtime needs no flags, which is only true of a profile granting
// everything.
type Profile map[string][]string

// Setup is the runtime configuration a run resolves against. The zero value
// means the built-ins.
type Setup struct {
	Runtimes map[string]RuntimeDef
	// Order is what auto tries; empty means defaults.RuntimeOrder.
	Order []string
	// Profiles are the user's; each replaces a built-in of the same name.
	Profiles map[string]Profile
}

// ParseSetup reads the three settings that make up a Setup. Each is JSON
// (or, for order, a list) as the settings layer holds it; empty is fine.
func ParseSetup(runtimesJSON string, order []string, profilesJSON string) (Setup, error) {
	var s Setup
	if strings.TrimSpace(runtimesJSON) != "" {
		if err := json.Unmarshal([]byte(runtimesJSON), &s.Runtimes); err != nil {
			return s, fmt.Errorf("script.runtimes: want an object of {kind, bin, args}: %w", err)
		}
		for name, d := range s.Runtimes {
			if !knownKind(d.Kind) {
				return s, fmt.Errorf("script.runtimes.%s: kind %q is not one of %s",
					name, d.Kind, strings.Join(kinds, ", "))
			}
			if err := checkFlags(d.Args); err != nil {
				return s, fmt.Errorf("script.runtimes.%s.args: %w", name, err)
			}
		}
	}
	s.Order = order
	p, err := ParseProfiles([]byte(profilesJSON))
	if err != nil {
		return s, fmt.Errorf("script.profiles: %w", err)
	}
	s.Profiles = p
	return s, nil
}

// ParseProfiles reads a profile map. A profile is either an object of
// kind -> flag list, or a string of raw flags applied to every kind.
func ParseProfiles(b []byte) (map[string]Profile, error) {
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil, nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("want an object of profiles: %w", err)
	}
	out := make(map[string]Profile, len(raw))
	for name, v := range raw {
		if strings.ContainsAny(name, ", ") || strings.HasPrefix(name, RawPrefix) {
			return nil, fmt.Errorf("profile name %q may not contain a comma or space or start with %s", name, RawPrefix)
		}
		var str string
		if json.Unmarshal(v, &str) == nil {
			flags := strings.Fields(str)
			if err := checkFlags(flags); err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			p := Profile{}
			for _, k := range kinds {
				p[k] = flags
			}
			out[name] = p
			continue
		}
		var p Profile
		if err := json.Unmarshal(v, &p); err != nil {
			return nil, fmt.Errorf("%s: want a string of flags or an object of kind -> [flags]", name)
		}
		for k, flags := range p {
			if !knownKind(k) {
				return nil, fmt.Errorf("%s: kind %q is not one of %s", name, k, strings.Join(kinds, ", "))
			}
			if err := checkFlags(flags); err != nil {
				return nil, fmt.Errorf("%s.%s: %w", name, k, err)
			}
		}
		out[name] = p
	}
	return out, nil
}

// checkFlags insists every entry is a flag. A bare word in a flag list is
// almost always a path that lost its --allow-read=, and the runtime would
// take it for the script.
func checkFlags(flags []string) error {
	for _, f := range flags {
		if !strings.HasPrefix(f, "-") {
			return fmt.Errorf("%q is not a flag; every entry must start with -", f)
		}
	}
	return nil
}

var builtinProfiles = func() map[string]Profile {
	p, err := ParseProfiles(defaults.ProfilesJSON)
	if err != nil {
		panic("defaults.json script.profiles: " + err.Error())
	}
	return p
}()

// profile looks a name up, the user's first.
func (s Setup) profile(name string) (Profile, bool) {
	if p, ok := s.Profiles[name]; ok {
		return p, true
	}
	p, ok := builtinProfiles[name]
	return p, ok
}

// ProfileNames lists every profile available, for messages.
func (s Setup) ProfileNames() []string {
	seen := map[string]bool{}
	for n := range builtinProfiles {
		seen[n] = true
	}
	for n := range s.Profiles {
		seen[n] = true
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// permStep is one element of a permissions spec: a profile name or raw flags.
type permStep struct {
	name  string
	raw   []string
	isRaw bool
}

// parseSpec splits "a,b,raw:--x --y". raw: consumes the rest of the spec,
// commas included, because a flag value may contain one.
func parseSpec(spec string) ([]permStep, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		spec = defaults.Permissions
	}
	var steps []permStep
	for spec != "" {
		if strings.HasPrefix(spec, RawPrefix) {
			flags := strings.Fields(strings.TrimPrefix(spec, RawPrefix))
			if len(flags) == 0 {
				return nil, fmt.Errorf("permissions: %s with no flags", RawPrefix)
			}
			if err := checkFlags(flags); err != nil {
				return nil, fmt.Errorf("permissions %s: %w", RawPrefix, err)
			}
			steps = append(steps, permStep{raw: flags, isRaw: true})
			break
		}
		name := spec
		if i := strings.IndexByte(spec, ','); i >= 0 {
			name, spec = spec[:i], strings.TrimSpace(spec[i+1:])
		} else {
			spec = ""
		}
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		if strings.HasPrefix(name, "-") || strings.ContainsAny(name, " \t") {
			return nil, fmt.Errorf("permissions %q is not a profile name; pass runtime flags "+
				"explicitly as %s%s", name, RawPrefix, name)
		}
		steps = append(steps, permStep{name: name})
	}
	return steps, nil
}

// renderPerms composes the steps for one runtime kind.
//
// Profiles apply in order. The result is the union of their flags; where two
// give the same flag (the part before =) with different values, the later
// one wins, in the position the flag first appeared. A named profile with no
// entry for the kind is refused: the runtime cannot enforce it.
func (s Setup) renderPerms(steps []permStep, kind string) ([]string, *refusal, error) {
	var out []string
	at := map[string]int{}
	add := func(f string) {
		key := f
		if i := strings.IndexByte(f, '='); i >= 0 {
			key = f[:i]
		}
		if i, ok := at[key]; ok {
			out[i] = f
			return
		}
		at[key] = len(out)
		out = append(out, f)
	}
	for _, st := range steps {
		flags := st.raw
		if !st.isRaw {
			p, ok := s.profile(st.name)
			if !ok {
				return nil, nil, fmt.Errorf("no permission profile %q; known: %s, or raw flags as %s--flag",
					st.name, strings.Join(s.ProfileNames(), ", "), RawPrefix)
			}
			f, ok := p[kind]
			if !ok {
				return nil, &refusal{profile: st.name, kind: kind, enforcedBy: p}, nil
			}
			flags = f
		}
		for _, f := range flags {
			add(f)
		}
	}
	return out, nil, nil
}

type refusal struct {
	profile    string
	kind       string
	enforcedBy Profile
}

func (r *refusal) msg(name string) string {
	var by []string
	for k := range r.enforcedBy {
		by = append(by, k)
	}
	sort.Strings(by)
	what := name
	if name != r.kind {
		what = fmt.Sprintf("%s (a %s runtime)", name, r.kind)
	}
	hint := "no runtime defines it"
	if len(by) > 0 {
		hint = "it is defined for " + strings.Join(by, ", ")
	}
	model := ""
	if r.kind == KindBun {
		model = "; bun has no permission model, so nothing can be defined for it that would be enforced"
	}
	return fmt.Sprintf("permission profile %q defines no flags for %s, so %s cannot enforce it "+
		"(%s); use --runtime %s, or define script.profiles.%s.%s%s",
		r.profile, r.kind, what, hint, firstOr(by, "deno"), r.profile, r.kind, model)
}

func firstOr(l []string, d string) string {
	if len(l) > 0 {
		return l[0]
	}
	return d
}

// candidate is a runtime to try: its name, kind, binary and extra args.
type candidate struct {
	name, kind, bin string
	args            []string
}

// lookup turns a requested name into a candidate.
//
// A declared name wins. Otherwise a kind name is that binary on PATH, and
// anything else -- a path, or a name on PATH such as deno2 -- is accepted
// when its basename, without any executable extension, is a kind. Resolution
// goes through exec.LookPath, which is what knows about PATHEXT on Windows.
func (s Setup) lookup(name string) (candidate, error) {
	if d, ok := s.Runtimes[name]; ok {
		c := candidate{name: name, kind: d.Kind, bin: d.Bin, args: d.Args}
		if c.bin == "" {
			c.bin = name
		}
		bin, err := exec.LookPath(c.bin)
		if err != nil {
			return c, fmt.Errorf("runtime %s: %s not found: %w", name, c.bin, err)
		}
		c.bin = bin
		return c, nil
	}
	bin, err := exec.LookPath(name)
	if err != nil {
		return candidate{name: name}, fmt.Errorf("%s not found", name)
	}
	if knownKind(name) {
		return candidate{name: name, kind: name, bin: bin}, nil
	}
	base := filepath.Base(bin)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	if knownKind(strings.ToLower(stem)) {
		return candidate{name: name, kind: strings.ToLower(stem), bin: bin}, nil
	}
	return candidate{name: name}, fmt.Errorf("%s resolves to %s, which is not a known JavaScript "+
		"runtime; declare it under script.runtimes with a kind (%s)",
		name, bin, strings.Join(kinds, ", "))
}

// Resolve picks a runtime and renders the permission spec for it.
//
// An explicit preference is the only candidate. auto tries Setup.Order and
// skips any runtime that cannot enforce the permissions: falling back to one
// that would ignore a narrowed profile is the bug decisions/0003 records.
func Resolve(prefer, permSpec string, s Setup) (*Runtime, error) {
	steps, err := parseSpec(permSpec)
	if err != nil {
		return nil, err
	}
	explicit := prefer != "" && prefer != "auto"
	names := s.Order
	if len(names) == 0 {
		names = defaults.RuntimeOrder
	}
	if explicit {
		names = []string{prefer}
	}
	var tried []string
	for _, name := range names {
		c, lerr := s.lookup(name)
		if lerr != nil {
			if explicit {
				return nil, lerr
			}
			tried = append(tried, lerr.Error())
			continue
		}
		perms, ref, perr := s.renderPerms(steps, c.kind)
		if perr != nil {
			return nil, perr
		}
		if ref != nil {
			if explicit {
				return nil, fmt.Errorf("%s", ref.msg(c.name))
			}
			tried = append(tried, ref.msg(c.name))
			continue
		}
		return build(c, perms), nil
	}
	return nil, fmt.Errorf("no usable JavaScript runtime (tried %s):\n  %s",
		strings.Join(names, ", "), strings.Join(tried, "\n  "))
}

func build(c candidate, perms []string) *Runtime {
	rt := &Runtime{Name: c.name, Kind: c.kind, Bin: c.bin, Perms: perms}
	switch c.kind {
	case KindDeno:
		rt.Args = func(script string) []string {
			// --no-check skips type checking: the generated client is
			// machine-written and already correct, and a type error in the
			// agent's script surfaces at runtime anyway.
			a := []string{"run", "--quiet", "--no-check"}
			a = append(a, c.args...)
			a = append(a, perms...)
			return append(a, script)
		}
	case KindBun:
		rt.Args = func(script string) []string {
			a := append([]string{"run"}, c.args...)
			a = append(a, perms...)
			return append(a, script)
		}
	case KindNode:
		rt.Args = func(script string) []string {
			a := []string{"--no-warnings", "--experimental-strip-types"}
			a = append(a, c.args...)
			a = append(a, perms...)
			return append(a, script)
		}
	}
	return rt
}
