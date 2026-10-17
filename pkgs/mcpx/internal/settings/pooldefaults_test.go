package settings_test

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/settings"
)

// Every pool.* default in the registry must be the value defaults.json holds
// for it. The file is read here directly, not through the defaults package, so
// a literal that drifts from the file fails this test whichever way it was
// written. A default that disagrees with the file is a number `settings get`
// reports that the pools do not use.
func TestEveryPoolDefaultIsTheDefaultsJSONValue(t *testing.T) {
	raw, err := os.ReadFile("../defaults/defaults.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Pool map[string]json.RawMessage `json:"pool"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for key, v := range doc.Pool {
		var s string
		var n float64
		switch {
		case json.Unmarshal(v, &s) == nil:
			want["pool."+key] = s
		case json.Unmarshal(v, &n) == nil:
			want["pool."+key] = strconv.FormatFloat(n, 'f', -1, 64)
		default:
			t.Fatalf("defaults.json pool.%s is neither a string nor a number: %s", key, v)
		}
	}
	if len(want) != 7 {
		t.Fatalf("defaults.json has %d pool keys, this test knows seven; update it with the file", len(want))
	}

	sch, err := settings.New(settings.Registry())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, s := range sch.All() {
		if strings.HasPrefix(s.Path, "pool.") {
			got[s.Path] = s.Default
		}
	}
	for path, w := range want {
		g, ok := got[path]
		if !ok {
			t.Errorf("defaults.json has %s but the registry does not declare it", path)
			continue
		}
		if g != w {
			t.Errorf("%s: registry default %q, defaults.json %q", path, g, w)
		}
	}
	for path := range got {
		if _, ok := want[path]; !ok {
			t.Errorf("the registry declares %s but defaults.json has no such key; the default has nowhere to come from", path)
		}
	}
}
