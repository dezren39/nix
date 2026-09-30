package defaults_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/defaults"
)

func TestEmbeddedDefaultsParse(t *testing.T) {
	// The parse happens in a variable initialiser, so reaching this line at
	// all proves it succeeded. The assertions guard against a field being
	// renamed in the JSON and silently becoming a zero value.
	if defaults.Max <= 0 {
		t.Errorf("pool.max should be positive, got %d", defaults.Max)
	}
	if defaults.IdleTimeout != 5*time.Minute {
		t.Errorf("pool.idleTimeout = %v", defaults.IdleTimeout)
	}
	if defaults.CallTimeout <= defaults.StartTimeout {
		t.Errorf("a call should be allowed longer than a start: call=%v start=%v",
			defaults.CallTimeout, defaults.StartTimeout)
	}
	if defaults.LogFormat == "" || defaults.LogLevel == "" {
		t.Errorf("logging defaults should be set: %+v", defaults.Builtin().Logging)
	}
	if len(defaults.LogIncludes) == 0 {
		t.Error("logging.include should not be empty")
	}
	if defaults.CatalogBudget <= 0 {
		t.Errorf("catalog.budget should be positive, got %d", defaults.CatalogBudget)
	}
}

func TestUnknownFieldInDefaultsIsRejected(t *testing.T) {
	// DisallowUnknownFields is what makes a typo in defaults.json loud. A
	// misspelled key would otherwise parse cleanly and leave a zero value.
	var d defaults.Defaults
	dec := json.NewDecoder(strings.NewReader(`{"pool":{"maxx":9}}`))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err == nil {
		t.Fatal("a misspelled key should be rejected, not silently ignored")
	}
}

func TestBuiltinJSONMatchesTheParsedStruct(t *testing.T) {
	var round defaults.Defaults
	if err := json.Unmarshal(defaults.BuiltinJSON(), &round); err != nil {
		t.Fatalf("what --defaults prints should itself be valid: %v", err)
	}
	if round.Pool.Max != defaults.Max {
		t.Errorf("printed defaults disagree with the values in use: %d vs %d",
			round.Pool.Max, defaults.Max)
	}
}
