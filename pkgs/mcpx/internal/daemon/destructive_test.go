package daemon

import (
	"encoding/json"
	"testing"
)

// The specification's ToolAnnotations: destructiveHint defaults to true and
// is meaningful only when readOnlyHint is false. An absent hint was read as
// non-destructive, so elicit.confirmDestructive let unannotated tools through.
func TestDestructiveHintUsesTheSpecDefaults(t *testing.T) {
	for _, c := range []struct {
		raw  string
		want bool
	}{
		{``, true},
		{`{}`, true},
		{`{"title":"x"}`, true},
		{`{"readOnlyHint":false}`, true},
		{`{"destructiveHint":true}`, true},
		{`{"destructiveHint":false}`, false},
		{`{"readOnlyHint":true}`, false},
		{`{"readOnlyHint":true,"destructiveHint":true}`, false},
	} {
		if got := destructiveHint(json.RawMessage(c.raw)); got != c.want {
			t.Errorf("destructiveHint(%q) = %v, want %v", c.raw, got, c.want)
		}
	}
}
