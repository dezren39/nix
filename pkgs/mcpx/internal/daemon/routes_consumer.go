package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dezren39/mcpx/internal/diagnose"
	"github.com/dezren39/mcpx/internal/logging"
	"github.com/dezren39/mcpx/internal/recipes"
	"github.com/dezren39/mcpx/internal/scripting"
	"github.com/dezren39/mcpx/internal/settings"
)

// routesConsumer registers the operations behind the things mcpx asks for
// itself. Declared in internal/api/ops_consumer.go; a parity test fails if
// the two disagree.
func (s *Server) routesConsumer(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/diagnose", s.handleDiagnose)
	mux.HandleFunc("GET /v1/catalog/history", s.handleCatalogHistory)
	mux.HandleFunc("GET /v1/recipes", s.handleRecipesList)
	mux.HandleFunc("GET /v1/recipes/{name}", s.handleRecipeGet)
	mux.HandleFunc("POST /v1/recipes/{name}", s.handleRecipeSave)
	mux.HandleFunc("POST /v1/recipes/{name}/run", s.handleRecipeRun)
	mux.HandleFunc("POST /v1/intent", s.handleIntent)
}

// ---- diagnostics ----

func (s *Server) handleDiagnose(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Source  string `json:"source"`
		Session string `json:"session"`
		Repair  bool   `json:"repair"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Source) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("source is required"))
		return
	}
	s.warmIfCold(r.Context())
	cat := s.reg.DiagnoseCatalog()
	val := scripting.Validate(req.Source, &cat)
	s.recordDiagnosis(req.Session, len(val.Diagnostics), val.HasFatal())

	resp := map[string]any{
		"valid":          val.Valid,
		"diagnostics":    nonNilDiagnostics(val.Diagnostics),
		"fatal":          val.HasFatal(),
		"questions":      val.Questions,
		"suggestedFixes": val.SuggestedFixes,
	}

	if req.Repair && val.HasFatal() {
		prov := s.resolveProvider()
		if prov != nil {
			rep, rerr := scripting.Repair(r.Context(), req.Source, nil, val.Diagnostics, &cat, prov)
			if rerr == nil && rep != nil {
				resp["repaired"] = rep.RepairedSource
				resp["repairedValid"] = !rep.Validation.HasFatal()
			}
		}
	}

	writeJSON(w, 200, resp)
}

// recordDiagnosis writes one line about a diagnostic run. Silent when the
// daemon has no durable log, which it survives.
func (s *Server) recordDiagnosis(session string, found int, fatal bool) {
	if s.sink == nil {
		return
	}
	attrs := map[string]any{
		logging.KeyEvent: "diagnose.run",
		"diagnostics":    found,
		"fatal":          fatal,
	}
	if session != "" {
		attrs["session"] = session
	}
	s.sink.Write(logging.Record{
		Time: time.Now(), Level: slog.LevelInfo,
		Msg: "diagnosed a script", Attrs: attrs,
	}, nil)
}

func nonNilDiagnostics(ds []diagnose.Diagnostic) []diagnose.Diagnostic {
	if ds == nil {
		return []diagnose.Diagnostic{}
	}
	return ds
}

func anyFatal(ds []diagnose.Diagnostic) bool {
	for _, d := range ds {
		if d.Fatal {
			return true
		}
	}
	return false
}

func (s *Server) handleCatalogHistory(w http.ResponseWriter, r *http.Request) {
	all := map[string][]diagnose.Change{}
	if s.consumer != nil {
		all = s.consumer.history.All()
	}
	if tool := r.URL.Query().Get("tool"); tool != "" {
		one := map[string][]diagnose.Change{}
		if ch, ok := all[tool]; ok {
			one[tool] = ch
		}
		all = one
	}
	names := make([]string, 0, len(all))
	for k := range all {
		names = append(names, k)
	}
	sort.Strings(names)
	writeJSON(w, 200, map[string]any{"tools": names, "changes": all})
}

// ---- recipes ----

// listing strips the source from a recipe. A list of twenty recipes that
// carries twenty scripts is not a list, it is the directory again.
func listing(r recipes.Recipe) recipes.Recipe {
	r.Source = ""
	return r
}

func (s *Server) handleRecipesList(w http.ResponseWriter, r *http.Request) {
	all := s.loadRecipes()
	q := r.URL.Query().Get("q")
	if q == "" {
		out := make([]recipes.Recipe, 0, len(all))
		for _, rec := range all {
			out = append(out, listing(rec))
		}
		writeJSON(w, 200, map[string]any{"recipes": out, "dirs": s.consumer.dirs})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = consumerPolicyFrom(s.callSettings(r)).RecipeLimit
	}
	cands := recipes.Match(all, q, limit)
	for i := range cands {
		cands[i].Recipe = listing(cands[i].Recipe)
	}
	if cands == nil {
		cands = []recipes.Candidate{}
	}
	writeJSON(w, 200, map[string]any{"candidates": cands, "query": q})
}

func (s *Server) handleRecipeGet(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.findRecipe(r.PathValue("name"))
	if !ok {
		writeErr(w, http.StatusNotFound,
			fmt.Errorf("no recipe named %q; looked in %s",
				r.PathValue("name"), strings.Join(s.consumer.dirs, ", ")))
		return
	}
	writeJSON(w, 200, rec)
}

// recipeNameOK rejects anything that is not a plain name.
//
// The value becomes a filename, and a route that will write to
// `../../../etc` on request is not a feature anybody asked for.
func recipeNameOK(name string) bool {
	if name == "" || strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		return false
	}
	return name == filepath.Base(name)
}

func (s *Server) handleRecipeSave(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !recipeNameOK(name) {
		writeErr(w, http.StatusBadRequest,
			fmt.Errorf("%q is not a usable recipe name", name))
		return
	}
	var req struct {
		Source    string `json:"source"`
		Overwrite bool   `json:"overwrite"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Source) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("source is required"))
		return
	}
	dir, err := recipes.SaveDir(s.consumer.dirs)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	path := filepath.Join(dir, name+recipes.Ext)
	if _, err := os.Stat(path); err == nil && !req.Overwrite {
		writeErr(w, http.StatusConflict,
			fmt.Errorf("%s already exists; pass overwrite to replace it", path))
		return
	}
	body := req.Source
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, 200, map[string]any{
		"recipe": listing(recipes.Parse(name, path, body)),
		"path":   path,
	})
}

// resolution is the answer shape shared by recipe_run and intent.
//
// One shape for both because they are the same operation seen from two
// distances: intent picks the recipe, recipe_run is told which. A caller that
// handles one handles the other.
type resolution struct {
	Autonomy string `json:"autonomy"`
	// Requested and ClampedBy are set when autonomy.max lowered the level
	// the caller asked for: the ceiling lowers rather than refuses, and says
	// so (docs/decisions/0002-autonomy-dial.md).
	Requested    string                `json:"requested,omitempty"`
	ClampedBy    string                `json:"clampedBy,omitempty"`
	Recipe       string                `json:"recipe,omitempty"`
	Source       string                `json:"source,omitempty"`
	Placeholders map[string]any        `json:"placeholders,omitempty"`
	Diagnostics  []diagnose.Diagnostic `json:"diagnostics,omitempty"`
	Result       *ExecResult           `json:"result,omitempty"`
	// Elicit is the question mcpx raised to fill in what was missing, so a
	// caller can answer it or look up why nobody did.
	Elicit     string              `json:"elicit,omitempty"`
	Candidates []recipes.Candidate `json:"candidates,omitempty"`
	Generated  bool                `json:"generated,omitempty"`
	// Model says where the script came from: a recipe, sampling, or nothing.
	Model   string `json:"model,omitempty"`
	Message string `json:"message,omitempty"`
	// Save is the offer to keep a generated script that worked.
	Save *saveOffer `json:"save,omitempty"`
}

type saveOffer struct {
	SuggestedName string `json:"suggestedName"`
	How           string `json:"how"`
}

func (s *Server) handleRecipeRun(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Placeholders map[string]any `json:"placeholders"`
		Autonomy     string         `json:"autonomy"`
		Session      string         `json:"session"`
	}
	// Every field here is optional, so an empty body is a legal request for
	// a recipe that takes no placeholders.
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	rec, ok := s.findRecipe(r.PathValue("name"))
	if !ok {
		writeErr(w, http.StatusNotFound, fmt.Errorf("no recipe named %q", r.PathValue("name")))
		return
	}
	// A route named run runs. propose is here so a caller can see what
	// would happen first, which is the same courtesy intent extends by
	// default.
	cs := s.callSettings(r)
	level, err := promptAutonomy(cs, req.Autonomy, "run")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	r = r.WithContext(withPolicy(r.Context(), consumerPolicyFrom(cs)))
	out, code := s.resolveRecipe(r, rec, req.Placeholders, level, req.Session)
	writeJSON(w, code, out)
}

// resolveRecipe fills a recipe's holes, checks the result and, if asked,
// runs it.
func (s *Server) resolveRecipe(r *http.Request, rec recipes.Recipe,
	values map[string]any, level settings.Clamp, session string) (resolution, int) {
	s.warmIfCold(r.Context())

	out := resolution{Recipe: rec.Name, Model: "recipe"}
	out.setLevel(level)
	if values == nil {
		values = map[string]any{}
	}
	if missing := rec.Missing(values); len(missing) > 0 {
		answered, id, err := s.elicitPlaceholders(r.Context(), rec, missing, session)
		out.Elicit = id
		if err != nil {
			out.Message = err.Error()
			return out, http.StatusUnprocessableEntity
		}
		for k, v := range answered {
			if _, already := values[k]; !already {
				values[k] = v
			}
		}
	}
	if missing := rec.Missing(values); len(missing) > 0 {
		out.Message = fmt.Sprintf("%s still needs %s", rec.Name, names(missing))
		return out, http.StatusUnprocessableEntity
	}
	out.Placeholders = values

	source, err := rec.Render(values)
	if err != nil {
		out.Message = err.Error()
		return out, http.StatusUnprocessableEntity
	}
	out.Diagnostics = nonNilDiagnostics(diagnose.Script(source, s.reg.DiagnoseCatalog()))
	if !settings.AutonomyAtLeast(level.Value, "propose") {
		out.Message = belowPropose(level.Value)
		return out, 200
	}
	out.Source = source
	if anyFatal(out.Diagnostics) {
		out.Message = "the recipe does not match the tools as they are now"
		return out, http.StatusUnprocessableEntity
	}
	if level.Value != "run" {
		return out, 200
	}
	res, rerr := s.execScript(r.Context(), source, session)
	if rerr != nil {
		out.Message = rerr.Error()
		return out, http.StatusInternalServerError
	}
	out.Result = res
	return out, 200
}

// ---- intent ----

func (s *Server) handleIntent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Prompt       string         `json:"prompt"`
		Autonomy     string         `json:"autonomy"`
		Placeholders map[string]any `json:"placeholders"`
		Session      string         `json:"session"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("prompt is required"))
		return
	}
	// The caller's own policy, per request: its prompt.autonomy from the
	// environment, a flag or PUT /v1/settings arrives on this request, not
	// in the files the daemon read when it started. Reading the startup
	// copy ignored all three while PUT answered "applied".
	cs := s.callSettings(r)
	pol := consumerPolicyFrom(cs)
	r = r.WithContext(withPolicy(r.Context(), pol))
	level, err := promptAutonomy(cs, req.Autonomy, "")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	all := s.loadRecipes()
	cands := recipes.Match(all, req.Prompt, pol.RecipeLimit)
	if best, ok := recipes.Decide(cands,
		pol.RecipeMinScore, pol.RecipeMatchMargin); ok {
		out, code := s.resolveRecipe(r, best.Recipe, req.Placeholders, level, req.Session)
		out.Candidates = stripSources(cands)
		writeJSON(w, code, out)
		return
	}

	out := resolution{Candidates: stripSources(cands)}
	out.setLevel(level)
	if !settings.AutonomyAtLeast(level.Value, "propose") {
		// Generating a script nobody may see would spend a model's time on
		// nothing, so below propose there is no sampling request at all.
		out.Model = "none"
		out.Message = belowPropose(level.Value)
		writeJSON(w, 200, out)
		return
	}
	prov := s.resolveProvider()
	if (pol.PromptSample != "ask" || s.reg.broker == nil) && prov == nil {
		out.Model = "none"
		out.Message = "no recipe matched, and generating one is off " +
			"(prompt.sample is " + pol.PromptSample + "). " +
			"The ranked recipes above are everything mcpx can offer deterministically."
		writeJSON(w, 200, out)
		return
	}

	source, id, err := s.generate(r.Context(), req.Prompt, req.Session)
	if err != nil {
		out.Model = "none"
		out.Message = fmt.Sprintf(
			"no recipe matched, and no model answered the sampling request within %s "+
				"(elicitation %s). mcpx has no model of its own: generation needs "+
				"whatever drives it -- the agent, the opencode plugin, or an MCP "+
				"client that declared sampling -- to answer. The ranked recipes "+
				"above are everything mcpx can offer without one.",
			pol.PromptSampleTimeout, id)
		writeJSON(w, 200, out)
		return
	}

	out.Generated = true
	if strings.HasPrefix(id, "provider:") {
		out.Model = id
	} else {
		out.Model = "sampling"
	}
	out.Source = source
	cat := s.reg.DiagnoseCatalog()
	val := scripting.Validate(source, &cat)
	out.Diagnostics = nonNilDiagnostics(val.Diagnostics)
	if val.HasFatal() && prov != nil {
		if rep, rerr := scripting.Repair(r.Context(), source, nil, val.Diagnostics, &cat, prov); rerr == nil && rep != nil {
			out.Source = rep.RepairedSource
			source = rep.RepairedSource
			out.Diagnostics = nonNilDiagnostics(rep.Validation.Diagnostics)
		}
	}
	if anyFatal(out.Diagnostics) {
		out.Message = "the generated script does not match the tools as they are; " +
			"it is returned unrun so it can be corrected"
		writeJSON(w, http.StatusUnprocessableEntity, out)
		return
	}
	if level.Value == "run" {
		res, rerr := s.execScript(r.Context(), source, req.Session)
		if rerr != nil {
			out.Message = rerr.Error()
			writeJSON(w, http.StatusInternalServerError, out)
			return
		}
		out.Result = res
	}
	// The offer, not the act. Saving is a write into the user's project and
	// belongs to them; what mcpx can usefully do is say how.
	name := suggestName(req.Prompt)
	out.Save = &saveOffer{
		SuggestedName: name,
		How: fmt.Sprintf("POST /v1/recipes/%s {\"source\": ...}, or "+
			"`mcpx recipes save %s <file>`; then this request is free next time", name, name),
	}
	writeJSON(w, 200, out)
}

func stripSources(cs []recipes.Candidate) []recipes.Candidate {
	out := make([]recipes.Candidate, 0, len(cs))
	for _, c := range cs {
		c.Recipe = listing(c.Recipe)
		out = append(out, c)
	}
	return out
}

// suggestName turns a request into a plausible filename.
func suggestName(prompt string) string {
	terms := recipes.Terms(prompt)
	if len(terms) > 4 {
		terms = terms[:4]
	}
	if len(terms) == 0 {
		return "recipe"
	}
	return strings.Join(terms, "-")
}

// promptAutonomy validates a requested level for the prompt routes, which
// have two of the dial's six: propose returns the script, run executes it,
// and bounds it by autonomy.max. An unknown value is refused -- "plan" used
// to be read as the default without a word, which is the silent downgrade
// the dial exists to end. A known value above the ceiling is lowered, not
// refused, and the Clamp says so. An empty requested is fallback, or the
// caller's own prompt.autonomy when fallback is empty too.
func promptAutonomy(cs *settings.Set, requested, fallback string) (settings.Clamp, error) {
	switch requested {
	case "":
		requested = fallback
	case "propose", "run":
	default:
		return settings.Clamp{}, fmt.Errorf("autonomy %q: the prompt routes take propose or run "+
			"(docs/decisions/0002-autonomy-dial.md)", requested)
	}
	return cs.Clamp("prompt.autonomy", requested)
}

func (o *resolution) setLevel(c settings.Clamp) {
	o.Autonomy = c.Value
	if c.Lowered() {
		o.Requested = c.Requested
		o.ClampedBy = c.ClampedBy()
	}
}

// belowPropose is the answer at a level that may not hand back a script:
// autonomy.max set to off, advise or ask.
func belowPropose(level string) string {
	return fmt.Sprintf("autonomy is %s, which does not permit returning a script, "+
		"let alone running one; the diagnostics and candidates are all this "+
		"daemon will give (autonomy.max)", level)
}
