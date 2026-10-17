//go:build !(linux || darwin)

package cli

import (
	"errors"
	"os"
)

// readHidden refuses where echo cannot be turned off here. A value is then
// given on standard input, which is never echoed by the terminal in the first
// place: the input is piped, not typed.
func readHidden(_ *os.File) (string, error) {
	return "", errors.New("this terminal cannot hide what is typed; pipe the value in on standard input")
}
