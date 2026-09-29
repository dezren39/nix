package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// This file is the one place mcpx edits a configuration file.
//
// There used to be exactly one writer, buried in `mcpx registry add`, and it
// did the right thing: read, modify in place, write back, so comments and
// ordering a person put there deliberately survive. Now that the daemon can
// add a server over /v1 and `mcpx settings set` can persist a knob, the same
// care has to apply to three callers, and three copies of read-modify-write
// would be three chances to truncate somebody's file.

// WriteScope says which configuration file a change is written to.
type WriteScope string

const (
	// ScopeProject is the nearest configuration file: the one this
	// repository or directory already uses, or a new .mcpx.json here.
	ScopeProject WriteScope = "project"
	// ScopeUser is the per-user file, which applies everywhere.
	ScopeUser WriteScope = "user"
)

// UserConfigPath is where a user-level setting is written.
//
// XDG_CONFIG_HOME when it is set, because a user who set it meant it, and
// ~/.config/mcpx/config.json otherwise. Both are already on the search path,
// so a file written here is a file that will be read.
func UserConfigPath() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "mcpx", "config.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("no home directory, so there is nowhere to write a user setting: %w", err)
	}
	return filepath.Join(home, ".config", "mcpx", "config.json"), nil
}

// ProjectConfigPath is the nearest existing configuration file, or the name a
// new one should take here.
//
// Preferring an existing file matters: writing .mcpx.json next to a project
// that already keeps its configuration in .config/mcpx/config.json would
// create a second file that silently outranks the first.
func ProjectConfigPath(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if wd, err := os.Getwd(); err == nil {
		for _, name := range []string{
			filepath.Join(".config", "mcpx", "config.json"),
			".mcpx.json",
			filepath.Join(".mcpx", "config.json"),
			".mcp.json",
		} {
			p := filepath.Join(wd, name)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ".mcpx.json"
}

// ResolveWritePath picks the file a scope names.
func ResolveWritePath(scope WriteScope, explicit string) (string, error) {
	switch scope {
	case ScopeUser:
		return UserConfigPath()
	case ScopeProject, "":
		return ProjectConfigPath(explicit), nil
	}
	return "", fmt.Errorf("no such config scope %q; want project or user", scope)
}

// Edit reads a configuration file, hands the decoded document to fn, and
// writes it back if fn reports a change.
//
// A file that exists and is not JSON is an error rather than something to
// overwrite. Rewriting it would discard whatever the person actually had
// there, and the mistake is theirs to see rather than ours to bury.
func Edit(path string, fn func(doc map[string]any) (bool, error)) error {
	doc := map[string]any{}
	b, rerr := os.ReadFile(path)
	if rerr == nil {
		if err := json.Unmarshal(StripJSONC(b), &doc); err != nil {
			return fmt.Errorf("%s is not valid JSON, so mcpx will not rewrite it: %w", path, err)
		}
	} else if !os.IsNotExist(rerr) {
		return rerr
	}
	changed, err := fn(doc)
	if err != nil || !changed {
		return err
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, DirMode); err != nil {
			return err
		}
	}
	return os.WriteFile(path, append(out, '\n'), FileMode)
}

// SetKey writes a dotted setting path into a document, creating the
// intermediate objects.
func SetKey(path string, dotted string, value any) error {
	return Edit(path, func(doc map[string]any) (bool, error) {
		parts := strings.Split(dotted, ".")
		node := doc
		for _, p := range parts[:len(parts)-1] {
			next, _ := node[p].(map[string]any)
			if next == nil {
				next = map[string]any{}
				node[p] = next
			}
			node = next
		}
		node[parts[len(parts)-1]] = value
		return true, nil
	})
}

// DeleteKey removes a dotted setting path, and any object it leaves empty.
//
// Leaving `"logging": {}` behind is not wrong, but a file that accumulates
// empty objects every time somebody unsets something reads as neglected.
func DeleteKey(path string, dotted string) (bool, error) {
	removed := false
	err := Edit(path, func(doc map[string]any) (bool, error) {
		parts := strings.Split(dotted, ".")
		chain := []map[string]any{doc}
		node := doc
		for _, p := range parts[:len(parts)-1] {
			next, _ := node[p].(map[string]any)
			if next == nil {
				return false, nil
			}
			node = next
			chain = append(chain, next)
		}
		if _, ok := node[parts[len(parts)-1]]; !ok {
			return false, nil
		}
		delete(node, parts[len(parts)-1])
		removed = true
		for i := len(chain) - 1; i > 0; i-- {
			if len(chain[i]) == 0 {
				delete(chain[i-1], parts[i-1])
			}
		}
		return true, nil
	})
	return removed, err
}

// AddServer inserts one server entry.
//
// Refusing a name the file already defines rather than replacing it: a silent
// overwrite of a server somebody configured by hand is the kind of loss that
// is only noticed later, when the thing that used it stops working.
func AddServer(path, name string, entry map[string]any, replace bool) error {
	return Edit(path, func(doc map[string]any) (bool, error) {
		servers, _ := doc["mcpServers"].(map[string]any)
		if servers == nil {
			servers = map[string]any{}
		}
		if _, exists := servers[name]; exists && !replace {
			return false, fmt.Errorf("%s already defines %q; remove it first or pick another name",
				path, name)
		}
		servers[name] = entry
		doc["mcpServers"] = servers
		return true, nil
	})
}

// RemoveServer drops one server entry, reporting whether it was there.
func RemoveServer(path, name string) (bool, error) {
	found := false
	err := Edit(path, func(doc map[string]any) (bool, error) {
		servers, _ := doc["mcpServers"].(map[string]any)
		if servers == nil {
			return false, nil
		}
		if _, exists := servers[name]; !exists {
			return false, nil
		}
		delete(servers, name)
		doc["mcpServers"] = servers
		found = true
		return true, nil
	})
	return found, err
}
