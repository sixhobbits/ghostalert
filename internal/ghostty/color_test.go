//go:build unix

package ghostty

import (
	"os"
	"path/filepath"
	"testing"
)

// SetColors opens the tty by path, so a plain file stands in for one here.
func writeTo(t *testing.T, background string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tty")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetColors(path, background); err != nil {
		t.Fatalf("SetColors(%q): %v", background, err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSetColorsWritesBackgroundAndReadableForeground(t *testing.T) {
	// The rainbow palette is light, so the ink has to go dark.
	got := writeTo(t, "#f7f3de")
	want := "\033]11;#f7f3de\033\\\033]10;#4a4842\033\\"
	if got != want {
		t.Errorf("SetColors wrote %q, want %q", got, want)
	}
}

func TestSetColorsLightensInkOnADarkBackground(t *testing.T) {
	got := writeTo(t, "#101014")
	want := "\033]11;#101014\033\\\033]10;#b7b7b8\033\\"
	if got != want {
		t.Errorf("SetColors wrote %q, want %q", got, want)
	}
}

func TestSetColorsRejectsBadInput(t *testing.T) {
	if err := SetColors("", "#ffffff"); err == nil {
		t.Error("an empty tty should be rejected")
	}
	if err := SetColors(filepath.Join(t.TempDir(), "tty"), "chartreuse"); err == nil {
		t.Error("a non-hex colour should be rejected")
	}
}
