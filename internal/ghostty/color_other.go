//go:build !unix

package ghostty

import "errors"

// SetColors repaints the terminal on tty with a background colour.
func SetColors(tty, background string) error {
	return errors.New("recolouring a terminal needs a POSIX terminal device")
}
