//go:build darwin

package ghostty

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Ghostty exposes its native tab bar to the accessibility API as a tab group of
// radio buttons, one per tab: the button's name is the tab title and its value
// says whether that tab is showing. Clicking the button switches tab, which
// beats sending Cmd+N keystrokes because it addresses more than nine tabs and
// does not depend on which window is frontmost.
//
// Every script below iterates *all* processes named Ghostty. A single "tell
// process "Ghostty"" only ever reaches one of them, which silently ignores
// every window belonging to a second instance (for example a hotkey window
// started with its own --config-file).

const listScript = `
tell application "System Events"
	set out to ""
	repeat with p in (every process whose name is "Ghostty")
		set pid to ""
		try
			set pid to (unix id of p) as text
		end try
		set wi to 0
		repeat with w in windows of p
			set wi to wi + 1
			set wn to ""
			try
				set wn to (name of w) as text
			end try
			set isMain to "0"
			try
				if (value of attribute "AXMain" of w) is true then set isMain to "1"
			end try
			set out to out & "W" & tab & pid & tab & (wi as text) & tab & isMain & tab & wn & linefeed
			-- Read names and values with the plural accessors. Holding on to
			-- individual radio button references while looping over several
			-- processes resolves them against the wrong process and every
			-- property read fails.
			set ns to {}
			set vs to {}
			try
				set ns to name of radio buttons of tab group 1 of w
				set vs to value of radio buttons of tab group 1 of w
			end try
			if (count of ns) is 0 then
				-- A window with a single tab has no tab bar; treat it as tab 1.
				set out to out & "T" & tab & pid & tab & (wi as text) & tab & "1" & tab & "1" & tab & wn & linefeed
			else
				repeat with i from 1 to count of ns
					set sel to "0"
					try
						if (item i of vs) is true then set sel to "1"
					end try
					set tn to ""
					try
						set tn to (item i of ns) as text
					end try
					set out to out & "T" & tab & pid & tab & (wi as text) & tab & (i as text) & tab & sel & tab & tn & linefeed
				end repeat
			end if
		end repeat
	end repeat
	return out
end tell
`

// Every sweep re-runs "every process whose name is Ghostty" and works on the
// loop variable directly. Stashing those process objects in a list first breaks
// them: reads against the second instance start failing or, worse, answer for
// the first one.
const focusScript = `
on sweep(wantPid, tname, idx)
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
					if tname is not "" then
						repeat with i from 1 to count of ns
							if ((item i of ns) as text) is tname then
								try
									set frontmost of p to true
								end try
								try
									perform action "AXRaise" of w
								end try
								try
									click radio button i of tab group 1 of w
								end try
								return "tab-title"
							end if
						end repeat
						if (count of ns) is 0 then
							set wn to ""
							try
								set wn to (name of w) as text
							end try
							if wn is tname then
								try
									set frontmost of p to true
								end try
								try
									perform action "AXRaise" of w
								end try
								return "window-title"
							end if
						end if
					else if idx > 0 then
						if idx <= (count of ns) then
							try
								set frontmost of p to true
							end try
							try
								perform action "AXRaise" of w
							end try
							try
								click radio button idx of tab group 1 of w
							end try
							return "tab-index"
						end if
					end if
				end repeat
			end if
		end repeat
	end tell
	return ""
end sweep

on run argv
	set wantPid to item 1 of argv
	set tname to item 2 of argv
	set idx to (item 3 of argv) as integer

	tell application "System Events"
		if (count of (every process whose name is "Ghostty")) is 0 then return "no-process"
	end tell

	-- Most specific first: the right title in the instance this tab belongs to,
	-- then that title anywhere, then position, which is all that is left once a
	-- tab has been renamed.
	if tname is not "" then
		if wantPid is not "" then
			set r to my sweep(wantPid, tname, 0)
			if r is not "" then return r
		end if
		set r to my sweep("", tname, 0)
		if r is not "" then return r
	end if
	if idx > 0 then
		if wantPid is not "" then
			set r to my sweep(wantPid, "", idx)
			if r is not "" then return r
		end if
		set r to my sweep("", "", idx)
		if r is not "" then return r
	end if
	return "not-found"
end run
`

func osa(script string, args ...string) (string, error) {
	cmd := exec.Command("osascript", append([]string{"-"}, args...)...)
	cmd.Stdin = strings.NewReader(script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if strings.Contains(msg, "-1719") || strings.Contains(msg, "-25211") || strings.Contains(msg, "assistive") {
			return "", errors.New("accessibility permission denied: allow the app that runs ghostalert in System Settings > Privacy & Security > Accessibility")
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New("osascript: " + msg)
	}
	return strings.TrimRight(stdout.String(), "\n"), nil
}

// List returns every window of every running Ghostty instance.
func List() ([]Window, error) {
	out, err := osa(listScript)
	if err != nil {
		return nil, err
	}
	var windows []Window
	index := map[string]int{}
	key := func(pid, win string) string { return pid + "/" + win }

	for _, line := range strings.Split(out, "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) < 5 {
			continue
		}
		switch parts[0] {
		case "W":
			pid, _ := strconv.Atoi(parts[1])
			wi, _ := strconv.Atoi(parts[2])
			index[key(parts[1], parts[2])] = len(windows)
			windows = append(windows, Window{
				PID:   pid,
				Index: wi,
				Main:  parts[3] == "1",
				Title: strings.Join(parts[4:], "\t"),
			})
		case "T":
			if len(parts) < 6 {
				continue
			}
			pos, ok := index[key(parts[1], parts[2])]
			if !ok {
				continue
			}
			tabIdx, _ := strconv.Atoi(parts[3])
			title := strings.Join(parts[5:], "\t")
			windows[pos].Tabs = append(windows[pos].Tabs, title)
			if parts[4] == "1" {
				windows[pos].Selected = tabIdx
			}
		}
	}
	return windows, nil
}

// Locate returns the tab currently shown in the window of the Ghostty instance
// that owns tty. Ghostty offers no way to ask "which tab is this shell in", and
// tabs given a title by hand ignore OSC title sequences, which rules out
// stamping a marker to find them. The showing tab is the right answer whenever
// this is run from the tab being registered, which is how registration works.
func Locate(tty string) (Location, error) {
	pid, err := AppPIDForTTY(tty)
	if err != nil {
		return Location{}, err
	}
	return LocateSelected(pid)
}

// LocateSelected returns the showing tab of a Ghostty instance's main window.
func LocateSelected(pid int) (Location, error) {
	windows, err := List()
	if err != nil {
		return Location{}, err
	}
	var candidate *Window
	for i := range windows {
		w := &windows[i]
		if w.PID != pid || w.Selected == 0 {
			continue
		}
		if candidate == nil || (w.Main && !candidate.Main) {
			candidate = w
		}
	}
	if candidate == nil {
		return Location{}, fmt.Errorf("no Ghostty window found for process %d", pid)
	}
	loc := Location{
		PID:         candidate.PID,
		WindowIndex: candidate.Index,
		WindowTitle: candidate.Title,
		Tab:         candidate.Selected,
	}
	if candidate.Selected >= 1 && candidate.Selected <= len(candidate.Tabs) {
		loc.TabTitle = candidate.Tabs[candidate.Selected-1]
	}
	return loc, nil
}

// Focus raises a tab, preferring an exact tab-title match within the Ghostty
// instance pid and falling back to the tab index. Either pid or tabIndex may be
// zero and tabTitle may be empty.
func Focus(pid int, tabTitle string, tabIndex int) (string, error) {
	if tabTitle == "" && tabIndex <= 0 {
		return "", errors.New("nothing to focus: no tab title and no tab index")
	}
	pidArg := ""
	if pid > 0 {
		pidArg = strconv.Itoa(pid)
	}
	out, err := osa(focusScript, pidArg, tabTitle, strconv.Itoa(tabIndex))
	if err != nil {
		return "", err
	}
	switch out {
	case "no-process":
		return "", errors.New("Ghostty is not running")
	case "not-found":
		return "", fmt.Errorf("no Ghostty tab matched title %q or index %d", tabTitle, tabIndex)
	}
	return out, nil
}

// Running reports whether at least one Ghostty instance is running.
func Running() bool {
	out, err := osa(`tell application "System Events" to return (count of (every process whose name is "Ghostty")) as text`)
	if err != nil {
		return false
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	return err == nil && n > 0
}
