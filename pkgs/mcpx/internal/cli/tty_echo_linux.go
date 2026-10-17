//go:build linux

package cli

import "syscall"

// The terminal attribute ioctls on Linux.
const (
	ioctlGetTermios = syscall.TCGETS
	ioctlSetTermios = syscall.TCSETS
)
