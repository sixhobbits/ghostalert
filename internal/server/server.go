// Package server exposes the tile grid over HTTP and drives Ghostty.
package server

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sixhobbits/ghostalert/internal/config"
	"github.com/sixhobbits/ghostalert/internal/ghostty"
	"github.com/sixhobbits/ghostalert/internal/state"
)

//go:embed web
var webFS embed.FS

// Server wires the HTTP API to the tile store and to Ghostty.
type Server struct {
	cfg   *config.Config
	store *state.Store
	log   *log.Logger
}

// New returns an http.Handler serving the ghostalert API and web UI.
func New(cfg *config.Config, store *state.Store, logger *log.Logger) http.Handler {
	s := &Server{cfg: cfg, store: store, log: logger}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /api/state", s.auth(s.handleState))
	mux.HandleFunc("GET /api/events", s.auth(s.handleEvents))
	mux.HandleFunc("POST /api/tile", s.auth(s.handleTile))
	mux.HandleFunc("POST /api/focus", s.auth(s.handleFocus))
	mux.HandleFunc("POST /api/grid", s.auth(s.handleGrid))
	mux.HandleFunc("POST /api/clear", s.auth(s.handleClear))
	mux.HandleFunc("GET /api/tabs", s.auth(s.handleTabs))
	mux.HandleFunc("POST /api/refresh", s.auth(s.handleRefresh))

	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /", http.FileServer(http.FS(sub)))
	return logging(logger, mux)
}

func logging(logger *log.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		if r.URL.Path != "/api/events" {
			logger.Printf("%s %s %s", r.RemoteAddr, r.Method, r.URL.Path)
		}
	})
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-Ghostalert-Token")
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		if token != s.cfg.Token {
			writeErr(w, http.StatusUnauthorized, errors.New("bad or missing token"))
			return
		}
		next(w, r)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "ghostalert"})
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.Snapshot())
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	ch, cancel := s.store.Subscribe()
	defer cancel()

	// Snapshots can arrive out of order: a change published between subscribing
	// and reading the initial state would otherwise land after it and roll the
	// client back. Revisions only ever increase, so drop anything older.
	var sentRev int64 = -1
	send := func(snap state.Snapshot) bool {
		if snap.Rev < sentRev {
			return true
		}
		sentRev = snap.Rev
		b, err := json.Marshal(snap)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "event: state\ndata: %s\n\n", b); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if !send(s.store.Snapshot()) {
		return
	}

	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case snap, ok := <-ch:
			if !ok || !send(snap) {
				return
			}
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

type tileRequest struct {
	Slot     *int    `json:"slot,omitempty"`
	Name     *string `json:"name,omitempty"`
	State    *string `json:"state,omitempty"`
	Message  *string `json:"message,omitempty"`
	TTY      *string `json:"tty,omitempty"`
	TabTitle *string `json:"tabTitle,omitempty"`
	PID      *int    `json:"pid,omitempty"`
	Window   *string `json:"window,omitempty"`
	TabIndex *int    `json:"tabIndex,omitempty"`
}

func (s *Server) handleTile(w http.ResponseWriter, r *http.Request) {
	var req tileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	slot, err := s.resolveSlot(&req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	patch := state.Patch{
		Name:     req.Name,
		State:    req.State,
		Message:  req.Message,
		TTY:      req.TTY,
		TabTitle: req.TabTitle,
		PID:      req.PID,
		Window:   req.Window,
		TabIndex: req.TabIndex,
	}
	tile, err := s.store.Apply(slot, patch)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, tile)
}

// resolveSlot decides which tile a request is talking about, in order of how
// specific the caller was: an explicit slot, then the terminal device the shell
// is on, then a tab title claimed by `ghostalert register`, then the tile name.
// Anything left over lands on a free slot, preferring the one matching the
// tab's position so an adopted grid keeps its natural order.
func (s *Server) resolveSlot(req *tileRequest) (int, error) {
	if req.Slot != nil {
		if *req.Slot < 1 {
			return 0, errors.New("slot must be >= 1")
		}
		return *req.Slot, nil
	}
	if req.TTY != nil && *req.TTY != "" {
		if t, ok := s.store.FindByTTY(*req.TTY); ok {
			return t.Slot, nil
		}
	}
	if req.TabTitle != nil && *req.TabTitle != "" {
		if t, ok := s.store.FindByTabTitle(*req.TabTitle); ok {
			return t.Slot, nil
		}
	}
	if req.Name != nil && *req.Name != "" {
		if t, ok := s.store.FindByName(*req.Name); ok {
			return t.Slot, nil
		}
	}
	known := (req.TTY != nil && *req.TTY != "") ||
		(req.TabTitle != nil && *req.TabTitle != "") ||
		(req.Name != nil && *req.Name != "")
	if !known {
		return 0, errors.New("cannot tell which tile you mean: pass --slot, or run `ghostalert register` in this tab first")
	}
	preferred := 0
	if req.TabIndex != nil {
		preferred = *req.TabIndex
	}
	return s.store.FreeSlot(preferred), nil
}

type focusRequest struct {
	Slot int `json:"slot"`
}

func (s *Server) handleFocus(w http.ResponseWriter, r *http.Request) {
	var req focusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	tile, ok := s.store.Get(req.Slot)
	if !ok {
		writeErr(w, http.StatusNotFound, fmt.Errorf("slot %d is empty", req.Slot))
		return
	}

	title := tile.TabTitle
	if title == "" {
		title = tile.Name
	}
	// A live tty is the freshest source of truth for which Ghostty instance the
	// tab belongs to; the stored pid may be from a previous run of the app.
	pid := tile.PID
	if tile.TTY != "" {
		if p, err := ghostty.AppPIDForTTY(tile.TTY); err == nil {
			pid = p
		}
	}

	via, err := ghostty.Focus(pid, title, tile.TabIndex)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if pid != tile.PID && pid > 0 {
		if _, err := s.store.Apply(tile.Slot, state.Patch{PID: &pid}); err != nil {
			s.log.Printf("update pid for slot %d: %v", tile.Slot, err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "via": via})
}

type gridRequest struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

func (s *Server) handleGrid(w http.ResponseWriter, r *http.Request) {
	var req gridRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Cols < 0 || req.Rows < 0 || req.Cols > 6 || req.Rows > 12 {
		writeErr(w, http.StatusBadRequest, errors.New("cols must be 1-6 and rows 1-12"))
		return
	}
	snap := s.store.SetGrid(req.Cols, req.Rows)
	s.cfg.Cols, s.cfg.Rows = snap.Cols, snap.Rows
	if err := s.cfg.Save(); err != nil {
		s.log.Printf("save config: %v", err)
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) handleClear(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Slot int  `json:"slot"`
		All  bool `json:"all"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var err error
	if req.All {
		err = s.store.ClearAll()
	} else {
		err = s.store.Clear(req.Slot)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, s.store.Snapshot())
}

func (s *Server) handleTabs(w http.ResponseWriter, r *http.Request) {
	windows, err := ghostty.List()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, windows)
}

// handleRefresh rebuilds the grid from a Ghostty window's tab bar. It is both
// first-time setup and the answer to "I closed a tab and renamed another":
// tabs that are still open keep their tile, including its state and the shell
// bound to it, and tiles whose tab is gone disappear.
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Window    int `json:"window"`
		StartSlot int `json:"startSlot"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	windows, err := ghostty.List()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	target, err := pickWindow(windows, req.Window)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	start := req.StartSlot
	if start < 1 {
		start = 1
	}
	writeJSON(w, http.StatusOK, s.rebuild(target, start))
}

// pickWindow resolves a number from `ghostalert tabs`, which numbers every
// window of every Ghostty instance in one list. With no number, it takes the
// window with the most tabs: that is the one worth mirroring.
func pickWindow(windows []ghostty.Window, n int) (ghostty.Window, error) {
	if len(windows) == 0 {
		return ghostty.Window{}, errors.New("Ghostty has no open windows")
	}
	if n > 0 {
		if n > len(windows) {
			return ghostty.Window{}, fmt.Errorf("no Ghostty window %d (there are %d)", n, len(windows))
		}
		return windows[n-1], nil
	}
	best := windows[0]
	for _, win := range windows {
		if len(win.Tabs) > len(best.Tabs) {
			best = win
		}
	}
	return best, nil
}

func (s *Server) rebuild(target ghostty.Window, start int) []state.Tile {
	old := s.store.Tiles()
	claimed := make([]bool, len(old))
	matched := make([]*state.Tile, len(target.Tabs))

	// Title and position together first. Two tabs can share a title — two vim
	// sessions on the same file, say — and matching on the title alone would
	// let them swap states.
	for i, title := range target.Tabs {
		for j := range old {
			if claimed[j] || old[j].TabTitle != title || old[j].TabIndex != i+1 {
				continue
			}
			claimed[j] = true
			matched[i] = &old[j]
			break
		}
	}
	// Then title alone, so closing a tab shifts everything else along without
	// any tile picking up the wrong tab's state.
	for i, title := range target.Tabs {
		if matched[i] != nil {
			continue
		}
		for j := range old {
			if claimed[j] || old[j].TabTitle != title {
				continue
			}
			claimed[j] = true
			matched[i] = &old[j]
			break
		}
	}
	// Then position, which is what identifies a tab that was renamed: same
	// place in the same window, no other tab claiming it.
	for i := range target.Tabs {
		if matched[i] != nil {
			continue
		}
		for j := range old {
			if claimed[j] || old[j].PID != target.PID || old[j].TabIndex != i+1 {
				continue
			}
			claimed[j] = true
			matched[i] = &old[j]
			break
		}
	}

	tiles := make([]state.Tile, 0, len(target.Tabs))
	for i, title := range target.Tabs {
		slot := start + i
		tile := state.Tile{Slot: slot, Name: title, State: state.StateIdle}
		if prev := matched[i]; prev != nil {
			tile = *prev
			tile.Slot = slot
			// A name the tile never had customised should follow the tab; one
			// set with --name is the user's and stays put.
			if prev.Name == "" || prev.Name == prev.TabTitle {
				tile.Name = title
			}
		}
		tile.TabTitle = title
		tile.TabIndex = i + 1
		tile.PID = target.PID
		tile.Window = target.Title
		tiles = append(tiles, tile)
	}

	if err := s.store.Replace(tiles); err != nil {
		s.log.Printf("save after refresh: %v", err)
	}
	return tiles
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

// LANAddr returns a URL the phone can reach, preferring a private IPv4 address
// over loopback.
func LANAddr(listen string) string {
	_, port, err := net.SplitHostPort(listen)
	if err != nil || port == "" {
		port = "7337"
	}
	ip := lanIP()
	return "http://" + net.JoinHostPort(ip, port)
}

func lanIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1"
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if strings.HasPrefix(iface.Name, "utun") || strings.HasPrefix(iface.Name, "awdl") {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
				continue
			}
			return ip4.String()
		}
	}
	return "127.0.0.1"
}

// PortOf extracts the port from a listen address.
func PortOf(listen string) int {
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		return 7337
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		return 7337
	}
	return n
}
