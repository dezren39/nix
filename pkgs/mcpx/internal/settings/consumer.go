package settings

import (
	"encoding/json"
	"os"
	"strconv"

	"github.com/dezren39/mcpx/internal/defaults"
)

// consumerSettings are the knobs for the things mcpx asks for itself.
//
// Sampling, elicitation and completion are things a server asks of its
// client, and mcpx is a client to its upstream servers. Everything here is a
// case where mcpx stops forwarding and initiates -- which is exactly the
// behaviour somebody will want to switch off, so each one is a setting rather
// than a decision buried in a handler.
//
// Defaults are taken from the embedded layer rather than written twice.
func consumerSettings() []Setting {
	return []Setting{
		{
			Path: "elicit.disambiguate", Kind: KindEnum, Default: defaults.Disambiguate,
			Enum: []string{"never", "ask"},
			Name: "Disambiguate instances",
			Short: "ask which instance of a stateful server to use when several " +
				"are live and the script did not choose",
			Long: "With several exclusive instances of one server -- three browsers, " +
				"three indexes -- a script that names none of them gets whichever " +
				"the scope resolves to, which may be a fourth. Set to ask and mcpx " +
				"raises a question listing the live ones, who holds each and what " +
				"it has been doing. Never is the default because a question nobody " +
				"answers costs the whole deadline.",
		},
		{
			Path: "elicit.disambiguateDefault", Kind: KindEnum, Default: defaults.DisambiguateDefault,
			Enum: []string{"new", "recent"},
			Name: "Disambiguation default",
			Short: "which instance is used when nobody answers in time: the one " +
				"the scope would have chosen, or the most recently used",
			Long: "Every question needs an answer for the case where there is nobody " +
				"to ask. New keeps today's behaviour, which is the conservative " +
				"choice: a caller that wanted a particular browser and did not say " +
				"so gets a fresh one rather than somebody else's.",
		},
		{
			Path: "elicit.confirmDestructive", Kind: KindBool, Default: boolDefault(defaults.ConfirmDestructive),
			Name:  "Confirm destructive calls",
			Short: "ask before a call to a tool annotated destructiveHint",
			Long: "MCP tool annotations carry destructiveHint, and a human-driven " +
				"client uses it to ask before acting. A script has no such step. " +
				"Turning this on gives it one. Off by default: it puts a question " +
				"on the path of every destructive call, and a caller that cannot " +
				"answer waits out the deadline before the call proceeds.",
		},
		{
			Path: "elicit.askTimeout", Kind: KindDuration, Default: defaults.AskTimeout.String(),
			Name:  "Question deadline",
			Short: "how long mcpx waits for an answer to a question it raised itself",
			Long: "Bounded far below the deadline on a question an upstream server " +
				"asked, because this one is on the path of a call that would " +
				"otherwise have already happened.",
		},
		{
			Path: "diagnose.preflight", Kind: KindBool, Default: boolDefault(defaults.DiagnosePreflight),
			Commands: []string{"run", "exec", "diagnose"},
			Name:     "Diagnose before running",
			Short:    "check a script's tool calls against the live schemas first",
			Long: "Deterministic and local: it compares what the script calls against " +
				"what the servers currently publish, and explains any difference " +
				"in terms of what changed and when. No model is involved.",
		},
		{
			Path: "diagnose.history", Kind: KindInt, Default: strconv.Itoa(defaults.HistoryPerTool),
			Plumbing: true,
			Name:     "Schema changes remembered",
			Short:    "how many changes are kept per tool",
		},
		{
			Path: "recipes.minScore", Kind: KindInt, Default: strconv.Itoa(defaults.RecipeMinScore),
			Name:  "Recipe match floor",
			Short: "the score below which a request is not considered a match for any recipe",
		},
		{
			Path: "recipes.matchMargin", Kind: KindInt, Default: strconv.Itoa(defaults.RecipeMatchMargin),
			Name:  "Recipe match margin",
			Short: "how far ahead the best recipe must be, as a percentage of the next one",
			Long: "150 means half as much again. Below the margin mcpx returns the " +
				"candidates instead of choosing, because running the wrong saved " +
				"script is a side effect rather than a wrong answer.",
		},
		{
			Path: "recipes.limit", Kind: KindInt, Default: strconv.Itoa(defaults.RecipeLimit),
			Name: "Recipe candidates", Short: "how many ranked recipes are returned",
		},
		{
			Path: "prompt.mode", Kind: KindEnum, Default: defaults.PromptMode,
			Enum: []string{"script", "run"},
			Name: "Prompt mode", Short: "whether a request returns the script or runs it",
			Long: "Returning the script is the default and the safe direction: " +
				"generated code runs with your credentials, and review before " +
				"execution needs no new trust decision.",
		},
		{
			Path: "prompt.sample", Kind: KindEnum, Default: defaults.PromptSample,
			Enum: []string{"never", "ask"},
			Name: "Generate by sampling",
			Short: "whether a request with no matching recipe may ask the caller's " +
				"model to write one",
			Long: "mcpx has no model. Set to ask, it raises a sampling request " +
				"through the broker, which whatever drives mcpx may answer. Never " +
				"is the default for the reason the feature was sequenced last: " +
				"most callers cannot answer sampling, and one that cannot pays " +
				"the whole deadline to find out. Turn it on where the harness " +
				"answers -- the opencode plugin, an agent watching `mcpx elicit`, " +
				"an MCP client that declared the capability.",
		},
		{
			Path: "prompt.catalogBudget", Kind: KindInt, Default: strconv.Itoa(defaults.PromptCatalogBudget),
			Name:  "Prompt catalog budget",
			Short: "approximate token ceiling for the slice of catalog sent with a sampling request",
			Long: "The point of mcpx is that tool schemas stay out of a model's " +
				"context. A generation request has to send some, so it sends the " +
				"slice the existing search ranked for this request and no more.",
		},
		{
			Path: "prompt.sampleTimeout", Kind: KindDuration, Default: defaults.PromptSampleTimeout.String(),
			Name:  "Sampling deadline",
			Short: "how long a generation request waits for a model",
			Long: "When it expires, mcpx concludes there is no sampling answerer and " +
				"returns the ranked recipes instead.",
		},
		{
			Path: "prompt.maxTokens", Kind: KindInt, Default: strconv.Itoa(defaults.PromptMaxTokens),
			Plumbing: true,
			Name:     "Sampling token ceiling", Short: "maxTokens on the sampling request",
		},
		{
			Path: "prompt.runTimeout", Kind: KindDuration, Default: defaults.RunTimeout.String(),
			Name: "Recipe run timeout", Short: "how long a recipe or generated script may run",
		},
	}
}

func boolDefault(b bool) string { return strconv.FormatBool(b) }

// Resolve builds a settings set outside the CLI.
//
// The daemon needs the same answers the command line does -- whether to ask
// before a destructive call is not a question with two correct answers
// depending on which process is asking -- but it has no App and never sees a
// flag. So it folds the same two layers everything else starts from: the
// configuration files that produced it, farthest first, and the environment.
//
// A file that will not parse is skipped rather than fatal. The daemon is
// already running by the time this is called, and refusing to answer because
// a policy file is malformed would take out the tool calls too.
func Resolve(filesNearestFirst []string) (*Set, error) {
	sch, err := New(Registry())
	if err != nil {
		return nil, err
	}
	set := NewSet(sch)
	for i := len(filesNearestFirst) - 1; i >= 0; i-- {
		path := filesNearestFirst[i]
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			continue
		}
		var doc map[string]any
		if json.Unmarshal(b, &doc) != nil {
			continue
		}
		rank := len(filesNearestFirst) - 1 - i
		if aerr := sch.ApplyFile(set, doc, path, rank); aerr != nil {
			return nil, aerr
		}
	}
	if eerr := sch.ApplyEnv(set, Environ()); eerr != nil {
		return nil, eerr
	}
	return set, nil
}
