package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/testsupport"
)

// #367: `mcpx run greet hi` used to die with "SyntaxError: Expression
// expected" because run executed a recipe's @param holes as written.
// run must refuse early and name the command that fills them.

const holesRecipe = `// Greet someone through the demo server.
// @param message:string   what to say
// @param loud:boolean = false   shout it
const text = @loud ? String(@message).toUpperCase() : @message;
console.log(text);
`

func TestRunRefusesRecipeHolesAndPointsToRecipesRun(t *testing.T) {
	root := testsupport.TempDir(t)
	writeScript(t, filepath.Join(root, ScriptsDirName), "greet.ts", holesRecipe)
	chdir(t, root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))

	err := refuseRecipeHoles("greet")
	if err == nil {
		t.Fatal("a recipe with unfilled holes must not pass to the runtime")
	}
	msg := err.Error()
	for _, want := range []string{
		"greet is a recipe", "message", "loud",
		"mcpx recipes run greet message=<value> [loud=<value>]",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error should mention %q:\n%s", want, msg)
		}
	}
}

func TestRunLeavesPlainScriptsAlone(t *testing.T) {
	root := testsupport.TempDir(t)
	writeScript(t, filepath.Join(root, ScriptsDirName), "plain.ts",
		"console.log(\"hello\");\n")
	// A body @name with no @param declaration is not a recipe: run's
	// business is the script's, not the recipe check's.
	writeScript(t, filepath.Join(root, ScriptsDirName), "undeclared.ts",
		"const x = @thing;\nconsole.log(x);\n")
	chdir(t, root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))

	for _, name := range []string{"plain", "undeclared"} {
		if err := refuseRecipeHoles(name); err != nil {
			t.Errorf("%s: plain scripts must run as before, got:\n%v", name, err)
		}
	}
}

func TestRunRecipeOutsideScriptPathPointsToSave(t *testing.T) {
	root := testsupport.TempDir(t)
	p := writeScript(t, root, "greet.ts", holesRecipe)
	chdir(t, root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))

	err := refuseRecipeHoles(p)
	if err == nil {
		t.Fatal("a recipe addressed by path must still be refused")
	}
	if !strings.Contains(err.Error(), "mcpx recipes save greet ") {
		t.Errorf("a recipe the daemon cannot find by name should be pointed at save:\n%v", err)
	}
}

func TestRunMissingScriptIsLeftToTheRun(t *testing.T) {
	root := testsupport.TempDir(t)
	chdir(t, root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))

	if err := refuseRecipeHoles("nope"); err != nil {
		t.Errorf("an unresolvable name is resolveScript's error to report, got:\n%v", err)
	}
}
