package daemon

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/pool"
	"github.com/dezren39/mcpx/internal/secrets"
)

// SecretRequired is a call or a script refused because a server it needs
// cannot run yet: a secret it requires has no value. Nothing ran. The check
// is made before any child is started, so a script that needs a waiting
// server fails before its first statement, not in the middle of its work.
type SecretRequired struct {
	Server    string
	Secrets   []string
	RequestID string
	Message   string
}

func (e SecretRequired) Error() string { return e.Message }

// SecretRequiredBody is the answer to a refused call or script: a 409 whose
// code the core and OpenChamber read, and whose request id they wait on.
type SecretRequiredBody struct {
	Code      string   `json:"code"`
	Server    string   `json:"server"`
	Secrets   []string `json:"secrets"`
	Message   string   `json:"message"`
	RequestID string   `json:"requestId"`
}

// Body is the wire form of the refusal.
func (e SecretRequired) Body() SecretRequiredBody {
	return SecretRequiredBody{
		Code:      "secret_required",
		Server:    e.Server,
		Secrets:   e.Secrets,
		Message:   e.Message,
		RequestID: e.RequestID,
	}
}

// failureBody is what a failed call answers with. A refusal for secrets has
// a shape of its own; anything else keeps the diagnostic body.
func failureBody(err error) any {
	var secret SecretRequired
	if errors.As(err, &secret) {
		return secret.Body()
	}
	return callErrorBody(err)
}

// bindSecrets lets a view see the values the daemon holds. It is done as each
// view is built, so no view answers from a copy of the store taken earlier.
func (r *Registry) bindSecrets(v *config.Resolved) {
	if v != nil && r.secrets != nil {
		v.Runtime = r.secrets.Lookup
	}
}

// blocked returns the server that stops view from running, and the secrets
// that server is missing; nil means nothing stops it. The view itself is
// checked first; then any server it requires, since a server cannot run while
// one it depends on is waiting. The answer is the view rather than its name,
// so a server that somehow has no name is still refused.
func (r *Registry) blocked(view *config.Resolved) (*config.Resolved, []string) {
	return r.blockedBy(view, map[*config.Resolved]bool{})
}

func (r *Registry) blockedBy(view *config.Resolved, seen map[*config.Resolved]bool) (*config.Resolved, []string) {
	if view == nil || seen[view] {
		return nil, nil
	}
	seen[view] = true
	if missing := view.MissingSecrets(); len(missing) > 0 {
		return view, missing
	}
	for _, dep := range view.RequiresServers {
		if depView, ok := r.View(dep); ok {
			if blocker, missing := r.blockedBy(depView, seen); blocker != nil {
				return blocker, missing
			}
		}
	}
	return nil, nil
}

// refuseBlocked is the refusal for a server that cannot run, or nil when it
// can. Calls, resource reads and prompt fetches all start a child, so all of
// them ask this first.
func (r *Registry) refuseBlocked(server string) error {
	view, ok := r.View(server)
	if !ok {
		return nil
	}
	if blocker, missing := r.blocked(view); blocker != nil {
		return r.secretRequired(blocker.Name, missing)
	}
	return nil
}

// secretRequired is the refusal for a call to a blocked server. The request it
// names is created here, or the one already waiting for the same secrets is
// reused.
func (r *Registry) secretRequired(server string, missing []string) SecretRequired {
	req := r.board.Require(server, missing)
	return SecretRequired{Server: server, Secrets: missing, RequestID: req.ID, Message: req.Message}
}

// secretHas is the board's question: does this server have a value for this
// name, from the runtime store, its own environment or the daemon's.
func (r *Registry) secretHas(server, name string) bool {
	view, ok := r.View(server)
	if !ok {
		return false
	}
	_, has := view.SecretValue(name)
	return has
}

// SetSecret gives name a value in memory, and brings back to life what was
// waiting for it. A child keeps the environment it was started with, so the
// processes of every server that requires name are retired: the next call
// starts them with the value. Nothing is restarted eagerly, and calls in
// flight finish on the child they began on.
func (r *Registry) SetSecret(name, value string) error {
	if err := r.secrets.Set(name, value); err != nil {
		return err
	}
	r.afterSecretChange(name)
	return nil
}

// UnsetSecret drops the runtime value for name. The environment is left alone,
// and so is any value it holds. It reports whether there was a runtime value.
func (r *Registry) UnsetSecret(name string) bool {
	removed := r.secrets.Unset(name)
	if removed {
		r.afterSecretChange(name)
	}
	return removed
}

func (r *Registry) afterSecretChange(name string) {
	r.mu.RLock()
	var pools []*pool.Pool
	seen := map[*pool.Pool]bool{}
	for _, n := range r.order {
		if !containsName(r.views[n].SecretNames(), name) {
			continue
		}
		if p := r.pools[n]; !seen[p] {
			seen[p] = true
			pools = append(pools, p)
		}
	}
	r.mu.RUnlock()
	for _, p := range pools {
		p.Restart(context.Background(), true)
	}
	r.board.Settle(r.secretHas)
}

// SecretRequests lists the requests, after settling them, so one that was
// waiting on a value that has since been given reads as resolved however the
// caller got here.
func (r *Registry) SecretRequests() []secrets.Request {
	r.board.Settle(r.secretHas)
	return r.board.List()
}

// SecretRequest returns one request, settled, and whether it exists.
func (r *Registry) SecretRequest(id string) (secrets.Request, bool) {
	r.board.Settle(r.secretHas)
	return r.board.Get(id)
}

// DeclineSecretRequest ends a pending request. False means there is no such
// request.
func (r *Registry) DeclineSecretRequest(id string) (secrets.Request, bool) {
	return r.board.Decline(id)
}

// SecretInfo is one row of the secrets listing: a name, whether it has a
// value, and where that value comes from. Never the value.
type SecretInfo struct {
	Name   string  `json:"name"`
	Set    bool    `json:"set"`
	Source *string `json:"source"`
}

// SecretList is every name a configured server requires, and every name that
// has a value at runtime, sorted.
func (r *Registry) SecretList() []SecretInfo {
	names := map[string]bool{}
	r.mu.RLock()
	for _, n := range r.order {
		for _, s := range r.views[n].SecretNames() {
			names[s] = true
		}
	}
	r.mu.RUnlock()
	for _, n := range r.secrets.Names() {
		names[n] = true
	}
	out := make([]SecretInfo, 0, len(names))
	for _, n := range sortedNames(names) {
		info := SecretInfo{Name: n}
		src := r.secretSource(n)
		info.Set = src != ""
		if src != "" {
			info.Source = &src
		}
		out = append(out, info)
	}
	return out
}

// secretSource says where name's value comes from when no server is asked:
// "runtime" when one was given, "env" when the environment has one, else "".
func (r *Registry) secretSource(name string) string {
	if _, ok := r.secrets.Lookup(name); ok {
		return "runtime"
	}
	if r.inEnvironment(name) {
		return "env"
	}
	return ""
}

// inEnvironment reports whether the daemon's environment, or a server's own
// environment block, has a value for name.
func (r *Registry) inEnvironment(name string) bool {
	if os.Getenv(name) != "" {
		return true
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, v := range r.views {
		if v.Env[name] != "" {
			return true
		}
	}
	return false
}

// PreflightReport is what a script needs before it may run. Every field is
// computed from the script's text and the configuration; no tool runs.
type PreflightReport struct {
	Namespaces []string         `json:"namespaces"`
	Unknown    []string         `json:"unknown"`
	Ready      bool             `json:"ready"`
	Missing    []MissingSecrets `json:"missing"`
	Requests   []string         `json:"requests"`
}

// MissingSecrets is one server that cannot run, and the secrets it lacks.
type MissingSecrets struct {
	Server  string   `json:"server"`
	Secrets []string `json:"secrets"`
}

// Preflight reads a script and reports whether every server it reaches can
// run. The only thing it changes is the set of secret requests: one is created
// for each server that is missing secrets, or the pending one is reused.
func (r *Registry) Preflight(source string) PreflightReport {
	rep := PreflightReport{
		Namespaces: []string{}, Unknown: []string{}, Ready: true,
		Missing: []MissingSecrets{}, Requests: []string{},
	}
	refs := secrets.References(source, func(ns string) bool {
		_, ok := r.viewByNamespace(ns)
		return ok
	})
	rep.Namespaces = refs
	seen := map[*config.Resolved]bool{}
	for _, ns := range refs {
		view, ok := r.viewByNamespace(ns)
		if !ok {
			rep.Unknown = append(rep.Unknown, ns)
			continue
		}
		blocker, missing := r.blocked(view)
		if blocker == nil {
			continue
		}
		rep.Ready = false
		if seen[blocker] {
			continue
		}
		seen[blocker] = true
		rep.Missing = append(rep.Missing, MissingSecrets{Server: blocker.Name, Secrets: missing})
		rep.Requests = append(rep.Requests, r.board.Require(blocker.Name, missing).ID)
	}
	return rep
}

// refuseScript is the check a script passes before it starts: nil when every
// server it reaches can run, else the refusal for the first one that cannot.
func (r *Registry) refuseScript(source string) *SecretRequired {
	rep := r.Preflight(source)
	if rep.Ready {
		return nil
	}
	first := rep.Missing[0]
	refusal := r.secretRequired(first.Server, first.Secrets)
	if len(rep.Missing) > 1 {
		var others []string
		for _, m := range rep.Missing[1:] {
			others = append(others, m.Server)
		}
		refusal.Message += " The script also reaches " + strings.Join(others, ", ") + ", which cannot run either."
	}
	return &refusal
}

// viewByNamespace finds the server a script reaches through a namespace. Only
// the namespace counts here: a script names namespaces, not server names.
func (r *Registry) viewByNamespace(ns string) (*config.Resolved, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, name := range r.order {
		if v := r.views[name]; v.Namespace == ns {
			return v, true
		}
	}
	return nil, false
}

func containsName(list []string, name string) bool {
	for _, n := range list {
		if n == name {
			return true
		}
	}
	return false
}

func sortedNames(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
