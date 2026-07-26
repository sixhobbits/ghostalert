//go:build darwin

package ghostty

import (
	"errors"
	"fmt"
	"strings"
)

// Ghostty ships an AppleScript dictionary, which is a better interface than the
// accessibility tree: it names tabs and terminals by id, selects a tab without
// clicking anything, and can set a tab's title. It needs no Accessibility
// permission either.
//
// It has one limit that keeps the accessibility path alive. Apple events are
// addressed to a bundle identifier, so with several Ghostty processes running —
// a hotkey window started with its own --config-file is one — only whichever
// instance the system has registered answers. The others are invisible here and
// reachable only through the accessibility tree.

// ErrNotScriptable means the tab exists but is in a Ghostty instance that Apple
// events cannot reach.
var ErrNotScriptable = errors.New("that tab is in a Ghostty instance AppleScript cannot address")

const scriptSelectTab = `
on run argv
	set wanted to item 1 of argv
	tell application "Ghostty"
		repeat with w in windows
			repeat with t in tabs of w
				if (name of t) as text is wanted then
					activate window w
					select tab t
					return "ok"
				end if
			end repeat
		end repeat
	end tell
	return ""
end run
`

const scriptSetTitle = `
on run argv
	set wanted to item 1 of argv
	set newTitle to item 2 of argv
	tell application "Ghostty"
		repeat with w in windows
			repeat with t in tabs of w
				if (name of t) as text is wanted then
					perform action ("set_tab_title:" & newTitle) on (focused terminal of t)
					return "ok"
				end if
			end repeat
		end repeat
	end tell
	return ""
end run
`

const scriptListTabs = `
tell application "Ghostty"
	set out to ""
	repeat with w in windows
		repeat with t in tabs of w
			set out to out & (name of t) & linefeed
		end repeat
	end repeat
	return out
end tell
`

// Scriptable reports whether a tab with this title is in the Ghostty instance
// Apple events reach.
func Scriptable(tabTitle string) bool {
	if tabTitle == "" {
		return false
	}
	out, err := osa(scriptListTabs)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		if line == tabTitle {
			return true
		}
	}
	return false
}

// SelectTab raises and selects a tab by title without touching the
// accessibility tree. It reports whether it found one.
func SelectTab(tabTitle string) (bool, error) {
	if tabTitle == "" {
		return false, nil
	}
	out, err := osa(scriptSelectTab, tabTitle)
	if err != nil {
		return false, err
	}
	return out == "ok", nil
}

// SetTabTitle renames a tab. Ghostty treats a title set this way as an
// override, which is exactly what makes it stick: nothing running in the tab
// can paint over it afterwards.
func SetTabTitle(tabTitle, newTitle string) error {
	if tabTitle == "" || newTitle == "" {
		return errors.New("need both the current and the new title")
	}
	out, err := osa(scriptSetTitle, tabTitle, newTitle)
	if err != nil {
		return err
	}
	if out != "ok" {
		return fmt.Errorf("%w: %q", ErrNotScriptable, tabTitle)
	}
	return nil
}
