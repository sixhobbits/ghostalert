//go:build !unix

package ghostty

import (
	"errors"
	"time"
)

// TerminalOwned reports whether this process can safely talk to its terminal.
func TerminalOwned() bool { return false }

// QueryBackground returns the background colour of the controlling terminal.
func QueryBackground(timeout time.Duration) (string, error) {
	return "", errors.New("reading the terminal colour needs a POSIX terminal")
}
