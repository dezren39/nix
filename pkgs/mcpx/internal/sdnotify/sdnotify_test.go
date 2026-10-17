package sdnotify

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func listen(t *testing.T, name string) *net.UnixConn {
	t.Helper()
	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: name, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func next(t *testing.T, conn *net.UnixConn) string {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 256)
	n, _, err := conn.ReadFromUnix(buf)
	if err != nil {
		t.Fatalf("no datagram: %v", err)
	}
	return string(buf[:n])
}

func TestSendIsANoOpOutsideSystemd(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "")
	if err := Ready(); err != nil {
		t.Fatal(err)
	}
}

func TestReadyAndMainPIDReachTheSocket(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "notify.sock")
	conn := listen(t, sock)
	t.Setenv("NOTIFY_SOCKET", sock)

	if err := Ready(); err != nil {
		t.Fatal(err)
	}
	if got := next(t, conn); got != "READY=1" {
		t.Fatalf("Ready sent %q", got)
	}
	if err := MainPID(4242); err != nil {
		t.Fatal(err)
	}
	if got := next(t, conn); got != "MAINPID=4242\nREADY=1" {
		t.Fatalf("MainPID sent %q", got)
	}
}

func TestAbstractSocketName(t *testing.T) {
	name := fmt.Sprintf("@mcpx-sdnotify-%d-%d", os.Getpid(), time.Now().UnixNano())
	conn := listen(t, name)
	t.Setenv("NOTIFY_SOCKET", name)

	if err := Ready(); err != nil {
		t.Fatal(err)
	}
	if got := next(t, conn); got != "READY=1" {
		t.Fatalf("Ready sent %q", got)
	}
}

func TestMissingSocketIsReported(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", filepath.Join(t.TempDir(), "absent.sock"))
	if err := Ready(); err == nil {
		t.Fatal("expected an error for a socket nobody listens on")
	}
}
