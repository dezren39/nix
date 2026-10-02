package e2e_test

import (
	"bytes"
	"sync"
)

// syncBuffer is a buffer safe to read while a child process is still writing
// to it.
//
// os/exec copies a command's output on a goroutine of its own and stops only
// at EOF, so reading a bytes.Buffer given as cmd.Stdout before Wait returns
// races that copier. Two streaming tests do exactly that on purpose -- they
// report what the child has said so far while it is deliberately still
// running -- and the race detector reported all four of its findings against
// the suite there, which hid anything the product might have raced on.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *syncBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}
