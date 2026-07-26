//go:build !darwin

package ghostty

// List returns every window of every running Ghostty instance.
func List() ([]Window, error) { return nil, ErrUnsupported }

// Locate returns the tab currently shown in the Ghostty instance owning tty.
func Locate(tty string) (Location, error) { return Location{}, ErrUnsupported }

// LocateSelected returns the showing tab of a Ghostty instance's main window.
func LocateSelected(pid int) (Location, error) { return Location{}, ErrUnsupported }

// Focus raises a tab by title or index.
func Focus(pid int, tabTitle string, tabIndex int) (string, error) { return "", ErrUnsupported }

// Running reports whether at least one Ghostty instance is running.
func Running() bool { return false }
