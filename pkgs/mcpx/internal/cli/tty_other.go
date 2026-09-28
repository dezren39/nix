//go:build !(darwin || linux || freebsd || netbsd || openbsd)

package cli

import "os"

// isTerminal is conservative where the ioctl is unavailable: without a way to
// ask, assume not interactive, so a prompt loop never starts somewhere it
// cannot be answered.
func isTerminal(f *os.File) bool { return false }
