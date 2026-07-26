package config

import "testing"

func TestResolveColor(t *testing.T) {
	cases := map[string]string{
		"yellow":  "#f7f3de",
		"YELLOW":  "#f7f3de",
		" blue ":  "#dee7f7",
		"grey":    "#cccccc",
		"#F7DEDE": "#f7dede",
		"f7dede":  "#f7dede",
		"#abc":    "#aabbcc",
	}
	for in, want := range cases {
		got, err := ResolveColor(in)
		if err != nil {
			t.Errorf("ResolveColor(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ResolveColor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveColorRejectsRubbish(t *testing.T) {
	for _, in := range []string{"", "chartreuse", "#12345", "#gggggg"} {
		if got, err := ResolveColor(in); err == nil {
			t.Errorf("ResolveColor(%q) = %q, want an error", in, got)
		}
	}
}
