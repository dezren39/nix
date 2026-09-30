package diagnose

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// historyVersion guards the on-disk format. A file from an older version is
// discarded rather than migrated: it is a cache of observations, and the
// worst consequence of losing it is that the next diagnostic says "changed"
// without saying when.
const historyVersion = 1

// History remembers what every tool's schema looked like, and what changed.
//
// mcpx already computes a catalog fingerprint; what it did not do was keep
// one, so the diff it could produce was always against nothing. The gap
// mattered: "argument 1 became required" is a useful sentence only if mcpx
// can say when, and "when" is exactly what a fingerprint that is never stored
// cannot tell you.
//
// Kept as one small JSON file beside the schema cache. The volume is a few
// hundred tools times a few changes each, written when schemas are refreshed
// and read when something has gone wrong.
type History struct {
	mu    sync.Mutex
	path  string
	limit int
	doc   historyDoc
}

type historyDoc struct {
	Version int                   `json:"version"`
	Tools   map[string]*toolEntry `json:"tools"`
}

type toolEntry struct {
	Shape   Shape     `json:"shape"`
	SeenAt  time.Time `json:"seenAt"`
	Changes []Change  `json:"changes,omitempty"`
}

// OpenHistory reads the history at path, or starts an empty one.
//
// An unreadable or stale file is not an error. The caller is on a path where
// the history is an enrichment, and refusing to start a daemon because a
// diagnostic cache would not parse is the wrong trade.
func OpenHistory(path string, limit int) *History {
	h := &History{path: path, limit: limit,
		doc: historyDoc{Version: historyVersion, Tools: map[string]*toolEntry{}}}
	b, err := os.ReadFile(path)
	if err != nil {
		return h
	}
	var doc historyDoc
	if json.Unmarshal(b, &doc) != nil || doc.Version != historyVersion || doc.Tools == nil {
		return h
	}
	h.doc = doc
	return h
}

// Observe records the current catalog and returns everything that changed.
//
// The first observation of a tool is not a change. A daemon starting for the
// first time would otherwise report every tool it has as newly added, and a
// diagnostic quoting that would be actively misleading.
func (h *History) Observe(now time.Time, tools []Tool) []Change {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	seen := map[string]bool{}
	var changes []Change
	first := len(h.doc.Tools) == 0

	for _, t := range tools {
		path := t.Path()
		seen[path] = true
		prev, had := h.doc.Tools[path]
		if !had {
			h.doc.Tools[path] = &toolEntry{Shape: t.Shape, SeenAt: now}
			if !first {
				changes = append(changes, Change{
					When: now, Kind: ChangeToolAdded, Tool: path,
					What: "the tool appeared",
				})
			}
			continue
		}
		diffs := diffShape(now, path, prev.Shape, t.Shape)
		prev.Shape = t.Shape
		prev.SeenAt = now
		if len(diffs) > 0 {
			prev.Changes = append(diffs, prev.Changes...)
			if len(prev.Changes) > h.limit && h.limit > 0 {
				prev.Changes = prev.Changes[:h.limit]
			}
			changes = append(changes, diffs...)
		}
	}

	// A tool that is simply not in this observation may be a server that
	// failed to start rather than a tool that went away, so removal is
	// recorded only when something else from the same namespace was seen.
	namespaces := map[string]bool{}
	for _, t := range tools {
		namespaces[t.Namespace] = true
	}
	for path, entry := range h.doc.Tools {
		if seen[path] {
			continue
		}
		ns := path
		if i := lastDot(path); i > 0 {
			ns = path[:i]
		}
		if !namespaces[ns] {
			continue
		}
		if len(entry.Changes) > 0 && entry.Changes[0].Kind == ChangeToolRemoved {
			continue
		}
		ch := Change{When: now, Kind: ChangeToolRemoved, Tool: path,
			What: "the tool was removed"}
		entry.Changes = append([]Change{ch}, entry.Changes...)
		changes = append(changes, ch)
	}

	sort.SliceStable(changes, func(i, j int) bool { return changes[i].Tool < changes[j].Tool })
	return changes
}

func lastDot(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '.' {
			return i
		}
	}
	return -1
}

// Changes returns what is known about one tool, newest first.
func (h *History) Changes(path string) []Change {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	e, ok := h.doc.Tools[path]
	if !ok {
		return nil
	}
	return append([]Change(nil), e.Changes...)
}

// All returns the change lists for every tool, for the catalog history
// endpoint and for building a Catalog.
func (h *History) All() map[string][]Change {
	out := map[string][]Change{}
	if h == nil {
		return out
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for path, e := range h.doc.Tools {
		if len(e.Changes) > 0 {
			out[path] = append([]Change(nil), e.Changes...)
		}
	}
	return out
}

// Save writes the history atomically.
func (h *History) Save() error {
	if h == nil || h.path == "" {
		return nil
	}
	h.mu.Lock()
	b, err := json.Marshal(h.doc)
	h.mu.Unlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(h.path), 0o700); err != nil {
		return err
	}
	tmp := h.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, h.path)
}

// diffShape describes the difference between two versions of one schema.
func diffShape(now time.Time, path string, before, after Shape) []Change {
	var out []Change
	beforeReq, afterReq := before.RequiredSet(), after.RequiredSet()

	names := map[string]bool{}
	for n := range before.Props {
		names[n] = true
	}
	for n := range after.Props {
		names[n] = true
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)

	for _, name := range sorted {
		bt, hadBefore := before.Props[name]
		at, hasAfter := after.Props[name]
		switch {
		case !hadBefore && hasAfter:
			what := fmt.Sprintf("`%s` was added", name)
			if afterReq[name] {
				what = fmt.Sprintf("`%s` was added and is required", name)
			}
			out = append(out, Change{When: now, Kind: ChangeArgAdded, Tool: path, Field: name, What: what})
		case hadBefore && !hasAfter:
			out = append(out, Change{When: now, Kind: ChangeArgRemoved, Tool: path, Field: name,
				What: fmt.Sprintf("`%s` was removed", name)})
		default:
			if bt != at {
				out = append(out, Change{When: now, Kind: ChangeArgRetyped, Tool: path, Field: name,
					What: fmt.Sprintf("`%s` changed from %s to %s", name, bt, at)})
			}
			switch {
			case !beforeReq[name] && afterReq[name]:
				out = append(out, Change{When: now, Kind: ChangeArgRequired, Tool: path, Field: name,
					What: fmt.Sprintf("`%s` became required", name)})
			case beforeReq[name] && !afterReq[name]:
				out = append(out, Change{When: now, Kind: ChangeArgOptional, Tool: path, Field: name,
					What: fmt.Sprintf("`%s` became optional", name)})
			}
		}
	}
	return out
}
