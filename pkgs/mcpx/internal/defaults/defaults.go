package defaults

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
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
	// Plumbing are internals. They are here rather than inline so that a
	// number nobody expected to matter can still be changed without a
	// rebuild, and so that every constant in the program has one home.
	Plumbing struct {
		ShutdownGrace        string `json:"shutdownGrace"`
		StdioDrainGrace      string `json:"stdioDrainGrace"`
		StdioMaxLine         string `json:"stdioMaxLine"`
		DaemonConnectTimeout string `json:"daemonConnectTimeout"`
		DaemonPollInterval   string `json:"daemonPollInterval"`
		DaemonRestartSettle  string `json:"daemonRestartSettle"`
		HTTPIdleTimeout      string `json:"httpIdleTimeout"`
		HTTPRequestTimeout   string `json:"httpRequestTimeout"`
		FollowPollInterval   string `json:"followPollInterval"`
		LogQueryLimit        int    `json:"logQueryLimit"`
		RestartBackoffStep   string `json:"restartBackoffStep"`
		RestartBackoffMax    string `json:"restartBackoffMax"`
		RegistryTimeout      string `json:"registryTimeout"`
		EventHistory         int    `json:"eventHistory"`
		StreamReconnect      string `json:"streamReconnect"`
		TaskTTL              string `json:"taskTTL"`
		TaskResultWait       string `json:"taskResultWait"`
		StatsTop             int    `json:"statsTop"`
		RegistryLimit        int    `json:"registryLimit"`
	} `json:"plumbing"`
	// Elicit governs questions a server asks back.
	Elicit struct {
		TTL            string `json:"ttl"`
		PollInterval   string `json:"pollInterval"`
		HandlerTimeout string `json:"handlerTimeout"`
		InputRounds    int    `json:"inputRounds"`
	} `json:"elicit"`
	// Proto governs how mcpx speaks the protocol to its own clients: which
	// era mechanisms it offers, and how long it will hold a call open while
	// somebody answers a question.
	Proto struct {
		Native      bool   `json:"native"`
		AskTimeout  string `json:"askTimeout"`
		AskPoll     string `json:"askPoll"`
		AskRounds   int    `json:"askRounds"`
		StateTTL    string `json:"stateTTL"`
		SessionIdle string `json:"sessionIdle"`
		AskTTL      string `json:"askTTL"`
		MCPPath     string `json:"mcpPath"`
	} `json:"proto"`
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

// mustBytes parses a size the same way a user would write one.
func mustBytes(s, field string) int64 {
	n, err := parseBytes(s)
	if err != nil {
		panic(fmt.Sprintf("mcpx: defaults.json %s is not a size: %v", field, err))
	}
	return n
}

func parseBytes(v string) (int64, error) {
	v = strings.TrimSpace(v)
	mult := int64(1)
	upper := strings.ToUpper(v)
	for _, suf := range []struct {
		s string
		m int64
	}{
		{"KIB", 1 << 10}, {"MIB", 1 << 20}, {"GIB", 1 << 30},
		{"KB", 1000}, {"MB", 1000 * 1000}, {"GB", 1000 * 1000 * 1000},
		{"B", 1},
	} {
		if strings.HasSuffix(upper, suf.s) {
			mult = suf.m
			v = strings.TrimSpace(v[:len(v)-len(suf.s)])
			break
		}
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("not a size: %q", v)
	}
	return int64(f * float64(mult)), nil
}

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

	ShutdownGrace        = mustDur(builtin.Plumbing.ShutdownGrace, "plumbing.shutdownGrace")
	StdioDrainGrace      = mustDur(builtin.Plumbing.StdioDrainGrace, "plumbing.stdioDrainGrace")
	StdioMaxLine         = mustBytes(builtin.Plumbing.StdioMaxLine, "plumbing.stdioMaxLine")
	DaemonConnectTimeout = mustDur(builtin.Plumbing.DaemonConnectTimeout, "plumbing.daemonConnectTimeout")
	DaemonPollInterval   = mustDur(builtin.Plumbing.DaemonPollInterval, "plumbing.daemonPollInterval")
	DaemonRestartSettle  = mustDur(builtin.Plumbing.DaemonRestartSettle, "plumbing.daemonRestartSettle")
	HTTPIdleTimeout      = mustDur(builtin.Plumbing.HTTPIdleTimeout, "plumbing.httpIdleTimeout")
	HTTPRequestTimeout   = mustDur(builtin.Plumbing.HTTPRequestTimeout, "plumbing.httpRequestTimeout")
	FollowPollInterval   = mustDur(builtin.Plumbing.FollowPollInterval, "plumbing.followPollInterval")
	LogQueryLimit        = builtin.Plumbing.LogQueryLimit
	RestartBackoffStep   = mustDur(builtin.Plumbing.RestartBackoffStep, "plumbing.restartBackoffStep")
	RestartBackoffMax    = mustDur(builtin.Plumbing.RestartBackoffMax, "plumbing.restartBackoffMax")
	RegistryTimeout      = mustDur(builtin.Plumbing.RegistryTimeout, "plumbing.registryTimeout")
	EventHistory         = builtin.Plumbing.EventHistory
	StreamReconnect      = mustDur(builtin.Plumbing.StreamReconnect, "plumbing.streamReconnect")
	TaskTTL              = mustDur(builtin.Plumbing.TaskTTL, "plumbing.taskTTL")
	TaskResultWait       = mustDur(builtin.Plumbing.TaskResultWait, "plumbing.taskResultWait")
	StatsTop             = builtin.Plumbing.StatsTop
	RegistryLimit        = builtin.Plumbing.RegistryLimit

	ElicitTTL            = mustDur(builtin.Elicit.TTL, "elicit.ttl")
	ElicitPollInterval   = mustDur(builtin.Elicit.PollInterval, "elicit.pollInterval")
	ElicitHandlerTimeout = mustDur(builtin.Elicit.HandlerTimeout, "elicit.handlerTimeout")
	// InputRounds bounds how many times one 2026-07-28 request may come back
	// input_required. A server that keeps asking is broken or adversarial,
	// and without a bound the client would answer it forever.
	InputRounds = builtin.Elicit.InputRounds

	// ProtoNative turns native elicitation and sampling to mcpx's own MCP
	// clients on. Off, every upstream question goes to the broker's default
	// audience, which is what happened before a client could answer one.
	ProtoNative = builtin.Proto.Native
	// ProtoAskTimeout bounds how long one client request may be held while
	// a question goes unanswered. A legacy client is blocked for all of it,
	// so it has to sit inside whatever that client's own timeout is.
	ProtoAskTimeout = mustDur(builtin.Proto.AskTimeout, "proto.askTimeout")
	ProtoAskPoll    = mustDur(builtin.Proto.AskPoll, "proto.askPoll")
	// ProtoAskRounds bounds how many times one request may come back asking
	// for more. A server that never stops asking is broken or adversarial.
	ProtoAskRounds = builtin.Proto.AskRounds
	// ProtoStateTTL is how long a requestState may be resumed with. Beyond
	// it the call it names has been reaped anyway, so a longer window would
	// only turn a gone call into a confusing one.
	ProtoStateTTL = mustDur(builtin.Proto.StateTTL, "proto.stateTTL")
	// ProtoSessionIdle drops a Streamable HTTP session nothing has used.
	ProtoSessionIdle = mustDur(builtin.Proto.SessionIdle, "proto.sessionIdle")
	// ProtoAskTTL is how long the daemon keeps a call that is waiting for
	// an answer, and its result once it has one.
	ProtoAskTTL = mustDur(builtin.Proto.AskTTL, "proto.askTTL")
	// ProtoMCPPath is where the daemon serves MCP itself.
	ProtoMCPPath = builtin.Proto.MCPPath

	CatalogBudget = builtin.Catalog.Budget

	Permissions      = builtin.Script.Permissions
	CaptureConsole   = builtin.Script.CaptureConsole
	TypecheckTimeout = mustDur(builtin.Script.TypecheckTimeout, "script.typecheckTimeout")
)
