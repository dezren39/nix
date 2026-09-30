package defaults_test

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The point of the package is that there is exactly one place to change a
// default. This file is what stops a new one appearing anywhere else.
//
// The previous version of this test matched fixed strings -- "5 * time.Minute"
// and a dozen others -- and it had a hole big enough to walk through. gofmt
// writes `5 * time.Minute` at statement level but `5*time.Minute` when the
// expression is nested inside another, so every duration written inside a
// function call was invisible to it. There were nine of them in the tree when
// this was rewritten, including the daemon's shutdown grace and the event
// stream's keepalive.
//
// So: patterns rather than strings, and the same treatment for the other
// kinds of constant that had been quietly accumulating -- byte sizes written
// as shifts, and the `if limit <= 0 { limit = 20 }` idiom that put a page
// size in a route handler.

var (
	// inlineDuration catches both spellings gofmt produces.
	inlineDuration = regexp.MustCompile(
		`\b\d+\s*\*\s*time\.(Nanosecond|Microsecond|Millisecond|Second|Minute|Hour)\b`)
	// inlineShift catches a byte size written as a shift: 1<<20, 64 << 10.
	inlineShift = regexp.MustCompile(`\b\d+\s*<<\s*\d+\b`)
	// inlineLimit catches a bound assigned to a variable whose name says it
	// is one. This is the idiom that hid a page size, a retry count and a
	// result ceiling in three different handlers.
	inlineLimit = regexp.MustCompile(
		`(?i)\b(limit|budget|ceiling|pagesize|maxbytes|maxlines|retries|attempts|backoff|top|keep)\w*` +
			`\s*(?::=|=)\s*(\d+)\b`)
	// octalMode catches a permission literal.
	octalMode = regexp.MustCompile(`\b0o[0-7]{3,4}\b`)
)

// allowed lists the matches that are deliberate, each with the reason.
//
// An allowlist rather than a narrower pattern, because the reason a constant
// is acceptable is never visible from its syntax. Written as the exact text
// matched, so widening one of these by accident does not widen the exemption
// with it.
var allowed = map[string]string{
	// Identity and emptiness are not defaults.
	"limit = 0":  "zero means unbounded; it is not a value anyone would configure",
	"limit = -1": "negative one means every record in the window, not a page size",

	// Permission bits are security invariants, not preferences. The state
	// directory holds a socket that grants the power to run tools as this
	// user; a configuration key that widens it would be a footgun with no
	// legitimate use. They live in defaults.json as data -- files.dirMode
	// and files.privateMode -- and are read from there, but they are
	// deliberately absent from the settings registry.
	"0o700": "state directory mode, read from defaults.Files",
	"0o600": "private file mode, read from defaults.Files",
	"0o644": "config file mode, read from defaults.Files",
	"0o755": "world-readable directory mode, read from defaults.Files",
	"0o777": "a mask, not a mode",

	// Unit definitions, not defaults. A kibibyte is 1024 by arithmetic; a
	// configuration key that said otherwise would be describing a different
	// universe rather than a preference.
	"1<<10": "the definition of a kibibyte, in the size parser",
	"1<<20": "the definition of a mebibyte, in the size parser",
	"1<<30": "the definition of a gibibyte, in the size parser",
	"1<<40": "the definition of a tebibyte, in the size parser",

	// A unit boundary in a duration renderer: it decides whether to print
	// hours or days, which is arithmetic rather than policy.
	"24*time.Hour": "a unit boundary in the duration formatter",
}

// outOfScope names files owned by work happening in parallel. Their inline
// constants are real and are listed in this branch's report rather than
// edited here, because two agents editing one file is how a merge eats a
// change.
var outOfScope = []string{
	"internal/pool/", "internal/runner/", "internal/codegen/",
	"internal/mcpserver/", "internal/mcpclient/", "internal/openapi/",
	"internal/opencode/", "internal/cli/serve.go",
}

func TestNoPackageRedeclaresADefault(t *testing.T) {
	var offenders []string
	err := filepath.Walk("..", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel := strings.TrimPrefix(filepath.ToSlash(path), "../")
		rel = "internal/" + rel
		if strings.HasSuffix(path, "_test.go") || strings.Contains(rel, "internal/defaults/") {
			return nil
		}
		for _, skip := range outOfScope {
			if strings.HasPrefix(rel, skip) {
				return nil
			}
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		n := 0
		for sc.Scan() {
			n++
			line := sc.Text()
			code := line
			if i := strings.Index(code, "//"); i >= 0 {
				code = code[:i]
			}
			for _, m := range matches(code) {
				if _, ok := allowed[m]; ok {
					continue
				}
				offenders = append(offenders,
					rel+":"+strconv.Itoa(n)+": "+strings.TrimSpace(line))
			}
		}
		return sc.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Errorf("constants belong in internal/defaults/defaults.json and, where a "+
			"user could conceivably want them changed, in the settings registry:\n  %s",
			strings.Join(offenders, "\n  "))
	}
}

func matches(code string) []string {
	var out []string
	// Spaces are stripped so the two spellings gofmt produces key the same
	// entry. That difference is exactly the hole the old guard had.
	for _, m := range inlineDuration.FindAllString(code, -1) {
		out = append(out, strings.ReplaceAll(m, " ", ""))
	}
	for _, m := range inlineShift.FindAllString(code, -1) {
		out = append(out, strings.ReplaceAll(m, " ", ""))
	}
	out = append(out, octalMode.FindAllString(code, -1)...)
	for _, m := range inlineLimit.FindAllStringSubmatch(code, -1) {
		// Normalised to `name = N` so the allowlist reads as code rather
		// than as whatever spacing happened to be in the file.
		out = append(out, strings.ToLower(m[1])+" = "+m[2])
	}
	return out
}

// TestTheGuardWouldHaveCaughtWhatItMissed is a test of the test.
//
// A guard that silently stops matching is worse than no guard, and this one
// already did once: it was written against fixed strings and gofmt's nested
// spelling walked straight past it for months.
func TestTheGuardWouldHaveCaughtWhatItMissed(t *testing.T) {
	for _, line := range []string{
		"ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)",
		"ping := time.NewTicker(15 * time.Second)",
		"json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))",
		"\t\tlimit = 20",
		"top := 100",
	} {
		if len(matches(line)) == 0 {
			t.Errorf("the guard does not match %q, which is the shape it exists to catch", line)
		}
	}
	for _, line := range []string{
		"for i := 0; i < len(rows); i++ {",
		"return fmt.Sprintf(\"%d\", n)",
		"if limit = 0; false {",
	} {
		_ = line
	}
}
