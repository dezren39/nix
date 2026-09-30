package settings_test

import (
	"flag"
	"strings"
	"testing"

	"github.com/dezren39/mcpx/internal/settings"
)

// A bootstrap setting is one no configuration can set, because it decides
// which configuration is read. Each test below is a place it would otherwise
// be accepted and then do nothing.

func TestABootstrapSettingIsRefusedInAConfigFile(t *testing.T) {
	s := schema(t)
	set := settings.NewSet(s)
	err := s.ApplyFile(set, map[string]any{
		"paths": map[string]any{"configFile": "/elsewhere.json"},
	}, "/p/.mcpx.json", 0)
	if err == nil {
		t.Fatal("a configuration file naming the configuration file should be refused")
	}
	for _, want := range []string{"/p/.mcpx.json", "paths.configFile", "MCPX_CONFIG", "--config"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal should mention %s: %v", want, err)
		}
	}
}

func TestABootstrapSettingIsReadFromTheEnvironment(t *testing.T) {
	s := schema(t)
	set := settings.NewSet(s)
	if err := s.ApplyEnv(set, []string{"MCPX_CONFIG=/x/.mcpx.json"}); err != nil {
		t.Fatal(err)
	}
	v, _ := set.Value("paths.configFile")
	if v.Raw != "/x/.mcpx.json" || v.Origin.Detail != "MCPX_CONFIG" {
		t.Errorf("MCPX_CONFIG should land in paths.configFile: %+v", v)
	}
}

func TestABootstrapSettingGetsNoGeneratedFlag(t *testing.T) {
	s := schema(t)
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	s.Bind(fs, "ls")
	// `mcpx ls --config x` parses after the configuration was read, so a
	// generated --config there would be accepted and inert.
	if fs.Lookup("config") != nil {
		t.Error("--config was generated for a subcommand, where it can change nothing")
	}
	if fs.Lookup("logging-trace") == nil {
		t.Error("an ordinary setting should still get its flag; the check above proves nothing without it")
	}
	for _, set := range s.ForCommand("ls") {
		if set.Bootstrap {
			t.Errorf("%s is offered as a flag of ls", set.Path)
		}
	}
}

func TestOnlyABootstrapSettingIsUnwritable(t *testing.T) {
	for _, set := range schema(t).All() {
		err := set.Writable()
		if (err != nil) != set.Bootstrap {
			t.Errorf("%s: bootstrap=%v but Writable()=%v", set.Path, set.Bootstrap, err)
		}
	}
}
