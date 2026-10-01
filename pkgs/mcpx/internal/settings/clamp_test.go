package settings_test

import (
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/settings"
)

// The ClampedBy capability of docs/decisions/0002-autonomy-dial.md: a
// daemon-scoped ceiling no layer of a call-scoped setting can get above.

var ladder = []string{"off", "advise", "ask", "propose", "apply", "run"}

func clampSchema(t *testing.T, mutate func(ceil, dial *settings.Setting)) (*settings.Schema, error) {
	t.Helper()
	ceil := settings.Setting{Path: "t.max", Kind: settings.KindEnum, Enum: ladder,
		Default: "run", Scope: settings.ScopeDaemon}
	dial := settings.Setting{Path: "t.dial", Kind: settings.KindEnum, Enum: []string{"propose", "run"},
		Default: "propose", Scope: settings.ScopeCall, Hot: true, ClampedBy: "t.max"}
	if mutate != nil {
		mutate(&ceil, &dial)
	}
	return settings.New([]settings.Setting{ceil, dial})
}

func TestNewRefusesAClampThatCannotHold(t *testing.T) {
	if _, err := clampSchema(t, nil); err != nil {
		t.Fatalf("the well-formed pair should be accepted: %v", err)
	}
	for name, m := range map[string]func(c, d *settings.Setting){
		"missing ceiling":     func(c, d *settings.Setting) { d.ClampedBy = "t.nope" },
		"hot ceiling":         func(c, d *settings.Setting) { c.Hot = true },
		"call-scoped ceiling": func(c, d *settings.Setting) { c.Scope = settings.ScopeCall },
		"daemon-scoped dial":  func(c, d *settings.Setting) { d.Scope = settings.ScopeDaemon },
		"not an enum":         func(c, d *settings.Setting) { d.Kind = settings.KindString },
		"unordered values":    func(c, d *settings.Setting) { d.Enum = []string{"run", "propose"} },
		"foreign value":       func(c, d *settings.Setting) { d.Enum = []string{"propose", "plan"} },
		"default above":       func(c, d *settings.Setting) { c.Default = "ask" },
	} {
		if _, err := clampSchema(t, m); err == nil {
			t.Errorf("%s: New accepted it", name)
		}
	}
}

func TestACallerCannotRaiseAClampedSetting(t *testing.T) {
	sch, err := clampSchema(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	set := settings.NewSet(sch)
	if err := set.Apply("t.max", "propose", settings.Origin{Layer: settings.LayerFile, Detail: "/etc/mcpx.json"}); err != nil {
		t.Fatal(err)
	}
	// A file layer of the daemon's own above the ceiling.
	if err := set.Apply("t.dial", "run", settings.Origin{Layer: settings.LayerFile, Detail: "/home/me/.mcpx.json"}); err != nil {
		t.Fatal(err)
	}
	v, _ := set.Value("t.dial")
	if v.Raw != "propose" || v.Requested != "run" || !strings.HasPrefix(v.ClampedBy, "t.max (file:/etc/mcpx.json)") {
		t.Errorf("a file value above the ceiling should read as the ceiling, with the request and origin: %+v", v)
	}
	if got := set.String("t.dial"); got != "propose" {
		t.Errorf("String read %q past the ceiling", got)
	}

	// A caller's header: the call-scoped override, and its attempt at the
	// ceiling itself, which a daemon never takes from a caller -- here the
	// view is built the way the daemon builds it, call-scoped only.
	view := set.WithOverrides(map[string]string{"t.dial": "run"}, "X-Mcpx-Settings")
	if got := view.String("t.dial"); got != "propose" {
		t.Errorf("a call-scoped override above the ceiling read as %q", got)
	}

	// A request body.
	c, err := view.Clamp("t.dial", "run")
	if err != nil {
		t.Fatal(err)
	}
	if c.Value != "propose" || c.Requested != "run" || !c.Lowered() {
		t.Errorf("Clamp should lower run to propose: %+v", c)
	}
	// Lowers, never raises.
	if c, _ := view.Clamp("t.dial", "propose"); c.Value != "propose" || c.Lowered() {
		t.Errorf("a request at or below the ceiling is untouched: %+v", c)
	}
	if err := set.Apply("t.max", "off", settings.Origin{Layer: settings.LayerEnv, Detail: "MCPX_T_MAX"}); err != nil {
		t.Fatal(err)
	}
	if c, _ := set.Clamp("t.dial", "propose"); c.Value != "off" {
		t.Errorf("a ceiling below the dial's whole range should still bound it: %+v", c)
	}
	if _, err := set.Clamp("t.dial", "plan"); err == nil {
		t.Errorf("an unknown level should be an error, not a silent default")
	}
}
