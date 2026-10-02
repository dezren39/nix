package settings

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// ApplyFile folds one configuration document in. Nested objects are flattened
// to dotted paths, so `{"logging":{"level":"debug"}}` sets `logging.level`.
//
// rank orders files against each other: nearest should be highest.
func (s *Schema) ApplyFile(set *Set, doc map[string]any, path string, rank int) error {
	flat := map[string]string{}
	flatten("", doc, flat)

	keys := make([]string, 0, len(flat))
	for k := range flat {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var unknown []string
	for _, k := range keys {
		decl, ok := s.Lookup(k)
		if !ok {
			// A key that is only a branch -- "logging", when "logging.level"
			// is a setting -- is not unknown, it is the path to one. And a
			// section that is not settings at all, like mcpServers, is the
			// document's own content.
			//
			// Reporting those made the warning useless: it listed every
			// section and every server, so a real typo was invisible among
			// them.
			if s.isBranch(k) || notASetting(k) {
				continue
			}
			unknown = append(unknown, k)
			continue
		}
		if err := decl.Writable(); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if err := set.Apply(k, flat[k], Origin{
			Layer: LayerFile, Detail: path, Rank: rank,
		}); err != nil {
			return err
		}
	}
	if len(unknown) > 0 {
		// Reported, not fatal. A configuration file is shared with other
		// sections -- mcpServers among them -- and a strict reading would
		// reject documents that are perfectly valid.
		set.unknown = append(set.unknown, UnknownKeys{File: path, Keys: unknown})
	}
	return nil
}

// isBranch reports whether a key is the prefix of a real setting.
func (s *Schema) isBranch(key string) bool {
	prefix := key + "."
	for _, set := range s.settings {
		if strings.HasPrefix(set.Path, prefix) {
			return true
		}
	}
	return false
}

// notASetting names the top-level sections a configuration file holds that
// are content rather than configuration.
func notASetting(key string) bool {
	head := key
	if i := strings.IndexByte(key, '.'); i >= 0 {
		head = key[:i]
	}
	switch head {
	case "mcpServers", "servers", "adapters", "apis", "profiles",
		"$schema", "script", "plumbing", "presets":
		return true
	}
	return false
}

// UnknownKeys is a key in a config file that no setting claims.
type UnknownKeys struct {
	File string   `json:"file"`
	Keys []string `json:"keys"`
}

func flatten(prefix string, node map[string]any, out map[string]string) {
	for k, v := range node {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		switch t := v.(type) {
		case map[string]any:
			// A nested object is both a branch and, potentially, a leaf: a
			// setting may legitimately hold an object. Record it either way
			// and let the lookup decide.
			out[path] = mustJSON(t)
			flatten(path, t, out)
		case []any:
			out[path] = mustJSON(t)
		case nil:
			out[path] = ""
		case bool:
			out[path] = fmt.Sprint(t)
		case float64:
			if t == float64(int64(t)) {
				out[path] = fmt.Sprintf("%d", int64(t))
				continue
			}
			out[path] = fmt.Sprintf("%g", t)
		default:
			out[path] = fmt.Sprint(t)
		}
	}
}

// CheckRequirements runs every declared cross-field condition.
//
// These are the errors that would otherwise be silence: a setting that is
// syntactically fine, accepted without complaint, and then ignored because
// something else was not switched on. The user learns by noticing an absence,
// which is the worst way to learn anything.
func (s *Set) CheckRequirements() error {
	var problems []string
	for _, set := range s.schema.All() {
		v, ok := s.Value(set.Path)
		if !ok || v.Origin.Layer == LayerDefault {
			continue // not set by anyone; nothing to be inconsistent with
		}
		for _, req := range set.Requires {
			other, ok := s.Value(req.Path)
			if !ok {
				continue
			}
			bad := false
			if req.Equals != "" {
				bad = !strings.EqualFold(strings.TrimSpace(other.Raw), req.Equals)
			} else {
				bad = other.Origin.Layer == LayerDefault
			}
			if bad {
				msg := fmt.Sprintf("%s was set (%s) but %s is %q",
					set.Path, v.Origin, req.Path, other.Raw)
				if req.Equals != "" {
					msg += fmt.Sprintf(", not %q", req.Equals)
				}
				if req.Because != "" {
					msg += "; " + req.Because
				}
				problems = append(problems, msg)
			}
		}
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf("configuration is inconsistent:\n  %s", strings.Join(problems, "\n  "))
}

// Unknown returns keys in configuration files that no setting claims.
func (s *Set) Unknown() []UnknownKeys { return s.unknown }

// Describe renders the whole schema for `mcpx config --schema`.
func (s *Schema) Describe(includePlumbing bool) string {
	var b strings.Builder
	section := ""
	for _, set := range s.All() {
		if set.Plumbing && !includePlumbing {
			continue
		}
		if head := strings.SplitN(set.Path, ".", 2)[0]; head != section {
			section = head
			fmt.Fprintf(&b, "\n%s\n", strings.ToUpper(section))
		}
		tag := ""
		if set.Plumbing {
			tag = "  [plumbing]"
		}
		if set.Bootstrap {
			tag += "  [environment or --" + set.FlagName() + " only]"
		}
		fmt.Fprintf(&b, "  %-34s %-9s default %s%s\n", set.Path, set.Kind, quoteEmpty(set.Default), tag)
		if set.Short != "" {
			fmt.Fprintf(&b, "  %-34s %s\n", "", set.Short)
		}
		fmt.Fprintf(&b, "  %-34s --%s   %s\n", "", set.FlagName(), set.EnvName())
		if len(set.FlagAliases) > 0 || len(set.EnvAliases) > 0 {
			var alias []string
			for _, a := range set.FlagAliases {
				alias = append(alias, "--"+a)
			}
			alias = append(alias, set.EnvAliases...)
			fmt.Fprintf(&b, "  %-34s also %s\n", "", strings.Join(alias, ", "))
		}
		if len(set.Enum) > 0 {
			fmt.Fprintf(&b, "  %-34s one of %s\n", "", set.EnumWords())
		}
	}
	return strings.TrimLeft(b.String(), "\n")
}

func quoteEmpty(v string) string {
	if v == "" {
		return `""`
	}
	return v
}

// JSON renders the schema as data, for anything that wants to generate from
// it -- shell completion, an editor schema, documentation.
func (s *Schema) JSON(includePlumbing bool) string {
	type entry struct {
		Path       string              `json:"path"`
		Kind       string              `json:"kind"`
		Default    string              `json:"default"`
		Name       string              `json:"name,omitempty"`
		Short      string              `json:"short,omitempty"`
		Long       string              `json:"long,omitempty"`
		Flag       string              `json:"flag"`
		FlagAlias  []string            `json:"flagAliases,omitempty"`
		Env        string              `json:"env"`
		EnvAlias   []string            `json:"envAliases,omitempty"`
		Enum       []string            `json:"enum,omitempty"`
		EnumAlias  map[string][]string `json:"enumAliases,omitempty"`
		Bare       string              `json:"bare,omitempty"`
		Repeatable bool                `json:"repeatable,omitempty"`
		Commands   []string            `json:"commands,omitempty"`
		Plumbing   bool                `json:"plumbing,omitempty"`
		Bootstrap  bool                `json:"bootstrap,omitempty"`
	}
	var out []entry
	for _, set := range s.All() {
		if set.Plumbing && !includePlumbing {
			continue
		}
		out = append(out, entry{
			Path: set.Path, Kind: set.Kind.String(), Default: set.Default,
			Name: set.Name, Short: set.Short, Long: set.Long,
			Flag: set.FlagName(), FlagAlias: set.FlagAliases,
			Env: set.EnvName(), EnvAlias: set.EnvAliases,
			Enum: set.Enum, EnumAlias: set.EnumAliases, Bare: set.Bare, Repeatable: set.Repeatable,
			Commands: set.Commands, Plumbing: set.Plumbing, Bootstrap: set.Bootstrap,
		})
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return string(b)
}
