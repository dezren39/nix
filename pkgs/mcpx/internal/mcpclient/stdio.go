package mcpclient

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"github.com/dezren39/mcpx/internal/defaults"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// StdioTransport runs an MCP server as a child process and speaks
// newline-delimited JSON-RPC over its stdin/stdout.
type StdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	// outR and errR are the parent's read ends, closed by Close once the
	// child is gone. Ours rather than cmd.StdoutPipe/StderrPipe: see NewStdio.
	outR, errR *os.File
	// stderrDone is closed when the child's stderr has been read to EOF.
	stderrDone chan struct{}

	writeMu sync.Mutex
	closeMu sync.Mutex
	closed  bool

	label   string
	stderr  *ringBuffer
	exited  chan struct{}
	waitErr error

	stdinGrace, termGrace time.Duration
}

// StdioOptions configure a child MCP server process.
type StdioOptions struct {
	Command string
	Args    []string
	Env     map[string]string
	Cwd     string
	// InheritEnv includes the parent environment (default true).
	InheritEnv bool
	// StderrTo, if non-nil, receives a copy of the child's stderr.
	StderrTo io.Writer
	// StdinGrace and TermGrace are Close's waits after closing stdin and
	// after SIGTERM. Zero means defaults.StdioStdinGrace / StdioTermGrace.
	StdinGrace, TermGrace time.Duration
}

// maxLine bounds a single JSON-RPC frame. MCP servers such as
// chrome-devtools-mcp emit multi-megabyte DOM snapshots, so this is generous.
var maxLine = int(defaults.StdioMaxLine)

// NewStdio spawns the child process.
func NewStdio(opts StdioOptions) (*StdioTransport, error) {
	if opts.Command == "" {
		return nil, errors.New("stdio transport: empty command")
	}
	cmd := exec.Command(opts.Command, opts.Args...)
	cmd.Dir = opts.Cwd

	env := []string{}
	if opts.InheritEnv {
		env = append(env, os.Environ()...)
	}
	for k, v := range opts.Env {
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	// Put the child in its own process group so we can kill the whole tree
	// (chrome-devtools-mcp forks a browser).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	// Plain pipes, not cmd.StdoutPipe/StderrPipe. Those are closed by
	// cmd.Wait as soon as the child exits, which races the readers: a child
	// that prints why it is refusing to start and exits had its stderr
	// thrown away ("read |0: file already closed", stderr empty) about one
	// start in twenty. With our own pipes the readers drain to EOF.
	outR, outW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		outR.Close()
		outW.Close()
		return nil, err
	}
	cmd.Stdout, cmd.Stderr = outW, errW
	startErr := cmd.Start()
	// The child holds its own copies; ours must go, or EOF never arrives.
	outW.Close()
	errW.Close()
	if startErr != nil {
		outR.Close()
		errR.Close()
		return nil, fmt.Errorf("start %s: %w", opts.Command, startErr)
	}

	t := &StdioTransport{
		cmd:        cmd,
		stdin:      stdin,
		stdout:     bufio.NewReaderSize(outR, 1<<20),
		outR:       outR,
		errR:       errR,
		label:      strings.TrimSpace(opts.Command + " " + strings.Join(opts.Args, " ")),
		stderr:     newRingBuffer(64 << 10),
		exited:     make(chan struct{}),
		stderrDone: make(chan struct{}),
		stdinGrace: cmp.Or(opts.StdinGrace, defaults.StdioStdinGrace),
		termGrace:  cmp.Or(opts.TermGrace, defaults.StdioTermGrace),
	}

	go func() {
		defer close(t.stderrDone)
		var w io.Writer = t.stderr
		if opts.StderrTo != nil {
			w = io.MultiWriter(t.stderr, opts.StderrTo)
		}
		_, _ = io.Copy(w, errR)
	}()
	go func() {
		t.waitErr = cmd.Wait()
		close(t.exited)
	}()

	return t, nil
}

// Send writes one frame.
func (t *StdioTransport) Send(ctx context.Context, msg []byte) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if t.closed {
		return errors.New("stdio transport closed")
	}
	if _, err := t.stdin.Write(append(msg, '\n')); err != nil {
		return fmt.Errorf("write to %s: %w (stderr: %s)", t.label, err, t.lastWords(400))
	}
	return nil
}

// lastWords is the tail of the child's stderr, waited for rather than
// sampled.
//
// The ring buffer is filled by a goroutine, so at the moment a write or a read
// fails it may still be empty even though the child has already printed the
// only explanation there is. Recv waited for the drain; Send sampled -- and
// Send is the one that fails first when a server refuses to start, because
// cmd.Wait closes our end of its stdin as soon as the child is reaped, so the
// `initialize` frame hits "file already closed" before anything is ever read
// back. The error a person then sees named the pipe and not the reason:
//
//	server "fake": initialize: write to .../fakemcp: write |1: file already
//	closed (stderr: )
//
// Both waits are bounded. A grandchild that inherited the pipe can hold it
// open past its parent's exit, and a diagnostic is not worth hanging for.
func (t *StdioTransport) lastWords(n int) string {
	<-waitOrTimeout(t.exited, defaults.StdioExitGrace)
	<-waitOrTimeout(t.stderrDone, defaults.StdioDrainGrace)
	return t.stderr.Tail(n)
}

// Recv reads one frame, skipping any non-JSON noise a server prints to stdout.
func (t *StdioTransport) Recv() ([]byte, error) {
	for {
		line, err := readLine(t.stdout)
		if err != nil {
			if errors.Is(err, io.EOF) {
				// The reason a server died is usually its last words on
				// stderr. Taken before waitErr is read, because that field
				// is written by the reaping goroutine and only settled once
				// `exited` has closed, which lastWords waits for.
				tail := t.lastWords(800)
				return nil, fmt.Errorf("%s exited: %v (stderr: %s)", t.label, t.waitErr, tail)
			}
			return nil, err
		}
		line = trimSpace(line)
		if len(line) == 0 {
			continue
		}
		if line[0] != '{' && line[0] != '[' {
			// Servers occasionally log banners on stdout. Ignore them.
			continue
		}
		return line, nil
	}
}

func readLine(r *bufio.Reader) ([]byte, error) {
	var buf []byte
	for {
		chunk, isPrefix, err := r.ReadLine()
		if err != nil {
			return nil, err
		}
		buf = append(buf, chunk...)
		if len(buf) > maxLine {
			return nil, fmt.Errorf("frame exceeds %d bytes", maxLine)
		}
		if !isPrefix {
			return buf, nil
		}
	}
}

func trimSpace(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && (b[i] == ' ' || b[i] == '\t' || b[i] == '\r' || b[i] == '\n') {
		i++
	}
	for j > i && (b[j-1] == ' ' || b[j-1] == '\t' || b[j-1] == '\r' || b[j-1] == '\n') {
		j--
	}
	return b[i:j]
}

func waitOrTimeout(ch <-chan struct{}, d time.Duration) <-chan struct{} {
	out := make(chan struct{})
	go func() {
		select {
		case <-ch:
		case <-time.After(d):
		}
		close(out)
	}()
	return out
}

// Close ends the child the way every revision's stdio transport says a
// client SHOULD: close its stdin and let it exit by itself, then SIGTERM if
// it has not within a grace, then SIGKILL. Signalling at once, as mcpx used
// to, denied a well-behaved server the clean exit that EOF is meant to give
// it (#203, LV-39). Signals go to the whole process group, so a server's own
// children (chrome-devtools-mcp's browser) end with it.
// https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle#stdio
func (t *StdioTransport) Close() error {
	t.closeMu.Lock()
	if t.closed {
		t.closeMu.Unlock()
		return nil
	}
	t.closed = true
	t.closeMu.Unlock()

	_ = t.stdin.Close()
	if t.cmd.Process == nil {
		return nil
	}
	pgid := -t.cmd.Process.Pid
	select {
	case <-t.exited:
	case <-time.After(t.stdinGrace):
		_ = syscall.Kill(pgid, syscall.SIGTERM)
		select {
		case <-t.exited:
		case <-time.After(t.termGrace):
			_ = syscall.Kill(pgid, syscall.SIGKILL)
			<-waitOrTimeout(t.exited, defaults.StdioKillWait)
		}
	}
	t.outR.Close()
	t.errR.Close()
	return nil
}

// Info describes the child process.
func (t *StdioTransport) Info() string { return t.label }

// Stderr returns recent stderr output, for diagnostics.
func (t *StdioTransport) Stderr(n int) string { return t.stderr.Tail(n) }

// PID returns the child process id (0 if not started).
func (t *StdioTransport) PID() int {
	if t.cmd.Process == nil {
		return 0
	}
	return t.cmd.Process.Pid
}

// ringBuffer keeps the last n bytes written to it.
type ringBuffer struct {
	mu   sync.Mutex
	buf  []byte
	size int
}

func newRingBuffer(size int) *ringBuffer { return &ringBuffer{size: size} }

func (r *ringBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	if len(r.buf) > r.size {
		r.buf = r.buf[len(r.buf)-r.size:]
	}
	return len(p), nil
}

func (r *ringBuffer) Tail(n int) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	b := r.buf
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return strings.TrimSpace(string(b))
}
