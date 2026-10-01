package settings

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Binding is a flag set generated from the schema, plus the machinery to read
// what was actually given back out.
type Binding struct {
	schema *Schema
	fs     *flag.FlagSet
	// seen records which spelling set which path, so that two aliases of one
	// setting can be reported as the conflict they are rather than silently
	// racing.
	seen map[string][]spelling
	// preset names the preset whose flags are being parsed, or "", and
	// presets counts them so a later preset ranks above an earlier one.
	preset  string
	presets int
}

type spelling struct {
	flagName string
	raw      string
	preset   string
	rank     int
}

// FromPreset marks the flags parsed from now on as coming from the named
// preset; "" returns to the command line.
func (b *Binding) FromPreset(name string) {
	b.preset = name
	if name != "" {
		b.presets++
	}
}

// flagValue adapts one setting to flag.Value.
type flagValue struct {
	set *Setting
	// name is the spelling this instance was registered under, so the
	// conflict message can say which alias was used.
	name string
	b    *Binding
}

func (f *flagValue) String() string {
	if f == nil || f.set == nil {
		return ""
	}
	return f.set.Default
}

func (f *flagValue) Set(v string) error {
	// A bare boolean-style flag arrives as "true". For a setting that
	// declares a Bare value, that is the signal to use it -- this is how
	// `--log-source` alone means "all of it" while `--log-source=warn` names
	// a level.
	if f.set.Bare != "" && strings.EqualFold(v, "true") {
		v = f.set.Bare
	}
	f.b.seen[f.set.Path] = append(f.b.seen[f.set.Path], spelling{flagName: f.name, raw: v,
		preset: f.b.preset, rank: f.b.presets})
	return nil
}

// IsBoolFlag lets a flag stand alone when the setting is a boolean or
// declares a bare form.
func (f *flagValue) IsBoolFlag() bool {
	return f.set.Kind == KindBool || f.set.Bare != ""
}

// Bind generates the flags a subcommand accepts.
//
// A name already registered on the FlagSet is skipped rather than replaced.
// Commands still declare some flags by hand, and those hand-written ones own
// their spelling; without the skip, Go's flag package panics on the
// duplicate. The effect is that the registry fills in everything the command
// did not already provide, which is what makes `mcpx config --schema` true
// rather than aspirational.
//
// Skipping the registration must not mean skipping the setting. A hand-written
// flag with a setting's spelling is wrapped so that whatever it is given also
// reaches the resolved set; before that, `mcpx exec --runtime deno` reached the
// local variable and left script.runtime at auto for everything that read the
// set instead (#231). except names hand-written flags that share a spelling
// but mean something else -- `--keep` on exec keeps temp files, not log files --
// and are left alone.
func (s *Schema) Bind(fs *flag.FlagSet, cmd string) *Binding {
	return s.BindExcept(fs, cmd, nil)
}

// BindExcept is Bind with a list of hand-written spellings that are not the
// setting they collide with.
func (s *Schema) BindExcept(fs *flag.FlagSet, cmd string, except map[string]bool) *Binding {
	b := &Binding{schema: s, fs: fs, seen: map[string][]spelling{}}
	for _, set := range s.ForCommand(cmd) {
		p := set
		for _, name := range append([]string{p.FlagName()}, p.FlagAliases...) {
			if f := fs.Lookup(name); f != nil {
				if _, already := f.Value.(*teeValue); !already && !except[name] {
					f.Value = &teeValue{Value: f.Value, also: &flagValue{set: &p, name: name, b: b}}
				}
				continue
			}
			usage := p.Short
			if p.Plumbing {
				// Marked rather than hidden. Go's flag package has no notion
				// of a hidden flag, and inventing one would mean a user who
				// reads the source finds a flag that `--help` denies exists.
				usage = "[plumbing] " + usage
			}
			fs.Var(&flagValue{set: &p, name: name, b: b}, name, usage)
		}
	}
	return b
}

// teeValue keeps a hand-written flag's own behaviour and also records the
// value against the setting it shares a name with.
type teeValue struct {
	flag.Value
	also *flagValue
}

func (t *teeValue) Set(v string) error {
	if err := t.Value.Set(v); err != nil {
		return err
	}
	return t.also.Set(v)
}

// String tolerates the zero value, which the flag package builds by
// reflection to decide whether a default is worth printing.
func (t *teeValue) String() string {
	if t == nil || t.Value == nil {
		return ""
	}
	return t.Value.String()
}

func (t *teeValue) IsBoolFlag() bool {
	if t == nil || t.Value == nil {
		return false
	}
	bf, ok := t.Value.(interface{ IsBoolFlag() bool })
	return ok && bf.IsBoolFlag()
}

// ApplyTo folds everything the command line gave into a set.
func (b *Binding) ApplyTo(s *Set) error {
	paths := make([]string, 0, len(b.seen))
	for p := range b.seen {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, path := range paths {
		var presetGiven, flagGiven []spelling
		for _, g := range b.seen[path] {
			if g.preset != "" {
				presetGiven = append(presetGiven, g)
			} else {
				flagGiven = append(flagGiven, g)
			}
		}
		// Presets first, so the command line lands above them and records
		// them as what it overrode.
		// One preset at a time, ranked by order, so a later preset wins and
		// the earlier one is recorded as what it overrode.
		for len(presetGiven) > 0 {
			n := 1
			for n < len(presetGiven) && presetGiven[n].rank == presetGiven[0].rank {
				n++
			}
			if err := b.apply(s, path, presetGiven[:n], LayerPreset); err != nil {
				return err
			}
			presetGiven = presetGiven[n:]
		}
		if len(flagGiven) > 0 {
			if err := b.apply(s, path, flagGiven, LayerFlag); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *Binding) apply(s *Set, path string, given []spelling, layer Layer) error {
	set, _ := b.schema.Lookup(path)
	detail := func(g spelling) string {
		if g.preset != "" {
			return g.preset + " (--" + g.flagName + ")"
		}
		return "--" + g.flagName
	}
	// Two different spellings of one setting on one command line is the
	// conflict worth refusing. The same spelling twice is not -- that is
	// a person editing their own command, and the last one is what they
	// meant. Presets compose by order, so among them the later one wins
	// whatever it was spelled.
	if len(given) > 1 && !set.Repeatable && layer == LayerFlag {
		distinct := map[string]bool{}
		for _, g := range given {
			distinct[g.flagName] = true
		}
		if len(distinct) > 1 {
			names := make([]string, 0, len(distinct))
			for n := range distinct {
				names = append(names, "--"+n)
			}
			sort.Strings(names)
			return fmt.Errorf("%s given as both %s; they are the same setting, "+
				"so there is no order to pick", path, strings.Join(names, " and "))
		}
	}
	if set.Repeatable {
		parts := make([]string, 0, len(given))
		for _, g := range given {
			if g.raw == "-" {
				parts = append(parts, NullMarker)
				continue
			}
			parts = append(parts, g.raw)
		}
		return s.Apply(path, encodeList(parts), Origin{Layer: layer, Detail: detail(given[0]), Rank: given[0].rank})
	}
	last := given[len(given)-1]
	return s.Apply(path, last.raw, Origin{Layer: layer, Detail: detail(last), Rank: last.rank})
}

func encodeList(parts []string) string {
	arr := make([]any, len(parts))
	for i, p := range parts {
		if p == NullMarker {
			arr[i] = nil
			continue
		}
		arr[i] = p
	}
	return mustJSON(arr)
}

// ApplyEnv folds the environment in.
//
// Every setting has a derived MCPX_ variable whether or not anyone uses it, so
// there is never a knob reachable from a file but not from the environment.
// Aliases exist for the handful whose derived name is unbearable.
func (s *Schema) ApplyEnv(set *Set, environ []string) error {
	type hit struct {
		name string
		val  string
	}
	byPath := map[string][]hit{}
	for _, kv := range environ {
		i := strings.IndexByte(kv, '=')
		if i < 0 {
			continue
		}
		name, val := kv[:i], kv[i+1:]
		decl, ok := s.ByEnv(name)
		if !ok {
			continue
		}
		byPath[decl.Path] = append(byPath[decl.Path], hit{name: name, val: val})
	}
	paths := make([]string, 0, len(byPath))
	for p := range byPath {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, path := range paths {
		hits := byPath[path]
		if len(hits) > 1 {
			// Unlike a command line, the environment has no order. Two
			// variables meaning one setting genuinely cannot be resolved, so
			// say so instead of picking.
			sort.Slice(hits, func(i, j int) bool { return hits[i].name < hits[j].name })
			names := make([]string, len(hits))
			for i, h := range hits {
				names[i] = h.name
			}
			distinct := map[string]bool{}
			for _, h := range hits {
				distinct[h.val] = true
			}
			if len(distinct) > 1 {
				return fmt.Errorf("%s set by %s with different values; the environment "+
					"has no order, so there is nothing to prefer -- unset one",
					path, strings.Join(names, " and "))
			}
		}
		if err := set.Apply(path, hits[0].val, Origin{
			Layer: LayerEnv, Detail: hits[0].name,
		}); err != nil {
			return err
		}
	}
	return nil
}

// Environ is os.Environ, split out so tests can supply their own.
func Environ() []string { return os.Environ() }
