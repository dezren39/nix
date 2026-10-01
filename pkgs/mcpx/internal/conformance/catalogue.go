// Package conformance is the MCP conformance matrix: every normative
// requirement of every specification revision, which side of the wire it
// binds, whether it applies to mcpx, and which tests hold mcpx to it.
//
// The catalogue is data (requirements.json), generated from the prose
// catalogue docs/spec/requirements.md by cmd/genreq, with applicability
// re-judged per requirement in overrides/*.json. Coverage is code
// (coverage_*.go) so a renamed test breaks the build of the matrix rather
// than silently orphaning a row: matrix_test.go parses the Go test sources
// and refuses a reference to a test that does not exist.
//
// Nothing in the mcpx binary imports this package.
package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Revisions are the specification revisions the catalogue covers, oldest
// first. A fact about the specification, not a setting.
var Revisions = []string{"2024-11-05", "2025-03-26", "2025-06-18", "2025-11-25", "2026-07-28"}

// SpecBase is the prefix of every specification URL.
const SpecBase = "https://modelcontextprotocol.io/specification/"

// Sides mcpx can occupy.
const (
	Server = "server"
	Client = "client"
)

// Applicability values: "applies", "n/a:<reason>", "not-implemented:<issue>".
const (
	Applies        = "applies"
	NAPrefix       = "n/a:"
	NotImplPrefix  = "not-implemented:"
	oauthIssue     = "#253"
	notChosenLabel = "optional feature mcpx does not implement"
)

// Requirement is one row of the catalogue.
type Requirement struct {
	ID    string   `json:"id"`
	Revs  []string `json:"revs"`
	Side  string   `json:"side"`
	Level string   `json:"level"`
	Area  string   `json:"area"`
	URL   string   `json:"url"`
	Quote string   `json:"quote"`
	// Applicability is per side: "server" and "client".
	Applicability map[string]string `json:"applicability"`
	// Notes are the catalogue's original judgement of mcpx, kept so a
	// reviewer can see what the applicability was derived from.
	Notes map[string]string `json:"notes,omitempty"`
	// Claims is what a note asserts about mcpx, per side, read from the
	// prose so the matrix can hold the note to the tests: ClaimGap when the
	// note says mcpx does not meet the requirement.
	Claims map[string]string `json:"claims,omitempty"`
}

// ClaimGap is a note's claim that mcpx does not meet a requirement.
const ClaimGap = "gap"

// gapWordRe is how the catalogue's notes say "gap": `**gap**`, `GAP`,
// "gap (...)", "GAP-ish".
var gapWordRe = regexp.MustCompile(`(?i)\bgap\b`)

// claimsOf reads the notes' claims.
func claimsOf(notes map[string]string) map[string]string {
	var out map[string]string
	for side, n := range notes {
		if gapWordRe.MatchString(n) {
			if out == nil {
				out = map[string]string{}
			}
			out[side] = ClaimGap
		}
	}
	return out
}

// Override re-judges one requirement's applicability. Empty fields keep the
// derived value.
type Override struct {
	Server string `json:"server,omitempty"`
	Client string `json:"client,omitempty"`
	Why    string `json:"why,omitempty"`
}

var (
	revRe   = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)
	rangeRe = regexp.MustCompile(`(\d{4}-\d{2}-\d{2})\s*(?:\.\.|–|-)\s*(\d{4}-\d{2}-\d{2})`)
)

// ExpandRevs turns "a..b", "a – b", "a" or "a (ext)" into the revisions it
// names.
func ExpandRevs(s string) ([]string, error) {
	if m := rangeRe.FindStringSubmatch(s); m != nil {
		var out []string
		in := false
		for _, r := range Revisions {
			if r == m[1] {
				in = true
			}
			if in {
				out = append(out, r)
			}
			if r == m[2] {
				if !in {
					break
				}
				return out, nil
			}
		}
		return nil, fmt.Errorf("revision range %q", s)
	}
	var out []string
	for _, r := range revRe.FindAllString(s, -1) {
		if !isRev(r) {
			return nil, fmt.Errorf("unknown revision %q in %q", r, s)
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no revision in %q", s)
	}
	return out, nil
}

func isRev(r string) bool {
	for _, x := range Revisions {
		if x == r {
			return true
		}
	}
	return false
}

func splitRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	var cells []string
	start := 0
	for i := 0; i < len(line); i++ {
		if line[i] == '|' && (i == 0 || line[i-1] != '\\') {
			cells = append(cells, strings.TrimSpace(line[start:i]))
			start = i + 1
		}
	}
	return append(cells, strings.TrimSpace(line[start:]))
}

// sidesOf reads the catalogue's side column: which of mcpx's two roles the
// requirement addresses. An intermediary requirement binds mcpx in both,
// since mcpx is one.
func sidesOf(side string) (server, client bool) {
	s := strings.ToLower(side)
	if i := strings.Index(s, "("); i >= 0 {
		s = s[:i]
	}
	for _, tok := range strings.Split(s, ",") {
		switch strings.TrimSpace(tok) {
		case "both", "intermediary":
			server, client = true, true
		case "server":
			server = true
		case "client":
			client = true
		}
	}
	return server, client
}

// derive turns the catalogue's free-text judgement into an applicability.
// Anything not plainly inapplicable applies: a requirement wrongly marked as
// applying shows up as a missing test, which someone then looks at; one
// wrongly marked n/a is never looked at again.
func derive(bound bool, area, note string) string {
	if !bound {
		return NAPrefix + "addresses the other side"
	}
	if strings.Contains(area, "authorization") {
		return NotImplPrefix + oauthIssue
	}
	n := strings.ToLower(strings.TrimSpace(note))
	switch {
	case strings.HasPrefix(n, "n/a"):
		return NAPrefix + reason(note, "n/a")
	case strings.HasPrefix(n, "not used"):
		return NAPrefix + reason(note, "not used")
	case strings.Contains(n, "not-implemented-by-choice"):
		return NAPrefix + notChosenLabel
	}
	return Applies
}

func reason(note, prefix string) string {
	r := strings.TrimSpace(note[len(prefix):])
	r = strings.Trim(r, " ()—-:;,")
	if r == "" {
		return "catalogue marks it n/a"
	}
	return r
}

// Parse reads docs/spec/requirements.md.
func Parse(path string) ([]Requirement, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Requirement
	inDupes := false
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "## ") {
			inDupes = strings.Contains(line, "Folded duplicates")
			continue
		}
		if inDupes || !strings.HasPrefix(line, "| ") {
			continue
		}
		c := splitRow(line)
		if len(c) != 10 || c[0] == "id" || strings.HasPrefix(c[0], "---") {
			continue
		}
		revs, err := ExpandRevs(c[1])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", c[0], err)
		}
		url := c[6]
		if !strings.HasPrefix(url, "http") {
			url = SpecBase + url
		}
		srv, cli := sidesOf(c[2])
		out = append(out, Requirement{
			ID: c[0], Revs: revs, Side: c[2], Level: c[3], Area: c[4], URL: url, Quote: c[5],
			Applicability: map[string]string{
				Server: derive(srv, c[4], c[7]),
				Client: derive(cli, c[4], c[8]),
			},
			Notes: map[string]string{Server: c[7], Client: c[8]},
		})
		out[len(out)-1].Claims = claimsOf(out[len(out)-1].Notes)
	}
	return out, nil
}

// LoadOverrides reads every overrides/*.json.
func LoadOverrides(dir string) (map[string]Override, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	out := map[string]Override{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var m map[string]Override
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		for id, o := range m {
			if _, dup := out[id]; dup {
				return nil, fmt.Errorf("%s: %s overridden twice", f, id)
			}
			out[id] = o
		}
	}
	return out, nil
}

// ValidApplicability reports whether v is one of the three forms.
func ValidApplicability(v string) bool {
	return v == Applies ||
		strings.HasPrefix(v, NAPrefix) && len(v) > len(NAPrefix) ||
		strings.HasPrefix(v, NotImplPrefix) && len(v) > len(NotImplPrefix)
}

// Build is Parse plus overrides.
func Build(mdPath, overridesDir string) ([]Requirement, error) {
	reqs, err := Parse(mdPath)
	if err != nil {
		return nil, err
	}
	ov, err := LoadOverrides(overridesDir)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for i := range reqs {
		r := &reqs[i]
		if seen[r.ID] {
			return nil, fmt.Errorf("duplicate id %s", r.ID)
		}
		seen[r.ID] = true
		o, ok := ov[r.ID]
		if !ok {
			continue
		}
		for side, v := range map[string]string{Server: o.Server, Client: o.Client} {
			if v == "" {
				continue
			}
			if !ValidApplicability(v) {
				return nil, fmt.Errorf("override %s/%s: %q", r.ID, side, v)
			}
			r.Applicability[side] = v
		}
		delete(ov, r.ID)
	}
	for id := range ov {
		return nil, fmt.Errorf("override for unknown requirement %s", id)
	}
	return reqs, nil
}

// Encode is the committed form of requirements.json.
func Encode(reqs []Requirement) ([]byte, error) {
	b, err := json.MarshalIndent(reqs, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Load reads requirements.json.
func Load(path string) ([]Requirement, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var reqs []Requirement
	return reqs, json.Unmarshal(b, &reqs)
}
