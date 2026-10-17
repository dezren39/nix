package daemon

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

// A takeover hands the daemon's open keep-alive connections to its successor
// along with the listeners. A client holding one of them then sends its next
// request into a socket that the successor is already reading, instead of into
// one this process has closed.
//
// The rule that makes that safe: a connection is handed on only when no byte has
// been read from it since this process last wrote to it, and once it is frozen
// no read reaches the socket from here again. Every read and write goes through
// handoffConn, so that is known. The bytes still in the kernel stay there for
// the successor.
//
// Why "since the last write" and not "since the last request": net/http reads
// one byte from the socket in the background while a handler runs, to notice a
// client that has gone. It flushes the response before it stops that read, so a
// client that is quick to send its next request can have its first byte read
// into the server's buffer after the answer has gone out. That byte is held by
// the server, invisible from here, and a client cannot send anything in reply
// before it has the answer. So a byte read after the last write is treated as
// held. A byte read before it has been consumed, since the server only wrote
// once it had finished reading the request.
//
// A client that pipelines, sending a request before it has read the answer to
// the last, can have a whole second request buffered inside the server before
// any write. That is not covered, and is reported.

// errFrozen is what a read of a frozen connection returns. net/http treats a
// timeout on a read between requests as the client going away and closes
// quietly. Any other error would make it write a 400 onto a socket that now
// belongs to the successor.
var errFrozen = os.ErrDeadlineExceeded

// pastDeadline wakes a read that is blocked on the socket.
var pastDeadline = time.Unix(1, 0)

// handoffConn is a connection the daemon serves.
type handoffConn struct {
	net.Conn

	// passed marks a connection that came from a predecessor. It is treated
	// as idle from the moment it is accepted.
	passed bool

	mu      sync.Mutex
	cond    *sync.Cond
	reading bool // a Read on Conn is in progress
	wrote   uint64
	readAt  uint64 // the value of wrote when the last read that returned data finished
	gotData bool   // a read has returned data
	frozen  bool   // this process may not read again; the socket is being handed on
	woken   bool   // a read deadline was set to wake a blocked read
	closed  bool   // the socket itself is closed
}

// held reports whether a byte may be in the server's hands: one was read and
// nothing has been written since. See the note at the top of this file.
func (c *handoffConn) held() bool {
	return c.gotData && c.readAt == c.wrote
}

func newHandoffConn(c net.Conn, passed bool) *handoffConn {
	hc := &handoffConn{Conn: c, passed: passed}
	hc.cond = sync.NewCond(&hc.mu)
	return hc
}

func (c *handoffConn) Read(b []byte) (int, error) {
	c.mu.Lock()
	if c.frozen {
		c.mu.Unlock()
		return 0, errFrozen
	}
	c.reading = true
	c.mu.Unlock()

	n, err := c.Conn.Read(b)

	c.mu.Lock()
	c.reading = false
	if n > 0 {
		c.gotData = true
		c.readAt = c.wrote
	}
	c.cond.Broadcast()
	c.mu.Unlock()
	return n, err
}

// Write counts the write before it is made, so that the count is raised before
// the client can have received anything it could answer.
func (c *handoffConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	c.wrote++
	c.mu.Unlock()
	return c.Conn.Write(b)
}

// Close is what the HTTP server calls when a connection's serve loop ends. A
// frozen connection is not closed here: the handover owns the socket until it
// is released or thawed.
func (c *handoffConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.frozen {
		return nil
	}
	c.closed = true
	return c.Conn.Close()
}

// CloseWrite passes through so that net/http's lingering close still sends a
// FIN on the socket it would have sent on before the wrapper existed.
func (c *handoffConn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}

// file returns a duplicate of the socket, close-on-exec, for sending.
func (c *handoffConn) file() (*os.File, error) {
	f, ok := c.Conn.(interface{ File() (*os.File, error) })
	if !ok {
		return nil, errors.New("connection has no descriptor")
	}
	return f.File()
}

// freeze stops this process from reading the connection. It reports whether the
// connection is now frozen. Without wake, the connection must not be in a read,
// which is the case when it is called from the serve loop itself. With wake, a
// blocked read is interrupted and freeze waits for it to return, so that a
// read which did get bytes is seen before the decision is made.
func (c *handoffConn) freeze(wake bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.frozen {
		return true
	}
	if c.held() || c.closed {
		return false
	}
	c.frozen = true
	if !c.reading {
		return true
	}
	if !wake {
		c.frozen = false
		return false
	}
	_ = c.Conn.SetReadDeadline(pastDeadline)
	c.woken = true
	for c.reading {
		c.cond.Wait()
	}
	if c.held() {
		c.frozen = false
		c.unwakeLocked()
		return false
	}
	return true
}

func (c *handoffConn) isFrozen() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.frozen
}

// thaw gives a frozen connection back to the process, for a handover that did
// not complete. Its serve loop must have returned first.
func (c *handoffConn) thaw() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.frozen = false
	c.gotData = false
	c.unwakeLocked()
}

func (c *handoffConn) unwakeLocked() {
	if c.woken {
		_ = c.Conn.SetReadDeadline(time.Time{})
		c.woken = false
	}
}

// release closes this process's copy of a connection that has been handed on.
// The socket stays open for the successor, which holds its own descriptor.
func (c *handoffConn) release() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	_ = c.Conn.Close()
}

// observeConn is the http.Server ConnState hook for the daemon's connections.
func (s *Server) observeConn(c net.Conn, st http.ConnState) {
	if hc, ok := c.(*handoffConn); ok {
		s.conns.observe(hc, st)
	}
}

// connTracker knows every connection this process is serving, and which of
// them have been frozen for a handover.
type connTracker struct {
	mu          sync.Mutex
	open        map[*handoffConn]http.ConnState
	handingOver bool
	passing     map[*handoffConn]bool
}

func (t *connTracker) add(c *handoffConn, st http.ConnState) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.open == nil {
		t.open = map[*handoffConn]http.ConnState{}
	}
	t.open[c] = st
}

// observe is the http.Server ConnState hook. An idle connection is frozen at
// once when a handover is under way: this goroutine is the only reader, so
// nothing can be in flight, and the next read it attempts fails.
func (t *connTracker) observe(c *handoffConn, st http.ConnState) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.open == nil {
		t.open = map[*handoffConn]http.ConnState{}
	}
	switch st {
	case http.StateClosed, http.StateHijacked:
		delete(t.open, c)
		return
	case http.StateNew:
		if c.passed {
			st = http.StateIdle
		}
	case http.StateIdle:
		if t.handingOver && c.freeze(false) {
			t.markPassingLocked(c)
		}
	}
	t.open[c] = st
}

func (t *connTracker) markPassingLocked(c *handoffConn) {
	if t.passing == nil {
		t.passing = map[*handoffConn]bool{}
	}
	t.passing[c] = true
}

func (t *connTracker) setHandingOver(on bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.handingOver = on
}

// sweep freezes every connection that is not in a request, and returns how
// many connections are still open. A connection that is open after a sweep is
// either in a request, which will be frozen when it goes idle, or still
// finishing a frozen request. Zero means nothing here can read a socket.
func (t *connTracker) sweep() int {
	t.mu.Lock()
	var cands []*handoffConn
	for c, st := range t.open {
		if st != http.StateActive && !c.isFrozen() {
			cands = append(cands, c)
		}
	}
	n := len(t.open)
	t.mu.Unlock()

	for _, c := range cands {
		if c.freeze(true) {
			t.mu.Lock()
			t.markPassingLocked(c)
			t.mu.Unlock()
		}
	}
	return n
}

// takePassing returns the frozen connections and forgets them.
func (t *connTracker) takePassing() []*handoffConn {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]*handoffConn, 0, len(t.passing))
	for c := range t.passing {
		out = append(out, c)
	}
	t.passing = nil
	return out
}

// waitGone waits until the serve loops of the connections have returned, and
// gives back the ones that are then free to be served again. A connection whose
// loop is still running at the deadline is closed instead: it must not be
// served by two loops.
func (t *connTracker) waitGone(ctx context.Context, poll time.Duration, list []*handoffConn) []*handoffConn {
	tick := time.NewTicker(poll)
	defer tick.Stop()
	for {
		t.mu.Lock()
		var still []*handoffConn
		for _, c := range list {
			if _, ok := t.open[c]; ok {
				still = append(still, c)
			}
		}
		t.mu.Unlock()
		if len(still) == 0 {
			return list
		}
		select {
		case <-ctx.Done():
			gone := map[*handoffConn]bool{}
			for _, c := range still {
				gone[c] = true
				c.release()
			}
			var back []*handoffConn
			for _, c := range list {
				if !gone[c] {
					back = append(back, c)
				}
			}
			return back
		case <-tick.C:
		}
	}
}

// splitPassed makes the connections a predecessor passed into the queues of the
// listeners they belong to, unix and tcp. Each connection goes to exactly one
// queue. Anything of another kind is closed.
func splitPassed(conns []net.Conn) (unixQ, tcpQ []*handoffConn) {
	for _, c := range conns {
		switch c.LocalAddr().Network() {
		case "unix":
			unixQ = append(unixQ, newHandoffConn(c, true))
		case "tcp":
			tcpQ = append(tcpQ, newHandoffConn(c, true))
		default:
			_ = c.Close()
		}
	}
	return unixQ, tcpQ
}

// connListener is the listener a server accepts from. Connections it has been
// given before it starts, from a predecessor or a restored handover, are
// yielded before any new one is accepted.
type connListener struct {
	inner   net.Listener
	tracker *connTracker

	mu      sync.Mutex
	stopped bool
	queue   []*handoffConn

	// acceptMu is held across an inner Accept and the tracking of what it
	// returns, so that stop can wait until no connection is in between.
	acceptMu sync.Mutex
}

func newConnListener(inner net.Listener, t *connTracker, queued []*handoffConn) *connListener {
	return &connListener{inner: inner, tracker: t, queue: queued}
}

func (l *connListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	if len(l.queue) > 0 {
		c := l.queue[0]
		l.queue = l.queue[1:]
		l.mu.Unlock()
		l.tracker.add(c, http.StateIdle)
		return c, nil
	}
	l.mu.Unlock()

	l.acceptMu.Lock()
	defer l.acceptMu.Unlock()
	if l.isStopped() {
		return nil, http.ErrServerClosed
	}
	c, err := l.inner.Accept()
	if err != nil {
		if l.isStopped() {
			return nil, http.ErrServerClosed
		}
		return nil, err
	}
	hc := newHandoffConn(c, false)
	l.tracker.add(hc, http.StateNew)
	return hc, nil
}

func (l *connListener) isStopped() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.stopped
}

// stop makes Accept return http.ErrServerClosed, so that the serve loop ends
// without reporting an error. When it returns, no connection is being accepted
// and none will be from here.
func (l *connListener) stop() {
	l.mu.Lock()
	l.stopped = true
	l.mu.Unlock()
	_ = l.inner.Close()
	l.acceptMu.Lock()
	l.acceptMu.Unlock()
}

func (l *connListener) Close() error {
	l.stop()
	return nil
}

func (l *connListener) Addr() net.Addr {
	return l.inner.Addr()
}

// file duplicates the listening socket, for handing on or for putting the
// listener back if a handover does not complete.
func (l *connListener) file() (*os.File, error) {
	f, ok := l.inner.(interface{ File() (*os.File, error) })
	if !ok {
		return nil, errors.New("listener has no descriptor")
	}
	return f.File()
}

// keepSocketFile stops the listener removing the socket file when it closes,
// because the successor is serving on the same socket.
func (l *connListener) keepSocketFile() {
	if u, ok := l.inner.(*net.UnixListener); ok {
		u.SetUnlinkOnClose(false)
	}
}
