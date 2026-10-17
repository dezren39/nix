package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// These tests drive the daemon over its TCP endpoint, the way a long-running
// client does, across a real `daemon --takeover` process. They check the
// connection itself: the next call must go out on the connection the client
// already held, and the successor must hold that connection's socket.

// keepAliveCaller holds one TCP connection open between calls.
type keepAliveCaller struct {
	hc  *http.Client
	url string
}

func newKeepAliveCaller(endpoint string) *keepAliveCaller {
	return &keepAliveCaller{
		hc:  &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: 4}},
		url: endpoint + "/v1/call",
	}
}

// post sends one call. It returns the local port of the connection the call
// went out on, whether that connection had carried a call before, and the
// answer.
func (c *keepAliveCaller) post(body string) (port string, reused bool, out string, err error) {
	var info httptrace.GotConnInfo
	trace := &httptrace.ClientTrace{GotConn: func(i httptrace.GotConnInfo) { info = i }}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(context.Background(), trace),
		http.MethodPost, c.url, strings.NewReader(body))
	if err != nil {
		return "", false, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", false, "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if info.Conn != nil {
		_, port, _ = net.SplitHostPort(info.Conn.LocalAddr().String())
	}
	if err == nil && resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("%s: %s", resp.Status, b)
	}
	return port, info.Reused, string(b), err
}

// echo makes an echo call and checks the answer carries the message.
func (c *keepAliveCaller) echo(message string) (port string, reused bool, err error) {
	body, _ := json.Marshal(map[string]any{
		"server": "demo", "tool": "echo", "args": map[string]any{"message": message},
	})
	port, reused, out, err := c.post(string(body))
	if err == nil && !strings.Contains(out, message) {
		err = fmt.Errorf("echo answered %s", out)
	}
	return port, reused, err
}

// tcpEndpoint is the TCP base URL the running daemon reports.
func (e *env) tcpEndpoint(t *testing.T) string {
	t.Helper()
	var st struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.Unmarshal([]byte(jsonOf(t, e.run("--json", "status"))), &st); err != nil {
		t.Fatal(err)
	}
	if st.Endpoint == "" {
		t.Fatal("the daemon reports no TCP endpoint")
	}
	return st.Endpoint
}

// serverSocketInode is the inode of the daemon's end of the TCP connection
// whose client end uses clientPort. It is found in /proc/net/tcp, which is
// where the kernel shows every socket's inode.
func serverSocketInode(t *testing.T, clientPort string) uint64 {
	t.Helper()
	for _, name := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		b, err := os.ReadFile(name)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) < 10 {
				continue
			}
			_, rport, ok := strings.Cut(f[2], ":")
			if !ok {
				continue
			}
			if n, err := strconv.ParseUint(rport, 16, 16); err != nil || strconv.FormatUint(n, 10) != clientPort {
				continue
			}
			if inode, err := strconv.ParseUint(f[9], 10, 64); err == nil && inode != 0 {
				return inode
			}
		}
	}
	t.Fatalf("no socket of the daemon is connected to port %s", clientPort)
	return 0
}

// processHoldsSocket reports whether a process has the socket open, by the
// socket:[inode] links in its descriptor table.
func processHoldsSocket(t *testing.T, pid int, inode uint64) bool {
	t.Helper()
	dir := fmt.Sprintf("/proc/%d/fd", pid)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the descriptors of pid %d: %v", pid, err)
	}
	want := fmt.Sprintf("socket:[%d]", inode)
	for _, en := range entries {
		if target, err := os.Readlink(filepath.Join(dir, en.Name())); err == nil && target == want {
			return true
		}
	}
	return false
}

// A keep-alive connection that is idle when the takeover starts carries the
// client's next call, on the same TCP connection, through the successor.
func TestAnIdleKeepAliveConnectionSurvivesATakeover(t *testing.T) {
	e := newEnv(t, oneServer)
	e.envVars = append(e.envVars, "MCPX_DAEMON_WATCH_CONFIG=false")
	e.run("call", "demo.echo", `{"message":"warm"}`)
	before := e.daemonState(t)

	caller := newKeepAliveCaller(e.tcpEndpoint(t))
	port, _, err := caller.echo("before")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond) // the connection is idle when the successor starts

	s := startSuccessor(t, e)
	e.awaitSuccessor(t, s)
	port2, reused, err := caller.echo("after")
	if err != nil {
		t.Fatalf("the call on the idle connection failed: %v", err)
	}
	if !reused || port2 != port {
		t.Fatalf("the call did not use the connection it held: port %s -> %s, reused %v", port, port2, reused)
	}
	if !processHoldsSocket(t, s.cmd.Process.Pid, serverSocketInode(t, port)) {
		t.Fatalf("the successor does not hold the socket of connection %s", port)
	}
	s.mainPIDReported(t)
	awaitExit(t, before.PID, "the previous daemon")
}

// A call in flight when the takeover starts is answered by the old daemon, and
// the connection it was on carries the next call on the successor.
func TestACallInFlightAtATakeoverIsAnsweredAndItsConnectionMovesOn(t *testing.T) {
	e := newEnv(t, oneServer)
	e.envVars = append(e.envVars, "MCPX_DAEMON_WATCH_CONFIG=false")
	e.run("call", "demo.echo", `{"message":"warm"}`)
	before := e.daemonState(t)

	caller := newKeepAliveCaller(e.tcpEndpoint(t))
	port, _, err := caller.echo("warm again")
	if err != nil {
		t.Fatal(err)
	}
	slow := make(chan error, 1)
	go func() {
		p, _, out, err := caller.post(`{"server":"demo","tool":"slow","args":{"ms":1000}}`)
		if err == nil && (p != port || !strings.Contains(out, "slept")) {
			err = fmt.Errorf("the slow call was answered on port %s: %s", p, out)
		}
		slow <- err
	}()
	time.Sleep(200 * time.Millisecond)

	s := startSuccessor(t, e)
	e.awaitSuccessor(t, s)
	if err := <-slow; err != nil {
		t.Fatalf("the call in flight at the takeover was not answered: %v", err)
	}
	port2, reused, err := caller.echo("after")
	if err != nil {
		t.Fatalf("the call after the takeover failed: %v", err)
	}
	if !reused || port2 != port {
		t.Fatalf("the connection was not reused after the takeover: port %s -> %s, reused %v", port, port2, reused)
	}
	s.mainPIDReported(t)
	awaitExit(t, before.PID, "the previous daemon")
}

// Keep-alive callers keep calling through a real takeover, some of them going
// idle between calls. No call may fail.
func TestKeepAliveCallersLoseNoCallsAcrossARealTakeover(t *testing.T) {
	e := newEnv(t, oneServer)
	e.envVars = append(e.envVars, "MCPX_DAEMON_WATCH_CONFIG=false")
	e.run("call", "demo.echo", `{"message":"warm"}`)
	before := e.daemonState(t)
	endpoint := e.tcpEndpoint(t)

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
			c := newKeepAliveCaller(endpoint)
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, _, err := c.echo("steady"); err != nil {
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

	time.Sleep(time.Second)
	s := startSuccessor(t, e)
	e.awaitSuccessor(t, s)
	time.Sleep(time.Second)
	close(stop)
	wg.Wait()
	awaitExit(t, before.PID, "the previous daemon")

	t.Logf("%d calls across the takeover from %d keep-alive callers; %d failed", ok.Load(), callers, failed.Load())
	if failed.Load() > 0 {
		t.Fatalf("%d of %d calls failed across the takeover; first: %v", failed.Load(), ok.Load()+failed.Load(), firstErr)
	}
	if ok.Load() < callers {
		t.Fatalf("the callers made only %d calls", ok.Load())
	}
}
