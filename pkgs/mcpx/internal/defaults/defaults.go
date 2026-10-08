package defaults

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
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
		Trace    bool     `json:"trace"`
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
		StdioExitGrace       string `json:"stdioExitGrace"`
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
		EventSubscriberBuf   int    `json:"eventSubscriberBuffer"`
		StreamReconnect      string `json:"streamReconnect"`
		TaskTTL              string `json:"taskTTL"`
		TaskResultWait       string `json:"taskResultWait"`
		StatsTop             int    `json:"statsTop"`
		RegistryLimit        int    `json:"registryLimit"`
	} `json:"plumbing"`
	// Resolve governs GET /v1/resolve, which answers "which daemon serves
	// this directory" for a caller that has no mcpx binary.
	Resolve struct {
		DialTimeout string `json:"dialTimeout"`
	} `json:"resolve"`
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
		Native       bool   `json:"native"`
		AskTimeout   string `json:"askTimeout"`
		AskPoll      string `json:"askPoll"`
		AskRounds    int    `json:"askRounds"`
		StateTTL     string `json:"stateTTL"`
		SessionIdle  string `json:"sessionIdle"`
		AskTTL       string `json:"askTTL"`
		AbandonGrace string `json:"abandonGrace"`
		MCPPath      string `json:"mcpPath"`
	} `json:"proto"`
	// Transport governs how mcpx's own MCP server behaves on the wire, as
	// opposed to what it says: keep-alives, shutdown, who may connect.
	Transport struct {
		SSEKeepAlive   string   `json:"sseKeepAlive"`
		StdioDrain     string   `json:"stdioDrain"`
		LoopbackHosts  []string `json:"loopbackHosts"`
		AllowedOrigins []string `json:"allowedOrigins"`
	} `json:"transport"`

	// CLI governs how the command line renders what it did not write a
	// formatter for: the commands generated from the /v1 operation table.
	CLI struct {
		CellWidth   int `json:"cellWidth"`
		UsageColumn int `json:"usageColumn"`
	} `json:"cli"`
	Catalog struct {
		Budget int `json:"budget"`
	} `json:"catalog"`
	Script struct {
		Permissions      string          `json:"permissions"`
		RuntimeOrder     []string        `json:"runtimeOrder"`
		Profiles         json.RawMessage `json:"profiles"`
		CaptureConsole   bool            `json:"captureConsole"`
		TypecheckTimeout string          `json:"typecheckTimeout"`
	} `json:"script"`
	// Consumer governs the things mcpx asks for on its own behalf: which
	// instance to lease, whether to confirm a destructive call, how a
	// request in words is turned into a script. Grouped under one name
	// because they share a justification -- mcpx initiating a conversation
	// rather than forwarding one -- and that is the thing a reader will want
	// to find or switch off as a whole.
	Consumer struct {
		Disambiguate        string `json:"disambiguate"`
		DisambiguateDefault string `json:"disambiguateDefault"`
		ConfirmDestructive  bool   `json:"confirmDestructive"`
		AskTimeout          string `json:"askTimeout"`
		DiagnosePreflight   bool   `json:"diagnosePreflight"`
		HistoryPerTool      int    `json:"historyPerTool"`
		RecipeMinScore      int    `json:"recipeMinScore"`
		RecipeMatchMargin   int    `json:"recipeMatchMargin"`
		RecipeLimit         int    `json:"recipeLimit"`
		PromptAutonomy      string `json:"promptAutonomy"`
		AutonomyMax         string `json:"autonomyMax"`
		RepairAutonomy      string `json:"repairAutonomy"`
		HooksAutonomy       string `json:"hooksAutonomy"`
		PromptSample        string `json:"promptSample"`
		PromptCatalogBudget int    `json:"promptCatalogBudget"`
		PromptSampleTimeout string `json:"promptSampleTimeout"`
		PromptMaxTokens     int    `json:"promptMaxTokens"`
		RunTimeout          string `json:"runTimeout"`
	} `json:"consumer"`
	// Exec governs running a script, whether the CLI does it or the daemon
	// does it on a caller's behalf.
	Exec struct {
		Timeout      string `json:"timeout"`
		WorkDirs     int    `json:"workDirs"`
		Programs     int    `json:"programs"`
		Entries      int    `json:"entries"`
		StderrLimit  int    `json:"stderrLimit"`
		Output       string `json:"output"`
		RemoteOutput string `json:"remoteOutput"`
		Delivery     string `json:"delivery"`
	} `json:"exec"`
	// Artifacts governs the outbox: files a script produced, held for a
	// caller to fetch rather than printed into its context.
	Artifacts struct {
		TTL                string `json:"ttl"`
		MaxBytes           string `json:"maxBytes"`
		Quota              string `json:"quota"`
		InlineMaxBytes     string `json:"inlineMaxBytes"`
		ChunkBytes         string `json:"chunkBytes"`
		GCInterval         string `json:"gcInterval"`
		InterceptImages    bool   `json:"interceptImages"`
		NameMaxLength      int    `json:"nameMaxLength"`
		NameCollisionLimit int    `json:"nameCollisionLimit"`
		IDBytes            int    `json:"idBytes"`
		ListLimit          int    `json:"listLimit"`
	} `json:"artifacts"`
	// HTTP governs the daemon's own listener and the clients that talk to
	// it. Every one of these was an inline literal beside the code that
	// happened to need it, which meant the request body ceiling was three
	// different numbers depending on which route you hit and no document
	// said so.
	HTTP struct {
		BodyLimit         string `json:"bodyLimit"`
		CallBodyLimit     string `json:"callBodyLimit"`
		ControlBodyLimit  string `json:"controlBodyLimit"`
		ReadHeaderTimeout string `json:"readHeaderTimeout"`
		ShutdownGrace     string `json:"shutdownGrace"`
		SSEPing           string `json:"ssePing"`
		SSERetry          string `json:"sseRetry"`
		StreamBufferInit  string `json:"streamBufferInit"`
		StreamBufferMax   string `json:"streamBufferMax"`
		IdleConns         int    `json:"idleConns"`
		RemoteIdleConns   int    `json:"remoteIdleConns"`
	} `json:"http"`
	// Autostart is how a CLI command brings a daemon into being when there
	// is not one. The binary and the argument list are here because a
	// packaged mcpx may not be the one on $PATH, and a sandbox may need the
	// daemon started differently without patching the binary.
	Autostart struct {
		Bin            string   `json:"bin"`
		Args           []string `json:"args"`
		IdleExit       string   `json:"idleExit"`
		ConnectTimeout string   `json:"connectTimeout"`
		PollInterval   string   `json:"pollInterval"`
		PingTimeout    string   `json:"pingTimeout"`
		LogTail        string   `json:"logTail"`
	} `json:"autostart"`
	Limits struct {
		SearchLimit          int    `json:"searchLimit"`
		CompletionValues     int    `json:"completionValues"`
		ElicitPending        int    `json:"elicitPending"`
		RegistryPageSize     int    `json:"registryPageSize"`
		RegistryMaxPages     int    `json:"registryMaxPages"`
		RegistryNameFallback int    `json:"registryNameFallback"`
		LeaseTTL             string `json:"leaseTTL"`
		SocketProbeTimeout   string `json:"socketProbeTimeout"`
		WarmTimeout          string `json:"warmTimeout"`
		RefreshTimeout       string `json:"refreshTimeout"`
		InlineStartTimeout   string `json:"inlineStartTimeout"`
		InlineStartPoll      string `json:"inlineStartPoll"`
		InlineProbeTimeout   string `json:"inlineProbeTimeout"`
		DoctorTimeout        string `json:"doctorTimeout"`
		FollowBacklog        int    `json:"followBacklog"`
		ReleaseTimeout       string `json:"releaseTimeout"`
	} `json:"limits"`
	// Files are the permission bits mcpx creates things with. They are data
	// for the same reason everything else here is -- one place to read the
	// answer -- but they are deliberately *not* settings: the state
	// directory holds a socket that grants the power to run tools as this
	// user, and a configuration key that widens it is a footgun with no
	// legitimate use.
	Files struct {
		DirMode       string `json:"dirMode"`
		PublicDirMode string `json:"publicDirMode"`
		PrivateMode   string `json:"privateMode"`
		PublicMode    string `json:"publicMode"`
	} `json:"files"`
	// Git is how the repo and worktree scopes find a repository. Discovery
	// reads .git itself (internal/config/gitdiscover.go); Bin is only run for
	// the layouts that port does not vouch for, and GitfileMaxBytes is git's
	// own ceiling on a .git file, kept so an absurd one is refused by both
	// rather than read into the daemon's memory. Neither is a setting: PATH
	// already chooses the git, and changing the ceiling would only make mcpx
	// disagree with git about which files are valid.
	Git struct {
		Bin             string `json:"bin"`
		GitfileMaxBytes string `json:"gitfileMaxBytes"`
	} `json:"git"`
	// Plugin is read by the opencode plugin rather than by this binary. It
	// is declared here so that `mcpx settings` can answer what the plugin
	// will do, which is otherwise only discoverable by reading TypeScript.
	Plugin struct {
		Bin            string   `json:"bin"`
		BinArgs        []string `json:"binArgs"`
		Backend        string   `json:"backend"`
		DiscoveryRetry string   `json:"discoveryRetry"`
		ToolTiming     bool     `json:"toolTiming"`
		Env            string   `json:"env"`
		Instructions   bool     `json:"instructions"`
		Remember       string   `json:"remember"`
		Annotate       bool     `json:"annotate"`
		Tools          bool     `json:"tools"`
		// Headless and DaemonTools are "auto" rather than a boolean: the
		// plugin decides them at boot from where it is running and what it
		// found, and a fixed true or false here would be right in only one
		// of those cases.
		Headless    string `json:"headless"`
		DaemonTools string `json:"daemonTools"`
	} `json:"plugin"`
	// ProtoMessages governs the per-message fields mcpx's own MCP server
	// attaches: cache hints on results and keep-alives on listen streams.
	ProtoMessages struct {
		ListMaxAge string `json:"listMaxAge"`
		ReadMaxAge string `json:"readMaxAge"`
		TaskAfter  string `json:"taskAfter"`
	} `json:"protoMessages"`
	// ProtoTasks governs the tasks mcpx hands out, on MCP and on /v1.
	ProtoTasks struct {
		PollInterval string `json:"pollInterval"`
	} `json:"protoTasks"`
	// StdioShutdown is how mcpx ends a stdio server, per every revision's
	// stdio shutdown SHOULD: close stdin and wait StdinGrace for it to exit
	// by itself, then SIGTERM and wait TermGrace, then SIGKILL and wait at
	// most KillWait for the reaper.
	StdioShutdown struct {
		StdinGrace string `json:"stdinGrace"`
		TermGrace  string `json:"termGrace"`
		KillWait   string `json:"killWait"`
	} `json:"stdioShutdown"`
	// Upstream governs how mcpx connects to the servers it fronts: which
	// protocol era it tries first, how long it waits to find out, and where
	// it remembers the answer.
	Upstream struct {
		Protocol       string `json:"protocol"`
		ProbeTimeout   string `json:"probeTimeout"`
		EraCache       bool   `json:"eraCache"`
		EraFile        string `json:"eraFile"`
		EraFileVersion int    `json:"eraFileVersion"`
		ErrorBodyLimit string `json:"errorBodyLimit"`
		// SSEReconnectDelay is the wait before resuming a Streamable HTTP
		// stream the server closed without saying how long to wait (no
		// retry field).
		SSEReconnectDelay string `json:"sseReconnectDelay"`
		// SSEReconnectAttempts bounds consecutive resumptions of one stream.
		SSEReconnectAttempts int `json:"sseReconnectAttempts"`
		// ListenReopenDelay is the wait before reopening a
		// subscriptions/listen stream that ended without a response.
		ListenReopenDelay string `json:"listenReopenDelay"`
		// LegacyStreamEndpointTimeout bounds the wait for an HTTP+SSE
		// (2024-11-05) server's endpoint event.
		LegacyStreamEndpointTimeout string `json:"legacyStreamEndpointTimeout"`
		// CancelSendTimeout bounds sending notifications/cancelled.
		CancelSendTimeout string `json:"cancelSendTimeout"`
		// ElicitReplyTimeout bounds sending the reply to a server's request.
		ElicitReplyTimeout string `json:"elicitReplyTimeout"`
		// SessionDeleteTimeout bounds the DELETE that ends an HTTP session.
		SessionDeleteTimeout string `json:"sessionDeleteTimeout"`
		// ListPageLimit bounds the pages one list request follows.
		ListPageLimit int `json:"listPageLimit"`
		// VersionAttempts bounds how often a probe offers one version.
		VersionAttempts int `json:"versionAttempts"`
	} `json:"upstream"`
	Embeddings struct {
		RemoteTimeout string `json:"remoteTimeout"`
		SearchTimeout string `json:"searchTimeout"`
	} `json:"embeddings"`
}

// Embeddings defaults.
var (
	EmbeddingsRemoteTimeout = mustDur(builtin.Embeddings.RemoteTimeout, "embeddings.remoteTimeout")
	EmbeddingsSearchTimeout = mustDur(builtin.Embeddings.SearchTimeout, "embeddings.searchTimeout")
)

// Upstream connection defaults.
var (
	// UpstreamProtocol is the era preference for a server that names none.
	UpstreamProtocol = builtin.Upstream.Protocol
	// UpstreamProbeTimeout is how long a stdio server/discover may go
	// unanswered before initialize is sent alongside it.
	UpstreamProbeTimeout = mustDur(builtin.Upstream.ProbeTimeout, "upstream.probeTimeout")
	// UpstreamEraCache remembers each server configuration's era.
	UpstreamEraCache = builtin.Upstream.EraCache
	// UpstreamEraFile is the era cache's name inside the state directory.
	UpstreamEraFile = builtin.Upstream.EraFile
	// UpstreamEraFileVersion is bumped when the file's shape changes; a file
	// of any other version is ignored and rewritten.
	UpstreamEraFileVersion = builtin.Upstream.EraFileVersion
	// HTTPErrorBodyLimit bounds how much of a non-2xx body is read: enough
	// for a JSON-RPC error, not enough for an HTML error page to matter.
	HTTPErrorBodyLimit = mustBytes(builtin.Upstream.ErrorBodyLimit, "upstream.errorBodyLimit")
	// UpstreamSSEReconnectDelay is the resume wait when a server sent no
	// retry field.
	UpstreamSSEReconnectDelay = mustDur(builtin.Upstream.SSEReconnectDelay, "upstream.sseReconnectDelay")
	// UpstreamSSEReconnectAttempts bounds consecutive resumptions.
	UpstreamSSEReconnectAttempts = builtin.Upstream.SSEReconnectAttempts
	// UpstreamListenReopenDelay is the wait before reopening a listen stream.
	UpstreamListenReopenDelay = mustDur(builtin.Upstream.ListenReopenDelay, "upstream.listenReopenDelay")
	// UpstreamLegacyStreamEndpointTimeout bounds the HTTP+SSE endpoint event.
	UpstreamLegacyStreamEndpointTimeout = mustDur(builtin.Upstream.LegacyStreamEndpointTimeout,
		"upstream.legacyStreamEndpointTimeout")
	// UpstreamCancelSendTimeout bounds sending notifications/cancelled.
	UpstreamCancelSendTimeout = mustDur(builtin.Upstream.CancelSendTimeout, "upstream.cancelSendTimeout")
	// UpstreamElicitReplyTimeout bounds sending a reply to a server request.
	UpstreamElicitReplyTimeout = mustDur(builtin.Upstream.ElicitReplyTimeout, "upstream.elicitReplyTimeout")
	// UpstreamSessionDeleteTimeout bounds the session DELETE on close.
	UpstreamSessionDeleteTimeout = mustDur(builtin.Upstream.SessionDeleteTimeout, "upstream.sessionDeleteTimeout")
	// ListPageLimit bounds the pages one list request follows, so a server
	// whose cursors never end cannot hold a listing forever.
	ListPageLimit = builtin.Upstream.ListPageLimit
	// UpstreamVersionAttempts bounds how often server/discover offers one
	// version, so a server that rejects what it lists cannot loop a probe.
	UpstreamVersionAttempts = builtin.Upstream.VersionAttempts
)

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
	LogTrace    = builtin.Logging.Trace

	ReapInterval = mustDur(builtin.Daemon.ReapInterval, "daemon.reapInterval")
	SaveInterval = mustDur(builtin.Daemon.SaveInterval, "daemon.saveInterval")

	ShutdownGrace        = mustDur(builtin.Plumbing.ShutdownGrace, "plumbing.shutdownGrace")
	StdioExitGrace       = mustDur(builtin.Plumbing.StdioExitGrace, "plumbing.stdioExitGrace")
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
	// EventSubscriberBuf is how many events one subscriber may fall behind
	// before it is dropped as slow.
	EventSubscriberBuf = builtin.Plumbing.EventSubscriberBuf
	StreamReconnect    = mustDur(builtin.Plumbing.StreamReconnect, "plumbing.streamReconnect")
	TaskTTL            = mustDur(builtin.Plumbing.TaskTTL, "plumbing.taskTTL")
	TaskResultWait     = mustDur(builtin.Plumbing.TaskResultWait, "plumbing.taskResultWait")
	StatsTop           = builtin.Plumbing.StatsTop
	RegistryLimit      = builtin.Plumbing.RegistryLimit

	// ResolveDialTimeout bounds the liveness check /v1/resolve makes against
	// the socket it is about to name. It is a local connect on a unix
	// socket, so it either succeeds immediately or the daemon is gone;
	// waiting longer only makes a dead daemon slower to report.
	ResolveDialTimeout = mustDur(builtin.Resolve.DialTimeout, "resolve.dialTimeout")

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
	// ProtoAbandonGrace bounds telling the daemon to stop a call nobody is
	// coming back for. Best effort by definition: the request that wanted it
	// is already being failed.
	ProtoAbandonGrace = mustDur(builtin.Proto.AbandonGrace, "proto.abandonGrace")
	// ProtoMCPPath is where the daemon serves MCP itself.
	ProtoMCPPath = builtin.Proto.MCPPath

	// ProtoListMaxAge is the ttlMs a 2026-07-28 client is given on
	// server/discover and every list result. Short, because the lists follow
	// configuration and a list_changed only reaches a client that listens.
	ProtoListMaxAge = mustDur(builtin.ProtoMessages.ListMaxAge, "protoMessages.listMaxAge")
	// ProtoReadMaxAge is the ttlMs on resources/read. Zero: a resource is
	// whatever an upstream server says it is now.
	ProtoReadMaxAge = mustDur(builtin.ProtoMessages.ReadMaxAge, "protoMessages.readMaxAge")
	// ProtoTaskAfter is how long a tools/call from a client that declared
	// the tasks extension runs in line before mcpx hands back a task
	// instead of the result.
	ProtoTaskAfter = mustDur(builtin.ProtoMessages.TaskAfter, "protoMessages.taskAfter")
	// TaskPollInterval is the pollInterval every task carries: how often a
	// client is told it may usefully ask after one.
	TaskPollInterval = mustDur(builtin.ProtoTasks.PollInterval, "protoTasks.pollInterval")

	StdioStdinGrace = mustDur(builtin.StdioShutdown.StdinGrace, "stdioShutdown.stdinGrace")
	StdioTermGrace  = mustDur(builtin.StdioShutdown.TermGrace, "stdioShutdown.termGrace")
	StdioKillWait   = mustDur(builtin.StdioShutdown.KillWait, "stdioShutdown.killWait")

	// TransportSSEKeepAlive is how often an otherwise quiet event stream
	// carries a comment line, so an intermediary or a client idle timeout
	// does not close a stream that is merely waiting.
	TransportSSEKeepAlive = mustDur(builtin.Transport.SSEKeepAlive, "transport.sseKeepAlive")
	// TransportStdioDrain bounds how long `mcpx serve` keeps answering
	// requests already in flight after its input closes, before it cancels
	// them and exits.
	TransportStdioDrain = mustDur(builtin.Transport.StdioDrain, "transport.stdioDrain")
	// TransportLoopbackHosts are the Origin hosts a browser page on this
	// machine presents; any port, http or https.
	TransportLoopbackHosts = builtin.Transport.LoopbackHosts
	// TransportAllowedOrigins are further Origins allowed to reach /mcp.
	TransportAllowedOrigins = builtin.Transport.AllowedOrigins

	CatalogBudget = builtin.Catalog.Budget

	// OpCellWidth caps a table cell in a generated command's output; a
	// description in full turns a table into a wall.
	OpCellWidth = builtin.CLI.CellWidth
	UsageColumn = builtin.CLI.UsageColumn

	Permissions = builtin.Script.Permissions
	// RuntimeOrder is the order auto tries runtimes in.
	RuntimeOrder = builtin.Script.RuntimeOrder
	// ProfilesJSON are the built-in permission profiles, parsed by the
	// runner with the same reader as a user's script.profiles so the two
	// cannot disagree about the shape.
	ProfilesJSON     = builtin.Script.Profiles
	CaptureConsole   = builtin.Script.CaptureConsole
	TypecheckTimeout = mustDur(builtin.Script.TypecheckTimeout, "script.typecheckTimeout")

	Disambiguate        = builtin.Consumer.Disambiguate
	DisambiguateDefault = builtin.Consumer.DisambiguateDefault
	ConfirmDestructive  = builtin.Consumer.ConfirmDestructive
	AskTimeout          = mustDur(builtin.Consumer.AskTimeout, "consumer.askTimeout")
	DiagnosePreflight   = builtin.Consumer.DiagnosePreflight
	HistoryPerTool      = builtin.Consumer.HistoryPerTool
	RecipeMinScore      = builtin.Consumer.RecipeMinScore
	RecipeMatchMargin   = builtin.Consumer.RecipeMatchMargin
	RecipeLimit         = builtin.Consumer.RecipeLimit
	PromptAutonomy      = builtin.Consumer.PromptAutonomy
	AutonomyMax         = builtin.Consumer.AutonomyMax
	RepairAutonomy      = builtin.Consumer.RepairAutonomy
	HooksAutonomy       = builtin.Consumer.HooksAutonomy
	PromptSample        = builtin.Consumer.PromptSample
	PromptCatalogBudget = builtin.Consumer.PromptCatalogBudget
	PromptSampleTimeout = mustDur(builtin.Consumer.PromptSampleTimeout, "consumer.promptSampleTimeout")
	PromptMaxTokens     = builtin.Consumer.PromptMaxTokens
	RunTimeout          = mustDur(builtin.Consumer.RunTimeout, "consumer.runTimeout")
	ExecTimeout         = mustDur(builtin.Exec.Timeout, "exec.timeout")
	ExecOutput          = builtin.Exec.Output
	// ExecStderrLimit bounds how much of a failed script's stderr is kept to
	// explain the failure. Unbounded, a script looping on stderr would be
	// held in memory in full, by the daemon, on someone else's behalf.
	ExecStderrLimit = builtin.Exec.StderrLimit
	// ExecWorkDirs bounds how many content-addressed client directories are
	// kept. One per distinct catalogue; without a bound it grows forever.
	ExecWorkDirs = builtin.Exec.WorkDirs
	// ExecPrograms bounds how many generated programs one client directory
	// keeps as type-check cache entries.
	ExecPrograms = builtin.Exec.Programs
	// ExecEntries bounds how many generated entry points one script leaves
	// beside itself. They are visible in the user's directory.
	ExecEntries      = builtin.Exec.Entries
	ExecRemoteOutput = builtin.Exec.RemoteOutput
	ExecDelivery     = builtin.Exec.Delivery

	ArtifactTTL = mustDur(builtin.Artifacts.TTL, "artifacts.ttl")
	// ArtifactMaxBytes is the ceiling for one artifact. A screenshot is
	// under a megabyte; the headroom is for the video and the core dump
	// somebody will eventually want to hand back.
	ArtifactMaxBytes = mustBytes(builtin.Artifacts.MaxBytes, "artifacts.maxBytes")
	ArtifactQuota    = mustBytes(builtin.Artifacts.Quota, "artifacts.quota")
	// ArtifactInlineMaxBytes bounds what may be base64'd into a structured
	// result. Well under the per-artifact cap on purpose: inline delivery
	// puts bytes in the caller's context, which is the cost this whole
	// feature exists to avoid.
	ArtifactInlineMaxBytes  = mustBytes(builtin.Artifacts.InlineMaxBytes, "artifacts.inlineMaxBytes")
	ArtifactChunkBytes      = mustBytes(builtin.Artifacts.ChunkBytes, "artifacts.chunkBytes")
	ArtifactGCInterval      = mustDur(builtin.Artifacts.GCInterval, "artifacts.gcInterval")
	ArtifactInterceptImages = builtin.Artifacts.InterceptImages
	ArtifactNameMaxLength   = builtin.Artifacts.NameMaxLength
	// ArtifactNameCollisionLimit bounds the -1, -2, -3 search when a name is
	// already taken in an output directory. A directory holding this many
	// files of one name is a bug in the caller, and looping forever hides it.
	ArtifactNameCollisionLimit = builtin.Artifacts.NameCollisionLimit
	// ArtifactIDBytes is the width of the random handle an artifact is
	// served by. It is the whole of the access control until OAuth scopes
	// land, so it is sized as a secret rather than as an identifier.
	ArtifactIDBytes       = builtin.Artifacts.IDBytes
	ArtifactListLimit     = builtin.Artifacts.ListLimit
	HTTPBodyLimit         = mustBytes(builtin.HTTP.BodyLimit, "http.bodyLimit")
	HTTPCallBodyLimit     = mustBytes(builtin.HTTP.CallBodyLimit, "http.callBodyLimit")
	HTTPControlBodyLimit  = mustBytes(builtin.HTTP.ControlBodyLimit, "http.controlBodyLimit")
	HTTPReadHeaderTimeout = mustDur(builtin.HTTP.ReadHeaderTimeout, "http.readHeaderTimeout")
	HTTPShutdownGrace     = mustDur(builtin.HTTP.ShutdownGrace, "http.shutdownGrace")
	HTTPSSEPing           = mustDur(builtin.HTTP.SSEPing, "http.ssePing")
	HTTPSSERetry          = mustDur(builtin.HTTP.SSERetry, "http.sseRetry")
	HTTPStreamBufferInit  = mustBytes(builtin.HTTP.StreamBufferInit, "http.streamBufferInit")
	HTTPStreamBufferMax   = mustBytes(builtin.HTTP.StreamBufferMax, "http.streamBufferMax")
	HTTPIdleConns         = builtin.HTTP.IdleConns
	HTTPRemoteIdleConns   = builtin.HTTP.RemoteIdleConns

	AutostartBin            = builtin.Autostart.Bin
	AutostartArgs           = builtin.Autostart.Args
	AutostartIdleExit       = mustDur(builtin.Autostart.IdleExit, "autostart.idleExit")
	AutostartConnectTimeout = mustDur(builtin.Autostart.ConnectTimeout, "autostart.connectTimeout")
	AutostartPollInterval   = mustDur(builtin.Autostart.PollInterval, "autostart.pollInterval")
	AutostartPingTimeout    = mustDur(builtin.Autostart.PingTimeout, "autostart.pingTimeout")
	AutostartLogTail        = mustBytes(builtin.Autostart.LogTail, "autostart.logTail")

	SearchLimit          = builtin.Limits.SearchLimit
	CompletionValues     = builtin.Limits.CompletionValues
	ElicitPending        = builtin.Limits.ElicitPending
	RegistryPageSize     = builtin.Limits.RegistryPageSize
	RegistryMaxPages     = builtin.Limits.RegistryMaxPages
	RegistryNameFallback = builtin.Limits.RegistryNameFallback
	LeaseTTL             = mustDur(builtin.Limits.LeaseTTL, "limits.leaseTTL")
	SocketProbeTimeout   = mustDur(builtin.Limits.SocketProbeTimeout, "limits.socketProbeTimeout")
	WarmTimeout          = mustDur(builtin.Limits.WarmTimeout, "limits.warmTimeout")
	RefreshTimeout       = mustDur(builtin.Limits.RefreshTimeout, "limits.refreshTimeout")
	InlineStartTimeout   = mustDur(builtin.Limits.InlineStartTimeout, "limits.inlineStartTimeout")
	InlineStartPoll      = mustDur(builtin.Limits.InlineStartPoll, "limits.inlineStartPoll")
	InlineProbeTimeout   = mustDur(builtin.Limits.InlineProbeTimeout, "limits.inlineProbeTimeout")
	DoctorTimeout        = mustDur(builtin.Limits.DoctorTimeout, "limits.doctorTimeout")
	ReleaseTimeout       = mustDur(builtin.Limits.ReleaseTimeout, "limits.releaseTimeout")
	FollowBacklog        = builtin.Limits.FollowBacklog

	DirMode       = mustMode(builtin.Files.DirMode, "files.dirMode")
	PublicDirMode = mustMode(builtin.Files.PublicDirMode, "files.publicDirMode")
	PrivateMode   = mustMode(builtin.Files.PrivateMode, "files.privateMode")
	PublicMode    = mustMode(builtin.Files.PublicMode, "files.publicMode")

	GitBin          = builtin.Git.Bin
	GitfileMaxBytes = mustBytes(builtin.Git.GitfileMaxBytes, "git.gitfileMaxBytes")

	PluginBin            = builtin.Plugin.Bin
	PluginBinArgs        = builtin.Plugin.BinArgs
	PluginBackend        = builtin.Plugin.Backend
	PluginDiscoveryRetry = mustDur(builtin.Plugin.DiscoveryRetry, "plugin.discoveryRetry")
	PluginToolTiming     = builtin.Plugin.ToolTiming
	PluginEnv            = builtin.Plugin.Env
	PluginInstructions   = builtin.Plugin.Instructions
	PluginTools          = builtin.Plugin.Tools
	PluginRemember       = builtin.Plugin.Remember
	PluginAnnotate       = builtin.Plugin.Annotate
	PluginHeadless       = builtin.Plugin.Headless
	PluginDaemonTools    = builtin.Plugin.DaemonTools
)

// mustMode reads an octal permission string. Written as "0700" rather than as
// the number 448, because the only readers who will ever check it think in
// octal and a JSON file cannot hold an octal literal.
func mustMode(s, field string) os.FileMode {
	n, err := strconv.ParseUint(strings.TrimSpace(s), 8, 32)
	if err != nil {
		panic(fmt.Sprintf("mcpx: defaults.json %s is not an octal mode: %v", field, err))
	}
	return os.FileMode(n)
}

// Str renders a duration the way a user would type it, so a default can go
// into the settings registry without being restated.
func Str(d time.Duration) string { return d.String() }

// Bytes renders a size in the syntax the settings parser accepts.
func Bytes(n int64) string { return strconv.FormatInt(n, 10) }

// Num renders an integer default.
func Num(n int) string { return strconv.Itoa(n) }

// Flag renders a boolean default.
func Flag(b bool) string { return strconv.FormatBool(b) }

// CSV renders a list default.
func CSV(v []string) string { return strings.Join(v, ",") }
