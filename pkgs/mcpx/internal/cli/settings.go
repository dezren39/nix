package cli

import (
	"encoding/json"
	"fmt"
	"os"
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
				continue
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
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
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
