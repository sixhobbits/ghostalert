// Package config loads and persists ghostalert's on-disk configuration.
package config

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// DefaultPalette matches the tab colours used by the Ghostty "rainbow" window
// (see hack/open-rainbow.sh): yellow, blue, red, purple, orange, green, white,
// black, pink, then a cyan for slot 10+.
var DefaultPalette = []string{
	"#f7f3de",
	"#dee7f7",
	"#f7dede",
	"#e7def7",
	"#f7ebde",
	"#def7e2",
	"#fafafa",
	"#cccccc",
	"#f7deeb",
	"#def3f7",
}

// Config is the daemon's persisted configuration.
type Config struct {
	// Addr is the listen address for the daemon, e.g. ":7337".
	Addr string `json:"addr"`
	// Token is a shared secret every API request must present.
	Token string `json:"token"`
	// Cols and Rows describe the phone's tile grid.
	Cols int `json:"cols"`
	Rows int `json:"rows"`
	// Palette supplies default tile colours, indexed by slot - 1.
	Palette []string `json:"palette"`

	path string
}

// Dir returns the directory holding config.json and state.json.
func Dir() string {
	if d := os.Getenv("GHOSTALERT_HOME"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "ghostalert")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".ghostalert"
	}
	return filepath.Join(home, ".config", "ghostalert")
}

// Path returns the config file path.
func Path() string { return filepath.Join(Dir(), "config.json") }

// Load reads the config, creating it with defaults on first run.
func Load() (*Config, error) {
	c := &Config{path: Path()}
	b, err := os.ReadFile(c.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		c.applyDefaults()
		return c, c.Save()
	case err != nil:
		return nil, err
	}
	if err := json.Unmarshal(b, c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", c.path, err)
	}
	c.applyDefaults()
	return c, nil
}

func (c *Config) applyDefaults() {
	if c.Addr == "" {
		c.Addr = ":7337"
	}
	if c.Cols <= 0 {
		c.Cols = 2
	}
	if c.Rows <= 0 {
		c.Rows = 5
	}
	if len(c.Palette) == 0 {
		c.Palette = append([]string(nil), DefaultPalette...)
	}
	if c.Token == "" {
		c.Token = newToken()
	}
	if c.path == "" {
		c.path = Path()
	}
}

// Save writes the config back to disk.
func (c *Config) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, append(b, '\n'), 0o600)
}

// ColorFor returns the default colour for a slot (1-based).
func (c *Config) ColorFor(slot int) string {
	if slot < 1 || len(c.Palette) == 0 {
		return "#dddddd"
	}
	return c.Palette[(slot-1)%len(c.Palette)]
}

const tokenAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"

func newToken() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand does not fail in practice; a fixed token would be worse
		// than a panic here because it would silently weaken the daemon.
		panic("ghostalert: cannot generate token: " + err.Error())
	}
	for i, v := range b {
		b[i] = tokenAlphabet[int(v)%len(tokenAlphabet)]
	}
	return string(b)
}
