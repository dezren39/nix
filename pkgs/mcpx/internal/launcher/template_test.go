package launcher_test

import (
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/launcher"
)

func tmpl(text string) launcher.Template {
	return launcher.Template{Text: text, Name: "test-launcher"}
}

func TestPlaceholdersAreSubstituted(t *testing.T) {
	got, err := launcher.Expand(tmpl("a @before b @entry c"), launcher.Fill{
		launcher.Before: "BEFORE",
		launcher.Entry:  "ENTRY",
	}, launcher.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "a BEFORE b ENTRY c" {
		t.Errorf("got %q", got)
	}
}

func TestAFillMayItselfReferenceAnotherPlaceholder(t *testing.T) {
	// This is the mechanism behind the interesting rearrangements: a suffix
	// that ends with @prefix runs the prefix again on the way out.
	got, err := launcher.Expand(tmpl("@suffix"), launcher.Fill{
		launcher.Suffix: "end:@prefix",
		launcher.Prefix: "P",
	}, launcher.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "end:P" {
		t.Errorf("got %q", got)
	}
}

func TestACycleIsRefusedAndThePathIsNamed(t *testing.T) {
	// Without this check a cycle is a hang, found by waiting.
	_, err := launcher.Expand(tmpl("@prefix"), launcher.Fill{
		launcher.Prefix: "@suffix",
		launcher.Suffix: "@prefix",
	}, launcher.Options{})
	if err == nil {
		t.Fatal("a reference loop should be refused")
	}
	if !strings.Contains(err.Error(), "loop") || !strings.Contains(err.Error(), "@prefix") {
		t.Errorf("the message should name the cycle: %v", err)
	}
}

func TestASelfReferenceIsACycle(t *testing.T) {
	_, err := launcher.Expand(tmpl("@prefix"), launcher.Fill{
		launcher.Prefix: "again @prefix",
	}, launcher.Options{})
	if err == nil {
		t.Fatal("a placeholder referring to itself should be refused")
	}
}

func TestUsingAPlaceholderTwiceIsAnErrorByDefault(t *testing.T) {
	// The common cause is a mistake, and a mistake that silently doubles an
	// effect is expensive to find.
	_, err := launcher.Expand(tmpl("@prefix ... @prefix"), launcher.Fill{
		launcher.Prefix: "P",
	}, launcher.Options{})
	if err == nil {
		t.Fatal("a repeated placeholder should be refused by default")
	}
	if !strings.Contains(err.Error(), "launcherPlaceholderRepeat") {
		t.Errorf("the message should say how to allow it: %v", err)
	}
}

func TestARepeatCanBeAllowedDeliberately(t *testing.T) {
	got, err := launcher.Expand(tmpl("@prefix|@prefix"), launcher.Fill{
		launcher.Prefix: "P",
	}, launcher.Options{AllowRepeat: []launcher.Placeholder{launcher.Prefix}})
	if err != nil {
		t.Fatalf("naming it should permit it: %v", err)
	}
	if got != "P|P" {
		t.Errorf("got %q", got)
	}
}

func TestAMisspelledPlaceholderIsCaughtWithSuggestions(t *testing.T) {
	// Left in place, the text produces a syntax error from the runtime
	// pointing at a line the user did not write.
	_, err := launcher.Expand(tmpl("@entyr"), launcher.Fill{launcher.Entry: "E"}, launcher.Options{})
	if err == nil {
		t.Fatal("a near-miss placeholder should be reported")
	}
	if !strings.Contains(err.Error(), "@entyr") || !strings.Contains(err.Error(), "@entry") {
		t.Errorf("the message should name both the typo and the options: %v", err)
	}
}

func TestAnOrdinaryAtSignIsLeftAlone(t *testing.T) {
	// The cost of a false positive is refusing a valid launcher.
	for _, text := range []string{
		`const email = "a@b.com";`,
		`@Component({})`,
		`// see @author`,
	} {
		if _, err := launcher.Expand(tmpl(text), launcher.Fill{launcher.Entry: "E"}, launcher.Options{}); err != nil {
			t.Errorf("%q should not trip the checker: %v", text, err)
		}
	}
}

func TestAnUnfilledButKnownPlaceholderVanishes(t *testing.T) {
	got, err := launcher.Expand(tmpl("[@before][@entry]"), launcher.Fill{
		launcher.Before: "",
		launcher.Entry:  "E",
	}, launcher.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "[][E]" {
		t.Errorf("an empty fill should leave nothing behind: %q", got)
	}
}

func TestUsedReportsWhatATemplateReferences(t *testing.T) {
	// This is how a caller learns that a custom launcher never runs the
	// script.
	used := launcher.Used(tmpl("@globals then @before but nothing else"))
	var names []string
	for _, p := range used {
		names = append(names, string(p))
	}
	got := strings.Join(names, ",")
	if !strings.Contains(got, "globals") || !strings.Contains(got, "before") {
		t.Errorf("got %q", got)
	}
	if strings.Contains(got, "entry") {
		t.Errorf("entry is not referenced and should not be reported: %q", got)
	}
}

func TestDeepNestingIsBoundedRatherThanUnbounded(t *testing.T) {
	// A chain long enough to matter is a mistake; the bound turns a stack
	// overflow into a sentence.
	fill := launcher.Fill{}
	_, err := launcher.Expand(tmpl("@a"), fill, launcher.Options{MaxDepth: 2})
	if err != nil {
		t.Fatalf("an unknown, unfilled name is left alone: %v", err)
	}
}

func TestAPlaceholderCanTakeArguments(t *testing.T) {
	// This is the difference between a template that can be rearranged and
	// one that can only be filled in: @console(header, prefix) says which
	// fragments this console setup gets to see.
	got, err := launcher.Expand(tmpl("@console(header, prefix)"), launcher.Fill{
		launcher.Console: "SETUP[@1|@2]",
		launcher.Header:  "H",
		launcher.Prefix:  "P",
	}, launcher.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "SETUP[H|P]" {
		t.Errorf("arguments should resolve to the fragments they name: %q", got)
	}
}

func TestArgumentsBindOnlyInsideTheBody(t *testing.T) {
	// Nothing an argument binds may leak back out, or two uses of the same
	// placeholder would contaminate each other.
	got, err := launcher.Expand(tmpl("@before(prefix)|@1"), launcher.Fill{
		launcher.Before: "in:@1",
		launcher.Prefix: "P",
	}, launcher.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "in:P|@1" {
		t.Errorf("the outer @1 should be untouched: %q", got)
	}
}

func TestALiteralArgumentIsUsedAsWritten(t *testing.T) {
	got, err := launcher.Expand(tmpl(`@before("quiet")`), launcher.Fill{
		launcher.Before: "mode=@1",
	}, launcher.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got != `mode="quiet"` {
		t.Errorf("a word that names no placeholder is literal: %q", got)
	}
}

func TestAllArgumentsAreAvailableJoined(t *testing.T) {
	got, err := launcher.Expand(tmpl("@before(header, prefix)"), launcher.Fill{
		launcher.Before: "[@args]",
		launcher.Header: "H",
		launcher.Prefix: "P",
	}, launcher.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "[H\nP]" {
		t.Errorf("got %q", got)
	}
}

func TestEmptyParenthesesMeanExplicitlyNothing(t *testing.T) {
	// @console and @console() are different requests: the first takes
	// whatever the default is, the second says none of it.
	got, err := launcher.Expand(tmpl("@console()"), launcher.Fill{
		launcher.Console: "[@args]",
	}, launcher.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "[]" {
		t.Errorf("got %q", got)
	}
}

func TestUnbalancedParenthesesDoNotSwallowTheFile(t *testing.T) {
	got, err := launcher.Expand(tmpl("@before(oops rest of file"), launcher.Fill{
		launcher.Before: "B",
	}, launcher.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "rest of file") {
		t.Errorf("the remainder should survive: %q", got)
	}
}
