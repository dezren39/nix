package settings_test

import (
	"strconv"
	"testing"

	"github.com/dezren39/mcpx/internal/defaults"
	"github.com/dezren39/mcpx/internal/settings"
)

func TestAFloorRefusesAWholeNumberBelowIt(t *testing.T) {
	// Validate is the one parser every layer shares, so a floor written here
	// is enforced by the environment, the file, the flag and the runtime API
	// alike.
	s := settings.Setting{Path: "x.n", Kind: settings.KindInt, Default: "1", Floor: 1}
	for _, raw := range []string{"0", "-1", "-40"} {
		if err := settings.Validate(s, raw); err == nil {
			t.Errorf("%q: want a refusal below the floor of 1", raw)
		}
	}
	for _, raw := range []string{"1", "9"} {
		if err := settings.Validate(s, raw); err != nil {
			t.Errorf("%q: want acceptance at or above the floor, got %v", raw, err)
		}
	}
}

func TestNoFloorLeavesAnIntegerAlone(t *testing.T) {
	// Only a setting that declares a floor has one. Every other integer keeps
	// the behaviour it had, zero and negatives included.
	s := settings.Setting{Path: "x.n", Kind: settings.KindInt, Default: "0"}
	for _, raw := range []string{"0", "-1"} {
		if err := settings.Validate(s, raw); err != nil {
			t.Errorf("%q: a setting without a floor should accept it: %v", raw, err)
		}
	}
}

func TestThePoolMaxDefaultIsTheBuiltInFour(t *testing.T) {
	// Four is the built-in default. The registry reads it from defaults.json
	// rather than restating it, so the two cannot disagree.
	if defaults.Max != 4 {
		t.Errorf("the built-in pool.max is %d, want 4", defaults.Max)
	}
	sch, err := settings.New(settings.Registry())
	if err != nil {
		t.Fatal(err)
	}
	d, ok := sch.Lookup("pool.max")
	if !ok {
		t.Fatal("pool.max is not a setting")
	}
	if d.Default != strconv.Itoa(defaults.Max) {
		t.Errorf("registry default %q disagrees with defaults.json %d", d.Default, defaults.Max)
	}
	if d.Floor != 1 {
		t.Errorf("pool.max floor = %d, want 1", d.Floor)
	}
}
