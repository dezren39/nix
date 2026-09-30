package recipes

import (
	"sort"
	"strings"
)

// Candidate is one recipe ranked against a request.
type Candidate struct {
	Recipe Recipe `json:"recipe"`
	Score  int    `json:"score"`
	// Why lists what matched, because a ranking nobody can inspect is a
	// ranking nobody can correct. It is also what /v1/intent shows when no
	// model is available to generate anything.
	Why []string `json:"why,omitempty"`
}

// stopwords are the words that carry no signal in a request like "go to the
// site and take a screenshot". Kept deliberately short: an aggressive list
// starts removing words that are the whole point ("open", "close").
var stopwords = map[string]bool{
	"a": true, "an": true, "and": true, "the": true, "to": true, "of": true,
	"for": true, "in": true, "on": true, "with": true, "please": true,
	"me": true, "my": true, "then": true, "it": true, "this": true, "that": true,
	"is": true, "be": true, "can": true, "you": true, "i": true,
}

// Match ranks recipes against free text, deterministically.
//
// No model, no embedding, no index. The scoring is over the four things a
// recipe already carries -- its name, its summary, its placeholder names and
// the tools it calls -- because those are what somebody describing the task
// will use the words of. It is not clever, and that is the feature: the same
// request produces the same recipe every time, at no cost.
func Match(rs []Recipe, text string, limit int) []Candidate {
	terms := Terms(text)
	if len(terms) == 0 {
		return nil
	}
	var out []Candidate
	for _, r := range rs {
		c := score(r, terms)
		if c.Score <= 0 {
			continue
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Recipe.Name < out[j].Recipe.Name
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Terms splits a request into the words worth matching on.
func Terms(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	var out []string
	seen := map[string]bool{}
	for _, f := range fields {
		if len(f) < 2 || stopwords[f] || seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	return out
}

// The weights. Written out rather than tuned: a name is what somebody would
// say if they knew the recipe existed, so it outranks everything; a tool name
// is the strongest evidence for somebody who does not.
const (
	weightNameExact   = 100
	weightNameTerm    = 40
	weightToolTerm    = 14
	weightSummaryTerm = 12
	weightParamTerm   = 8
)

func score(r Recipe, terms []string) Candidate {
	c := Candidate{Recipe: r}
	name := strings.ToLower(r.Name)
	nameTerms := map[string]bool{}
	for _, t := range Terms(r.Name) {
		nameTerms[t] = true
	}
	summary := strings.ToLower(r.Summary)

	joined := strings.Join(terms, " ")
	if name == joined || name == strings.ReplaceAll(joined, " ", "-") ||
		name == strings.ReplaceAll(joined, " ", "_") {
		c.Score += weightNameExact
		c.Why = append(c.Why, "the name is exactly the request")
	}
	for _, term := range terms {
		switch {
		case nameTerms[term]:
			c.Score += weightNameTerm
			c.Why = append(c.Why, "name mentions "+term)
		case strings.Contains(name, term):
			c.Score += weightNameTerm / 2
			c.Why = append(c.Why, "name contains "+term)
		}
		if strings.Contains(summary, term) {
			c.Score += weightSummaryTerm
			c.Why = append(c.Why, "description mentions "+term)
		}
		for _, tool := range r.Tools {
			if strings.Contains(strings.ToLower(tool), term) {
				c.Score += weightToolTerm
				c.Why = append(c.Why, "calls "+tool)
				break
			}
		}
		for _, p := range r.Placeholders {
			if strings.Contains(strings.ToLower(p.Name), term) ||
				strings.Contains(strings.ToLower(p.Description), term) {
				c.Score += weightParamTerm
				c.Why = append(c.Why, "takes "+p.Name)
				break
			}
		}
	}
	c.Why = dedupe(c.Why)
	return c
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// Decide picks a winner, or refuses to.
//
// Two rules, both about not being clever. A candidate below the floor is not
// a match at all. A candidate that is not ahead of the next one by the
// required margin is not a choice mcpx should make silently: running the
// wrong saved script is a side effect, not a wrong answer, and it happens
// with the user's credentials.
//
// marginPercent is how far ahead the winner must be, as a percentage of the
// runner-up: 150 means half as much again.
func Decide(cands []Candidate, floor, marginPercent int) (Candidate, bool) {
	if len(cands) == 0 || cands[0].Score < floor {
		return Candidate{}, false
	}
	if len(cands) > 1 && cands[0].Score*100 < cands[1].Score*marginPercent {
		return Candidate{}, false
	}
	return cands[0], true
}
