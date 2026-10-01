package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// IgnoredKey is a key on a server entry that mcpx does not read.
type IgnoredKey struct {
	File   string `json:"file"`
	Server string `json:"server"`
	Key    string `json:"key"`
}

// ignoredIn records which file the ignored keys came from, which parse
// cannot know.
func (c *Config) ignoredIn(path string) {
	for i := range c.Ignored {
		c.Ignored[i].File = path
	}
}

// checkKeys looks at each server entry for keys the loader would drop.
//
// Two rules, because the two places are owned differently. A server's own
// fields are shared with every other MCP host -- Claude Code writes "type",
// Cline writes "alwaysAllow" -- so a key mcpx does not know there may be
// somebody else's, and it is recorded rather than refused. The "mcpx" block
// is mcpx's alone: nothing else writes into it, so a key it does not know is
// a typo or a name that no longer exists, and it is refused.
//
// Refusing is the point. encoding/json drops unknown fields without a word,
// which is how `mcpx init` wrote "mode": "session" into every new config for
// months: the key had been split into sharing and scope, the loader
// discarded it, and every browser server the starter file described ran as
// one shared process instead of one per session.
func checkKeys(stripped []byte, c *Config) error {
	var raw struct {
		MCPServers map[string]map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(stripped, &raw); err != nil {
		// The typed decode already succeeded, so this cannot fail on a
		// well-formed document; a server that is not an object was refused
		// there with a better message.
		return nil
	}
	serverKeys := jsonKeys(reflect.TypeOf(Server{}))
	names := make([]string, 0, len(raw.MCPServers))
	for name := range raw.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fields := raw.MCPServers[name]
		keys := make([]string, 0, len(fields))
		for k := range fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if !knows(serverKeys, k) {
				c.Ignored = append(c.Ignored, IgnoredKey{Server: name, Key: k})
			}
		}
		block, ok := fields["mcpx"]
		if !ok || bytes.Equal(bytes.TrimSpace(block), []byte("null")) {
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(block))
		dec.DisallowUnknownFields()
		var x Extras
		if err := dec.Decode(&x); err != nil {
			if key, found := strings.CutPrefix(err.Error(), "json: unknown field "); found {
				return fmt.Errorf("server %q: its \"mcpx\" block has no key %s; it takes %s",
					name, key, strings.Join(sortedKeys(jsonKeys(reflect.TypeOf(Extras{}))), ", "))
			}
			return fmt.Errorf("server %q: %w", name, err)
		}
	}
	return nil
}

// jsonKeys is every key a struct decodes, read from its tags so the list
// cannot drift from the struct it describes.
//
// Keyed by the lower-cased name and valued by the canonical spelling, because
// encoding/json matches a field case-insensitively when no exact match exists
// (encoding/json.Unmarshal: "to match ... prefers an exact match but also
// accepts a case-insensitive match"). A set of canonical names alone reported
// {"Command": "echo"} as an unknown key while json had already decoded it into
// Server.Command -- which meant doctor warned about a working config and
// plumbing.strictUnknownKeys refused one.
//
// Anonymous fields are recursed into rather than taken by name: json promotes
// an embedded struct's fields to the outer object, so its type name is never a
// key. Neither Server nor Extras embeds anything today; doing it here means the
// day one does, the keys it promotes are known rather than silently refused.
func jsonKeys(t reflect.Type) map[string]string {
	out := map[string]string{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "-" {
			continue
		}
		// Checked before IsExported: an anonymous field's name is its type's
		// name, so an embedded unexported struct type reads as unexported
		// while json still promotes the exported fields inside it.
		if f.Anonymous && name == "" {
			et := f.Type
			for et.Kind() == reflect.Pointer {
				et = et.Elem()
			}
			if et.Kind() == reflect.Struct {
				for k, v := range jsonKeys(et) {
					out[k] = v
				}
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		out[strings.ToLower(name)] = name
	}
	return out
}

// knows reports whether a document key names a field the struct decodes.
func knows(keys map[string]string, k string) bool {
	_, ok := keys[strings.ToLower(k)]
	return ok
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
