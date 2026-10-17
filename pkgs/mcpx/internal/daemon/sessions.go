package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/dezren39/mcpx/internal/defaults"
)

// The lease table says which caller holds which instance. It is written next
// to the schema cache so a restarted daemon still knows, and a takeover ships
// the live table so no lease is lost between the last save and the handover.

const sessionsVersion = 1

type sessionsFile struct {
	Version int                    `json:"version"`
	Leases  map[string]leaseRecord `json:"leases"`
}

type leaseRecord struct {
	LastSeen time.Time         `json:"lastSeen"`
	Owned    map[string]string `json:"owned,omitempty"`
}

func (r *Registry) sessionsPath() string {
	if r.paths.State == "" {
		return ""
	}
	return filepath.Join(r.paths.State, "sessions.json")
}

func (r *Registry) snapshotLeases() map[string]leaseRecord {
	r.sessMu.Lock()
	defer r.sessMu.Unlock()
	out := make(map[string]leaseRecord, len(r.leases))
	for id, st := range r.leases {
		owned := make(map[string]string, len(st.owned))
		for server, key := range st.owned {
			owned[server] = key
		}
		out[id] = leaseRecord{LastSeen: st.lastSeen, Owned: owned}
	}
	return out
}

// SaveSessions writes the lease table atomically, readable only by its owner.
func (r *Registry) SaveSessions() error {
	path := r.sessionsPath()
	if path == "" {
		return nil
	}
	b, err := json.Marshal(sessionsFile{Version: sessionsVersion, Leases: r.snapshotLeases()})
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, defaults.PrivateMode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (r *Registry) loadSessions() {
	path := r.sessionsPath()
	if path == "" {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var f sessionsFile
	if err := json.Unmarshal(b, &f); err != nil || f.Version != sessionsVersion {
		r.logf("ignoring %s: not a lease table this daemon reads", path)
		return
	}
	r.adoptLeases(f.Leases)
}

// adoptLeases takes the leases a predecessor held. A caller present in both
// keeps the record from the predecessor, which is the newer one.
func (r *Registry) adoptLeases(recs map[string]leaseRecord) {
	if len(recs) == 0 {
		return
	}
	r.sessMu.Lock()
	defer r.sessMu.Unlock()
	for id, rec := range recs {
		owned := make(map[string]string, len(rec.Owned))
		for server, key := range rec.Owned {
			owned[server] = key
		}
		r.leases[id] = &leaseState{lastSeen: rec.LastSeen, owned: owned}
	}
}
