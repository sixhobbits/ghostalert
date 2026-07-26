//go:build darwin

package ghostty

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Renaming a tab has no clean interface. Ghostty publishes its tabs to the
// accessibility API read-only — AXTitle reports settable=false and writing to
// it is silently ignored — and the "Change Tab Title" prompt is drawn inside
// the GPU surface, so it adds nothing to the accessibility tree to type into.
//
// What does work is driving the menu the way a person would: select the tab,
// open the prompt, and paste. Typing is not an option because System Events
// drops anything outside the basic plane, which is every coloured square. The
// paste goes through the Edit menu rather than ⌘V, which the prompt ignores.

const renameScript = `
on run argv
	set wantPid to item 1 of argv
	set tabName to item 2 of argv
	tell application "System Events"
		repeat with p in (every process whose name is "Ghostty")
			set okProc to true
			if wantPid is not "" then
				set okProc to false
				try
					set okProc to (((unix id of p) as text) is wantPid)
				end try
			end if
			if okProc then
				repeat with w in windows of p
					set ns to {}
					try
						set ns to name of radio buttons of tab group 1 of w
					end try
					repeat with i from 1 to count of ns
						if ((item i of ns) as text) is tabName then
							set frontmost of p to true
							delay 0.3
							try
								perform action "AXRaise" of w
							end try
							click radio button i of tab group 1 of w
							delay 0.3
							click menu item "Change Tab Title..." of menu 1 of menu bar item "View" of menu bar 1 of p
							delay 0.7
							click menu item "Paste" of menu 1 of menu bar item "Edit" of menu bar 1 of p
							delay 0.4
							key code 36
							return "ok"
						end if
					end repeat
				end repeat
			end if
		end repeat
	end tell
	return ""
end run
`

// Rename changes a tab's title, preferring Ghostty's scripting interface and
// falling back to driving its menu. It confirms the result by reading the tab
// bar back, because the menu route is typed at a prompt nothing can inspect.
func Rename(pid int, from, to string) error {
	if from == "" || to == "" {
		return fmt.Errorf("need both the current and the new title")
	}
	if err := SetTabTitle(from, to); err == nil {
		return nil
	}

	restore, err := clipboard()
	if err == nil {
		defer setClipboard(restore)
	}
	if err := setClipboard(to); err != nil {
		return fmt.Errorf("put the title on the clipboard: %w", err)
	}

	pidArg := ""
	if pid > 0 {
		pidArg = strconv.Itoa(pid)
	}
	out, err := osa(renameScript, pidArg, from)
	if err != nil {
		return err
	}
	if out != "ok" {
		return fmt.Errorf("no Ghostty tab titled %q", from)
	}

	// The prompt is invisible to the accessibility API, so the only proof it
	// took is the tab bar itself. Give the UI a moment to settle first.
	for i := 0; i < 10; i++ {
		time.Sleep(150 * time.Millisecond)
		windows, err := List()
		if err != nil {
			continue
		}
		for _, w := range windows {
			for _, tab := range w.Tabs {
				if tab == to {
					return nil
				}
			}
		}
	}
	return fmt.Errorf("renamed %q but the tab bar still does not show %q", from, to)
}

func clipboard() (string, error) {
	out, err := exec.Command("pbpaste").Output()
	return string(out), err
}

func setClipboard(s string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(s)
	return cmd.Run()
}
