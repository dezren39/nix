package catalog

import (
	"sort"
	"sync"
	"time"
)

// ToolOrigin records how a tool entered the working set.
type ToolOrigin int

const (
	OriginInjected ToolOrigin = iota // Speculatively matched from user prompt
	OriginSticky                     // Explicitly requested by model/agent (e.g. request_tools)
	OriginPinned                     // Configured pinned tool (never evicted)
)

// RetainedTool represents an active tool in the retained working set.
type RetainedTool struct {
	Namespace string     `json:"namespace"`
	Tool      string     `json:"tool"`
	Origin    ToolOrigin `json:"origin"`
	Seq       uint64     `json:"seq"`
	AddedAt   time.Time  `json:"addedAt"`
	LastUsed  time.Time  `json:"lastUsed"`
}

func (r *RetainedTool) Key() string {
	return ToolKey(r.Namespace, r.Tool)
}

// ToolKey formats a namespace and tool name into a canonical key.
func ToolKey(ns, tool string) string {
	if ns == "" {
		return tool
	}
	return ns + "." + tool
}

// WorkingSet maintains a bounded set of tools with monotonic recency tracking
// and sticky retention logic.
type WorkingSet struct {
	mu           sync.RWMutex
	maxTools     int
	stickyWindow time.Duration
	seqCounter   uint64
	tools        map[string]*RetainedTool
	pinned       map[string]bool
}

// NewWorkingSet creates a new retention working set with given max limit and sticky TTL.
func NewWorkingSet(maxTools int, stickyWindow time.Duration, pinned []string) *WorkingSet {
	if maxTools <= 0 {
		maxTools = 24
	}
	if stickyWindow <= 0 {
		stickyWindow = time.Duration(120) * time.Minute
	}
	ws := &WorkingSet{
		maxTools:     maxTools,
		stickyWindow: stickyWindow,
		tools:        make(map[string]*RetainedTool),
		pinned:       make(map[string]bool),
	}
	for _, p := range pinned {
		ws.pinned[p] = true
		ws.Add("", p, OriginPinned)
	}
	return ws
}

// Add inserts or updates a tool in the working set.
func (ws *WorkingSet) Add(ns, tool string, origin ToolOrigin) *RetainedTool {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	ws.seqCounter++
	key := ToolKey(ns, tool)
	if ws.pinned[key] || ws.pinned[tool] {
		origin = OriginPinned
	}

	now := time.Now()
	if existing, ok := ws.tools[key]; ok {
		existing.Seq = ws.seqCounter
		existing.LastUsed = now
		if origin > existing.Origin {
			existing.Origin = origin
		}
		return existing
	}

	rt := &RetainedTool{
		Namespace: ns,
		Tool:      tool,
		Origin:    origin,
		Seq:       ws.seqCounter,
		AddedAt:   now,
		LastUsed:  now,
	}
	ws.tools[key] = rt

	ws.evictLocked()
	return rt
}

// evictLocked evicts tools if the count exceeds maxTools:
// 1. Unpinned, non-sticky tools (or expired sticky tools) are evicted oldest-seq first.
// 2. If still over ceiling, active sticky tools are evicted oldest-seq first.
// 3. Pinned tools are never evicted.
func (ws *WorkingSet) evictLocked() {
	if len(ws.tools) <= ws.maxTools {
		return
	}

	now := time.Now()
	type cand struct {
		key string
		seq uint64
	}

	// Pass 1: candidate unpinned non-sticky or expired sticky
	var nonSticky []cand
	for key, t := range ws.tools {
		if t.Origin == OriginPinned {
			continue
		}
		if t.Origin == OriginInjected || now.Sub(t.LastUsed) > ws.stickyWindow {
			nonSticky = append(nonSticky, cand{key: key, seq: t.Seq})
		}
	}
	sort.Slice(nonSticky, func(i, j int) bool {
		return nonSticky[i].seq < nonSticky[j].seq // oldest seq first
	})

	for _, c := range nonSticky {
		if len(ws.tools) <= ws.maxTools {
			return
		}
		delete(ws.tools, c.key)
	}

	// Pass 2: candidate active sticky tools if still exceeding
	if len(ws.tools) <= ws.maxTools {
		return
	}
	var sticky []cand
	for key, t := range ws.tools {
		if t.Origin == OriginPinned {
			continue
		}
		sticky = append(sticky, cand{key: key, seq: t.Seq})
	}
	sort.Slice(sticky, func(i, j int) bool {
		return sticky[i].seq < sticky[j].seq
	})

	for _, c := range sticky {
		if len(ws.tools) <= ws.maxTools {
			return
		}
		delete(ws.tools, c.key)
	}
}

// Touch refreshes the sequence and timestamp of an active tool.
func (ws *WorkingSet) Touch(ns, tool string) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if t, ok := ws.tools[ToolKey(ns, tool)]; ok {
		ws.seqCounter++
		t.Seq = ws.seqCounter
		t.LastUsed = time.Now()
	}
}

// Contains checks if a tool is currently present.
func (ws *WorkingSet) Contains(ns, tool string) bool {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	_, ok := ws.tools[ToolKey(ns, tool)]
	return ok
}

// List returns all active tools in recency order (newest seq first).
func (ws *WorkingSet) List() []*RetainedTool {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	res := make([]*RetainedTool, 0, len(ws.tools))
	for _, t := range ws.tools {
		res = append(res, t)
	}
	sort.Slice(res, func(i, j int) bool {
		return res[i].Seq > res[j].Seq
	})
	return res
}

// Count returns the number of retained tools.
func (ws *WorkingSet) Count() int {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return len(ws.tools)
}
