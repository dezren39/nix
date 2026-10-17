package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
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
	mux.HandleFunc("GET /v1/servers/{name}", s.handleServerGet)
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

	// The file and the runtime value change together or not at all. Holding the
	// reload lock keeps the config watcher and any other reload from reading the
	// file between this write and its rollback, which would apply a value this
	// request is about to refuse.
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()

	var file *fileState
	if persist != "runtime" {
		path, err := config.ResolveWritePath(config.WriteScope(persist), "")
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		prior, err := captureFile(path)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		file = &prior
		if err := config.SetKey(path, decl.Path, settings.TypedValue(*decl, req.Value)); err != nil {
			s.refused(w, http.StatusInternalServerError, err, file)
			return
		}
		out["written"] = path
	}

	// Applied live regardless of where it was persisted: a change written to
	// a file and not applied would mean `mcpx settings get` answering with
	// the old value until a restart, which reads as the write having failed.
	switch {
	case decl.Hot || decl.Scope == settings.ScopeCall:
		prev, had := s.set.RuntimeOverride(decl.Path)
		if err := s.set.SetRuntime(decl.Path, req.Value); err != nil {
			s.refused(w, http.StatusBadRequest, err, file)
			return
		}
		if err := s.applyPoolRuntime(decl.Path); err != nil {
			s.restoreRuntime(decl.Path, prev, had)
			s.refused(w, http.StatusInternalServerError, reloadFailed(decl.Path, err), file)
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

// applyPoolRuntime makes a runtime change to a pool setting reach the pools
// that are already running.
//
// ApplyPoolSettings folds the pool knobs into the configuration, and that runs
// only when a configuration is loaded. Without a reload the change was stored
// and reported as applied while every running pool kept the value it started
// with. A reload treats these knobs like an edit to the file: each pool's
// identity includes them, so the affected servers are replaced. A change to
// any pool.* setting therefore restarts the servers that inherit it.
func (s *Server) applyPoolRuntime(path string) error {
	if !strings.HasPrefix(path, "pool.") {
		return nil
	}
	_, _, err := s.reloadConfig()
	return err
}

// restoreRuntime undoes a runtime change that could not be reloaded: the
// override it replaced comes back, or the one it added is removed. Without
// this the refused value would stay in force and be applied by whichever
// later reload happened to run next.
func (s *Server) restoreRuntime(path, prev string, had bool) {
	if !had {
		s.set.ClearRuntime(path)
		return
	}
	if err := s.set.SetRuntime(path, prev); err != nil {
		s.logger.Printf("restoring %s after a failed reload: %v", path, err)
	}
}

// reloadFailed is the error a caller sees when a runtime change was refused.
func reloadFailed(path string, err error) error {
	return fmt.Errorf("%s: the configuration could not be reloaded, so the runtime value was rolled back: %w", path, err)
}

// refused answers a change that was not applied. When the change was also
// written to a file, that file goes back to what it held before, so the file
// and the daemon agree again. A file holding a value the daemon refused is the
// disagreement this exists to prevent: the next reload that read it would apply
// the value.
func (s *Server) refused(w http.ResponseWriter, status int, err error, file *fileState) {
	if file != nil {
		if rerr := file.restore(); rerr != nil {
			s.logger.Printf("restoring %s after a refused change: %v", file.target, rerr)
			writeErr(w, status, fmt.Errorf("%w; %s could not be restored and still holds the refused value: %v",
				err, file.target, rerr))
			return
		}
		err = fmt.Errorf("%w; %s was restored to its previous contents", err, file.target)
	}
	writeErr(w, status, err)
}

// fileState is a configuration file as it was before a write: its bytes and
// mode, or that there was no file. A refused write is rolled back to it.
type fileState struct {
	target  string // the file written, with symlinks resolved
	existed bool
	data    []byte
	mode    fs.FileMode
}

func captureFile(path string) (fileState, error) {
	target := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		target = resolved
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fileState{}, err
	}
	st, err := os.Stat(target)
	if errors.Is(err, fs.ErrNotExist) {
		return fileState{target: target}, nil
	}
	if err != nil {
		return fileState{}, err
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return fileState{}, err
	}
	return fileState{target: target, existed: true, data: data, mode: st.Mode().Perm()}, nil
}

// restore puts the file back as it was captured: the same bytes and mode, or
// no file at all. The bytes go to a temporary file beside the target and are
// renamed over it, so a failure partway leaves the refused contents or the
// captured ones, never a truncated file.
func (f fileState) restore() error {
	if !f.existed {
		if err := os.Remove(f.target); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(f.target), "."+filepath.Base(f.target)+".restore-*")
	if err != nil {
		return err
	}
	if err := writeTemp(tmp, f.data, f.mode); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), f.target); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

// writeTemp writes and closes a file that restore is about to rename into place.
func writeTemp(f *os.File, data []byte, mode fs.FileMode) error {
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func (s *Server) handleSettingsUnset(w http.ResponseWriter, r *http.Request) {
	decl, _, err := s.lookupSetting(r.PathValue("path"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	prev, had := s.set.RuntimeOverride(decl.Path)
	if had {
		s.set.ClearRuntime(decl.Path)
		if err := s.applyPoolRuntime(decl.Path); err != nil {
			s.restoreRuntime(decl.Path, prev, true)
			writeErr(w, http.StatusInternalServerError, reloadFailed(decl.Path, err))
			return
		}
	}
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
	// RequiresSecrets is what the configuration says this server needs.
	// MissingSecrets is the part of it with no value. Active is false while
	// either holds, or while a server it requires cannot run.
	RequiresSecrets []string `json:"requiresSecrets"`
	MissingSecrets  []string `json:"missingSecrets"`
	Active          bool     `json:"active"`
}

// serverRecords is every configured server, as the configuration writes it,
// with what it needs and whether it can run now.
func (s *Server) serverRecords() ([]serverRecord, error) {
	out := []serverRecord{}
	cfg := s.reg.Config()
	if cfg == nil {
		return out, nil
	}
	resolved, err := cfg.ResolveAll()
	if err != nil {
		return nil, err
	}
	for _, sv := range resolved {
		// These are fresh views, so they need the daemon's values bound in
		// before they can say whether anything is missing.
		s.reg.bindSecrets(sv)
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
		rec.RequiresSecrets = nonNil(sv.SecretNames())
		rec.MissingSecrets = nonNil(sv.MissingSecrets())
		blocker, _ := s.reg.blocked(sv)
		rec.Active = blocker == nil
		out = append(out, rec)
	}
	return out, nil
}

func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

func (s *Server) handleServersList(w http.ResponseWriter, _ *http.Request) {
	out, err := s.serverRecords()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"servers": out})
}

func (s *Server) handleServerGet(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	out, err := s.serverRecords()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	for _, rec := range out {
		if rec.Name == name {
			writeJSON(w, http.StatusOK, rec)
			return
		}
	}
	writeErr(w, http.StatusNotFound, fmt.Errorf("no configured server named %q", name))
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
