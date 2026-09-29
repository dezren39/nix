package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/dezren39/mcpx/internal/diagnose"
	"github.com/dezren39/mcpx/internal/recipes"
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
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Source) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("source is required"))
		return
	}
	ds := diagnose.Script(req.Source, s.reg.DiagnoseCatalog())
	writeJSON(w, 200, map[string]any{
		"diagnostics": nonNilDiagnostics(ds),
		"fatal":       anyFatal(ds),
	})
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
		limit = s.consumer.policy.RecipeLimit
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
	Mode         string                `json:"mode"`
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
		Mode         string         `json:"mode"`
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
	// A route named run runs. The script mode is here so a caller can see
	// what would happen first, which is the same courtesy intent extends by
	// default.
	mode := req.Mode
	if mode == "" {
		mode = "run"
	}
	out, code := s.resolveRecipe(r, rec, req.Placeholders, mode, req.Session)
	writeJSON(w, code, out)
}

// resolveRecipe fills a recipe's holes, checks the result and, if asked,
// runs it.
func (s *Server) resolveRecipe(r *http.Request, rec recipes.Recipe,
	values map[string]any, mode, session string) (resolution, int) {

	out := resolution{Mode: mode, Recipe: rec.Name, Model: "recipe"}
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
	out.Source = source
	out.Diagnostics = nonNilDiagnostics(diagnose.Script(source, s.reg.DiagnoseCatalog()))
	if anyFatal(out.Diagnostics) {
		out.Message = "the recipe does not match the tools as they are now"
		return out, http.StatusUnprocessableEntity
	}
	if mode != "run" {
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
		Mode         string         `json:"mode"`
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
	mode := req.Mode
	if mode == "" {
		mode = s.consumer.policy.PromptMode
	}

	all := s.loadRecipes()
	cands := recipes.Match(all, req.Prompt, s.consumer.policy.RecipeLimit)
	if best, ok := recipes.Decide(cands,
		s.consumer.policy.RecipeMinScore, s.consumer.policy.RecipeMatchMargin); ok {
		out, code := s.resolveRecipe(r, best.Recipe, req.Placeholders, mode, req.Session)
		out.Candidates = stripSources(cands)
		writeJSON(w, code, out)
		return
	}

	out := resolution{Mode: mode, Candidates: stripSources(cands)}
	if s.consumer.policy.PromptSample != "ask" || s.reg.broker == nil {
		out.Model = "none"
		out.Message = "no recipe matched, and generating one is off " +
			"(prompt.sample is " + s.consumer.policy.PromptSample + "). " +
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
			s.consumer.policy.PromptSampleTimeout, id)
		writeJSON(w, 200, out)
		return
	}

	out.Generated = true
	out.Model = "sampling"
	out.Source = source
	out.Diagnostics = nonNilDiagnostics(diagnose.Script(source, s.reg.DiagnoseCatalog()))
	if anyFatal(out.Diagnostics) {
		out.Message = "the generated script does not match the tools as they are; " +
			"it is returned unrun so it can be corrected"
		writeJSON(w, http.StatusUnprocessableEntity, out)
		return
	}
	if mode == "run" {
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
