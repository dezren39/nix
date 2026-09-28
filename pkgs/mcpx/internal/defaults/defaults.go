package defaults

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"time"
)

// defaultsJSON is the base configuration layer.
//
// Every default the program has lives here rather than in a const beside the
// code that happens to need it. Two reasons. A reader asking "what is the idle
// timeout" should find one answer in one file, not grep four packages. And the
// values are then data, so they can be printed, diffed against an effective
// config, and merged by the same code that merges every other layer -- an
// embedded default that cannot be shown is indistinguishable from a magic
// number.
//
//go:embed defaults.json
var defaultsJSON []byte

// Defaults is the parsed content of defaults.json.
type Defaults struct {
	Pool struct {
		Max          int    `json:"max"`
		Min          int    `json:"min"`
		IdleTimeout  string `json:"idleTimeout"`
		CallTimeout  string `json:"callTimeout"`
		StartTimeout string `json:"startTimeout"`
		Sharing      string `json:"sharing"`
		Scope        string `json:"scope"`
	} `json:"pool"`
	Logging struct {
		Format   string   `json:"format"`
		Level    string   `json:"level"`
		Source   string   `json:"source"`
		MaxBytes int64    `json:"maxBytes"`
		MaxLines int64    `json:"maxLines"`
		MaxAge   string   `json:"maxAge"`
		Keep     int      `json:"keep"`
		Include  []string `json:"include"`
	} `json:"logging"`
	Daemon struct {
		ReapInterval string `json:"reapInterval"`
		SaveInterval string `json:"saveInterval"`
	} `json:"daemon"`
	Catalog struct {
		Budget int `json:"budget"`
	} `json:"catalog"`
	Script struct {
		Permissions      string `json:"permissions"`
		CaptureConsole   bool   `json:"captureConsole"`
		TypecheckTimeout string `json:"typecheckTimeout"`
	} `json:"script"`
}

// Parsed in a variable initialiser rather than in init(). Go evaluates
// package variables before it runs init(), so the exported values below would
// read a zeroed struct if the parse lived there -- which presented as an empty
// duration string rather than as an obvious ordering bug.
var builtin = mustParse(defaultsJSON)

func mustParse(b []byte) Defaults {
	var d Defaults
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		panic(fmt.Sprintf("mcpx: embedded defaults.json is invalid: %v", err))
	}
	return d
}

// Builtin returns the base layer. Callers must not mutate it.
func Builtin() Defaults { return builtin }

// BuiltinJSON returns defaults.json verbatim, for `mcpx config --defaults`.
func BuiltinJSON() []byte { return defaultsJSON }

func mustDur(s string, field string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		panic(fmt.Sprintf("mcpx: defaults.json %s is not a duration: %v", field, err))
	}
	return d
}

// The former consts, now derived from the embedded layer. They stay exported
// because the pool, the logger and the CLI each legitimately need a value
// before any configuration file has been read.
var (
	Max          = builtin.Pool.Max
	Min          = builtin.Pool.Min
	IdleTimeout  = mustDur(builtin.Pool.IdleTimeout, "pool.idleTimeout")
	CallTimeout  = mustDur(builtin.Pool.CallTimeout, "pool.callTimeout")
	StartTimeout = mustDur(builtin.Pool.StartTimeout, "pool.startTimeout")
	Sharing      = builtin.Pool.Sharing
	Scope        = builtin.Pool.Scope

	LogFormat   = builtin.Logging.Format
	LogLevel    = builtin.Logging.Level
	LogSource   = builtin.Logging.Source
	LogMaxBytes = builtin.Logging.MaxBytes
	LogMaxLines = builtin.Logging.MaxLines
	LogMaxAge   = mustDur(builtin.Logging.MaxAge, "logging.maxAge")
	LogKeep     = builtin.Logging.Keep
	LogIncludes = builtin.Logging.Include

	ReapInterval = mustDur(builtin.Daemon.ReapInterval, "daemon.reapInterval")
	SaveInterval = mustDur(builtin.Daemon.SaveInterval, "daemon.saveInterval")

	CatalogBudget = builtin.Catalog.Budget

	Permissions      = builtin.Script.Permissions
	CaptureConsole   = builtin.Script.CaptureConsole
	TypecheckTimeout = mustDur(builtin.Script.TypecheckTimeout, "script.typecheckTimeout")
)
