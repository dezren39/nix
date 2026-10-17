package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/testsupport"
)

// The promise these tests make: a client that holds a keep-alive TCP
// connection open across a takeover keeps using that connection. The
// connection is passed to the successor, not closed by the old daemon.

// tcpKeepAlive is a client that holds its TCP connection open between calls,
// the way a long-running client does, and reports which connection it used.
type tcpKeepAlive struct {
	hc       *http.Client
	endpoint string
}

func newTCPKeepAlive(endpoint string) *tcpKeepAlive {
	return &tcpKeepAlive{
		hc:       &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: 4}},
		endpoint: endpoint,
	}
}

// do makes one request. It returns the local port of the connection the
// request went out on, and whether that connection had carried a request
// before.
func (c *tcpKeepAlive) do(method, path string, body []byte) (status int, out []byte, port string, reused bool, err error) {
	var info httptrace.GotConnInfo
	trace := &httptrace.ClientTrace{GotConn: func(i httptrace.GotConnInfo) { info = i }}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(context.Background(), trace),
		method, c.endpoint+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, "", false, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, nil, "", false, err
	}
	defer resp.Body.Close()
	out, err = io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, out, "", false, err
	}
	_, port, _ = net.SplitHostPort(info.Conn.LocalAddr().String())
	return resp.StatusCode, out, port, info.Reused, nil
}

// call makes one tool call and checks the answer carries the text sent.
func (c *tcpKeepAlive) call(server, tool, text string) (port string, reused bool, err error) {
	body, _ := json.Marshal(map[string]any{
		"server": server, "tool": tool, "args": map[string]any{"message": text},
	})
	status, out, port, reused, err := c.do(http.MethodPost, "/v1/call", body)
	if err != nil {
		return port, reused, err
	}
	if status != http.StatusOK || !strings.Contains(string(out), text) {
		return port, reused, fmt.Errorf("call: %d %s", status, out)
	}
	return port, reused, nil
}

// startOldServer runs a daemon on the handoff paths, the way the one being
// replaced is running.
func startOldServer(t *testing.T, paths Paths, cfg *config.Config) *Server {
	t.Helper()
	srv, err := NewServer(handoffOptions(paths, cfg, "old"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.reg.Close)
	if err := srv.Listen(0); err != nil {
		t.Fatal(err)
	}
	srv.startHTTP()
	t.Cleanup(func() { _ = srv.httpSrv.Close() })
	return srv
}

// An idle keep-alive connection is passed to the successor. The client's next
// request goes out on the same connection and is answered there.
func TestAnIdleConnectionSurvivesATakeoverInProcess(t *testing.T) {
	paths := handoffPaths(t)
	cfg := &config.Config{MCPServers: map[string]*config.Server{
		"a": {Name: "a", Command: testsupport.FakeMCPBinary(t)},
	}}
	old := startOldServer(t, paths, cfg)
	client := newTCPKeepAlive(old.Endpoint())
	port, _, err := client.call("a", "echo", "before")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)

	successor, err := NewServer(handoffOptions(paths, cfg, "new"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(successor.reg.Close)
	h, err := successor.TakeOver()
	if err != nil {
		t.Fatalf("takeover: %v", err)
	}
	_ = h.Done()
	t.Cleanup(func() { _ = successor.httpSrv.Close() })

	port2, reused, err := client.call("a", "echo", "after")
	if err != nil {
		t.Fatalf("the call on the idle connection failed: %v", err)
	}
	if !reused || port2 != port {
		t.Fatalf("the call did not use the connection it held: port %s -> %s, reused %v", port, port2, reused)
	}
}

// A request that is in flight when the takeover begins is answered by the old
// daemon, and the connection it was on then goes to the successor.
func TestACallInFlightAtTheHandoverIsAnsweredByTheOldDaemon(t *testing.T) {
	paths := handoffPaths(t)
	cfg := &config.Config{MCPServers: map[string]*config.Server{
		"a": {Name: "a", Command: testsupport.FakeMCPBinary(t)},
	}}
	old := startOldServer(t, paths, cfg)
	client := newTCPKeepAlive(old.Endpoint())
	if _, _, err := client.call("a", "echo", "warm"); err != nil {
		t.Fatal(err)
	}
	port, _, err := client.call("a", "echo", "warm again")
	if err != nil {
		t.Fatal(err)
	}

	slow := make(chan error, 1)
	go func() {
		body, _ := json.Marshal(map[string]any{"server": "a", "tool": "slow", "args": map[string]any{"ms": 400}})
		status, out, p, _, err := client.do(http.MethodPost, "/v1/call", body)
		if err == nil && (status != http.StatusOK || p != port || !strings.Contains(string(out), "slept")) {
			err = fmt.Errorf("slow call: %d %s on port %s", status, out, p)
		}
		slow <- err
	}()
	time.Sleep(100 * time.Millisecond)

	successor, err := NewServer(handoffOptions(paths, cfg, "new"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(successor.reg.Close)
	h, err := successor.TakeOver()
	if err != nil {
		t.Fatalf("takeover: %v", err)
	}
	_ = h.Done()
	t.Cleanup(func() { _ = successor.httpSrv.Close() })
	if err := <-slow; err != nil {
		t.Fatalf("the call in flight at the handover was not answered by the old daemon: %v", err)
	}

	port2, reused, err := client.call("a", "echo", "after")
	if err != nil {
		t.Fatalf("the call after the handover failed: %v", err)
	}
	if !reused || port2 != port {
		t.Fatalf("the connection was not reused after the handover: port %s -> %s, reused %v", port, port2, reused)
	}
}

// A successor that refuses the connections leaves the old daemon serving, and
// the connections it held keep working on the same port.
func TestAFailedHandoffKeepsItsIdleConnectionsWorking(t *testing.T) {
	paths := handoffPaths(t)
	old := startOldServer(t, paths, &config.Config{MCPServers: map[string]*config.Server{}})
	client := newTCPKeepAlive(old.Endpoint())
	status, _, port, _, err := client.do(http.MethodGet, "/v1/health", nil)
	if err != nil || status != http.StatusOK {
		t.Fatalf("health before the handover: %d %v", status, err)
	}
	time.Sleep(100 * time.Millisecond)

	oldConn, successorConn := unixPair(t)
	handed := make(chan error, 1)
	go func() { handed <- old.handOver(oldConn) }()

	expect := func(kind string) {
		t.Helper()
		f, fd, err := recvFrame(successorConn)
		if err != nil {
			t.Fatal(err)
		}
		closeFile(fd)
		if f.Kind != kind {
			t.Fatalf("frame %q, want %q", f.Kind, kind)
		}
		if err := sendFrame(successorConn, takeoverFrame{Kind: kindAck}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := send(successorConn, kindHello, takeoverHello{Version: takeoverVersion, PID: 1}, nil); err != nil {
		t.Fatal(err)
	}
	expect(kindListener)
	expect(kindListener)
	expect(kindState)

	// The idle connection is handed over, and the successor refuses it.
	f, fd, err := recvFrame(successorConn)
	if err != nil {
		t.Fatal(err)
	}
	closeFile(fd)
	if f.Kind != kindConn {
		t.Fatalf("frame %q, want the connection %q", f.Kind, kindConn)
	}
	if err := sendFrame(successorConn, takeoverFrame{Kind: kindNack, Error: "refused by test"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := <-handed; err == nil {
		t.Fatal("a refused handoff reported success")
	}

	status, _, port2, reused, err := client.do(http.MethodGet, "/v1/health", nil)
	if err != nil || status != http.StatusOK {
		t.Fatalf("the old daemon stopped answering after a refused handoff: %d %v", status, err)
	}
	if !reused || port2 != port {
		t.Fatalf("the connection was not kept after a refused handoff: port %s -> %s, reused %v", port, port2, reused)
	}
}

// Keep-alive callers keep calling through a takeover, some of them going idle
// between calls. No call may fail: each is answered by one daemon or the other.
func TestKeepAliveCallersLoseNoCallsAcrossATakeover(t *testing.T) {
	paths := handoffPaths(t)
	cfg := &config.Config{MCPServers: map[string]*config.Server{
		"a": {Name: "a", Command: testsupport.FakeMCPBinary(t)},
	}}
	old := startOldServer(t, paths, cfg)
	endpoint := old.Endpoint()

	const callers = 8
	var ok, failed atomic.Int64
	var mu sync.Mutex
	var firstErr error
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := newTCPKeepAlive(endpoint)
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, _, err := c.call("a", "echo", "during handover"); err != nil {
					failed.Add(1)
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
				} else {
					ok.Add(1)
				}
				switch {
				case i%4 == 2:
					time.Sleep(150 * time.Millisecond)
				case i%2 == 1:
					time.Sleep(rand.N(40 * time.Millisecond))
				}
			}
		}(i)
	}

	time.Sleep(300 * time.Millisecond)
	successor, err := NewServer(handoffOptions(paths, cfg, "new"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(successor.reg.Close)
	h, err := successor.TakeOver()
	if err != nil {
		t.Fatalf("takeover: %v", err)
	}
	_ = h.Done()
	t.Cleanup(func() { _ = successor.httpSrv.Close() })
	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()

	if failed.Load() > 0 {
		t.Fatalf("%d of %d calls failed across the takeover; first: %v", failed.Load(), ok.Load()+failed.Load(), firstErr)
	}
	if ok.Load() == 0 {
		t.Fatal("no call was made")
	}
	t.Logf("%d calls across the takeover from %d keep-alive callers, none failed", ok.Load(), callers)
}

// net/http reads one byte in the background while a handler runs. It flushes
// the answer before it stops that read, so a client that is quick to send its
// next request can have the first byte of it held by the server, after the
// answer, where the handover cannot see it. Such a connection must not be
// passed: the successor would begin the request one byte short.
func TestAByteReadAfterTheLastAnswerIsNotPassed(t *testing.T) {
	server, client := unixPair(t)
	hc := newHandoffConn(server, false)
	tr := &connTracker{}
	tr.add(hc, http.StateActive)

	if _, err := client.Write([]byte("GET / HTTP/1.1\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	request := make([]byte, len("GET / HTTP/1.1\r\n\r\n"))
	if _, err := io.ReadFull(hc, request); err != nil {
		t.Fatal(err)
	}
	if _, err := hc.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Write([]byte("P")); err != nil {
		t.Fatal(err)
	}
	// The background read takes the byte the client sent straight after its answer.
	one := make([]byte, 1)
	if n, err := hc.Read(one); n != 1 || err != nil {
		t.Fatalf("background read: %d, %v", n, err)
	}

	tr.setHandingOver(true)
	tr.observe(hc, http.StateIdle)
	if hc.isFrozen() {
		t.Fatal("a connection with a byte held after its answer was passed")
	}
	if got := tr.takePassing(); len(got) != 0 {
		t.Fatalf("passing %d connections, want none", len(got))
	}
}

// A connection whose last read came before its answer holds nothing, and is
// passed once it goes idle.
func TestAConnectionIdleAfterItsAnswerIsPassed(t *testing.T) {
	server, client := unixPair(t)
	hc := newHandoffConn(server, false)
	tr := &connTracker{}
	tr.add(hc, http.StateActive)

	if _, err := client.Write([]byte("GET / HTTP/1.1\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	request := make([]byte, len("GET / HTTP/1.1\r\n\r\n"))
	if _, err := io.ReadFull(hc, request); err != nil {
		t.Fatal(err)
	}
	if _, err := hc.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")); err != nil {
		t.Fatal(err)
	}

	tr.setHandingOver(true)
	tr.observe(hc, http.StateIdle)
	got := tr.takePassing()
	if len(got) != 1 || got[0] != hc || !hc.isFrozen() {
		t.Fatalf("an idle connection with nothing held was not passed: %d passed, frozen %v", len(got), hc.isFrozen())
	}
}
