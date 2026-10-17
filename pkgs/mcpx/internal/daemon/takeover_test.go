package daemon

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/dezren39/mcpx/internal/config"
	"github.com/dezren39/mcpx/internal/defaults"
	"github.com/dezren39/mcpx/internal/testsupport"
)

func unixPair(t *testing.T) (*net.UnixConn, *net.UnixConn) {
	t.Helper()
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	a := os.NewFile(uintptr(fds[0]), "a")
	b := os.NewFile(uintptr(fds[1]), "b")
	defer a.Close()
	defer b.Close()
	ca, err := net.FileConn(a)
	if err != nil {
		t.Fatal(err)
	}
	cb, err := net.FileConn(b)
	if err != nil {
		ca.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { ca.Close(); cb.Close() })
	return ca.(*net.UnixConn), cb.(*net.UnixConn)
}

func TestAFrameCarriesItsDescriptorWithItsBody(t *testing.T) {
	a, b := unixPair(t)
	f, err := os.CreateTemp(t.TempDir(), "pipe")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("pipe-contents"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}

	sent := make(chan error, 1)
	go func() {
		sent <- send(a, kindPipe, takeoverPipe{Pool: 1, Instance: 2, Slot: "stdin"}, f)
	}()
	got, fd, err := recvFrame(b)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	if fd == nil {
		t.Fatal("the descriptor did not arrive with its frame")
	}
	defer fd.Close()
	if got.Kind != kindPipe {
		t.Fatalf("kind = %q, want %q", got.Kind, kindPipe)
	}
	var p takeoverPipe
	if err := json.Unmarshal(got.Body, &p); err != nil {
		t.Fatal(err)
	}
	if p != (takeoverPipe{Pool: 1, Instance: 2, Slot: "stdin"}) {
		t.Fatalf("body = %+v", p)
	}
	data, err := io.ReadAll(fd)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "pipe-contents" {
		t.Fatalf("descriptor reads %q", data)
	}
}

func TestAFrameOverTheLimitIsRefusedBeforeItIsRead(t *testing.T) {
	a, b := unixPair(t)
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(defaults.TakeoverMaxFrame)+1)
	go func() { _, _ = a.Write(hdr[:]) }()
	if _, _, err := recvFrame(b); err == nil {
		t.Fatal("a frame over the limit was accepted")
	}
}

func TestRegistryHandoffKeepsTheChildAcrossTheSuccessor(t *testing.T) {
	fake := testsupport.FakeMCPBinary(t)
	dir := t.TempDir()
	cfg := &config.Config{MCPServers: map[string]*config.Server{
		"a": {Name: "a", Command: fake},
	}}
	old, err := NewRegistry(cfg, Paths{State: dir, Cache: dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	pa, _ := old.Pool("a")
	pid := childPID(t, pa)

	ho, err := old.Detach(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ho.Commit()

	next, err := NewRegistry(cfg, Paths{State: dir, Cache: dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	ad, err := next.Adopt(ho.Pools)
	if err != nil {
		t.Fatal(err)
	}
	ad.Attach()
	pn, _ := next.Pool("a")
	if got := childPID(t, pn); got != pid {
		t.Fatalf("the successor restarted an unchanged child: pid %d -> %d", pid, got)
	}
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("child %d did not survive the handoff: %v", pid, err)
	}
}

func TestRegistryHandoffThatIsNotCommittedRestoresTheChild(t *testing.T) {
	fake := testsupport.FakeMCPBinary(t)
	dir := t.TempDir()
	cfg := &config.Config{MCPServers: map[string]*config.Server{
		"a": {Name: "a", Command: fake},
	}}
	r, err := NewRegistry(cfg, Paths{State: dir, Cache: dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	pa, _ := r.Pool("a")
	pid := childPID(t, pa)

	ho, err := r.Detach(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ho.Restore()
	if got := childPID(t, pa); got != pid {
		t.Fatalf("a restored child has pid %d, want %d", got, pid)
	}
	lease, err := pa.Acquire(context.Background(), "global")
	if err != nil {
		t.Fatalf("the restored pool does not serve: %v", err)
	}
	lease.Release()
}

func TestAFailedHandoffLeavesTheDaemonServing(t *testing.T) {
	dir, err := os.MkdirTemp("", "mxh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	paths := Paths{State: dir, Cache: dir, Socket: filepath.Join(dir, "d.sock"), Info: filepath.Join(dir, "d.json")}
	srv, err := NewServer(Options{
		Config:  &config.Config{MCPServers: map[string]*config.Server{}},
		Paths:   paths,
		Version: "test",
		Logger:  log.New(io.Discard, "", 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.reg.Close)
	if err := srv.Listen(0); err != nil {
		t.Fatal(err)
	}
	srv.startHTTP()
	t.Cleanup(func() { _ = srv.httpSrv.Close() })

	old, successor := unixPair(t)
	handed := make(chan error, 1)
	go func() { handed <- srv.handOver(old) }()

	mustSend := func(kind string, body any, fd *os.File) {
		t.Helper()
		if err := send(successor, kind, body, fd); err != nil {
			t.Fatal(err)
		}
	}
	expect := func(kind string) {
		t.Helper()
		f, fd, err := recvFrame(successor)
		if err != nil {
			t.Fatal(err)
		}
		closeFile(fd)
		if f.Kind != kind {
			t.Fatalf("frame %q, want %q", f.Kind, kind)
		}
		if err := sendFrame(successor, takeoverFrame{Kind: kindAck}, nil); err != nil {
			t.Fatal(err)
		}
	}

	mustSend(kindHello, takeoverHello{Version: takeoverVersion, PID: os.Getpid()}, nil)
	expect(kindListener)
	expect(kindListener)
	expect(kindState)
	// The successor refuses the children. Nothing was moved, so the old side
	// must go on serving.
	f, fd, err := recvFrame(successor)
	if err != nil {
		t.Fatal(err)
	}
	closeFile(fd)
	if f.Kind != kindPools {
		t.Fatalf("frame %q, want %q", f.Kind, kindPools)
	}
	if err := sendFrame(successor, takeoverFrame{Kind: kindNack, Error: "refused by test"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := <-handed; err == nil {
		t.Fatal("a refused handoff reported success")
	}

	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", paths.Socket)
		},
	}}
	resp, err := client.Get("http://mcpx/v1/health")
	if err != nil {
		t.Fatalf("the old daemon stopped answering after a refused handoff: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health after a refused handoff: %s", resp.Status)
	}
}
