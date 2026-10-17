package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dezren39/mcpx/internal/codegen"
	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/defaults"
	"github.com/dezren39/mcpx/internal/diagnose"
	"github.com/dezren39/mcpx/internal/elicit"
	"github.com/dezren39/mcpx/internal/events"
	"github.com/dezren39/mcpx/internal/mcpclient"
	"github.com/dezren39/mcpx/internal/pool"
	"github.com/dezren39/mcpx/internal/recipes"
	"github.com/dezren39/mcpx/internal/runner"
	"github.com/dezren39/mcpx/internal/scripting"
	"github.com/dezren39/mcpx/internal/searchpath"
	"github.com/dezren39/mcpx/internal/settings"
)

// ConsumerPolicy is the resolved policy for everything mcpx asks for on its
// own behalf rather than forwarding.
//
// Resolved once, in the daemon, from the same configuration files and
// variables the CLI reads. Whether a destructive call is confirmed is not a
// question with two correct answers depending on which process asked, so the
// answer is computed where the call happens.
type ConsumerPolicy struct {
	Disambiguate        string
	DisambiguateDefault string
	ConfirmDestructive  bool
	AskTimeout          time.Duration
	DiagnosePreflight   bool
	HistoryPerTool      int
	RecipeMinScore      int
	RecipeMatchMargin   int
	RecipeLimit         int
	PromptSample        string
	PromptCatalogBudget int
	PromptSampleTimeout time.Duration
	PromptMaxTokens     int
	RunTimeout          time.Duration
}

// consumerState is everything the consumer routes need that the daemon did
// not already hold.
type consumerState struct {
	policy  ConsumerPolicy
	history *diagnose.History
	// dirs is the recipe search path, nearest first.
	dirs []string
}

// CatalogHistoryFile is the name of the file that remembers what every tool's
// schema used to look like.
const CatalogHistoryFile = "catalog-history.json"

// initConsumer resolves the policy and opens the schema history.
//
// Failures here are not fatal. A daemon with no policy file still serves
// tools, and a diagnostic that cannot say when a schema changed is still a
// diagnostic that says what changed.
func (s *Server) initConsumer() {
	st := &consumerState{policy: defaultConsumerPolicy()}
	var sources []string
	if s.cfg != nil {
		sources = s.cfg.Sources
	}
	if set, err := settings.Resolve(sources); err != nil {
		s.logger.Printf("consumer policy: %v; using defaults", err)
	} else {
		st.policy = consumerPolicyFrom(set)
		st.dirs = recipeDirs(set.String("paths.scripts"))
	}
	if len(st.dirs) == 0 {
		st.dirs = recipeDirs("")
	}
	st.history = diagnose.OpenHistory(
		filepath.Join(s.paths.State, CatalogHistoryFile), st.policy.HistoryPerTool)

	s.consumer = st
	s.reg.consumer = st.policy
	s.reg.history = st.history
	// The cache the registry loaded at startup is an observation like any
	// other, and recording it now is what makes the first refresh after a
	// restart a diff rather than a first sighting.
	s.reg.ObserveCatalog()
}

func defaultConsumerPolicy() ConsumerPolicy {
	return ConsumerPolicy{
		Disambiguate:        defaults.Disambiguate,
		DisambiguateDefault: defaults.DisambiguateDefault,
		ConfirmDestructive:  defaults.ConfirmDestructive,
		AskTimeout:          defaults.AskTimeout,
		DiagnosePreflight:   defaults.DiagnosePreflight,
		HistoryPerTool:      defaults.HistoryPerTool,
		RecipeMinScore:      defaults.RecipeMinScore,
		RecipeMatchMargin:   defaults.RecipeMatchMargin,
		RecipeLimit:         defaults.RecipeLimit,
		PromptSample:        defaults.PromptSample,
		PromptCatalogBudget: defaults.PromptCatalogBudget,
		PromptSampleTimeout: defaults.PromptSampleTimeout,
		PromptMaxTokens:     defaults.PromptMaxTokens,
		RunTimeout:          defaults.RunTimeout,
	}
}

func consumerPolicyFrom(set *settings.Set) ConsumerPolicy {
	p := defaultConsumerPolicy()
	p.Disambiguate = set.String("elicit.disambiguate")
	p.DisambiguateDefault = set.String("elicit.disambiguateDefault")
	p.ConfirmDestructive = set.Bool("elicit.confirmDestructive")
	p.AskTimeout = set.Duration("elicit.askTimeout")
	p.DiagnosePreflight = set.Bool("diagnose.preflight")
	p.HistoryPerTool = set.Int("diagnose.history")
	p.RecipeMinScore = set.Int("recipes.minScore")
	p.RecipeMatchMargin = set.Int("recipes.matchMargin")
	p.RecipeLimit = set.Int("recipes.limit")
	p.PromptSample = set.String("prompt.sample")
	p.PromptCatalogBudget = set.Int("prompt.catalogBudget")
	p.PromptSampleTimeout = set.Duration("prompt.sampleTimeout")
	p.PromptMaxTokens = set.Int("prompt.maxTokens")
	p.RunTimeout = set.Duration("prompt.runTimeout")
	return p
}

// recipeDirs resolves the script search path from the daemon's own working
// directory, which is the project it was started for.
func recipeDirs(configured string) []string {
	wd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	builtin, _ := recipes.Dirs(wd, home, os.Getenv("XDG_CONFIG_HOME"))
	list := splitPathList(configured)
	if len(list) == 0 {
		return builtin
	}
	resolved := searchpath.Resolve(list, settings.NullMarker, searchpath.Options{
		Dir: wd, Builtin: builtin,
	})
	var out []string
	for _, e := range resolved.Entries {
		out = append(out, e.Path)
	}
	return out
}

// splitPathList accepts a JSON array as a config file holds it, or a
// separator-joined string as a variable must.
func splitPathList(v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	if strings.HasPrefix(v, "[") {
		var arr []any
		if json.Unmarshal([]byte(v), &arr) == nil {
			var parts []string
			for _, e := range arr {
				if e == nil {
					parts = append(parts, settings.NullMarker)
					continue
				}
				parts = append(parts, fmt.Sprint(e))
			}
			return parts
		}
	}
	var parts []string
	for _, part := range strings.Split(v, string(os.PathListSeparator)) {
		switch part = strings.TrimSpace(part); part {
		case "":
		case "-", "null":
			parts = append(parts, settings.NullMarker)
		default:
			parts = append(parts, part)
		}
	}
	return parts
}

// ---- the catalog's own history ----

// DiagnoseTools is every visible tool, flattened for the diagnostics.
func (r *Registry) DiagnoseTools() []diagnose.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []diagnose.Tool
	if len(r.testTools) > 0 {
		for _, t := range r.testTools {
			fn := t.Function
			if fn == "" {
				fn = codegen.ToolFuncName(t.Tool)
			}
			out = append(out, diagnose.Tool{
				Namespace: t.Namespace,
				Name:      t.Tool,
				Func:      fn,
				Shape:     diagnose.ShapeOf(t.InputSchema),
			})
		}
		return out
	}
	for _, name := range r.order {
		p := r.pools[name]
		view := r.views[name]
		all, _, at := p.CachedSchemas()
		if at.IsZero() {
			// Nothing has been read from this server yet. Reporting no tools
			// would let the history record every one of them as removed.
			continue
		}
		for _, t := range visibleTools(view, all) {
			out = append(out, diagnose.Tool{
				Namespace: view.Namespace,
				Name:      t.Name,
				Func:      codegen.ToolFuncName(t.Name),
				Shape:     diagnose.ShapeOf(t.InputSchema),
			})
		}
	}
	return out
}

// ObserveCatalog records the current schemas and remembers what changed.
//
// This is the half of the deterministic diagnostic that did not exist.
// codegen could already fingerprint a catalog and diff two of them; nothing
// ever kept one, so the diff was always against nothing and the question
// "when did this change" had no answer.
func (r *Registry) ObserveCatalog() {
	if r.history == nil {
		return
	}
	changes := r.history.Observe(time.Now(), r.DiagnoseTools())
	if err := r.history.Save(); err != nil {
		r.logf("saving catalog history: %v", err)
	}
	for _, ch := range changes {
		r.logf("catalog: %s %s", ch.Tool, ch.What)
		r.publish(events.Event{
			Kind: events.Kind("catalog.changed"), Data: mustJSON(ch),
		})
	}
}

// DiagnoseCatalog is the view a diagnostic is computed against.
func (r *Registry) DiagnoseCatalog() diagnose.Catalog {
	cat := diagnose.Catalog{Tools: r.DiagnoseTools()}
	if r.history != nil {
		cat.Changes = r.history.All()
	}
	return cat
}

// ---- elicit to disambiguate ----

// disambiguate asks which live instance a call should use.
//
// It only ever asks when the answer is genuinely open: the server is
// exclusive (so instances are distinct things rather than copies), several
// are live, and the caller's own scope key names none of them. When the
// caller already has an instance, the scope key is the answer and a question
// would be theatre.
//
// The returned key is the one to lease. Every path returns something: an
// unanswered question falls back to the configured default, because the
// caller is frequently headless and a call that waits forever is worse than
// one that takes the conservative option.
func (r *Registry) disambiguate(ctx context.Context, p *pool.Pool, cc config.CallContext, key string) string {
	if r.consumer.Disambiguate != "ask" || r.broker == nil {
		return key
	}
	st := p.Status()
	if st.Sharing != string(config.SharingExclusive) || len(st.Instances) < 2 {
		return key
	}
	choices := []elicit.Choice{{
		Value: key, Title: "new",
		Detail: "start an instance for this caller, which is what happens today",
	}}
	recent := key
	bestIdle := -1
	for _, in := range st.Instances {
		if in.Key == key {
			// The caller already has one. That is the answer.
			return key
		}
		choices = append(choices, elicit.Choice{
			Value: in.Key, Title: in.Key, Detail: describeInstance(in),
		})
		if bestIdle < 0 || in.IdleSec < bestIdle {
			bestIdle, recent = in.IdleSec, in.Key
		}
	}
	sort.SliceStable(choices[1:], func(i, j int) bool {
		return choices[1+i].Title < choices[1+j].Title
	})

	fallback := key
	if r.consumer.DisambiguateDefault == "recent" {
		fallback = recent
	}
	req := elicit.Disambiguation(elicit.Ask{
		Server:  p.Name(),
		Session: cc.SessionID,
		Message: fmt.Sprintf("%s has %d live instances and this call named none of them. Which should it use?",
			p.Namespace(), len(st.Instances)),
		Choices: choices,
		Default: fallback,
		TTL:     r.consumer.AskTimeout,
	})
	opened, err := r.broker.OpenRequest(req)
	if err != nil {
		r.logf("disambiguate %s: %v", p.Name(), err)
		return fallback
	}
	r.publish(events.Event{Kind: events.ElicitOpened, Server: p.Name(), Data: mustJSON(opened)})

	actx, cancel := context.WithTimeout(ctx, r.consumer.AskTimeout)
	defer cancel()
	ans, aerr := r.broker.Await(actx, opened.ID)
	if aerr != nil {
		r.logf("disambiguate %s: nobody answered, using %s", p.Name(), fallback)
		return fallback
	}
	chosen, answered := elicit.Resolve(ans, elicit.FieldInstance, fallback)
	if !answered {
		return fallback
	}
	// An answer naming something that is not on offer is a mistake, not an
	// instruction: honouring it would start a process keyed on arbitrary
	// text somebody typed.
	for _, c := range choices {
		if c.Value == chosen {
			return chosen
		}
	}
	r.logf("disambiguate %s: %q is not one of the choices, using %s", p.Name(), chosen, fallback)
	return fallback
}

func describeInstance(in pool.InstanceStatus) string {
	who := "idle"
	if in.Holders > 0 {
		who = fmt.Sprintf("held by %d caller(s)", in.Holders)
	}
	return fmt.Sprintf("%s, %d call(s), idle %ds, up %ds, pid %d",
		who, in.Calls, in.IdleSec, in.UptimeSec, in.PID)
}

// ---- confirm a destructive call ----

// ErrNotConfirmed is returned when a destructive call was not approved.
type ErrNotConfirmed struct {
	Tool   string
	Reason string
}

func (e ErrNotConfirmed) Error() string {
	return fmt.Sprintf("%s may be destructive and was not confirmed: %s", e.Tool, e.Reason)
}

// confirmDestructive asks before a call the server says cannot be undone.
//
// Off by default, and deliberately so: it puts a question on the path of
// every destructive call, and a caller with nobody to answer it waits out the
// deadline first. What it buys, when it is on, is the step a human-driven
// client has and a script does not -- the moment between deciding to delete
// fourteen issues and deleting them.
//
// An unanswered question refuses the call. That is the opposite of the
// disambiguation default, and for the opposite reason: there, doing nothing
// is safe; here, doing nothing is the destructive thing.
func (r *Registry) confirmDestructive(ctx context.Context, p *pool.Pool, tool string, cc config.CallContext) error {
	if !r.consumer.ConfirmDestructive || r.broker == nil {
		return nil
	}
	all, _, at := p.CachedSchemas()
	if at.IsZero() {
		// Never read, or invalidated by a list_changed. Deciding from an
		// empty or stale list would let the call through unasked -- which
		// is what happened on every call that arrived while a new daemon
		// was still reading schemas -- so read them now, and refuse if
		// they cannot be read: failing open is the one wrong answer here.
		fresh, _, err := p.RefreshSchemas(ctx)
		if err != nil {
			return ErrNotConfirmed{Tool: p.Namespace() + "." + tool,
				Reason: "its annotations could not be read: " + err.Error()}
		}
		all = fresh
	}
	var found *mcpclient.Tool
	for i := range all {
		if all[i].Name == tool {
			found = &all[i]
			break
		}
	}
	if found == nil || !destructiveHint(found.Annotations) {
		return nil
	}
	path := p.Namespace() + "." + tool
	req := elicit.Confirmation(elicit.Ask{
		Server:  p.Name(),
		Tool:    tool,
		Session: cc.SessionID,
		Message: fmt.Sprintf("%s.%s may be destructive: its server does not mark it read-only or non-destructive. Let this call proceed?",
			p.Namespace(), tool),
		Default: "false",
		TTL:     r.consumer.AskTimeout,
	})
	opened, err := r.broker.OpenRequest(req)
	if err != nil {
		return err
	}
	r.publish(events.Event{Kind: events.ElicitOpened, Server: p.Name(), Data: mustJSON(opened)})

	actx, cancel := context.WithTimeout(ctx, r.consumer.AskTimeout)
	defer cancel()
	ans, aerr := r.broker.Await(actx, opened.ID)
	if aerr != nil {
		return ErrNotConfirmed{Tool: path,
			Reason: "nobody answered within " + r.consumer.AskTimeout.String()}
	}
	if v, _ := elicit.Resolve(ans, elicit.FieldConfirm, "false"); v != "true" {
		return ErrNotConfirmed{Tool: path, Reason: "the answer was " + string(ans.Action)}
	}
	return nil
}

// destructiveHint reads the one annotation mcpx acts on, with the
// specification's defaults: destructiveHint is true when absent, and means
// something only when readOnlyHint is not true. An absent hint was read as
// false, so an unannotated tool -- which the schema says may destroy -- ran
// unconfirmed (#207, TOOL-17).
func destructiveHint(raw json.RawMessage) bool {
	var doc struct {
		ReadOnly    *bool `json:"readOnlyHint"`
		Destructive *bool `json:"destructiveHint"`
	}
	if len(raw) > 0 && json.Unmarshal(raw, &doc) != nil {
		// Unreadable annotations say nothing, so the default stands.
		return true
	}
	if doc.ReadOnly != nil && *doc.ReadOnly {
		return false
	}
	return doc.Destructive == nil || *doc.Destructive
}

// ---- recipes ----

// clientFileNames are files in a scripts directory that are machinery rather
// than something anybody would run.
func clientFileNames() map[string]bool {
	return map[string]bool{runner.ClientFileName: true, runner.GlobalsFileName: true}
}

func (s *Server) loadRecipes() []recipes.Recipe {
	if s.consumer == nil {
		return nil
	}
	return recipes.Load(s.consumer.dirs, clientFileNames())
}

func (s *Server) findRecipe(name string) (recipes.Recipe, bool) {
	for _, r := range s.loadRecipes() {
		if r.Name == name {
			return r, true
		}
	}
	return recipes.Recipe{}, false
}

// elicitPlaceholders asks for the values a recipe needs and did not get.
//
// One form for all of them rather than one question each: a person filling in
// four fields is doing one thing, and four questions with four deadlines is
// four chances for the run to die half-configured.
func (s *Server) elicitPlaceholders(ctx context.Context, r recipes.Recipe,
	missing []recipes.Placeholder, session string) (map[string]any, string, error) {

	if s.reg.broker == nil {
		return nil, "", fmt.Errorf("%s needs %s and there is nobody to ask: no question broker",
			r.Name, names(missing))
	}
	req := elicit.FormAsk(elicit.Ask{
		Session: session,
		Message: fmt.Sprintf("%s needs %s", r.Name, names(missing)),
		TTL:     s.consumer.policy.AskTimeout,
	}, recipes.FormSchema(missing))

	opened, err := s.reg.broker.OpenRequest(req)
	if err != nil {
		return nil, "", err
	}
	s.reg.publish(events.Event{Kind: events.ElicitOpened, Data: mustJSON(opened)})

	actx, cancel := context.WithTimeout(ctx, s.consumer.policy.AskTimeout)
	defer cancel()
	ans, aerr := s.reg.broker.Await(actx, opened.ID)
	if aerr != nil || ans.Action != elicit.Accept {
		return nil, opened.ID, fmt.Errorf(
			"%s needs %s, and nobody answered within %s (elicitation %s). "+
				"Supply the values with the request instead",
			r.Name, names(missing), s.consumer.policy.AskTimeout, opened.ID)
	}
	var out map[string]any
	if err := json.Unmarshal(ans.Content, &out); err != nil {
		return nil, opened.ID, fmt.Errorf("the answer to %s was not an object: %w", opened.ID, err)
	}
	return out, opened.ID, nil
}

func names(ps []recipes.Placeholder) string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return strings.Join(out, ", ")
}

// ---- running a script from the daemon ----

// ExecResult is what running a script produced.
type ExecResult struct {
	ExitCode int    `json:"exitCode"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	Error    string `json:"error,omitempty"`
}

// execScript runs generated or rendered source against this daemon.
//
// This is the seam, not the design. Running scripts belongs in an exec
// service with its own options and its own permission model, and one is being
// built; when it lands, this function becomes a call into it. Until then a
// recipe that cannot be run is a recipe that is half a feature, so it runs
// here, through the same runner the CLI uses, against this daemon's own
// endpoint.
func (s *Server) execScript(ctx context.Context, source, session string) (*ExecResult, error) {
	s.warmIfCold(ctx)
	nss, err := s.reg.CodegenNamespaces(nil, config.Profile{All: true})
	if err != nil {
		return nil, err
	}
	var out, errOut strings.Builder
	timeout := s.policyFor(ctx).RunTimeout
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	res, rerr := runner.Run(rctx, runner.Options{
		Source:        source,
		Prelude:       prelude(nss),
		ClientSource:  codegen.Module(nss, s.endpoint, session),
		GlobalsSource: codegen.GlobalDeclarations(nss),
		Timeout:       timeout,
		Stdout:        &out,
		Stderr:        &errOut,
		Env: map[string]string{
			"MCPX_SESSION":    session,
			"MCPX_SESSION_ID": session,
			"MCPX_ENDPOINT":   s.endpoint,
		},
	})
	result := &ExecResult{Stdout: out.String(), Stderr: errOut.String()}
	if rerr != nil {
		result.Error = rerr.Error()
		return result, nil
	}
	result.ExitCode = res.ExitCode
	return result, nil
}

// prelude gives a generated snippet the same surface a file script has:
// every namespace as a bare identifier, globals installed.
func prelude(nss []codegen.Namespace) string {
	var names []string
	for _, ns := range nss {
		names = append(names, ns.Name)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("// --- mcpx prelude (generated) ---\n")
	fmt.Fprintf(&b, "import tools, { call, readResource, log, emit, ToolError, installGlobals } from %q;\n",
		"./"+runner.ClientFileName)
	b.WriteString("installGlobals();\n")
	if len(names) > 0 {
		fmt.Fprintf(&b, "const { %s } = tools;\n", strings.Join(names, ", "))
		b.WriteString("void [" + strings.Join(names, ", ") + "];\n")
	}
	b.WriteString("void [tools, call, readResource, log, emit, ToolError];\n")
	b.WriteString("// --- end prelude ---\n\n")
	return b.String()
}

// ---- generation by sampling ----

// generate asks whatever drives mcpx to write a script.
//
// mcpx has no model, which is the whole reason this goes through the broker:
// the request is a sampling request like any other, so every answerer that
// already exists -- the agent through `mcpx elicit`, the plugin through the
// opencode SDK, an MCP client that declared sampling -- answers it without
// learning anything new.
//
// What it is given is the part that needs care. The reason mcpx is worth
// having is that tool schemas stay out of a model's context, so a generation
// request sends the slice of the catalog the existing search ranked for this
// request, inside a budget, and nothing else.
func (s *Server) resolveProvider() scripting.Provider {
	if s.set == nil {
		return scripting.ResolveProvider(scripting.ProviderConfig{}, nil)
	}
	getString := func(key string) string {
		if v, ok := s.set.Value(key); ok {
			return v.Raw
		}
		return ""
	}
	cfg := scripting.ProviderConfig{
		Type:    getString("repair.provider"),
		BaseURL: getString("repair.url"),
		APIKey:  getString("repair.apiKey"),
		Model:   getString("repair.model"),
		Command: getString("repair.command"),
	}
	return scripting.ResolveProvider(cfg, nil)
}

func (s *Server) generate(ctx context.Context, prompt, session string) (string, string, error) {
	nss, err := s.reg.CodegenNamespaces(nil, config.Profile{All: true})
	if err != nil {
		return "", "", err
	}
	slice := codegen.Catalog(nss, codegen.CatalogOptions{
		Budget: s.policyFor(ctx).PromptCatalogBudget,
		Bias:   recipes.Terms(prompt),
	})
	system := "You write short TypeScript programs for mcpx. " +
		"Every tool below is already bound as an async function on the named " +
		"namespace; call it directly, import nothing, and end by logging or " +
		"returning the result. Answer with the program only, no prose and no " +
		"code fences.\n\n" + slice

	prov := s.resolveProvider()
	if prov != nil {
		text, perr := prov.Complete(ctx, scripting.CompletionRequest{
			SystemPrompt: system,
			UserPrompt:   prompt,
			MaxTokens:    s.policyFor(ctx).PromptMaxTokens,
			Temperature:  0.2,
		})
		if perr == nil && strings.TrimSpace(text) != "" {
			return stripFences(text), "provider:" + prov.Name(), nil
		}
	}

	if s.reg.broker == nil {
		return "", "", errNoSampler
	}

	params, _ := json.Marshal(map[string]any{
		"messages": []any{map[string]any{
			"role":    "user",
			"content": map[string]any{"type": "text", "text": prompt},
		}},
		"systemPrompt": system,
		"maxTokens":    s.policyFor(ctx).PromptMaxTokens,
	})

	req := elicit.SampleAsk(elicit.Ask{
		Session: session,
		Message: "mcpx is asking for a script: " + prompt,
		TTL:     s.policyFor(ctx).PromptSampleTimeout,
	}, params)

	opened, oerr := s.reg.broker.OpenRequest(req)
	if oerr != nil {
		return "", "", oerr
	}
	s.reg.publish(events.Event{Kind: events.SampleOpened, Data: mustJSON(opened)})

	actx, cancel := context.WithTimeout(ctx, s.policyFor(ctx).PromptSampleTimeout)
	defer cancel()
	ans, aerr := s.reg.broker.Await(actx, opened.ID)
	if aerr != nil || ans.Action != elicit.Accept {
		return "", opened.ID, errNoSampler
	}
	text := sampledText(ans.Content)
	if strings.TrimSpace(text) == "" {
		return "", opened.ID, errNoSampler
	}
	return stripFences(text), opened.ID, nil
}

// errNoSampler means nothing answered the sampling request.
var errNoSampler = fmt.Errorf("no sampling answerer")

// sampledText pulls the completion out of a CreateMessageResult.
func sampledText(raw json.RawMessage) string {
	var doc struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	// Content is one block in the specification, but answerers write a list
	// often enough that accepting both costs one branch and saves a support
	// question.
	var one struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(doc.Content, &one) == nil && one.Text != "" {
		return one.Text
	}
	var many []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(doc.Content, &many) == nil {
		var parts []string
		for _, b := range many {
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// stripFences removes a markdown code fence a model wrapped the answer in.
//
// Asked for "the program only" it usually obliges; when it does not, the
// alternative to this is a script whose first line is three backticks and a
// syntax error nobody can explain.
func stripFences(s string) string {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "```") {
		return t
	}
	if i := strings.IndexByte(t, '\n'); i >= 0 {
		t = t[i+1:]
	}
	if j := strings.LastIndex(t, "```"); j >= 0 {
		t = t[:j]
	}
	return strings.TrimSpace(t)
}

type policyKey struct{}

// withPolicy carries a request's own consumer policy -- its call-scoped
// settings, resolved from the daemon's and the caller's layers -- down to
// the functions that run and generate for it.
//
// The daemon used to read these once, from its config files, at start: a
// caller's recipes.limit, prompt.runTimeout or prompt.autonomy from the
// environment, a flag or PUT /v1/settings never reached them, while the PUT
// answered "applied".
func withPolicy(ctx context.Context, p ConsumerPolicy) context.Context {
	return context.WithValue(ctx, policyKey{}, p)
}

// policyFor is the policy of the request ctx belongs to, or the daemon's.
func (s *Server) policyFor(ctx context.Context) ConsumerPolicy {
	if p, ok := ctx.Value(policyKey{}).(ConsumerPolicy); ok {
		return p
	}
	return s.consumer.policy
}

// warmIfCold reads the schemas when no server has any yet.
//
// A daemon that has just started reads them in the background. A recipe run,
// a generated script or a diagnosis that arrived first used the empty
// catalog: the client was generated with no namespaces, so the script died
// on "demo.echo is not a function", and a diagnosis compared the source with
// nothing and found nothing wrong. The CLI's own commands wait for the
// schemas; these routes are also reached by the plugin and by MCP, which do
// not. "No server has any" rather than "some server has none", as the CLI's
// ensureAnySchemas does it, so one server that cannot start does not put its
// start timeout on every request.
func (s *Server) warmIfCold(ctx context.Context) {
	s.reg.mu.RLock()
	cold := len(s.reg.pools) > 0
	for _, p := range s.reg.pools {
		if _, _, at := p.CachedSchemas(); !at.IsZero() {
			cold = false
			break
		}
	}
	s.reg.mu.RUnlock()
	if cold {
		s.reg.Warm(ctx, false)
	}
}
