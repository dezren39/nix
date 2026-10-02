package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/settings"
)

// CallSettingsHeader carries a client's call-scoped settings with a request.
//
// This exists because of a failure that has no symptom. A daemon is started
// once, in whatever environment it happened to have, and then serves every
// command for hours. A flag on one of those commands that governs something
// the daemon does -- how many search results, how large a catalog -- was
// simply lost: accepted, validated, printed back by `mcpx config`, and then
// ignored, because the process that would act on it had already read its
// configuration.
//
// Sending the effective value with the request fixes it without making the
// daemon stateful. The value applies to one request, from one client, and
// nothing else in the daemon can see it.
const CallSettingsHeader = "X-Mcpx-Settings"

func (s *Server) routesSettings(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/settings", s.handleSettingsList)
	mux.HandleFunc("GET /v1/settings/{path}", s.handleSettingsGet)
	mux.HandleFunc("PUT /v1/settings/{path}", s.handleSettingsSet)
	mux.HandleFunc("DELETE /v1/settings/{path}", s.handleSettingsUnset)
	mux.HandleFunc("GET /v1/servers", s.handleServersList)
	mux.HandleFunc("POST /v1/servers", s.handleServersAdd)
	mux.HandleFunc("DELETE /v1/servers/{name}", s.handleServersRemove)
}

// Settings exposes the daemon's resolved configuration.
func (s *Server) Settings() *settings.Set { return s.set }

// callSettings returns the settings this one request should be served with.
//
// Only call-scoped entries are honoured. A client asking the daemon to change
// its log directory for one request would be asking for something that cannot
// be true, and silently widening this to every setting would turn a header
// into a way for any client to reconfigure a shared daemon.
func (s *Server) callSettings(r *http.Request) *settings.Set {
	raw := r.Header.Get(CallSettingsHeader)
	if raw == "" {
		return s.set
	}
	var given map[string]string
	if err := json.Unmarshal([]byte(raw), &given); err != nil {
		// Ignored rather than refused. A malformed header is the client's
		// bug, and failing the actual request over it would turn a lost
		// preference into a lost call.
		s.logger.Printf("ignoring malformed %s header: %v", CallSettingsHeader, err)
		return s.set
	}
	sch := s.set.Schema()
	ov := map[string]string{}
	for path, v := range given {
		decl, ok := sch.Lookup(path)
		if !ok || decl.Scope != settings.ScopeCall {
			continue
		}
		nv, err := settings.Normalize(*decl, v)
		if err != nil {
			continue
		}
		ov[path] = nv
	}
	return s.set.WithOverrides(ov, CallSettingsHeader)
}

// SettingRecord is one setting on the wire: the declaration and the answer
// together, because either one alone leaves the reader with a question.
type SettingRecord struct {
	Path       string   `json:"path"`
	Kind       string   `json:"kind"`
	Value      string   `json:"value"`
	Default    string   `json:"default"`
	Source     string   `json:"source"`
	Shadowed   []string `json:"shadowed,omitempty"`
	Env        string   `json:"env"`
	EnvAliases []string `json:"envAliases,omitempty"`
	Flag       string   `json:"flag"`
	FlagAlias  []string `json:"flagAliases,omitempty"`
	Name       string   `json:"name,omitempty"`
	Short      string   `json:"description,omitempty"`
	Long       string   `json:"detail,omitempty"`
	Enum       []string `json:"enum,omitempty"`
	// EnumAliases lists, per Enum value, the other spellings accepted for it.
	EnumAliases map[string][]string `json:"enumAliases,omitempty"`
	Scope       string              `json:"scope"`
	Hot         bool                `json:"hot"`
	Plumbing    bool                `json:"plumbing,omitempty"`
	Commands    []string            `json:"commands,omitempty"`
	// Requested and ClampedBy say a ceiling lowered this value: what the
	// layers asked for, and which setting, from where, lowered it.
	Requested string `json:"requested,omitempty"`
	ClampedBy string `json:"clampedBy,omitempty"`
}

// Describe renders one setting with its resolved value.
//
// Exported so the CLI renders exactly what /v1 returns. A local listing and a
// remote one disagreeing about what a source string looks like would be a
// difference nobody could explain.
func Describe(set settings.Setting, v *settings.Value) SettingRecord {
	rec := SettingRecord{
		Path: set.Path, Kind: set.Kind.String(), Default: set.Default,
		Env: set.EnvName(), EnvAliases: set.EnvAliases,
		Flag: "--" + set.FlagName(), Name: set.Name,
		Short: set.Short, Long: set.Long, Enum: set.Enum, EnumAliases: set.EnumAliases,
		Scope: set.Scope.String(), Hot: set.Hot, Plumbing: set.Plumbing,
		Commands: set.Commands,
	}
	for _, a := range set.FlagAliases {
		rec.FlagAlias = append(rec.FlagAlias, "--"+a)
	}
	if v != nil {
		rec.Value = v.Raw
		rec.Source = v.Origin.String()
		rec.Requested, rec.ClampedBy = v.Requested, v.ClampedBy
		for _, sh := range v.Shadowed {
			rec.Shadowed = append(rec.Shadowed, sh.String())
		}
	}
	return rec
}

func (s *Server) handleSettingsList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	wantScope, scoped := settings.ParseScope(q.Get("scope"))
	if q.Get("scope") != "" && !scoped {
		writeErr(w, http.StatusBadRequest,
			fmt.Errorf("no such scope %q; want daemon, client, call or plugin", q.Get("scope")))
		return
	}
	withPlumbing := q.Get("plumbing") == "1"
	onlyChanged := q.Get("changed") == "1"

	out := []SettingRecord{}
	for _, set := range s.set.Schema().All() {
		if set.Plumbing && !withPlumbing {
			continue
		}
		if scoped && set.Scope != wantScope {
			continue
		}
		v, _ := s.set.Value(set.Path)
		if onlyChanged && (v == nil || v.Origin.Layer == settings.LayerDefault) {
			continue
		}
		out = append(out, Describe(set, v))
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": out})
}

func (s *Server) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	set, v, err := s.lookupSetting(r.PathValue("path"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, Describe(*set, v))
}

func (s *Server) lookupSetting(path string) (*settings.Setting, *settings.Value, error) {
	decl, ok := s.set.Schema().Lookup(path)
	if !ok {
		return nil, nil, fmt.Errorf("no setting %q; `mcpx settings list` shows them all", path)
	}
	v, _ := s.set.Value(path)
	return decl, v, nil
}

func (s *Server) handleSettingsSet(w http.ResponseWriter, r *http.Request) {
	decl, _, err := s.lookupSetting(r.PathValue("path"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if err := decl.Writable(); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var req struct {
		Value   string `json:"value"`
		Persist string `json:"persist"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body,
		s.set.Bytes("http.controlBodyLimit"))).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	nv, err := settings.Normalize(*decl, req.Value)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	req.Value = nv

	out := map[string]any{"path": decl.Path, "value": req.Value}
	persist := req.Persist
	if persist == "" {
		persist = "runtime"
	}

	if persist != "runtime" {
		path, err := config.ResolveWritePath(config.WriteScope(persist), "")
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if err := config.SetKey(path, decl.Path, settings.TypedValue(*decl, req.Value)); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		out["written"] = path
	}

	// Applied live regardless of where it was persisted: a change written to
	// a file and not applied would mean `mcpx settings get` answering with
	// the old value until a restart, which reads as the write having failed.
	switch {
	case decl.Hot || decl.Scope == settings.ScopeCall:
		if err := s.set.SetRuntime(decl.Path, req.Value); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		out["applied"] = true
	default:
		out["applied"] = false
		out["restartRequired"] = true
		out["note"] = decl.Path + " is read once when the process starts, so this " +
			"takes effect after `mcpx restart` for a server setting, or after the " +
			"daemon is restarted for a daemon one"
	}
	if decl.Scope == settings.ScopeClient || decl.Scope == settings.ScopePlugin {
		out["applied"] = false
		out["note"] = decl.Path + " is read by the " + decl.Scope.String() +
			", not by the daemon; persisting it is what makes it take effect"
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSettingsUnset(w http.ResponseWriter, r *http.Request) {
	decl, _, err := s.lookupSetting(r.PathValue("path"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	had := s.set.ClearRuntime(decl.Path)
	v, _ := s.set.Value(decl.Path)
	writeJSON(w, http.StatusOK, map[string]any{
		"path": decl.Path, "removed": had, "value": v.Raw, "source": v.Origin.String(),
	})
}

// ---- servers ----

type serverRecord struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Transport string            `json:"transport"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	EnvKeys   []string          `json:"envKeys,omitempty"`
	URL       string            `json:"url,omitempty"`
	Headers   []string          `json:"headerKeys,omitempty"`
	Sharing   string            `json:"sharing"`
	Scope     string            `json:"scope"`
	Max       int               `json:"max"`
	Disabled  bool              `json:"disabled,omitempty"`
	From      string            `json:"from,omitempty"`
	Extra     map[string]string `json:"-"`
}

func (s *Server) handleServersList(w http.ResponseWriter, _ *http.Request) {
	cfg := s.reg.Config()
	out := []serverRecord{}
	if cfg != nil {
		resolved, err := cfg.ResolveAll()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		for _, sv := range resolved {
			rec := serverRecord{
				Name: sv.Name, Namespace: sv.Namespace,
				Sharing: string(sv.Sharing), Scope: string(sv.Scope), Max: sv.Max,
				From: cfg.Origin[sv.Name],
			}
			if sv.Stdio() {
				rec.Transport, rec.Command, rec.Args = "stdio", sv.Command, sv.Args
			} else {
				rec.Transport, rec.URL = sv.Transport, sv.URL
			}
			// Keys, never values. A server's environment is where its API
			// tokens are, and an endpoint that lists configured servers is
			// not a place to leak one.
			rec.EnvKeys = sortedKeys(sv.Env)
			rec.Headers = sortedKeys(sv.Headers)
			out = append(out, rec)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"servers": out})
}

func sortedKeys(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type addServerReq struct {
	Name      string            `json:"name"`
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	Transport string            `json:"transport"`
	Mcpx      map[string]any    `json:"mcpx"`
	Scope     string            `json:"scope"`
	Replace   bool              `json:"replace"`
}

// ServerEntry turns a request into the JSON object a configuration file holds.
//
// Exported so the CLI builds the identical entry rather than a
// near-identical one; two spellings of "the same server" is how a server
// added from the command line ends up subtly different from one added over
// the API.
func ServerEntry(req addServerReq) (map[string]any, error) {
	if strings.TrimSpace(req.Name) == "" {
		return nil, errors.New("a server needs a name")
	}
	if req.Command == "" && req.URL == "" {
		return nil, errors.New("a server needs either a command to run or a url to reach")
	}
	if req.Command != "" && req.URL != "" {
		return nil, errors.New("a server is either a command or a url, not both")
	}
	entry := map[string]any{}
	if req.URL != "" {
		entry["url"] = req.URL
		if req.Transport != "" {
			entry["transport"] = req.Transport
		}
		if len(req.Headers) > 0 {
			entry["headers"] = req.Headers
		}
	} else {
		entry["command"] = req.Command
		if len(req.Args) > 0 {
			entry["args"] = req.Args
		}
	}
	if len(req.Env) > 0 {
		entry["env"] = req.Env
	}
	if len(req.Mcpx) > 0 {
		entry["mcpx"] = req.Mcpx
	}
	return entry, nil
}

func (s *Server) handleServersAdd(w http.ResponseWriter, r *http.Request) {
	var req addServerReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body,
		s.set.Bytes("http.bodyLimit"))).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	entry, err := ServerEntry(req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	scope := config.WriteScope(req.Scope)
	if scope == "" {
		scope = config.ScopeProject
	}
	path, err := config.ResolveWritePath(scope, s.configPathFor(scope))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := config.AddServer(path, req.Name, entry, req.Replace); err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	added, removed, rerr := s.reloadConfig()
	if rerr != nil {
		writeErr(w, http.StatusInternalServerError, rerr)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": req.Name, "written": path, "added": added, "removed": removed,
	})
}

func (s *Server) handleServersRemove(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	path := ""
	if scope := config.WriteScope(r.URL.Query().Get("scope")); scope != "" {
		p, err := config.ResolveWritePath(scope, s.configPathFor(scope))
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		path = p
	} else if cfg := s.reg.Config(); cfg != nil {
		// The file that defines it, because removing a server from a file
		// that never mentioned it would report success and change nothing.
		path = cfg.Origin[name]
		if path == "" {
			path = cfg.Path
		}
	}
	if path == "" {
		writeErr(w, http.StatusNotFound, fmt.Errorf("no configuration file defines %q", name))
		return
	}
	found, err := config.RemoveServer(path, name)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !found {
		writeErr(w, http.StatusNotFound, fmt.Errorf("%s does not define %q", path, name))
		return
	}
	added, removed, rerr := s.reloadConfig()
	if rerr != nil {
		writeErr(w, http.StatusInternalServerError, rerr)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": name, "written": path, "added": added, "removed": removed,
	})
}

// configPathFor keeps a write inside the file set this daemon already loaded.
//
// The daemon's socket is keyed to the set of configuration files it read, so
// a write that creates a *new* file changes which daemon a later command
// looks for. Preferring a file already in the set avoids that whenever there
// is one.
func (s *Server) configPathFor(scope config.WriteScope) string {
	cfg := s.currentConfig()
	if cfg == nil || scope != config.ScopeProject {
		return ""
	}
	if len(cfg.Sources) > 0 {
		return cfg.Sources[0]
	}
	return cfg.Path
}

// watchConfig re-reads the configuration when a file has changed on disk.
//
// Polled on the reap tick rather than watched with an OS notification: the
// interval is already there, the files are a handful, and a filesystem
// watcher that works on macOS, Linux and a network mount is a dependency and
// a class of bug for something that needs to be right within half a minute.
//
// The alternative -- and what happened before -- is that an edit to a config
// file does nothing until somebody notices the daemon is stale and restarts
// it. A server added by hand was invisible, and the reason was not guessable
// from anything mcpx printed.
func (s *Server) watchConfig() {
	cfg := s.currentConfig()
	if !s.set.Bool("daemon.watchConfig") || cfg == nil {
		return
	}
	// Two stages, because this runs on every request. The cheap stage is
	// stat: mtime and size of a handful of files, microseconds, and it is
	// unchanged almost always. Only when that moves is anything read.
	s.cfgMu.RLock()
	unchanged := stampConfig(cfg.Sources) == s.stamp
	s.cfgMu.RUnlock()
	if unchanged {
		return
	}

	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()
	s.cfgMu.Lock()
	s.stamp = stampConfig(cfg.Sources)
	changed := ContentFingerprint(cfg.Sources) != s.contentHash
	s.cfgMu.Unlock()
	if !changed {
		// A touched file with identical contents. Recording the new stamp
		// and doing nothing else is the point of the second stage.
		return
	}
	added, removed, err := s.reloadConfig()
	if err != nil {
		s.logger.Printf("a configuration file changed but could not be loaded: %v", err)
		return
	}
	if len(added) > 0 || len(removed) > 0 {
		s.logger.Printf("configuration changed on disk: %v added, %v removed", added, removed)
	}
}

// stampConfig is a cheap "did anything move" check: no file is read.
func stampConfig(paths []string) string {
	var b strings.Builder
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			b.WriteString(p + ":gone;")
			continue
		}
		fmt.Fprintf(&b, "%s:%d:%d;", p, st.ModTime().UnixNano(), st.Size())
	}
	return b.String()
}

// currentConfig returns the configuration in force, safely.
func (s *Server) currentConfig() *config.Config {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	return s.cfg
}

// reloadConfig re-reads the configuration and swaps it in.
func (s *Server) reloadConfig() (added, removed []string, err error) {
	cur := s.currentConfig()
	explicit := ""
	if cur != nil && len(cur.Sources) == 1 && cur.Path != "" {
		// An explicit --config was given; reloading has to read the same one
		// rather than rediscovering the search path.
		explicit = cur.Path
	}
	// Persist what the surviving pools know before the hash moves, or the
	// cache is written under a key nothing will look for.
	if serr := s.reg.SaveCache(); serr != nil {
		s.logger.Printf("save cache before reload: %v", serr)
	}
	cfg, err := config.Load(explicit)
	if err != nil {
		return nil, nil, err
	}
	// A reload re-reads the file and would otherwise drop whatever the
	// environment, a flag or a runtime override said about the pool.
	config.ApplyPoolSettings(cfg, s.set)
	if s.augment != nil {
		if err := s.augment(cfg); err != nil {
			return nil, nil, err
		}
	}
	added, removed, err = s.reg.Reload(cfg)
	if err != nil {
		return nil, nil, err
	}
	s.cfgMu.Lock()
	s.cfg = cfg
	s.contentHash = ContentFingerprint(cfg.Sources)
	s.stamp = stampConfig(cfg.Sources)
	s.cfgMu.Unlock()
	s.logger.Printf("config reloaded: %d added, %d removed", len(added), len(removed))
	return added, removed, nil
}
