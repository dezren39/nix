//go:build darwin || linux || freebsd || netbsd || openbsd

package cli

import (
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

// isTerminal reports whether a file is a terminal.
//
// The obvious test -- ModeCharDevice -- is wrong, and wrong in the direction
// that matters: /dev/null is a character device, so a command run with stdin
// redirected from it reads as interactive and then blocks on a prompt nobody
// is there to answer.
//
// Asking the kernel for the terminal attributes is the actual question. It
// succeeds only for a real tty. Done with a raw ioctl rather than a
// dependency because it is a dozen lines and one constant.
func isTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	var termios [256]byte
	_, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, f.Fd(),
		uintptr(tcGetAttr()), uintptr(unsafe.Pointer(&termios[0])), 0, 0, 0)
	return errno == 0
}

// tcGetAttr is the "read terminal attributes" ioctl, which differs by system.
func tcGetAttr() uint {
	switch runtime.GOOS {
	case "linux":
		return 0x5401 // TCGETS
	default:
		return 0x40487413 // TIOCGETA, on the BSDs and macOS
	}
}
