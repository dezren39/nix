package config

import (
	"reflect"
	"testing"
)

// The set of keys a struct decodes has to be the set encoding/json actually
// reads, or checkKeys reports a key that works as unknown -- which doctor then
// warns about and plumbing.strictUnknownKeys refuses.
//
// Both cases below are ones encoding/json handles and a tags-only reading of
// the struct does not.
func TestJSONKeysMatchesWhatEncodingJSONReads(t *testing.T) {
	type inner struct {
		Promoted string `json:"promoted"`
	}
	type outer struct {
		inner
		Named    string `json:"named"`
		Untagged string
		Skipped  string `json:"-"`
		unexport string //nolint:unused // present to prove it is skipped
	}

	keys := jsonKeys(reflect.TypeOf(outer{}))

	// An anonymous field's keys are promoted into the outer object by json, so
	// the embedded type's own name is never a key and its fields must be.
	if !knows(keys, "promoted") {
		t.Errorf("an embedded struct's field is promoted by json but jsonKeys missed it: %v", keys)
	}
	if knows(keys, "inner") {
		t.Errorf("the embedded type's name is not a key json reads: %v", keys)
	}

	// json prefers an exact match and otherwise accepts a case-insensitive
	// one, so every spelling below decodes.
	for _, spelling := range []string{"named", "Named", "NAMED", "nAmEd", "untagged", "Untagged"} {
		if !knows(keys, spelling) {
			t.Errorf("json decodes %q but jsonKeys calls it unknown", spelling)
		}
	}

	if knows(keys, "skipped") {
		t.Error(`a json:"-" field is not read`)
	}
	if knows(keys, "unexport") {
		t.Error("an unexported field is not read")
	}
}

// The claim above, made against the real structs rather than a fixture: for
// every key Server and Extras declare, every case variant of it is recognised.
func TestEveryDeclaredKeyIsRecognisedInAnyCase(t *testing.T) {
	for _, tc := range []struct {
		name string
		typ  reflect.Type
	}{
		{"Server", reflect.TypeOf(Server{})},
		{"Extras", reflect.TypeOf(Extras{})},
	} {
		keys := jsonKeys(tc.typ)
		if len(keys) == 0 {
			t.Fatalf("%s: no keys found; the struct changed shape and this test checks nothing", tc.name)
		}
		for lower, canonical := range keys {
			for _, spelling := range []string{lower, canonical, upperFirst(canonical)} {
				if !knows(keys, spelling) {
					t.Errorf("%s: %q is declared but %q is not recognised", tc.name, canonical, spelling)
				}
			}
		}
	}
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	b := []byte(s)
	if b[0] >= 'a' && b[0] <= 'z' {
		b[0] -= 'a' - 'A'
	}
	return string(b)
}
