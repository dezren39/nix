package logging

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"os/user"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/dezren39/mcpx/internal/defaults"
)

// TraceID identifies one thing that started and will end: a daemon, a server
// instance, a session, a script run, a tool call.
//
// The shape is deliberately span-like. A record carries the id of the thing it
// happened inside; the line that *creates* something carries both the new id
// and its parent, so the tree can be rebuilt from the log alone without every
// later record repeating its ancestry. That is the same trade a tracing system
// makes, and it is why a long-running daemon does not pay for its depth on
// every line.
type TraceID string

// NewTraceID mints an identifier with a short readable prefix.
func NewTraceID(prefix string) TraceID {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Randomness failing is not a reason to stop logging.
		return TraceID(prefix + "-0000000000000000")
	}
	return TraceID(prefix + "-" + hex.EncodeToString(b[:]))
}

// Trace keys. These are attribute names rather than a struct so that a record
// stays a flat map, which is what every log store wants.
const (
	// KeyTrace is the thing this record happened inside.
	KeyTrace = "trace"
	// KeyParent is set only on the record that creates a trace, naming what it
	// was created from.
	KeyParent = "trace.parent"
	// KeyEvent names a lifecycle moment: daemon.start, server.start, and so on.
	KeyEvent = "event"
)

// Include names an optional block of ambient facts.
type Include string

const (
	// IncludeHost is the machine: hostname, os, arch, cpus.
	IncludeHost Include = "host"
	// IncludeUser is who is running this.
	IncludeUser Include = "user"
	// IncludeProcess is pid, executable and working directory.
	IncludeProcess Include = "process"
	// IncludeNetwork is the primary interface's address and hardware address.
	// Off by default: it is slow to gather and rarely what anyone wanted.
	IncludeNetwork Include = "network"
	// IncludeVersion is the mcpx build.
	IncludeVersion Include = "version"
	// IncludeEnv is the MCPX_* environment, with values elided.
	IncludeEnv Include = "env"
)

// AllIncludes is every block, for documentation and for `--include all`.
var AllIncludes = []Include{
	IncludeHost, IncludeUser, IncludeProcess, IncludeNetwork, IncludeVersion, IncludeEnv,
}

// DefaultIncludes are gathered on lifecycle records unless configured
// otherwise. The set is listed in internal/defaults/defaults.json. Network is
// excluded there because enumerating interfaces costs milliseconds and almost
// never answers a question anyone asked.
var DefaultIncludes = func() []Include {
	out := make([]Include, 0, len(defaults.LogIncludes))
	for _, name := range defaults.LogIncludes {
		out = append(out, Include(name))
	}
	return out
}()

// ParseIncludes turns a comma or space separated list into blocks. "all" is
// every block, "none" is none.
func ParseIncludes(spec string) []Include {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return DefaultIncludes
	}
	fields := strings.FieldsFunc(spec, func(r rune) bool { return r == ',' || r == ' ' })
	var out []Include
	for _, f := range fields {
		switch strings.ToLower(f) {
		case "all":
			return AllIncludes
		case "none":
			return nil
		default:
			for _, known := range AllIncludes {
				if string(known) == strings.ToLower(f) {
					out = append(out, known)
				}
			}
		}
	}
	return out
}

var (
	ambientOnce sync.Once
	ambientAll  map[Include]map[string]any
)

// Ambient gathers the requested blocks. Everything is collected once and
// cached: none of it changes while the process runs, and the network block in
// particular is too slow to repeat.
func Ambient(version string, want []Include) map[string]any {
	ambientOnce.Do(func() { ambientAll = gatherAmbient(version) })
	out := map[string]any{}
	for _, w := range want {
		for k, v := range ambientAll[w] {
			out[k] = v
		}
	}
	return out
}

func gatherAmbient(version string) map[Include]map[string]any {
	all := map[Include]map[string]any{}

	host := map[string]any{"host.os": runtime.GOOS, "host.arch": runtime.GOARCH,
		"host.cpus": runtime.NumCPU()}
	if name, err := os.Hostname(); err == nil {
		host["host.name"] = name
	}
	all[IncludeHost] = host

	usr := map[string]any{}
	if u, err := user.Current(); err == nil {
		usr["user.name"] = u.Username
		usr["user.uid"] = u.Uid
	}
	all[IncludeUser] = usr

	proc := map[string]any{"process.pid": os.Getpid()}
	if exe, err := os.Executable(); err == nil {
		proc["process.exe"] = exe
	}
	if wd, err := os.Getwd(); err == nil {
		proc["process.cwd"] = wd
	}
	all[IncludeProcess] = proc

	all[IncludeVersion] = map[string]any{
		"mcpx.version": version,
		"go.version":   runtime.Version(),
	}

	all[IncludeNetwork] = gatherNetwork()

	env := map[string]any{}
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || !strings.HasPrefix(k, "MCPX_") {
			continue
		}
		env["env."+k] = Redact(k, v)
	}
	all[IncludeEnv] = env

	return all
}

// sensitiveName matches variable names that should never have their value
// recorded.
var sensitiveName = regexp.MustCompile(`(?i)(token|secret|key|password|passwd|pwd|credential|auth|session[_-]?id|cookie|signature|private)`)

// sensitiveValue matches values that carry a credential regardless of what
// the variable is called.
//
// Matching only on the name was not enough. MCPX_SCRIPT_ENV carries
// KEY=VALUE pairs for the script, so a perfectly innocuous name holds
// "API_TOKEN=..." as its value; the same is true of any variable holding a
// header. The name test cannot see inside those, so the value is checked too.
var sensitiveValue = regexp.MustCompile(`(?i)(bearer\s+\S+|` +
	// No \b around the keyword: underscore is a word character, so \btoken\b
	// does not match inside API_TOKEN, which is exactly the spelling that
	// matters. Surrounding name characters are absorbed instead.
	`[A-Za-z0-9_.-]*(token|secret|api[_-]?key|password|passwd|credential)[A-Za-z0-9_.-]*\s*[:=]\s*\S+|` +
	`\bgh[pousr]_[A-Za-z0-9]{16,}|` +
	`\bsk-[A-Za-z0-9]{16,}|` +
	`\bxox[baprs]-[A-Za-z0-9-]{10,}|` +
	`\bAKIA[0-9A-Z]{16}\b|` +
	`\bey[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.)`)

// Redact returns a value safe to record.
//
// Whole-value elision when the name is telling, and per-match elision when
// only part of the value is sensitive -- because a variable holding several
// pairs is still worth seeing, minus the one that matters.
func Redact(name, value string) string {
	if sensitiveName.MatchString(name) {
		return "<elided>"
	}
	if value == "" || !sensitiveValue.MatchString(value) {
		return value
	}
	return sensitiveValue.ReplaceAllStringFunc(value, func(m string) string {
		// Keep the part that identifies what was elided, drop the secret.
		if i := strings.IndexAny(m, ":="); i > 0 {
			return m[:i+1] + "<elided>"
		}
		if i := strings.IndexAny(m, " \t"); i > 0 {
			return m[:i] + " <elided>"
		}
		return "<elided>"
	})
}

// HarnessIDs reads MCPX_TRACE_IDS.
//
// The harness knows things the agent does not -- which session this is, whose
// child it is, which worktree it runs in -- and passes them as a list of
// pairs rather than a flat id, because a flat one cannot express a
// relationship. Reading it here means every record carries that context
// without any caller threading it through.
//
// Unknown keys are kept. The list exists to grow, and a reader that dropped
// what it did not recognise would defeat that.
func HarnessIDs() map[string]any {
	raw := strings.TrimSpace(os.Getenv("MCPX_TRACE_IDS"))
	if raw == "" {
		return nil
	}
	var pairs [][]string
	if err := json.Unmarshal([]byte(raw), &pairs); err != nil {
		return nil
	}
	out := map[string]any{}
	for _, p := range pairs {
		if len(p) < 2 || p[0] == "" {
			continue
		}
		// Entries longer than a pair name several ids for one key, which is
		// how a caller says "these are all the same thing".
		if len(p) == 2 {
			out["id."+p[0]] = p[1]
			continue
		}
		out["id."+p[0]] = p[1:]
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// gatherNetwork finds the first non-loopback interface that is up.
func gatherNetwork() map[string]any {
	out := map[string]any{}
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, i := range ifaces {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := i.Addrs()
		if err != nil || len(addrs) == 0 {
			continue
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil {
				continue
			}
			out["net.interface"] = i.Name
			out["net.ip"] = ipn.IP.String()
			if i.HardwareAddr != nil {
				out["net.mac"] = i.HardwareAddr.String()
			}
			return out
		}
	}
	return out
}
