//go:build linux || darwin

package cli

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

// readHidden reads one line from a terminal with echo turned off. Echo is put
// back before it returns, whatever happened in between, so a failed read cannot
// leave the person's terminal silent.
//
// Done with the terminal ioctls directly, as isTerminal is, rather than with a
// dependency that exists for this one call.
func readHidden(f *os.File) (string, error) {
	fd := f.Fd()
	var saved syscall.Termios
	if err := termios(fd, ioctlGetTermios, &saved); err != nil {
		return "", err
	}
	quiet := saved
	quiet.Lflag &^= syscall.ECHO
	if err := termios(fd, ioctlSetTermios, &quiet); err != nil {
		return "", err
	}
	defer func() { _ = termios(fd, ioctlSetTermios, &saved) }()

	line, err := bufio.NewReader(f).ReadString('\n')
	if err != nil && !(errors.Is(err, io.EOF) && line != "") {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func termios(fd uintptr, request uintptr, t *syscall.Termios) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request, uintptr(unsafe.Pointer(t))); errno != 0 {
		return errno
	}
	return nil
}
