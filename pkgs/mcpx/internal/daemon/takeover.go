package daemon

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/dezren39/mcpx/internal/defaults"
	"github.com/dezren39/mcpx/internal/pool"
)

// A takeover moves the running daemon's listening sockets and its stdio
// children to a successor process, so neither is closed on the way. The old
// daemon sends each file descriptor in its own frame and waits for an ack, so
// no two descriptors can arrive in one read.
//
// The old daemon changes nothing it cannot put back until the successor
// commits. The commit is the single reply that finishes the handoff, and after
// it the old daemon waits for the successor to hang up before it exits.

const (
	takeoverRoute     = "/v1/takeover"
	takeoverMediaType = "application/x-mcpx-takeover"
	takeoverVersion   = 1
)

const (
	kindHello    = "hello"
	kindListener = "listener"
	kindState    = "state"
	kindPools    = "pools"
	kindPipe     = "pipe"
	kindConn     = "conn"
	kindEnd      = "end"
	kindAck      = "ack"
	kindNack     = "nack"
	kindCommit   = "commit"
)

type takeoverFrame struct {
	Kind  string          `json:"kind"`
	Body  json.RawMessage `json:"body,omitempty"`
	Error string          `json:"error,omitempty"`
}

type takeoverHello struct {
	Version int `json:"version"`
	PID     int `json:"pid"`
}

type takeoverListener struct {
	Network string `json:"network"`
}

type takeoverState struct {
	Endpoint string `json:"endpoint"`
	Started  string `json:"started"`
	Version  string `json:"version"`
}

type takeoverPools struct {
	Pools  []pool.PoolHandoff     `json:"pools"`
	Leases map[string]leaseRecord `json:"leases,omitempty"`
}

type takeoverPipe struct {
	Pool     int    `json:"pool"`
	Instance int    `json:"instance"`
	Slot     string `json:"slot"`
}

func sendFrame(c *net.UnixConn, f takeoverFrame, fd *os.File) error {
	payload, err := json.Marshal(f)
	if err != nil {
		return err
	}
	msg := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(msg, uint32(len(payload)))
	copy(msg[4:], payload)
	var oob []byte
	if fd != nil {
		rc, err := fd.SyscallConn()
		if err != nil {
			return err
		}
		if err := rc.Control(func(s uintptr) { oob = syscall.UnixRights(int(s)) }); err != nil {
			return err
		}
	}
	n, _, err := c.WriteMsgUnix(msg, oob, nil)
	if err != nil {
		return err
	}
	if n < len(msg) {
		_, err = c.Write(msg[n:])
	}
	return err
}

func send(c *net.UnixConn, kind string, body any, fd *os.File) error {
	f := takeoverFrame{Kind: kind}
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		f.Body = raw
	}
	return sendFrame(c, f, fd)
}

// exchange sends a frame and waits for the ack that lets the next one go.
func exchange(c *net.UnixConn, kind string, body any, fd *os.File) error {
	if err := send(c, kind, body, fd); err != nil {
		return err
	}
	reply, extra, err := recvFrame(c)
	closeFile(extra)
	if err != nil {
		return err
	}
	if reply.Kind == kindNack {
		return fmt.Errorf("%s refused: %s", kind, reply.Error)
	}
	if reply.Kind != kindAck {
		return fmt.Errorf("%s: unexpected reply %q", kind, reply.Kind)
	}
	return nil
}

func recvFrame(c *net.UnixConn) (takeoverFrame, *os.File, error) {
	var f takeoverFrame
	var hdr [4]byte
	oob := make([]byte, syscall.CmsgSpace(4))
	n, oobn, _, _, err := c.ReadMsgUnix(hdr[:], oob)
	if err != nil {
		return f, nil, err
	}
	fd := passedFile(oob[:oobn])
	if _, err := io.ReadFull(c, hdr[n:]); err != nil {
		closeFile(fd)
		return f, nil, err
	}
	size := binary.BigEndian.Uint32(hdr[:])
	if int64(size) > defaults.TakeoverMaxFrame {
		closeFile(fd)
		return f, nil, fmt.Errorf("takeover frame of %d bytes exceeds the limit", size)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(c, payload); err != nil {
		closeFile(fd)
		return f, nil, err
	}
	if err := json.Unmarshal(payload, &f); err != nil {
		closeFile(fd)
		return f, nil, err
	}
	return f, fd, nil
}

func passedFile(oob []byte) *os.File {
	if len(oob) == 0 {
		return nil
	}
	msgs, err := syscall.ParseSocketControlMessage(oob)
	if err != nil {
		return nil
	}
	var out *os.File
	for i := range msgs {
		fds, err := syscall.ParseUnixRights(&msgs[i])
		if err != nil {
			continue
		}
		for _, fd := range fds {
			if out == nil {
				out = os.NewFile(uintptr(fd), "mcpx-handoff")
			} else {
				_ = syscall.Close(fd)
			}
		}
	}
	return out
}

func closeFile(f *os.File) {
	if f != nil {
		_ = f.Close()
	}
}

// handleTakeover answers a successor's request. The connection leaves HTTP at
// once, because the descriptors it carries cannot pass through net/http.
func (s *Server) handleTakeover(w http.ResponseWriter, r *http.Request) {
	if !s.takeoverMu.TryLock() {
		http.Error(w, "a takeover is already under way", http.StatusConflict)
		return
	}
	defer s.takeoverMu.Unlock()
	if u := s.upgrading.Load(); u != nil && !u.claim() {
		http.Error(w, "the upgrade that started this successor gave up on it", http.StatusGone)
		return
	}
	conn, rw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if hc, ok := conn.(*handoffConn); ok {
		conn = hc.Conn
	}
	defer conn.Close()
	uc, ok := conn.(*net.UnixConn)
	if !ok || rw.Reader.Buffered() > 0 {
		return
	}
	if _, err := io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Type: "+takeoverMediaType+"\r\nContent-Length: 0\r\n\r\n"); err != nil {
		return
	}
	s.logger.Printf("takeover requested by a successor")
	if err := s.handOver(uc); err != nil {
		s.logger.Printf("takeover not completed, still serving: %v", err)
		s.settleUpgrade(err)
	}
}

// handOver is the old side of a takeover. It returns nil only after the
// successor has committed and hung up.
func (s *Server) handOver(c *net.UnixConn) error {
	// A successor that stalls before committing must not hold the children
	// and the listeners indefinitely; the deadline turns that into a restore.
	_ = c.SetDeadline(time.Now().Add(s.set.Duration("daemon.takeoverTimeout")))
	hello, extra, err := recvFrame(c)
	closeFile(extra)
	if err != nil {
		return err
	}
	var h takeoverHello
	if hello.Kind != kindHello || json.Unmarshal(hello.Body, &h) != nil || h.Version != takeoverVersion {
		_ = sendFrame(c, takeoverFrame{Kind: kindNack, Error: "unsupported takeover protocol"}, nil)
		return fmt.Errorf("successor speaks an unsupported takeover protocol")
	}
	ul, ok1 := s.unixLn.(*connListener)
	tl, ok2 := s.tcpLn.(*connListener)
	if !ok1 || !ok2 {
		return errors.New("the daemon's listeners are not sockets")
	}
	unixDup, err := ul.file()
	if err != nil {
		return err
	}
	tcpDup, err := tl.file()
	if err != nil {
		unixDup.Close()
		return err
	}
	// The successor holds its own copies once it has them, and the duplicates
	// are only needed here for putting the listeners back.
	defer unixDup.Close()
	defer tcpDup.Close()

	if err := exchange(c, kindListener, takeoverListener{Network: "unix"}, unixDup); err != nil {
		return err
	}
	if err := exchange(c, kindListener, takeoverListener{Network: "tcp"}, tcpDup); err != nil {
		return err
	}
	state := takeoverState{Endpoint: s.endpoint, Started: s.started.Format(time.RFC3339Nano), Version: s.version}
	if err := exchange(c, kindState, state, nil); err != nil {
		return err
	}

	// From here the listeners are the successor's too. This process stops
	// accepting on them, so the socket file must outlive it.
	ul.keepSocketFile()
	if err := s.reg.SaveCache(); err != nil {
		s.logger.Printf("takeover: save cache: %v", err)
	}
	passing, err := s.passConns(ul, tl, s.set.Duration("http.shutdownGrace"))
	if err != nil {
		s.logger.Printf("takeover: draining http: %v", err)
		_ = s.restoreHTTP(unixDup, tcpDup, passing)
		return fmt.Errorf("draining http: %w", err)
	}
	_ = c.SetDeadline(time.Now().Add(s.set.Duration("daemon.takeoverTimeout")))

	restore := func(ho *RegistryHandoff) error {
		if ho != nil {
			ho.Restore()
		}
		return s.restoreHTTP(unixDup, tcpDup, passing)
	}
	if err := s.sendConns(c, passing); err != nil {
		_ = restore(nil)
		return err
	}
	ho, err := s.reg.Detach(s.set.Duration("http.shutdownGrace"))
	if err != nil {
		_ = restore(nil)
		return err
	}
	if err := s.sendChildren(c, ho); err != nil {
		_ = restore(ho)
		return err
	}
	if err := send(c, kindEnd, nil, nil); err != nil {
		_ = restore(ho)
		return err
	}
	reply, extra, err := recvFrame(c)
	closeFile(extra)
	if err != nil {
		_ = restore(ho)
		return err
	}
	if reply.Kind != kindCommit {
		_ = restore(ho)
		return fmt.Errorf("successor refused: %s", reply.Error)
	}

	ho.Commit()
	s.committed.Store(true)
	s.settleUpgrade(nil)
	// The successor has every connection and its own copy of each socket.
	s.releasePassed(passing)
	s.logger.Printf("takeover committed; handing over to the successor")
	_ = c.SetReadDeadline(time.Now().Add(s.set.Duration("daemon.takeoverTimeout")))
	_, _ = io.Copy(io.Discard, c)
	s.handedOffOnce.Do(func() { close(s.handedOff) })
	return nil
}

// passConns stops this process accepting connections and gathers every
// connection it holds into the set the successor takes. The listeners stay open
// in the successor's hands, so a call that arrives now waits in the socket's
// backlog and the successor serves it.
//
// A connection that is in a request is left to finish here, and it is frozen
// when it goes idle, so a client that reuses it finds the successor reading it.
// The set is complete when this returns: no connection stays open here, which
// means nothing in this process can read or answer on one after the successor
// commits. On the grace period running out it returns what it had frozen, so
// that the caller can put it back.
func (s *Server) passConns(ul, tl *connListener, grace time.Duration) ([]*handoffConn, error) {
	s.conns.setHandingOver(true)
	ul.stop()
	tl.stop()
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	tick := time.NewTicker(defaults.TakeoverPoll)
	defer tick.Stop()
	for {
		if s.conns.sweep() == 0 {
			return s.conns.takePassing(), nil
		}
		select {
		case <-ctx.Done():
			return s.conns.takePassing(), fmt.Errorf("connections still open after %s", grace)
		case <-tick.C:
		}
	}
}

func (s *Server) sendChildren(c *net.UnixConn, ho *RegistryHandoff) error {
	if err := exchange(c, kindPools, takeoverPools{Pools: ho.Pools, Leases: s.reg.snapshotLeases()}, nil); err != nil {
		return err
	}
	for pi := range ho.Pools {
		for ii := range ho.Pools[pi].Instances {
			st := ho.Pools[pi].Instances[ii].Stdio
			if st == nil {
				continue
			}
			pipes := []struct {
				slot string
				f    *os.File
			}{{"stdin", st.Stdin}, {"stdout", st.Stdout}, {"stderr", st.Stderr}}
			for _, p := range pipes {
				if err := exchange(c, kindPipe, takeoverPipe{Pool: pi, Instance: ii, Slot: p.slot}, p.f); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// sendConns passes each frozen connection to the successor, one descriptor per
// frame, so that the successor can attach each to its own listener.
func (s *Server) sendConns(c *net.UnixConn, passing []*handoffConn) error {
	for _, hc := range passing {
		f, err := hc.file()
		if err != nil {
			return err
		}
		err = exchange(c, kindConn, nil, f)
		closeFile(f)
		if err != nil {
			return err
		}
	}
	return nil
}

// releasePassed closes this process's copies of connections the successor now
// holds. Each socket stays open for the successor.
func (s *Server) releasePassed(passing []*handoffConn) {
	for _, hc := range passing {
		hc.release()
	}
}

// restoreHTTP serves again after a handover that did not complete. Listeners
// are put back from the duplicates kept for that purpose, and every frozen
// connection is given back to the new server, which reads it again.
func (s *Server) restoreHTTP(unixF, tcpF *os.File, passing []*handoffConn) error {
	s.conns.setHandingOver(false)
	// A connection can only have been frozen while the handover was on, so
	// this collects everything that was.
	passing = append(passing, s.conns.takePassing()...)
	ctx, cancel := context.WithTimeout(context.Background(), s.set.Duration("http.shutdownGrace"))
	defer cancel()
	back := s.conns.waitGone(ctx, defaults.TakeoverPoll, passing)
	for _, hc := range back {
		hc.thaw()
	}
	ul, err := net.FileListener(unixF)
	if err != nil {
		s.logger.Printf("takeover: restoring the unix listener: %v", err)
		s.releasePassed(back)
		return err
	}
	tl, err := net.FileListener(tcpF)
	if err != nil {
		s.logger.Printf("takeover: restoring the tcp listener: %v", err)
		ul.Close()
		s.releasePassed(back)
		return err
	}
	var unixQ, tcpQ []*handoffConn
	for _, hc := range back {
		if hc.LocalAddr().Network() == "unix" {
			unixQ = append(unixQ, hc)
		} else {
			tcpQ = append(tcpQ, hc)
		}
	}
	s.unixLn = newConnListener(ul, &s.conns, unixQ)
	s.tcpLn = newConnListener(tl, &s.conns, tcpQ)
	s.startHTTP()
	return nil
}

// Handover is a takeover the successor has committed to.
type Handover struct {
	conn *net.UnixConn
}

// Done releases the predecessor, which exits only once this happens.
func (h *Handover) Done() error {
	return h.conn.Close()
}

// takeoverIn is what a successor has received so far, before it commits.
type takeoverIn struct {
	listeners map[string]*os.File
	state     *takeoverState
	pools     []pool.PoolHandoff
	leases    map[string]leaseRecord
	pipes     map[takeoverPipe]*os.File
	conns     []net.Conn
}

func (in *takeoverIn) close() {
	for _, f := range in.listeners {
		closeFile(f)
	}
	for _, f := range in.pipes {
		closeFile(f)
	}
	for _, c := range in.conns {
		_ = c.Close()
	}
	in.conns = nil
}

// TakeOver receives the daemon running on this socket and serves in its place.
// It returns once the predecessor has committed and the HTTP listeners are
// serving. Until then the predecessor is unchanged, and a failure leaves it
// running while this process reports the reason.
func (s *Server) TakeOver() (*Handover, error) {
	timeout := s.set.Duration("daemon.takeoverTimeout")
	conn, err := net.DialTimeout("unix", s.paths.Socket, timeout)
	if err != nil {
		return nil, fmt.Errorf("no daemon to take over on %s: %w", s.paths.Socket, err)
	}
	uc := conn.(*net.UnixConn)
	fail := func(err error) (*Handover, error) {
		uc.Close()
		return nil, err
	}
	_ = uc.SetDeadline(time.Now().Add(timeout))
	if _, err := io.WriteString(uc, "POST "+takeoverRoute+" HTTP/1.1\r\nHost: mcpx\r\nContent-Length: 0\r\n\r\n"); err != nil {
		return fail(err)
	}
	// The predecessor sends nothing after its headers until it has our hello,
	// so the buffer cannot hold any of the frames that follow.
	br := bufio.NewReader(uc)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		return fail(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != takeoverMediaType {
		return fail(fmt.Errorf("daemon refused takeover: %s", resp.Status))
	}
	if br.Buffered() > 0 {
		return fail(errors.New("unexpected data after takeover headers"))
	}
	if err := send(uc, kindHello, takeoverHello{Version: takeoverVersion, PID: os.Getpid()}, nil); err != nil {
		return fail(err)
	}

	in := &takeoverIn{listeners: map[string]*os.File{}, pipes: map[takeoverPipe]*os.File{}}
	for {
		f, fd, err := recvFrame(uc)
		if err != nil {
			in.close()
			return fail(err)
		}
		if f.Kind == kindEnd {
			return s.commitTakeover(uc, in, fd)
		}
		if err := s.receive(in, f, fd); err != nil {
			closeFile(fd)
			_ = sendFrame(uc, takeoverFrame{Kind: kindNack, Error: err.Error()}, nil)
			in.close()
			return fail(err)
		}
		if err := send(uc, kindAck, nil, nil); err != nil {
			in.close()
			return fail(err)
		}
	}
}

func (s *Server) receive(in *takeoverIn, f takeoverFrame, fd *os.File) error {
	switch f.Kind {
	case kindListener:
		var l takeoverListener
		if err := json.Unmarshal(f.Body, &l); err != nil || fd == nil {
			return errors.New("listener frame without a socket")
		}
		in.listeners[l.Network] = fd
	case kindState:
		var st takeoverState
		if err := json.Unmarshal(f.Body, &st); err != nil {
			return err
		}
		in.state = &st
	case kindPools:
		var p takeoverPools
		if err := json.Unmarshal(f.Body, &p); err != nil {
			return err
		}
		in.pools = p.Pools
		in.leases = p.Leases
	case kindPipe:
		var p takeoverPipe
		if err := json.Unmarshal(f.Body, &p); err != nil || fd == nil {
			return errors.New("pipe frame without a descriptor")
		}
		in.pipes[p] = fd
	case kindConn:
		if fd == nil {
			return errors.New("connection frame without a socket")
		}
		// FileConn duplicates the descriptor, and the frame's copy is not kept:
		// the connection is what this process owns from here.
		nc, err := net.FileConn(fd)
		if err != nil {
			return err
		}
		closeFile(fd)
		in.conns = append(in.conns, nc)
	default:
		return fmt.Errorf("unexpected frame %q", f.Kind)
	}
	return nil
}

// commitTakeover adopts what was received and, if that works, tells the
// predecessor to finish. Everything before the commit can be undone.
func (s *Server) commitTakeover(uc *net.UnixConn, in *takeoverIn, endFd *os.File) (*Handover, error) {
	closeFile(endFd)
	fail := func(err error) (*Handover, error) {
		in.close()
		_ = sendFrame(uc, takeoverFrame{Kind: kindNack, Error: err.Error()}, nil)
		uc.Close()
		return nil, err
	}
	if in.state == nil || in.state.Endpoint == "" {
		return fail(errors.New("handoff carries no daemon state"))
	}
	unixF, tcpF := in.listeners["unix"], in.listeners["tcp"]
	if unixF == nil || tcpF == nil {
		return fail(errors.New("handoff is missing a listener"))
	}
	ul, err := net.FileListener(unixF)
	closeFile(unixF)
	delete(in.listeners, "unix")
	if err != nil {
		closeFile(tcpF)
		return fail(err)
	}
	tl, err := net.FileListener(tcpF)
	closeFile(tcpF)
	delete(in.listeners, "tcp")
	if err != nil {
		ul.Close()
		return fail(err)
	}
	if err := attachPipes(in); err != nil {
		ul.Close()
		tl.Close()
		return fail(err)
	}
	ad, err := s.reg.Adopt(in.pools)
	if err != nil {
		closePipes(in.pools)
		ul.Close()
		tl.Close()
		return fail(err)
	}
	if err := sendFrame(uc, takeoverFrame{Kind: kindCommit}, nil); err != nil {
		ad.Abort()
		ul.Close()
		tl.Close()
		in.close()
		uc.Close()
		return nil, err
	}
	_ = uc.SetDeadline(time.Time{})

	// The connections are served before any new one is accepted, so a client
	// that was holding one is answered by this process.
	unixQ, tcpQ := splitPassed(in.conns)
	in.conns = nil
	s.unixLn = newConnListener(ul, &s.conns, unixQ)
	s.tcpLn = newConnListener(tl, &s.conns, tcpQ)
	s.endpoint = in.state.Endpoint
	if t, err := time.Parse(time.RFC3339Nano, in.state.Started); err == nil {
		s.started = t
	}
	ad.Attach()
	s.reg.adoptLeases(in.leases)
	if err := s.reg.SaveSessions(); err != nil {
		s.logger.Printf("takeover: save sessions: %v", err)
	}
	s.startHTTP()
	if err := s.publish(); err != nil {
		s.logger.Printf("takeover: writing the daemon record: %v", err)
	}
	s.logger.Printf("took over from the previous daemon at %s", s.endpoint)
	return &Handover{conn: uc}, nil
}

// attachPipes moves each received descriptor into the child it belongs to.
// Every pipe a child needs must be present; the failure is checked before any
// is moved, so the caller still owns all of them.
func attachPipes(in *takeoverIn) error {
	for pi := range in.pools {
		for ii := range in.pools[pi].Instances {
			inst := &in.pools[pi].Instances[ii]
			if inst.Stdio == nil {
				if inst.HTTP != nil {
					continue
				}
				return fmt.Errorf("instance %s carries no child", inst.ID)
			}
			for _, slot := range []string{"stdin", "stdout", "stderr"} {
				if in.pipes[takeoverPipe{Pool: pi, Instance: ii, Slot: slot}] == nil {
					return fmt.Errorf("instance %s is missing its %s", inst.ID, slot)
				}
			}
		}
	}
	for pi := range in.pools {
		for ii := range in.pools[pi].Instances {
			inst := &in.pools[pi].Instances[ii]
			if inst.Stdio == nil {
				continue
			}
			inst.Stdio.Stdin = in.pipes[takeoverPipe{Pool: pi, Instance: ii, Slot: "stdin"}]
			inst.Stdio.Stdout = in.pipes[takeoverPipe{Pool: pi, Instance: ii, Slot: "stdout"}]
			inst.Stdio.Stderr = in.pipes[takeoverPipe{Pool: pi, Instance: ii, Slot: "stderr"}]
		}
	}
	in.pipes = nil
	return nil
}

func closePipes(pools []pool.PoolHandoff) {
	for pi := range pools {
		for _, inst := range pools[pi].Instances {
			if st := inst.Stdio; st != nil {
				closeFile(st.Stdin)
				closeFile(st.Stdout)
				closeFile(st.Stderr)
			}
		}
	}
}
