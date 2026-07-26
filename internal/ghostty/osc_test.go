//go:build unix

package ghostty

import "testing"

func TestParseOSCColor(t *testing.T) {
	cases := []struct {
		name  string
		reply string
		want  string
	}{
		{"four digit components, ST terminated", "\033]11;rgb:f7f7/f3f3/dede\033\\", "#f7f3de"},
		{"BEL terminated", "\033]11;rgb:dede/e7e7/f7f7\a", "#dee7f7"},
		{"two digit components", "\033]11;rgb:f7/de/de\033\\", "#f7dede"},
		{"single digit components are widened", "\033]11;rgb:f/0/8\a", "#ff0088"},
		{"leading noise from a shared tty", "junk\033]11;rgb:0000/0000/0000\033\\", "#000000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseOSCColor(tc.reply)
			if err != nil {
				t.Fatalf("parseOSCColor(%q): %v", tc.reply, err)
			}
			if got != tc.want {
				t.Errorf("parseOSCColor(%q) = %q, want %q", tc.reply, got, tc.want)
			}
		})
	}
}

func TestParseOSCColorRejectsRubbish(t *testing.T) {
	for _, reply := range []string{"", "hello", "\033]11;rgb:f7f7/f3f3\033\\", "\033]11;rgb:zz/00/00\a"} {
		if got, err := parseOSCColor(reply); err == nil {
			t.Errorf("parseOSCColor(%q) = %q, want an error", reply, got)
		}
	}
}
