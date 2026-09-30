package cli

import (
	"sort"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/api"
)

// TestEveryDispatchedCommandIsDeclared holds the dispatch table and the
// command table together, in both directions.
//
// They were two literals in two packages and had drifted by seven: serve,
// openapi, adapter, registry, api, man and completion all ran and none was
// declared, so none appeared in help, the man page or completion. A command
// that runs and is not declared is invisible; one that is declared and does
// not run is a lie in the manual.
func TestEveryDispatchedCommandIsDeclared(t *testing.T) {
	declared := map[string]string{}
	for _, c := range Commands() {
		for _, name := range append([]string{c.Name}, c.Aliases...) {
			if prev, dup := declared[name]; dup {
				t.Errorf("%q is declared twice (by %s and %s)", name, prev, c.Name)
			}
			declared[name] = c.Name
		}
	}
	handlers := (&App{}).Handlers()
	var undeclared, unrunnable []string
	for name := range handlers {
		if _, ok := declared[name]; !ok {
			undeclared = append(undeclared, name)
		}
	}
	for name := range declared {
		if _, ok := handlers[name]; !ok {
			unrunnable = append(unrunnable, name)
		}
	}
	sort.Strings(undeclared)
	sort.Strings(unrunnable)
	if len(undeclared) > 0 {
		t.Errorf("dispatched but not in Commands() -- absent from help, man and completion:\n  %s",
			strings.Join(undeclared, "\n  "))
	}
	if len(unrunnable) > 0 {
		t.Errorf("in Commands() but nothing runs them:\n  %s", strings.Join(unrunnable, "\n  "))
	}
}

// TestEveryOperationIsReachableFromTheCLI is the CLI's half of the parity
// rule: every /v1 operation is either named by a hand-written command, and
// that command exists, or reached by a generated one.
func TestEveryOperationIsReachableFromTheCLI(t *testing.T) {
	hand := (&App{}).handWritten()
	generated := map[string]api.Op{}
	for _, g := range opGroups() {
		for _, op := range g.ops() {
			generated[op.Name] = op
		}
	}
	for _, op := range api.Ops() {
		words := op.CLIWords()
		if op.GeneratedCommand() {
			if _, ok := generated[op.Name]; !ok {
				t.Errorf("%s declares no hand-written command and no generated one reaches it", op.Name)
			}
			continue
		}
		if op.CLI != "" {
			t.Errorf("%s sets both Command and CLI; CLI only renames a generated command", op.Name)
		}
		if _, ok := hand[words[0]]; !ok {
			t.Errorf("%s names %q as its command, and there is no such command", op.Name, op.Command)
		}
	}
}

// TestGeneratedCommandsDoNotShadow: a generated name that collides with a
// hand-written command would never run, because the hand-written one wins,
// and the operation would silently lose its only CLI path.
func TestGeneratedCommandsDoNotShadow(t *testing.T) {
	hand := (&App{}).handWritten()
	for _, g := range opGroups() {
		// Only the group name. An alias that collides is already dropped by
		// opGroups before it gets here, so asserting over g.Aliases as well
		// would be an arm that cannot fire -- it looked like a second check
		// and was not one. What the dropping risks instead is an operation
		// losing its only path, which is the test below.
		if _, taken := hand[g.Name]; taken {
			t.Errorf("generated command %q collides with a hand-written one; "+
				"give the operation a Command (if that command reaches it) or a CLI name", g.Name)
		}
		// A noun that is both an operation taking positional arguments and
		// a family of verbs cannot tell `mcpx x get` from `mcpx x <value
		// that happens to be "get">`.
		if g.Bare != nil && len(g.Verbs) > 0 && len(positional(*g.Bare)) > 0 {
			t.Errorf("%s takes positional arguments and also has verbs (%s); "+
				"a value spelled like a verb would be misread", g.Name, strings.Join(g.verbWords(), ", "))
		}
	}
}

func TestGeneratedCLIPathsAreUnique(t *testing.T) {
	seen := map[string]string{}
	for _, op := range api.Ops() {
		if !op.GeneratedCommand() {
			continue
		}
		path := strings.Join(op.CLIWords(), " ")
		if prev, dup := seen[path]; dup {
			t.Errorf("`mcpx %s` would be both %s and %s", path, prev, op.Name)
		}
		seen[path] = op.Name
	}
}

func TestEveryCommandIsInAListedGroup(t *testing.T) {
	// A group missing from commandGroups drops its commands from help and
	// the man page without any error.
	for _, c := range Commands() {
		if !contains(commandGroups, c.Group) {
			t.Errorf("%s is in group %q, which help and the man page do not list", c.Name, c.Group)
		}
		if strings.TrimSpace(c.Summary) == "" {
			t.Errorf("%s has no summary", c.Name)
		}
	}
	// The generated listing only. Usage() ends with usageFooter, whose
	// EXAMPLES block spells out `mcpx ls`, `mcpx types ...`, `mcpx exec ...`
	// and `mcpx call ...` in prose, and the `mcpx help <command>` line is
	// written unconditionally -- so searching the whole of Usage() reports
	// those five as listed however badly the listing breaks. Cutting at the
	// footer is what makes the check cover them.
	listing, _, _ := strings.Cut(Usage(), usageFooter)
	for _, c := range Commands() {
		if !strings.Contains(listing, "mcpx "+c.Name+" ") && !strings.Contains(listing, "mcpx "+c.Name+"\n") {
			t.Errorf("`mcpx help` does not list %s", c.Name)
		}
	}
}

func TestGeneratedFlagsFollowTheParameters(t *testing.T) {
	cases := map[string]string{
		"waitMs":      "wait-ms",
		"skipDefault": "skip-default",
		"id":          "id",
		"q":           "q",
	}
	for in, want := range cases {
		if got := paramFlag(in); got != want {
			t.Errorf("paramFlag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPositionalArgumentsFillThePathFirst(t *testing.T) {
	op, ok := api.ByName("artifact_put")
	if !ok {
		t.Fatal("artifact_put is not declared")
	}
	var names []string
	for _, p := range positional(op) {
		names = append(names, p.Name)
	}
	if got := strings.Join(names, ","); got != "name,content" {
		t.Errorf("artifact put positionals = %s, want name,content", got)
	}
	op, _ = api.ByName("task_result")
	names = nil
	for _, p := range positional(op) {
		names = append(names, p.Name)
	}
	if got := strings.Join(names, ","); got != "id" {
		t.Errorf("task result positionals = %s, want id", got)
	}
}

func TestHoistKeepsAValueWithItsFlag(t *testing.T) {
	got := hoistOpFlags([]string{"tsk-1", "--wait-ms", "5", "--json", "-"}, map[string]bool{"wait-ms": true})
	want := []string{"--wait-ms", "5", "--json", "tsk-1", "-"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("hoist = %q, want %q", got, want)
	}
}

func TestConvertRefusesTheWrongShape(t *testing.T) {
	if _, err := convertParam(api.Param{Name: "limit", Type: "integer"}, "ten"); err == nil {
		t.Error("an integer parameter accepted \"ten\"")
	}
	if v, err := convertParam(api.Param{Name: "limit", Type: "integer"}, "10"); err != nil || v != 10 {
		t.Errorf("an integer parameter gave %v, %v", v, err)
	}
	if _, err := convertParam(api.Param{Name: "by", Enum: []string{"calls", "errors"}}, "nope"); err == nil {
		t.Error("an enum parameter accepted a value outside it")
	}
	if _, err := convertParam(api.Param{Name: "args", Type: "object"}, "{not json"); err == nil {
		t.Error("an object parameter accepted invalid JSON")
	}
	v, err := convertParam(api.Param{Name: "args", Type: "object"}, `{"a":1}`)
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := v.(map[string]any); !ok || m["a"] != float64(1) {
		t.Errorf("object parameter gave %#v", v)
	}
}

// An operation whose natural alias collides with a hand-written command keeps
// a path of its own.
//
// opGroups drops such an alias silently, which is right -- the hand-written
// command must win -- but the operation still has to be reachable, or the
// drop has quietly cost it its only CLI surface. catalog_history is the live
// case: its first segment is "catalog", which is hand-written, so the alias
// goes and `mcpx history` is what remains.
func TestAnOperationWhoseAliasCollidesIsStillReachable(t *testing.T) {
	hand := (&App{}).handWritten()
	reach := map[string]bool{}
	for _, g := range opGroups() {
		if g.Bare != nil {
			reach[g.Bare.Name] = true
		}
		for _, v := range g.Verbs {
			reach[v.Op.Name] = true
		}
	}

	collided := 0
	for _, op := range api.Ops() {
		if op.Command != "" || op.CoveredBy != "" || op.Streams {
			continue
		}
		if _, taken := hand[strings.SplitN(op.Name, "_", 2)[0]]; !taken {
			continue
		}
		collided++
		if !reach[op.Name] {
			t.Errorf("%s lost its alias to a hand-written command and has no command of its own", op.Name)
		}
	}
	if collided == 0 {
		t.Skip("no operation's alias currently collides; nothing to check")
	}
}
