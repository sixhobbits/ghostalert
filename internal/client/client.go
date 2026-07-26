// Package client talks to a running ghostalert daemon.
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is a thin JSON client for the ghostalert API.
type Client struct {
	Base  string
	Token string
	HTTP  *http.Client
}

// New returns a client for a daemon at base (e.g. "http://127.0.0.1:7337").
func New(base, token string) *Client {
	return &Client{
		Base:  strings.TrimRight(base, "/"),
		Token: token,
		// Focusing a tab stamps a marker title and reads the tab bar back,
		// which can take a couple of seconds on a busy machine.
		HTTP: &http.Client{Timeout: 15 * time.Second},
	}
}

// Get performs a GET and decodes the response into out.
func (c *Client) Get(path string, out any) error {
	req, err := http.NewRequest(http.MethodGet, c.Base+path, nil)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

// Post performs a POST with a JSON body and decodes the response into out.
func (c *Client) Post(path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(http.MethodPost, c.Base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	req.Header.Set("X-Ghostalert-Token", c.Token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w (is `ghostalert serve` running?)", req.Method, req.URL.Path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(b, &e) == nil && e.Error != "" {
			return fmt.Errorf("%s: %s", resp.Status, e.Error)
		}
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(b, out)
}
