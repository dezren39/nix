package spec

import (
	"strings"
	"testing"
)

func TestDefaultIsNewestFirstAndStrict(t *testing.T) {
	p := Default()
	if len(p.Order()) != len(Revisions) || p.First() != Revisions[0] {
		t.Fatalf("default order %v", p.Order())
	}
	for i := 1; i < len(Revisions); i++ {
		if Revisions[i-1] <= Revisions[i] {
			t.Errorf("Revisions not newest first: %v", Revisions)
		}
	}
	for _, r := range Revisions {
		if !p.Strict(r) {
			t.Errorf("%s not strict by default", r)
		}
	}
}

func TestNew(t *testing.T) {
	p := Must(nil, "2024-11-05", nil)
	want := "2024-11-05,2026-07-28,2025-11-25,2025-06-18,2025-03-26"
	if got := strings.Join(p.Order(), ","); got != want {
		t.Errorf("--mcp-spec 2024-11-05: %s, want %s", got, want)
	}
	p = Must([]string{"2025-11-25", "2024-11-05"}, "", []string{"2026-07-28"})
	want = "2025-11-25,2024-11-05,2026-07-28,2025-06-18,2025-03-26"
	if got := strings.Join(p.Order(), ","); got != want {
		t.Errorf("partial precedence: %s, want %s", got, want)
	}
	if p.Strict("2026-07-28") || !p.Strict("2025-11-25") {
		t.Error("lenient not applied")
	}
	p = Must([]string{"2025-11-25", "2024-11-05"}, "2024-11-05", nil)
	if got := strings.Join(p.Order(), ","); !strings.HasPrefix(got, "2024-11-05,2025-11-25,2026-07-28") {
		t.Errorf("first over precedence: %s", got)
	}
	for _, bad := range [][3][]string{
		{{"2099-01-01"}, nil, nil}, {nil, {"2099-01-01"}, nil}, {nil, nil, {"nope"}},
		{{"2025-11-25", "2025-11-25"}, nil, nil},
	} {
		first := ""
		if bad[1] != nil {
			first = bad[1][0]
		}
		_, err := New(bad[0], first, bad[2])
		if err == nil {
			t.Errorf("accepted %v", bad)
			continue
		}
		if !strings.Contains(err.Error(), "twice") && !strings.Contains(err.Error(), strings.Join(Revisions, ", ")) {
			t.Errorf("error does not list valid revisions: %v", err)
		}
	}
}

func TestPredicates(t *testing.T) {
	cases := []struct {
		p    Pred
		yes  []string
		no   []string
		name string
	}{
		{AtOrAfter("2025-06-18"), []string{"2025-06-18", "2026-07-28"}, []string{"2025-03-26", ""}, "AtOrAfter"},
		{Before("2025-06-18"), []string{"2025-03-26", "2024-11-05"}, []string{"2025-06-18", ""}, "Before"},
		{Only("2024-11-05", "2026-07-28"), []string{"2024-11-05", "2026-07-28"}, []string{"2025-11-25"}, "Only"},
		{Between("2025-11-25", "2025-03-26"), []string{"2025-03-26", "2025-06-18", "2025-11-25"}, []string{"2024-11-05", "2026-07-28"}, "Between reversed"},
		{Between("2025-03-26", "2025-11-25"), []string{"2025-06-18"}, []string{"2026-07-28"}, "Between"},
	}
	for _, c := range cases {
		for _, r := range c.yes {
			if !c.p(r) {
				t.Errorf("%s(%q) false", c.name, r)
			}
		}
		for _, r := range c.no {
			if c.p(r) {
				t.Errorf("%s(%q) true", c.name, r)
			}
		}
	}
}

func TestSetRestores(t *testing.T) {
	restore := Set(Must(nil, "2024-11-05", nil))
	if Current().First() != "2024-11-05" {
		t.Fatal("Set did not install")
	}
	restore()
	if Current().First() != Revisions[0] {
		t.Fatal("restore did not")
	}
}

// Governs is the default way a conflict is settled: among the revisions it is
// between, the one ranked higher wins, and only that one's strictness counts.
func TestGovernsIsTheWinnersStrictness(t *testing.T) {
	all := Revisions
	newest, oldest := "2026-07-28", "2024-11-05"

	def, err := New(nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !def.Governs(newest) {
		t.Error("by default the newest revision governs")
	}
	if def.Governs(oldest) {
		t.Error("the oldest does not govern by default, whatever its strictness")
	}

	// Moving one to the front makes it the winner.
	first, err := New(nil, oldest, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Governs(oldest) || first.Governs(newest) {
		t.Errorf("order = %v: the revision put first should govern", first.Order())
	}

	// A loser's strictness is not consulted: the newest still governs with
	// every other revision lenient.
	lenient, err := New(nil, "", all[1:])
	if err != nil {
		t.Fatal(err)
	}
	if !lenient.Governs(newest) {
		t.Error("the winner governs however lenient the losers are")
	}

	// The winner's own strictness is consulted.
	off, err := New(nil, "", []string{newest})
	if err != nil {
		t.Fatal(err)
	}
	if off.Governs(newest) {
		t.Error("a lenient winner does not govern")
	}

	// Among a named subset, the rest of the order is ignored.
	if !def.Governs(oldest, "2024-11-05") {
		t.Error("a conflict with only itself in it is governed by it")
	}
	if def.Governs(oldest, newest) {
		t.Error("the newest outranks the oldest among those two")
	}
}
