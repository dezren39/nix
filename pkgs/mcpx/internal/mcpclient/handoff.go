package mcpclient

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/dezren39/mcpx/internal/defaults"
)

// StdioHandoff is a stdio child's identity and pipes, enough for another
// process to take the child over without restarting it.
type StdioHandoff struct {
	PID int `json:"pid"`
	// StartTime is the child's start time from /proc. With the pid it names
	// one process, so a recycled pid is not mistaken for the child.
	StartTime uint64 `json:"startTime"`
	Label     string `json:"label"`
	// Stdin, Stdout and Stderr are this process's ends of the child's pipes.
	// They travel as file descriptors, not in the JSON.
	Stdin, Stdout, Stderr *os.File `json:"-"`
	// Unread is output read from the child but not yet framed.
	Unread []byte `json:"unread,omitempty"`
}

// Session is what a client learned during its handshake. A successor resumes
// the session with it rather than handshaking again, which would be a second
// session as far as the child is concerned.
type Session struct {
	Era            Era                        `json:"era"`
	Negotiated     string                     `json:"negotiated,omitempty"`
	ServerInfo     ServerInfo                 `json:"serverInfo"`
	Capabilities   map[string]json.RawMessage `json:"capabilities,omitempty"`
	Instructions   string                     `json:"instructions,omitempty"`
	Source         string                     `json:"source,omitempty"`
	CachedEraWrong bool                       `json:"cachedEraWrong,omitempty"`
	MetaVersion    string                     `json:"metaVersion,omitempty"`
	LogLevel       string                     `json:"logLevel,omitempty"`
}

// Handoff is a stdio session in transit between processes.
type Handoff struct {
	Stdio   *StdioHandoff `json:"stdio"`
	Session Session       `json:"session"`
}

// adoptPollInterval is how often an adopted child's liveness is checked. The
// successor is not its parent, so it cannot wait for the child to exit.
const adoptPollInterval = 250 * time.Millisecond

// aLongTimeAgo is a read deadline that has already passed, which interrupts
// a read in progress on a poller-backed pipe.
var aLongTimeAgo = time.Unix(1, 0)

// Detach stops this process reading and writing the child's pipes, so another
// process can take them over. It signals nothing and closes nothing: the
// child keeps running with its stdin open. On error the transport is left
// as it was. On success it stays detached until Reattach.
func (t *StdioTransport) Detach() (*StdioHandoff, error) {
	start, err := ProcessStartTime(t.pid)
	if err != nil {
		return nil, fmt.Errorf("child %d: %w", t.pid, err)
	}
	t.writeMu.Lock()
	if t.closed.Load() || t.detached {
		t.writeMu.Unlock()
		return nil, errors.New("stdio transport is not attached")
	}
	t.detached = true
	t.writeMu.Unlock()

	_ = t.outR.SetReadDeadline(aLongTimeAgo)
	_ = t.errR.SetReadDeadline(aLongTimeAgo)
	t.readMu.Lock()
	t.readMu.Unlock()
	<-t.stderrDone

	buffered, _ := t.stdout.Peek(t.stdout.Buffered())
	unread := append(append([]byte(nil), t.pending...), buffered...)
	return &StdioHandoff{
		PID:       t.pid,
		StartTime: start,
		Label:     t.label,
		Stdin:     t.stdin,
		Stdout:    t.outR,
		Stderr:    t.errR,
		Unread:    unread,
	}, nil
}

// Reattach resumes reading after a Detach that was not handed on.
func (t *StdioTransport) Reattach() {
	t.writeMu.Lock()
	t.detached = false
	t.writeMu.Unlock()
	_ = t.outR.SetReadDeadline(time.Time{})
	_ = t.errR.SetReadDeadline(time.Time{})
	t.startStderr()
}

// AdoptStdio takes over a child a predecessor process detached. It fails if
// the child is no longer running, so a dead child is never resumed.
// ErrChildGone is returned by AdoptStdio when the child has exited, so a
// successor can drop it instead of failing the whole handoff over it.
var ErrChildGone = errors.New("child is not running")

func AdoptStdio(h StdioHandoff) (*StdioTransport, error) {
	if !childAlive(h.PID, h.StartTime) {
		return nil, fmt.Errorf("child %d (%s): %w", h.PID, h.Label, ErrChildGone)
	}
	_ = h.Stdout.SetReadDeadline(time.Time{})
	_ = h.Stderr.SetReadDeadline(time.Time{})
	t := &StdioTransport{
		pid:        h.PID,
		stdin:      h.Stdin,
		stdout:     bufio.NewReaderSize(h.Stdout, 1<<20),
		outR:       h.Stdout,
		errR:       h.Stderr,
		pending:    h.Unread,
		label:      h.Label,
		stderr:     newRingBuffer(64 << 10),
		exited:     make(chan struct{}),
		stderrDone: make(chan struct{}),
		stdinGrace: defaults.StdioStdinGrace,
		termGrace:  defaults.StdioTermGrace,
	}
	t.startStderr()
	go t.watchAdopted(h.StartTime)
	return t, nil
}

// watchAdopted closes exited once the child is gone, which Close and the
// reader both rely on in place of cmd.Wait.
func (t *StdioTransport) watchAdopted(start uint64) {
	tick := time.NewTicker(adoptPollInterval)
	defer tick.Stop()
	for range tick.C {
		if !childAlive(t.pid, start) {
			t.waitErr = errors.New("process ended")
			close(t.exited)
			return
		}
	}
}

// childAlive reports whether pid is still the process that started at start.
// A zombie has ended, though its pid still answers signal 0.
func childAlive(pid int, start uint64) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	st, state, err := procStat(pid)
	if err != nil {
		return false
	}
	return st == start && state != 'Z'
}

// ProcessStartTime is the start time of pid, in clock ticks since boot.
func ProcessStartTime(pid int) (uint64, error) {
	st, _, err := procStat(pid)
	return st, err
}

// procStat reads a process's start time and state from /proc/<pid>/stat.
func procStat(pid int) (uint64, byte, error) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, 0, err
	}
	// The command name is parenthesised and may contain spaces or parens,
	// so fields are counted from the last ')'.
	i := bytes.LastIndexByte(raw, ')')
	if i < 0 {
		return 0, 0, fmt.Errorf("unparseable /proc/%d/stat", pid)
	}
	fields := strings.Fields(string(raw[i+1:]))
	// fields[0] is field 3 (state); starttime is field 22.
	if len(fields) < 20 || len(fields[0]) != 1 {
		return 0, 0, fmt.Errorf("unparseable /proc/%d/stat", pid)
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("unparseable /proc/%d/stat: %w", pid, err)
	}
	return start, fields[0][0], nil
}

// Detach stops the read loop and gives the transport to the caller. Nothing
// pending is failed, so the session is intact for whoever resumes it.
func (c *Client) Detach() (*Handoff, error) {
	t, ok := c.t.(*StdioTransport)
	if !ok {
		return nil, errors.New("only a stdio session can be handed off")
	}
	c.detaching.Store(true)
	stdio, err := t.Detach()
	if err != nil {
		c.detaching.Store(false)
		return nil, err
	}
	<-c.loopDone
	return &Handoff{Stdio: stdio, Session: c.session()}, nil
}

// Reattach restarts the read loop after a Detach that was not handed on. The
// listen stream, if any, never stopped: it was waiting on the loop's answer.
func (c *Client) Reattach() {
	c.t.(*StdioTransport).Reattach()
	c.detaching.Store(false)
	c.loopDone = make(chan struct{})
	go c.recvLoop()
}

// Release answers what is in flight and marks the client closed without
// touching the transport, for the process that handed the child on.
func (c *Client) Release() {
	c.fail(errors.New("session handed to a successor process"))
}

// Abandon is Release for an HTTP session: the upstream session is left open
// for the successor, so the transport must not send its DELETE.
func (c *Client) Abandon() {
	c.fail(errors.New("session handed to a successor process"))
	if t, ok := c.t.(*HTTPTransport); ok {
		t.Abandon()
	}
}

// HTTPSession is the upstream session this client holds, if it is streamable HTTP.
func (c *Client) HTTPSession() (HTTPHandoff, bool) {
	t, ok := c.t.(*HTTPTransport)
	if !ok {
		return HTTPHandoff{}, false
	}
	return t.Handoff(), true
}

// SessionState is the protocol state a resumed client needs, without a handshake.
func (c *Client) SessionState() Session { return c.session() }

func (c *Client) session() Session {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Session{
		Era:            c.Era,
		Negotiated:     c.Negotiated,
		ServerInfo:     c.ServerInfo,
		Capabilities:   c.Capabilities,
		Instructions:   c.Instructions,
		Source:         c.Source,
		CachedEraWrong: c.CachedEraWrong,
		MetaVersion:    c.metaVersion,
		LogLevel:       c.logLevel,
	}
}

// Resume wraps a transport whose session a predecessor established, without
// a handshake. o carries the same hooks a new session would be given. Nothing
// reads the transport until Activate, so a successor that fails before it
// commits can hand the pipes back with no bytes consumed.
func Resume(t Transport, s Session, o Options) *Client {
	c := &Client{
		t:              t,
		pending:        map[int64]chan *rpcResponse{},
		closeCh:        make(chan struct{}),
		loopDone:       make(chan struct{}),
		clientName:     o.ClientName,
		clientVersion:  o.ClientVersion,
		onElicit:       o.OnServerRequest,
		roots:          append([]Root(nil), o.Roots...),
		modernVersions: o.ModernVersions,
		probeTimeout:   o.ProbeTimeout,
		ServerInfo:     s.ServerInfo,
		Capabilities:   s.Capabilities,
		Era:            s.Era,
		Negotiated:     s.Negotiated,
		Instructions:   s.Instructions,
		Source:         s.Source,
		CachedEraWrong: s.CachedEraWrong,
		metaVersion:    s.MetaVersion,
		logLevel:       s.LogLevel,
	}
	if len(c.modernVersions) == 0 {
		c.modernVersions = ModernVersions
	}
	if c.probeTimeout <= 0 {
		c.probeTimeout = defaults.UpstreamProbeTimeout
	}
	c.modern.Store(s.Era == EraModern)
	return c
}

// Activate starts the read loop of a resumed session, and its listen stream
// when the session is modern. Call it once, after the handoff has committed.
func (c *Client) Activate() {
	go c.recvLoop()
	if c.modern.Load() {
		c.startListen()
	}
}
