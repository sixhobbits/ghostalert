//go:build !darwin

package ghostty

// Rename changes a tab's title.
func Rename(pid int, from, to string) error { return ErrUnsupported }
