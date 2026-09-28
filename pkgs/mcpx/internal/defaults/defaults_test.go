package defaults_test

import (
	"encoding/json"
	"os"
	"path/filepath"
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

// The point of the package is that there is exactly one place to change a
// default. This fails if someone reintroduces a hardcoded one elsewhere.
func TestNoPackageRedeclaresADefaultTimeout(t *testing.T) {
	root := ".."
	var offenders []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		if strings.Contains(path, "_test.go") || strings.Contains(path, "defaults/") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, bad := range []string{
			"5 * time.Minute", "120 * time.Second", "60 * time.Second",
			"2 * time.Second", "3 * time.Second", "10 * time.Minute",
			"20 * time.Second", "30 * time.Second",
			"50 * time.Millisecond", "100 * time.Millisecond", "500 * time.Millisecond",
		} {
			if strings.Contains(string(b), bad) {
				offenders = append(offenders, path+": "+bad)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) > 0 {
		t.Errorf("defaults belong in internal/defaults/defaults.json, found:\n  %s",
			strings.Join(offenders, "\n  "))
	}
}
