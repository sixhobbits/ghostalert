//go:build darwin

package ghostty

import "fmt"

// Rename changes a tab's title through Ghostty's scripting interface.
//
// There is no safe second route. The accessibility tab bar is read-only —
// AXTitle reports settable=false and writing it is ignored — and Ghostty draws
// the "Change Tab Title" prompt inside its GPU surface, so it adds nothing to
// the accessibility tree to type into. Driving that prompt means activating the
// window and synthesising keystrokes, which takes the keyboard away from
// whoever is at the machine and misfires into another window if they switch
// mid-sequence. A background tool has no business doing that.
func Rename(pid int, from, to string) error {
	if from == "" || to == "" {
		return fmt.Errorf("need both the current and the new title")
	}
	return SetTabTitle(from, to)
}
