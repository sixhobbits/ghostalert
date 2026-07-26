// Package ghostty locates and activates Ghostty terminal tabs.
package ghostty

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// ErrUnsupported is returned on platforms with no Ghostty control support.
var ErrUnsupported = errors.New("ghostty control is only implemented on macOS")

// Window is one Ghostty window and the titles of its tabs, in order.
type Window struct {
	PID   int      `json:"pid"`
	Index int      `json:"index"`
	Title string   `json:"title"`
	Main  bool     `json:"main"`
	Tabs  []string `json:"tabs"`
	// Selected is the 1-based index of the tab currently shown, or 0.
	Selected int `json:"selected"`
}

// Location identifies a single tab.
type Location struct {
	PID         int    `json:"pid"`
	WindowIndex int    `json:"windowIndex"`
	WindowTitle string `json:"windowTitle"`
	Tab         int    `json:"tab"`
	TabTitle    string `json:"tabTitle"`
}

// CurrentTTY walks up the process tree until it finds a controlling terminal.
// The Bash tool, Claude Code hooks and other wrappers have no tty of their own,
// but one of their ancestors does.
func CurrentTTY() string {
	pid := os.Getpid()
	for i := 0; i < 12; i++ {
		out, err := exec.Command("ps", "-o", "tty=,ppid=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			return ""
		}
		fields := strings.Fields(string(out))
		if len(fields) < 2 {
			return ""
		}
		tty, parent := fields[0], fields[1]
		if tty != "" && tty != "??" && tty != "?" {
			return "/dev/" + tty
		}
		pid, err = strconv.Atoi(parent)
		if err != nil || pid <= 1 {
			return ""
		}
	}
	return ""
}

type procInfo struct {
	pid  int
	ppid int
	tty  string
	args string
}

func processTable() ([]procInfo, error) {
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,tty=,args=").Output()
	if err != nil {
		return nil, err
	}
	var procs []procInfo
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 4 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		procs = append(procs, procInfo{
			pid:  pid,
			ppid: ppid,
			tty:  fields[2],
			args: strings.Join(fields[3:], " "),
		})
	}
	return procs, sc.Err()
}

// AppPIDForTTY returns the pid of the Ghostty application instance that owns a
// terminal device. Several Ghostty instances can run at once (a hotkey window
// launched with its own --config-file is a separate process), so knowing which
// one a shell belongs to is what makes tab lookup unambiguous.
func AppPIDForTTY(tty string) (int, error) {
	if tty == "" {
		return 0, errors.New("no tty")
	}
	name := strings.TrimPrefix(tty, "/dev/")

	procs, err := processTable()
	if err != nil {
		return 0, err
	}
	byPID := make(map[int]procInfo, len(procs))
	for _, p := range procs {
		byPID[p.pid] = p
	}

	for _, p := range procs {
		if p.tty != name {
			continue
		}
		cur := p
		for i := 0; i < 16; i++ {
			parent, ok := byPID[cur.ppid]
			if !ok {
				break
			}
			if isGhosttyApp(parent.args) {
				return parent.pid, nil
			}
			cur = parent
		}
	}
	return 0, errors.New("no Ghostty process owns " + tty)
}

func isGhosttyApp(args string) bool {
	return strings.Contains(args, "Ghostty.app/Contents/MacOS")
}
