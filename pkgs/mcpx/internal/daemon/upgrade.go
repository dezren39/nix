package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

const upgradeRoute = "/v1/upgrade"

// An upgrade starts the new binary as this daemon's child. The child inherits
// the environment, including NOTIFY_SOCKET, so it is still in the unit's
// cgroup and may take over MAINPID when this process exits. The request is
// hijacked like a takeover is, because the handover drains this daemon's HTTP
// server, and a handler still waiting on it would hold that drain open.

// An upgrade whose successor does not dial back within the takeover timeout is
// abandoned: the child is killed, and the takeover it never began is refused.
type upgradeRun struct {
	conn net.Conn
	once sync.Once
	done chan struct{}

	mu        sync.Mutex
	dialed    bool
	abandoned bool
}

// claim lets a successor that dialed back proceed, unless the upgrade already
// gave up on it.
func (u *upgradeRun) claim() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.abandoned {
		return false
	}
	u.dialed = true
	return true
}

// abandon gives up on a successor that has not dialed back. It reports false
// when the successor got there first.
func (u *upgradeRun) abandon() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.dialed {
		return false
	}
	u.abandoned = true
	return true
}

func (u *upgradeRun) wasAbandoned() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.abandoned
}

// settle answers the upgrade once. The handover calls it before the old
// daemon waits for the successor to hang up, so the answer is on the wire
// before this process can exit.
func (u *upgradeRun) settle(err error) {
	u.once.Do(func() {
		defer close(u.done)
		code, body := http.StatusOK, map[string]any{"status": "upgraded"}
		if err != nil {
			code, body = http.StatusInternalServerError, map[string]any{"error": err.Error()}
		}
		b, _ := json.Marshal(body)
		resp := &http.Response{
			StatusCode:    code,
			ProtoMajor:    1,
			ProtoMinor:    1,
			Header:        http.Header{"Content-Type": {"application/json"}},
			ContentLength: int64(len(b)),
			Body:          io.NopCloser(bytes.NewReader(b)),
			Close:         true,
		}
		_ = resp.Write(u.conn)
	})
}

func (s *Server) settleUpgrade(err error) {
	if u := s.upgrading.Load(); u != nil {
		u.settle(err)
	}
}

func (s *Server) handleUpgrade(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Binary string `json:"binary"`
		// Start is the unit's start command ahead of the binary, when there
		// is one: the wrapper that sets the environment the service runs
		// with. See startSuccessor.
		Start []string `json:"start,omitempty"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil || json.Unmarshal(body, &req) != nil || req.Binary == "" {
		http.Error(w, "an upgrade needs the path of the new binary", http.StatusBadRequest)
		return
	}
	for _, a := range req.Start {
		if a == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "the start command has an empty argument"})
			return
		}
	}
	if err := checkUpgradeBinary(req.Binary); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	self, err := os.Executable()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if samePath(self, req.Binary) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "current"})
		return
	}
	if !s.upgradeMu.TryLock() {
		http.Error(w, "an upgrade is already under way", http.StatusConflict)
		return
	}
	defer s.upgradeMu.Unlock()
	conn, _, err := http.NewResponseController(w).Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	u := &upgradeRun{conn: conn, done: make(chan struct{})}
	// Registered before the successor starts, since it can dial back before
	// Start returns.
	s.upgrading.Store(u)
	defer s.upgrading.Store(nil)
	s.logger.Printf("upgrade requested: starting %s as a child", req.Binary)
	cmd, err := s.startSuccessor(req.Binary, req.Start)
	if err != nil {
		u.settle(err)
		return
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	timeout := s.set.Duration("daemon.takeoverTimeout")
	dialBack := time.AfterFunc(timeout, func() {
		if u.abandon() {
			_ = cmd.Process.Kill()
		}
	})
	defer dialBack.Stop()
	select {
	case <-u.done:
	case err := <-exited:
		// A handover that was under way holds takeoverMu until it has put the
		// children back, so the failure is reported after that.
		s.takeoverMu.Lock()
		s.takeoverMu.Unlock()
		msg := "the successor exited before it took over"
		if err != nil {
			msg += ": " + err.Error()
		}
		if u.wasAbandoned() {
			msg = fmt.Sprintf("the successor did not dial back within %s; the old daemon is still serving", timeout)
		}
		u.settle(errors.New(msg))
	}
}

// checkUpgradeBinary accepts only an absolute path to an executable regular
// file. The daemon runs whatever it is given, so a relative path would depend
// on the working directory of a service.
func checkUpgradeBinary(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("the upgrade binary %q must be an absolute path", path)
	}
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("the upgrade binary: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("the upgrade binary %s is not a regular file", path)
	}
	if fi.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("the upgrade binary %s is not executable", path)
	}
	return nil
}

// startSuccessor runs the new binary as this daemon's child.
//
// The successor is started through start, the unit's ExecStart ahead of the
// binary, so that the wrapper gives it the environment the unit gives a fresh
// start: a new token or variable reaches the successor, which an inherited
// environment would not. The wrapper execs the binary with the arguments it
// is given. With no start command the binary runs directly, under this
// daemon's environment, as before.
//
// The environment is still inherited because the successor needs
// NOTIFY_SOCKET, and the wrapper only adds to it. A variable the unit used to
// set and no longer does is therefore still present in the successor.
func (s *Server) startSuccessor(binary string, start []string) (*exec.Cmd, error) {
	args := append(append([]string{}, start...), binary, "daemon", "--takeover")
	if s.ConfigArg != "" {
		args = append(args, "--config", s.ConfigArg)
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", binary, err)
	}
	return cmd, nil
}

func samePath(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return ra == rb
}
