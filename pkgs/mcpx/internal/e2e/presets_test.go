package e2e_test

import (
	"encoding/json"
	"strings"
	"testing"
)

const presetConfig = `{
  "mcpServers": { "demo": { "command": "FAKE" } },
  "presets": {
    "quiet": ["--log-level=error", "--include=host"],
    "loud":  ["--log-level=debug"]
  }
}`

type presetSetting struct {
	Value    string   `json:"value"`
	Source   string   `json:"source"`
	Shadowed []string `json:"shadowed"`
}

func (e *env) settingVia(t *testing.T, args ...string) presetSetting {
	t.Helper()
	out := e.run(append([]string{"--json", "settings", "get"}, args...)...)
	var v presetSetting
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return v
}

// Presets apply in the order named, so the later one wins, and an explicit
// flag beats every preset. Provenance names the preset.
func TestPresetsApplyInOrderAndExplicitFlagsWin(t *testing.T) {
	e := newEnv(t, presetConfig)
	v := e.settingVia(t, "logging.level", "--preset=quiet,loud")
	if v.Value != "debug" || v.Source != "preset:loud (--log-level)" ||
		len(v.Shadowed) == 0 || v.Shadowed[0] != "preset:quiet (--log-level)" {
		t.Errorf("quiet,loud: %+v", v)
	}
	v = e.settingVia(t, "logging.level", "--preset=loud,quiet")
	if v.Value != "error" {
		t.Errorf("loud,quiet: %+v", v)
	}
	v = e.settingVia(t, "logging.level", "--preset=quiet,loud", "--log-level=warn")
	if v.Value != "warn" || v.Source != "flag:--log-level" {
		t.Errorf("explicit flag should win: %+v", v)
	}
	// MCPX_PRESET selects too, and an explicit flag still wins over it.
	v = e.withEnv("MCPX_PRESET=quiet").settingVia(t, "logging.level")
	if v.Value != "error" || !strings.HasPrefix(v.Source, "preset:quiet") {
		t.Errorf("MCPX_PRESET: %+v", v)
	}
	out := e.withEnv("MCPX_PRESET=quiet").run("--json", "config", "--sources")
	if !strings.Contains(out, `"from": "preset:quiet (--log-level)"`) {
		t.Errorf("config --sources should show the preset:\n%s", out)
	}
	if out, err := e.try("settings", "--preset=nope"); err == nil || !strings.Contains(out, `no preset "nope"`) {
		t.Errorf("unknown preset: %v\n%s", err, out)
	}
}
