//go:build unix

package ghostty

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Writing OSC 10 and 11 to a terminal device recolours that surface. Unlike the
// colour *query*, this is output only: nothing comes back through the tty's
// input, so it is safe to do to a tab running anything at all. It is the same
// mechanism the Ghostty rainbow-window script uses.

// SetColors repaints the terminal on tty with a background colour and a
// foreground dark enough to read against it.
func SetColors(tty, background string) error {
	if tty == "" {
		return errors.New("no tty")
	}
	r, g, b, err := parseHex(background)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(tty, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	fg := readableInk(r, g, b)
	_, err = fmt.Fprintf(f, "\033]11;#%02x%02x%02x\033\\\033]10;#%s\033\\", r, g, b, fg)
	return err
}

// readableInk returns a foreground that keeps its hue but is dark or light
// enough to read, matching how the rainbow palette pairs its colours.
func readableInk(r, g, b uint8) string {
	// Rec. 601 luma is good enough to decide which way to go.
	luma := (299*int(r) + 587*int(g) + 114*int(b)) / 1000
	if luma > 128 {
		return fmt.Sprintf("%02x%02x%02x", scale(r, 0.30), scale(g, 0.30), scale(b, 0.30))
	}
	return fmt.Sprintf("%02x%02x%02x", lift(r), lift(g), lift(b))
}

func scale(v uint8, f float64) uint8 { return uint8(float64(v) * f) }

func lift(v uint8) uint8 {
	x := int(v) + (255-int(v))*7/10
	if x > 255 {
		x = 255
	}
	return uint8(x)
}

func parseHex(s string) (uint8, uint8, uint8, error) {
	h := strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(h) != 6 {
		return 0, 0, 0, fmt.Errorf("%q is not a #rrggbb colour", s)
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("%q is not a #rrggbb colour", s)
	}
	return uint8(v >> 16), uint8(v >> 8), uint8(v), nil
}
