//go:build darwin

package cli

import "syscall"

// The terminal attribute ioctls on macOS.
const (
	ioctlGetTermios = syscall.TIOCGETA
	ioctlSetTermios = syscall.TIOCSETA
)
