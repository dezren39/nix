package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/dezren39/mcpx/internal/diagnose"
	"github.com/dezren39/mcpx/internal/recipes"
	"github.com/dezren39/mcpx/internal/scripting"
)

// ---- client methods ----

// DiagnoseResult is what POST /v1/diagnose answers.
type DiagnoseResult struct {
	Valid          bool                            `json:"valid"`
	Diagnostics    []diagnose.Diagnostic           `json:"diagnostics"`
	Fatal          bool                            `json:"fatal"`
	Questions      []scripting.ClarificationQuestion `json:"questions,omitempty"`
	SuggestedFixes []string                        `json:"suggestedFixes,omitempty"`
	Repaired       string                          `json:"repaired,omitempty"`
	RepairedValid  bool                            `json:"repairedValid,omitempty"`
}

// Diagnose asks the daemon to explain a script.
func (c *Client) Diagnose(ctx context.Context, source, session string) (*DiagnoseResult, error) {
	return c.DiagnoseWithRepair(ctx, source, session, false)
}

// DiagnoseWithRepair asks the daemon to diagnose and optionally repair a script.
func (c *Client) DiagnoseWithRepair(ctx context.Context, source, session string, repair bool) (*DiagnoseResult, error) {
	b, err := c.do(ctx, "POST", "/v1/diagnose",
		map[string]any{"source": source, "session": session, "repair": repair})
	if err != nil {
		return nil, err
	}
	var out DiagnoseResult
	return &out, json.Unmarshal(b, &out)
}

// RecipeList is the unranked listing.
type RecipeList struct {
	Recipes []recipes.Recipe `json:"recipes"`
	Dirs    []string         `json:"dirs"`
}

// RecipeMatches is the ranked listing.
type RecipeMatches struct {
	Candidates []recipes.Candidate `json:"candidates"`
	Query      string              `json:"query"`
}

func (c *Client) Recipes(ctx context.Context) (*RecipeList, error) {
	b, err := c.do(ctx, "GET", "/v1/recipes", nil)
	if err != nil {
		return nil, err
	}
	var out RecipeList
	return &out, json.Unmarshal(b, &out)
}

func (c *Client) MatchRecipes(ctx context.Context, q string) (*RecipeMatches, error) {
	b, err := c.do(ctx, "GET", "/v1/recipes?q="+url.QueryEscape(q), nil)
	if err != nil {
		return nil, err
	}
	var out RecipeMatches
	return &out, json.Unmarshal(b, &out)
}

func (c *Client) Recipe(ctx context.Context, name string) (*recipes.Recipe, error) {
	b, err := c.do(ctx, "GET", "/v1/recipes/"+url.PathEscape(name), nil)
	if err != nil {
		return nil, err
	}
	var out recipes.Recipe
	return &out, json.Unmarshal(b, &out)
}

func (c *Client) SaveRecipe(ctx context.Context, name, source string, overwrite bool) (json.RawMessage, error) {
	return c.do(ctx, "POST", "/v1/recipes/"+url.PathEscape(name),
		map[string]any{"source": source, "overwrite": overwrite})
}

// Resolution is the answer shape recipe_run and intent share.
type Resolution struct {
	Autonomy     string                `json:"autonomy"`
	Requested    string                `json:"requested,omitempty"`
	ClampedBy    string                `json:"clampedBy,omitempty"`
	Recipe       string                `json:"recipe,omitempty"`
	Source       string                `json:"source,omitempty"`
	Placeholders map[string]any        `json:"placeholders,omitempty"`
	Diagnostics  []diagnose.Diagnostic `json:"diagnostics,omitempty"`
	Result       *struct {
		ExitCode int    `json:"exitCode"`
		Stdout   string `json:"stdout,omitempty"`
		Stderr   string `json:"stderr,omitempty"`
		Error    string `json:"error,omitempty"`
	} `json:"result,omitempty"`
	Elicit     string              `json:"elicit,omitempty"`
	Candidates []recipes.Candidate `json:"candidates,omitempty"`
	Generated  bool                `json:"generated,omitempty"`
	Model      string              `json:"model,omitempty"`
	Message    string              `json:"message,omitempty"`
	Save       *struct {
		SuggestedName string `json:"suggestedName"`
		How           string `json:"how"`
	} `json:"save,omitempty"`
}

func (c *Client) RunRecipe(ctx context.Context, name string, values map[string]any, autonomy, session string) (*Resolution, error) {
	return c.resolution(ctx, "/v1/recipes/"+url.PathEscape(name)+"/run",
		map[string]any{"placeholders": values, "autonomy": autonomy, "session": session})
}

func (c *Client) Intent(ctx context.Context, prompt, autonomy string, values map[string]any, session string) (*Resolution, error) {
	return c.resolution(ctx, "/v1/intent",
		map[string]any{"prompt": prompt, "autonomy": autonomy, "placeholders": values, "session": session})
}

// resolution posts and reads the answer, refusal included.
//
// A 422 from these routes is not a transport failure: it is the daemon
// saying which placeholder it still needs, or which diagnostic stopped the
// run. Throwing that away and printing "http 422" would lose the only part
// the caller can act on.
func (c *Client) resolution(ctx context.Context, path string, body any) (*Resolution, error) {
	b, err := c.do(ctx, "POST", path, body)
	if err != nil {
		var he *HTTPError
		if !errors.As(err, &he) || he.Status < 400 || he.Status >= 500 {
			return nil, err
		}
		b = he.Body
	}
	var out Resolution
	if jerr := json.Unmarshal(b, &out); jerr != nil {
		if err != nil {
			return nil, err
		}
		return nil, jerr
	}
	if out.Autonomy == "" && out.Message == "" {
		return nil, err
	}
	return &out, nil
}

// ---- mcpx diagnose ----

// CmdDiagnose explains what is wrong with a script without running it.
func (a *App) CmdDiagnose(ctx context.Context, args []string) error {
	fs := newFlagSet("diagnose")
	session := fs.String("session", "", "session key")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return errors.New("usage: mcpx diagnose <script|file|-|'<source>'>")
	}
	source, err := readSourceArg(strings.Join(fs.Args(), " "))
	if err != nil {
		return err
	}
	c, err := a.ensure(ctx)
	if err != nil {
		return err
	}
	// A daemon that has just started reads its schemas in the background.
	// Diagnosing against the catalog before that finishes compared the
	// script with nothing and reported "nothing to report" -- a false all
	// clear, and a flaky test, found by running the suite three times.
	if err := a.ensureAnySchemas(ctx, c); err != nil {
		return err
	}
	res, err := c.Diagnose(ctx, source, *session)
	if err != nil {
		return err
	}
	if a.JSON {
		return a.out(res)
	}
	if len(res.Diagnostics) == 0 {
		fmt.Println("Nothing to report: every call matches the schemas as they are now.")
		return nil
	}
	fmt.Println(diagnose.Render(res.Diagnostics))
	if res.Fatal {
		return errors.New("the script does not match the tools it calls")
	}
	return nil
}

// readSourceArg accepts a script name, a path, "-" for standard input, or
// the source itself. The same latitude `mcpx run` gives, for the same
// reason: whichever one somebody has to hand should work.
func readSourceArg(arg string) (string, error) {
	arg = strings.TrimSpace(arg)
	if arg == "-" {
		b, err := readAllStdin()
		return string(b), err
	}
	if !looksLikePath(arg) && !strings.ContainsAny(arg, "(){};\n") {
		if path, err := resolveScript(arg); err == nil {
			b, rerr := os.ReadFile(path)
			return string(b), rerr
		}
	}
	if b, err := os.ReadFile(arg); err == nil {
		return string(b), nil
	}
	return arg, nil
}

func readAllStdin() ([]byte, error) { return io.ReadAll(os.Stdin) }

// ---- mcpx recipes ----

// CmdRecipes lists, shows, matches, saves and runs saved scripts.
func (a *App) CmdRecipes(ctx context.Context, args []string) error {
	sub := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	// Flags after the subcommand and its positional arguments are normal to
	// type and would otherwise be read as key=value pairs.
	args = hoistFlags(args, map[string]bool{"autonomy": true, "session": true})
	fs := newFlagSet("recipes")
	autonomy := fs.String("autonomy", "", "with run: propose renders without running; run executes (the default)")
	script := fs.Bool("script", false, "with run: render without running, the same as --autonomy propose")
	overwrite := fs.Bool("overwrite", false, "replace an existing recipe")
	session := fs.String("session", "", "session key")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	c, err := a.ensure(ctx)
	if err != nil {
		return err
	}

	switch sub {
	case "", "list":
		list, lerr := c.Recipes(ctx)
		if lerr != nil {
			return lerr
		}
		if a.JSON {
			return a.out(list)
		}
		if len(list.Recipes) == 0 {
			fmt.Printf("No recipes. A recipe is a saved script that declares its holes:\n"+
				"  mkdir -p %s\n"+
				"  cat > %s/close-stale.ts <<'EOF'\n"+
				"  // Close stale issues.\n"+
				"  // @param repo:string   which repository\n"+
				"  // @param days:number = 30   how old counts as stale\n"+
				"  EOF\n", ScriptsDirName, ScriptsDirName)
			return nil
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tTAKES\tCALLS\tSUMMARY")
		for _, r := range list.Recipes {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Name,
				orDash(placeholderNames(r)), orDash(strings.Join(r.Tools, " ")),
				trunc(r.Summary, 44))
		}
		return w.Flush()

	case "show":
		if fs.NArg() == 0 {
			return errors.New("usage: mcpx recipes show <name>")
		}
		r, gerr := c.Recipe(ctx, fs.Arg(0))
		if gerr != nil {
			return gerr
		}
		if a.JSON {
			return a.out(r)
		}
		fmt.Printf("%s  (%s)\n", r.Name, r.Path)
		if r.Summary != "" {
			fmt.Println(r.Summary)
		}
		if len(r.Placeholders) > 0 {
			fmt.Println("\nTakes:")
			for _, p := range r.Placeholders {
				def := ""
				if p.Default != nil {
					def = " = " + *p.Default
				} else if !p.Required {
					def = " (optional)"
				}
				fmt.Printf("  %s: %s%s\t%s\n", p.Name, p.Type, def, p.Description)
			}
		}
		fmt.Printf("\n%s\n", r.Source)
		return nil

	case "match":
		if fs.NArg() == 0 {
			return errors.New("usage: mcpx recipes match <words...>")
		}
		res, merr := c.MatchRecipes(ctx, strings.Join(fs.Args(), " "))
		if merr != nil {
			return merr
		}
		if a.JSON {
			return a.out(res)
		}
		if len(res.Candidates) == 0 {
			fmt.Println("Nothing matched.")
			return nil
		}
		for _, cand := range res.Candidates {
			fmt.Printf("%4d  %-24s %s\n", cand.Score, cand.Recipe.Name,
				strings.Join(cand.Why, "; "))
		}
		return nil

	case "save":
		if fs.NArg() < 2 {
			return errors.New("usage: mcpx recipes save <name> <file|-|'<source>'>")
		}
		source, rerr := readSourceArg(strings.Join(fs.Args()[1:], " "))
		if rerr != nil {
			return rerr
		}
		raw, serr := c.SaveRecipe(ctx, fs.Arg(0), source, *overwrite)
		if serr != nil {
			return serr
		}
		if a.JSON {
			return a.out(json.RawMessage(raw))
		}
		var doc struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal(raw, &doc)
		fmt.Printf("Saved %s\nRun it with: mcpx recipes run %s\n", doc.Path, fs.Arg(0))
		return nil

	case "run":
		if fs.NArg() == 0 {
			return errors.New("usage: mcpx recipes run <name> [key=value...]")
		}
		values, verr := parseAssignments(fs.Args()[1:])
		if verr != nil {
			return verr
		}
		m := *autonomy
		if *script {
			m = "propose"
		}
		res, rerr := c.RunRecipe(ctx, fs.Arg(0), values, m, *session)
		if rerr != nil {
			return rerr
		}
		return a.showResolution(res)
	}
	return fmt.Errorf("unknown subcommand %q; try list, show, match, save or run", sub)
}

func placeholderNames(r recipes.Recipe) string {
	var out []string
	for _, p := range r.Placeholders {
		out = append(out, p.Name)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

// parseAssignments turns key=value arguments into typed values.
//
// A value that parses as JSON is used as JSON, so `count=3` is a number and
// `tags=["a"]` is an array, and anything else is the string it looks like.
// The alternative -- making everything a string -- pushes the conversion into
// every recipe.
func parseAssignments(args []string) (map[string]any, error) {
	out := map[string]any{}
	for _, kv := range args {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return nil, fmt.Errorf("expected key=value, got %q", kv)
		}
		var parsed any
		if json.Unmarshal([]byte(v), &parsed) == nil {
			out[k] = parsed
			continue
		}
		out[k] = v
	}
	return out, nil
}

// ---- mcpx prompt ----

// CmdPrompt turns a request in words into a script, or runs it.
func (a *App) CmdPrompt(ctx context.Context, args []string) error {
	args = hoistFlags(args, map[string]bool{"autonomy": true, "session": true, "set": true})
	fs := newFlagSet("prompt")
	run := fs.Bool("run", false, "run the script instead of returning it, the same as --autonomy run")
	autonomy := fs.String("autonomy", "", "propose returns the script; run executes it (default: prompt.autonomy)")
	session := fs.String("session", "", "session key")
	set := newRepeatable()
	fs.Var(set, "set", "placeholder value for a matched recipe, key=value; repeatable")
	if err := parseFlags(a, fs, args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return errors.New(`usage: mcpx prompt "what you want done"`)
	}
	var assignments []string
	for _, v := range set.Values() {
		if s, ok := v.(string); ok {
			assignments = append(assignments, s)
		}
	}
	values, err := parseAssignments(assignments)
	if err != nil {
		return err
	}
	m := *autonomy
	if *run {
		m = "run"
	}
	c, err := a.ensure(ctx)
	if err != nil {
		return err
	}
	res, err := c.Intent(ctx, strings.Join(fs.Args(), " "), m, values, *session)
	if err != nil {
		return err
	}
	return a.showResolution(res)
}

// showResolution renders what a recipe or a request produced.
func (a *App) showResolution(res *Resolution) error {
	// On stderr even with --json: a run that did not run because the
	// daemon's ceiling lowered it must not look like one that did.
	if res.ClampedBy != "" {
		fmt.Fprintf(os.Stderr, "mcpx: requested %s, clamped to %s by %s\n",
			res.Requested, res.Autonomy, res.ClampedBy)
	}
	if a.JSON {
		return a.out(res)
	}
	// "No model is available" is information, not a failure: it is the
	// default state of every caller that cannot answer sampling, and exiting
	// non-zero for it would make the ordinary case look broken.
	if res.Source == "" && res.Model == "none" {
		fmt.Println(res.Message)
		if len(res.Candidates) > 0 {
			fmt.Println("\nClosest saved recipes:")
			for _, cand := range res.Candidates {
				fmt.Printf("  %4d  %-24s %s\n", cand.Score, cand.Recipe.Name,
					trunc(cand.Recipe.Summary, 48))
			}
			fmt.Printf("\nRun one with: mcpx recipes run %s\n", res.Candidates[0].Recipe.Name)
		}
		return nil
	}
	if res.Recipe != "" {
		fmt.Fprintf(os.Stderr, "mcpx: recipe %s\n", res.Recipe)
	} else if res.Generated {
		fmt.Fprintln(os.Stderr, "mcpx: generated by sampling; review it before running")
	}
	if len(res.Diagnostics) > 0 {
		fmt.Fprintln(os.Stderr, diagnose.Render(res.Diagnostics))
	}
	if res.Result == nil {
		if res.Source == "" && res.Message != "" {
			// Nothing to hand back: the message is the answer, and it is a
			// refusal. Exiting zero here would make an unfilled recipe look
			// like a successful no-op.
			if res.Elicit != "" {
				return fmt.Errorf("%s\n  the question was %s; `mcpx elicit show %s` says what it asked",
					res.Message, res.Elicit, res.Elicit)
			}
			return errors.New(res.Message)
		}
		if res.Source != "" {
			fmt.Print(res.Source)
		}
		if res.Message != "" {
			fmt.Fprintln(os.Stderr, "mcpx:", res.Message)
		}
		if res.Save != nil {
			fmt.Fprintf(os.Stderr, "mcpx: keep it with `mcpx recipes save %s -`\n",
				res.Save.SuggestedName)
		}
		return nil
	}
	if res.Result.Stdout != "" {
		fmt.Print(res.Result.Stdout)
	}
	if res.Result.Stderr != "" {
		fmt.Fprint(os.Stderr, res.Result.Stderr)
	}
	if res.Result.Error != "" {
		return errors.New(res.Result.Error)
	}
	if res.Result.ExitCode != 0 {
		return fmt.Errorf("the script exited %d", res.Result.ExitCode)
	}
	if res.Save != nil {
		fmt.Fprintf(os.Stderr, "mcpx: it worked; keep it with `mcpx recipes save %s -`\n",
			res.Save.SuggestedName)
	}
	return nil
}

// ---- preflight wiring ----

// diagnoseBeforeRun checks a script's tool calls against the live schemas and
// reports anything that will not work.
//
// On the run path rather than only on demand, because the failure it catches
// -- a tool whose schema moved under a saved script -- surfaces otherwise as
// an error from the server, halfway through, after the side effects of every
// call before it.
func (a *App) diagnoseBeforeRun(ctx context.Context, c *Client, source, session string) error {
	if !a.Settings().Bool("diagnose.preflight") || strings.TrimSpace(source) == "" {
		return nil
	}
	res, err := c.Diagnose(ctx, source, session)
	if err != nil {
		// A daemon that cannot diagnose must not stop a run. The check is an
		// enrichment; the run is the thing that was asked for.
		return nil
	}
	if len(res.Diagnostics) == 0 {
		return nil
	}
	fmt.Fprintln(os.Stderr, diagnose.Render(res.Diagnostics))
	if res.Fatal {
		return errors.New("the script does not match the tools it calls; " +
			"set --diagnose-preflight=false to run it anyway")
	}
	return nil
}

// sourceOfScript reads a resolved script file for diagnosis.
func sourceOfScript(path string) string {
	b, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return ""
	}
	return string(b)
}
