//go:build !darwin

package ghostty

import "errors"

// ErrNotScriptable means the tab is in a Ghostty instance Apple events cannot
// reach.
var ErrNotScriptable = errors.New("Ghostty scripting is only available on macOS")

// Scriptable reports whether a tab is reachable through Ghostty's scripting API.
func Scriptable(tabTitle string) bool { return false }

// SelectTab raises and selects a tab by title.
func SelectTab(tabTitle string) (bool, error) { return false, ErrUnsupported }

// SetTabTitle renames a tab.
func SetTabTitle(tabTitle, newTitle string) error { return ErrUnsupported }
