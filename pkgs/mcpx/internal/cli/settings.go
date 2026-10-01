package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/settings"
)

// Settings resolves every setting for this invocation.
//
// The order is defaults, then configuration files with the nearest last, then
// the environment, then flags. Flags are folded in by the command that owns
// them; everything before that happens once, here, and is cached because the
// answer cannot change within a single run.
//
// Doing this in one place is the point. Reading a setting used to mean
// knowing which of four mechanisms carried it, and half the knobs were
// reachable from only one.
func (a *App) Settings() *settings.Set {
	a.settingsOnce.Do(func() {
		sch, err := settings.New(settings.Registry())
		if err != nil {
			// Impossible unless the registry itself is malformed, which a
			// test catches. Failing loudly beats every later read silently
			// returning a zero value.
			panic(fmt.Sprintf("mcpx: settings registry is invalid: %v", err))
		}
		set := settings.NewSet(sch)

		// Configuration files, furthest first so the nearest ends up highest.
		files := configFilesFarthestFirst(a.ConfigPath)
		for i, path := range files {
			doc, rerr := readJSONFile(path)
			if rerr != nil {
				// A file that was found and cannot be read is an error, not a
				// reason to carry on with defaults. Skipping it silently made
				// a malformed config look accepted: the run proceeded, the
				// settings were the built-in ones, and nothing said why.
				//
				// The exception is a file that has since disappeared. The
				// list comes from a directory walk, so a file removed between
				// the walk and the read is a race rather than a mistake.
				if errors.Is(rerr, fs.ErrNotExist) {
					continue
				}
				a.settingsErr = fmt.Errorf("reading %s: %w", path, rerr)
				return
			}
			if aerr := sch.ApplyFile(set, doc, path, i); aerr != nil {
				a.settingsErr = aerr
				return
			}
		}
		if eerr := sch.ApplyEnv(set, settings.Environ()); eerr != nil {
			a.settingsErr = eerr
			return
		}
		if cerr := set.CheckRequirements(); cerr != nil {
			a.settingsErr = cerr
			return
		}
		// plumbing.strictUnknownKeys turns a key no setting claims from a
		// note `mcpx doctor` prints into a refusal. It is read from the set
		// that was just built, which is the only order that works: the switch
		// itself lives in the file being judged.
		if set.Bool("plumbing.strictUnknownKeys") {
			if uerr := unknownKeysError(set.Unknown()); uerr != nil {
				a.settingsErr = uerr
				return
			}
			// The same rule for the server entries, which the settings
			// layer does not read: a key there that mcpx ignores is either
			// another host's or a mistake, and strict mode is the request
			// to be told which.
			if cfg, cerr := config.Load(a.ConfigPath); cerr == nil && len(cfg.Ignored) > 0 {
				a.settingsErr = ignoredKeysError(cfg.Ignored)
				return
			}
		}
		a.settings = set
		a.settingsSchema = sch
	})
	if a.settings == nil {
		// A failure here is reported by SettingsErr; returning an empty set
		// keeps every caller from having to handle an error it cannot act on.
		sch, _ := settings.New(settings.Registry())
		return settings.NewSet(sch)
	}
	return a.settings
}

// SettingsErr reports a problem found while resolving settings.
func (a *App) SettingsErr() error {
	a.Settings()
	return a.settingsErr
}

// Plumbing reads an internal switch.
func (a *App) Plumbing(path string) bool { return a.Settings().Bool(path) }

// BindFlags registers every setting that applies to a command and has not
// already been declared by hand, then returns a function to fold what was
// given into the resolved set.
//
// Without this the registry would describe flags the command does not accept,
// which is worse than having no registry: `mcpx config --schema` and the man
// page would both promise a flag that fails. That was the state after the
// registry landed and before this call site existed, and it was found by
// trying one.
func (a *App) BindFlags(fs *flag.FlagSet, cmd string) func() error {
	_, apply := a.bindFlags(fs, cmd)
	return apply
}

func (a *App) bindFlags(fs *flag.FlagSet, cmd string) (*settings.Binding, func() error) {
	sch, err := settings.New(settings.Registry())
	if err != nil {
		return nil, func() error { return err }
	}
	b := sch.Bind(fs, cmd)
	return b, func() error {
		if err := b.ApplyTo(a.Settings()); err != nil {
			return err
		}
		return a.Settings().CheckRequirements()
	}
}

// applyPresets parses the flags of every selected preset into fs, in order,
// before the command line is parsed.
//
// Parsing them first is what makes an explicit flag win: a hand-written flag
// is simply set again by the command line, and a registry flag is recorded at
// the preset layer, below the flag layer. A preset flag the command does not
// accept is skipped, because one preset is meant to serve several commands.
func (a *App) applyPresets(fs *flag.FlagSet, b *settings.Binding, args []string) error {
	names, given := scanFlag(fs, args, "preset")
	var list []string
	if given {
		list = splitAll(strings.Split(names, ","))
	} else {
		list = a.Settings().List("preset")
	}
	if len(list) == 0 {
		return nil
	}
	defs, err := parsePresets(a.Settings().String("presets"))
	if err != nil {
		return err
	}
	for _, name := range list {
		flags, ok := defs[name]
		if !ok {
			known := make([]string, 0, len(defs))
			for n := range defs {
				known = append(known, n)
			}
			sort.Strings(known)
			return fmt.Errorf("no preset %q; defined: %s", name, quoteEmpty(strings.Join(known, ", ")))
		}
		var keep []string
		for _, f := range flags {
			fname := strings.TrimLeft(f, "-")
			if i := strings.IndexByte(fname, '='); i >= 0 {
				fname = fname[:i]
			}
			if fname == "preset" || fname == "presets" {
				return fmt.Errorf("preset %s: a preset cannot select presets", name)
			}
			if fs.Lookup(fname) == nil {
				continue
			}
			keep = append(keep, f)
		}
		if b != nil {
			b.FromPreset(name)
		}
		perr := fs.Parse(keep)
		if b != nil {
			b.FromPreset("")
		}
		if perr != nil {
			return fmt.Errorf("preset %s: %w", name, perr)
		}
	}
	return nil
}

// parsePresets reads the presets setting: name -> list of single-token flags.
func parsePresets(raw string) (map[string][]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var defs map[string][]string
	if err := json.Unmarshal([]byte(raw), &defs); err != nil {
		return nil, fmt.Errorf("presets: want an object of name -> [\"--flag=value\", ...]: %w", err)
	}
	for name, flags := range defs {
		for _, f := range flags {
			// One token per entry. A value given as the next entry would be
			// taken for a positional argument the moment its flag is skipped.
			if !strings.HasPrefix(f, "-") {
				return nil, fmt.Errorf("presets.%s: %q is not a flag; write a value as --flag=value", name, f)
			}
		}
	}
	return defs, nil
}

// scanFlag finds one flag's value in args without disturbing fs, by parsing
// a copy that has the same flags and the same notion of which are boolean.
// Anything simpler misreads `--timeout 5s --preset ci` or a script argument.
func scanFlag(fs *flag.FlagSet, args []string, name string) (string, bool) {
	scratch := flag.NewFlagSet("scan", flag.ContinueOnError)
	scratch.SetOutput(io.Discard)
	scratch.Usage = func() {}
	var val string
	var seen bool
	fs.VisitAll(func(f *flag.Flag) {
		bf, _ := f.Value.(interface{ IsBoolFlag() bool })
		sv := &scanValue{boolean: bf != nil && bf.IsBoolFlag()}
		if f.Name == name {
			sv.set = func(v string) { val, seen = v, true }
		}
		scratch.Var(sv, f.Name, "")
	})
	_ = scratch.Parse(args)
	return val, seen
}

type scanValue struct {
	boolean bool
	set     func(string)
}

func (s *scanValue) String() string { return "" }
func (s *scanValue) Set(v string) error {
	if s.set != nil {
		s.set(v)
	}
	return nil
}
func (s *scanValue) IsBoolFlag() bool { return s.boolean }

// unknownKeysError turns the collected unknown keys into one message naming
// every file, because a config split across three files that each contain a
// typo should be fixed in one pass rather than three runs.
func unknownKeysError(unknown []settings.UnknownKeys) error {
	if len(unknown) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("configuration keys no setting claims")
	for _, u := range unknown {
		fmt.Fprintf(&b, "\n  %s: %s", u.File, strings.Join(u.Keys, ", "))
	}
	b.WriteString("\n(run `mcpx config --schema` for the list, or turn " +
		"plumbing.strictUnknownKeys off)")
	return errors.New(b.String())
}

func ignoredKeysError(ignored []config.IgnoredKey) error {
	var b strings.Builder
	b.WriteString("server keys mcpx does not read")
	for _, k := range ignored {
		fmt.Fprintf(&b, "\n  %s: %s.%s", k.File, k.Server, k.Key)
	}
	b.WriteString("\n(mcpx-specific options go in each server's \"mcpx\" block; " +
		"or turn plumbing.strictUnknownKeys off)")
	return errors.New(b.String())
}

func configFilesFarthestFirst(explicit string) []string {
	if explicit != "" {
		return []string{explicit}
	}
	search := config.SearchPath()
	// SearchPath is nearest-first, because that is the order a lookup wants.
	// Layering wants the opposite, so that the nearest file is applied last
	// and therefore wins.
	out := make([]string, 0, len(search))
	for i := len(search) - 1; i >= 0; i-- {
		if _, err := os.Stat(search[i]); err == nil {
			out = append(out, search[i])
		}
	}
	return out
}

func readJSONFile(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// The same comment stripping the main config loader uses, so a file that
	// works there does not fail here for having a comment in it.
	var doc map[string]any
	if err := json.Unmarshal(config.StripJSONC(b), &doc); err != nil {
		return nil, fmt.Errorf("not valid JSON: %w", err)
	}
	return doc, nil
}

// settingsState is embedded in App.
type settingsState struct {
	settingsOnce   sync.Once
	settings       *settings.Set
	settingsSchema *settings.Schema
	settingsErr    error
}
