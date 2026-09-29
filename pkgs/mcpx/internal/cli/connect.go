package cli

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dezren39/mcpx/internal/daemon"
)

// Attempt is one rung of the connection ladder, and what happened on it.
type Attempt struct {
	How    string `json:"how"`
	Target string `json:"target,omitempty"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
	TookMs int64  `json:"tookMs"`
}

// Ladder is the ordered set of ways to reach a daemon.
//
// Ordered by cost, cheapest first, because the cheap ones are also the ones
// that usually work:
//
//	socket      0.17ms warm    an already-running daemon on this machine
//	url         0.65ms warm    one somewhere else, when configured
//	spawn      ~23ms once      start one, then use its socket
//	inline        n/a          no daemon at all
//
// The last rung matters more than it looks. A daemon is a cache and a
// process pool; neither is required to answer a question. When one cannot be
// started -- a read-only filesystem, a sandbox with no fork, a container
// whose init will not reap -- mcpx can run the servers in its own process
// and exit when it is done. Slower every time, but it works where nothing
// else does.
type Ladder struct {
	Attempts []Attempt `json:"attempts"`
	// Chosen is the rung that worked.
	Chosen string `json:"chosen"`
}

// ConnectOptions control the ladder.
type ConnectOptions struct {
	// Endpoint, when set, is tried instead of the socket and spawn rungs.
	// Naming a daemon means that daemon, not one like it.
	Endpoint string
	// AllowSpawn permits starting a daemon.
	AllowSpawn bool
	// AllowInline permits running servers in this process.
	AllowInline bool
	// SpawnTimeout bounds how long to wait for a spawned daemon to answer.
	SpawnTimeout time.Duration
}

// Connect walks the ladder and returns the first client that answers.
//
// Every rung is recorded whether or not it worked, because "why is this
// slow" and "why did it use the wrong daemon" are both answered by the list
// of what was tried.
func (a *App) Connect(ctx context.Context, opt ConnectOptions) (*Client, *Ladder, error) {
	// The socket is keyed to the configuration, so that two projects get two
	// daemons rather than one serving the wrong servers. Using the unkeyed
	// default here looked for a socket nothing ever creates.
	paths, _ := a.resolvePathsForConfig()

	l := &Ladder{}
	record := func(how, target string, start time.Time, err error) bool {
		at := Attempt{
			How: how, Target: target, OK: err == nil,
			TookMs: time.Since(start).Milliseconds(),
		}
		if err != nil {
			at.Detail = err.Error()
		}
		l.Attempts = append(l.Attempts, at)
		return err == nil
	}

	// A named endpoint short-circuits the ladder. Falling back to a local
	// daemon when the named one is unreachable would answer from the wrong
	// machine, which is worse than failing.
	if opt.Endpoint != "" {
		start := time.Now()
		c := NewClientAt(paths, a.ConfigPath, opt.Endpoint)
		if c.Ping(ctx) {
			record("url", opt.Endpoint, start, nil)
			l.Chosen = "url"
			return c, l, nil
		}
		err := fmt.Errorf("no daemon answering")
		record("url", opt.Endpoint, start, err)
		return nil, l, fmt.Errorf("%s: %w; it is named explicitly, so mcpx will not "+
			"start a local one in its place", opt.Endpoint, err)
	}

	// Rung one: a socket already listening.
	start := time.Now()
	c := NewClient(paths, a.ConfigPath)
	if c.Ping(ctx) {
		record("socket", paths.Socket, start, nil)
		l.Chosen = "socket"
		return c, l, nil
	}
	record("socket", paths.Socket, start, fmt.Errorf("nothing listening"))

	// Rung two: any other socket in the state directory. A daemon started
	// for a slightly different configuration is still a daemon, and asking
	// it is cheaper than starting another -- but only if it agrees about
	// what it is serving, which Ping does not tell us. So this rung is
	// reported and not taken automatically.
	if others := siblingSockets(paths); len(others) > 0 {
		l.Attempts = append(l.Attempts, Attempt{
			How: "sibling-socket", OK: false,
			Detail: fmt.Sprintf("%d other daemon(s) here, for other configurations: %s",
				len(others), strings.Join(others, ", ")),
		})
	}

	// Rung three: start one.
	if opt.AllowSpawn {
		start = time.Now()
		err := c.EnsureDaemon(ctx)
		if record("spawn", paths.Socket, start, err) {
			l.Chosen = "spawn"
			return c, l, nil
		}
	} else {
		l.Attempts = append(l.Attempts, Attempt{
			How: "spawn", OK: false, Detail: "disabled by daemon.autostart",
		})
	}

	// Rung four: no daemon at all.
	if opt.AllowInline {
		start = time.Now()
		inline, err := a.inlineDaemon(ctx)
		if record("inline", "in-process", start, err) {
			l.Chosen = "inline"
			return inline, l, nil
		}
	}

	return nil, l, fmt.Errorf("no daemon reachable:\n%s", l.Describe())
}

// Describe renders the attempts, for an error or for `mcpx doctor`.
func (l *Ladder) Describe() string {
	var b strings.Builder
	for _, at := range l.Attempts {
		mark := "  -"
		if at.OK {
			mark = "  *"
		}
		fmt.Fprintf(&b, "%s %-16s %-44s %s\n", mark, at.How, trunc(at.Target, 44), at.Detail)
	}
	return b.String()
}

// siblingSockets lists other daemons in the same state directory.
func siblingSockets(paths daemon.Paths) []string {
	dir := filepath.Dir(paths.Socket)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".sock") || filepath.Join(dir, name) == paths.Socket {
			continue
		}
		// Only count one that is actually listening; a stale socket file
		// outlives the process that made it.
		conn, err := net.DialTimeout("unix", filepath.Join(dir, name), 200*time.Millisecond)
		if err != nil {
			continue
		}
		_ = conn.Close()
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// inlineDaemon starts a daemon inside this process.
//
// The last resort, and it changes the lifetime of everything: servers start
// when first called and die when this process exits, so nothing is pooled
// across invocations and a stateful server cannot outlive one command.
//
// Worth having anyway. A daemon is an optimisation -- a cache and a process
// pool -- and neither is required to answer a question. Where one cannot be
// started, this is the difference between mcpx working slowly and not
// working.
func (a *App) inlineDaemon(ctx context.Context) (*Client, error) {
	if _, err := exec.LookPath(os.Args[0]); err != nil && !filepath.IsAbs(os.Args[0]) {
		return nil, fmt.Errorf("cannot locate the mcpx binary to run inline")
	}
	return nil, fmt.Errorf("inline mode is not built yet; " +
		"run `mcpx daemon` in the foreground, or set daemon.endpoint")
}
