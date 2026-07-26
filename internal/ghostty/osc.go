//go:build unix

package ghostty

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// A tab's colour is not in the accessibility tree, is not written to disk, and
// cannot be screenshotted without Screen Recording permission. The terminal
// itself is the only thing that knows it, and OSC 11 is how you ask.
//
// The reply comes back through the tty's input, so this is only safe when this
// process owns the terminal. Run it while a TUI is in the foreground and that
// program eats the reply, or takes it as keystrokes.

const oscQueryBackground = "\033]11;?\033\\"

// TerminalOwned reports whether this process can safely talk to its terminal.
// Checking that stdin is a character device is not enough: /dev/null passes
// that, and a Claude Code hook or a detached tool has no controlling terminal
// at all. Opening /dev/tty is the question actually being asked.
func TerminalOwned() bool {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer tty.Close()
	return sttyRun(tty, "-g") == nil
}

// QueryBackground returns the background colour of the terminal on the
// controlling tty, as "#rrggbb".
func QueryBackground(timeout time.Duration) (string, error) {
	if !TerminalOwned() {
		return "", errors.New("not running on a terminal: run this in the tab you want to read")
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", err
	}
	defer tty.Close()

	saved, err := sttyRead(tty, "-g")
	if err != nil {
		return "", fmt.Errorf("read terminal settings: %w", err)
	}
	defer func() { _ = sttyRun(tty, saved) }()

	// min 0 / time N makes each read return after N tenths of a second with
	// whatever has arrived, so a terminal that ignores the query cannot wedge
	// this on a blocking read.
	tenths := int(timeout / (100 * time.Millisecond))
	if tenths < 1 {
		tenths = 1
	}
	if err := sttyRun(tty, "raw", "-echo", "min", "0", "time", strconv.Itoa(tenths)); err != nil {
		return "", fmt.Errorf("set terminal to raw mode: %w", err)
	}

	if _, err := tty.WriteString(oscQueryBackground); err != nil {
		return "", err
	}

	var reply []byte
	deadline := time.Now().Add(timeout + 500*time.Millisecond)
	buf := make([]byte, 64)
	for time.Now().Before(deadline) {
		n, err := tty.Read(buf)
		if n > 0 {
			reply = append(reply, buf[:n]...)
			if bytes.ContainsAny(reply, "\a\\") && bytes.Contains(reply, []byte("rgb:")) {
				break
			}
		}
		if err != nil {
			break
		}
	}
	if len(reply) == 0 {
		return "", errors.New("the terminal did not answer the colour query")
	}
	return parseOSCColor(string(reply))
}

// parseOSCColor reads the "rgb:f7f7/f3f3/dede" form terminals reply with. The
// components can be 1 to 4 hex digits wide and are scaled to 8 bits.
func parseOSCColor(reply string) (string, error) {
	i := strings.Index(reply, "rgb:")
	if i < 0 {
		return "", fmt.Errorf("unexpected reply %q", strings.TrimSpace(reply))
	}
	rest := reply[i+len("rgb:"):]
	if end := strings.IndexAny(rest, "\a\033"); end >= 0 {
		rest = rest[:end]
	}
	parts := strings.Split(strings.TrimSpace(rest), "/")
	if len(parts) != 3 {
		return "", fmt.Errorf("unexpected colour %q", rest)
	}
	out := make([]byte, 0, 7)
	out = append(out, '#')
	for _, p := range parts {
		if p == "" || len(p) > 4 {
			return "", fmt.Errorf("unexpected colour component %q", p)
		}
		v, err := strconv.ParseUint(p, 16, 32)
		if err != nil {
			return "", fmt.Errorf("unexpected colour component %q", p)
		}
		// Widen or narrow to 8 bits: "f7" and "f7f7" both mean 0xf7.
		max := uint64(1)<<(4*len(p)) - 1
		scaled := (v*255 + max/2) / max
		out = append(out, "0123456789abcdef"[scaled>>4], "0123456789abcdef"[scaled&0xf])
	}
	return string(out), nil
}

func sttyRead(tty *os.File, args ...string) (string, error) {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = tty
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func sttyRun(tty *os.File, args ...string) error {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = tty
	return cmd.Run()
}
