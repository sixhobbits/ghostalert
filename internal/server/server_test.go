package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sixhobbits/ghostalert/internal/config"
	"github.com/sixhobbits/ghostalert/internal/ghostty"
	"github.com/sixhobbits/ghostalert/internal/state"
)

const token = "testtoken"

func newTestServer(t *testing.T) (http.Handler, *state.Store) {
	t.Helper()
	cfg := &config.Config{
		Addr:    ":0",
		Token:   token,
		Cols:    2,
		Rows:    5,
		Palette: config.DefaultPalette,
	}
	store, err := state.New(filepath.Join(t.TempDir(), "state.json"), "testhost", 2, 5)
	if err != nil {
		t.Fatal(err)
	}
	return New(cfg, store, log.New(io.Discard, "", 0)), store
}

func post(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("X-Ghostalert-Token", token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAuthRequired(t *testing.T) {
	h, _ := newTestServer(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/state", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: got %d, want 401", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/state?token=wrong", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong token: got %d, want 401", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/state?token="+token, nil))
	if rec.Code != http.StatusOK {
		t.Errorf("good token: got %d, want 200", rec.Code)
	}
}

func TestHealthAndUIAreUnauthenticated(t *testing.T) {
	h, _ := newTestServer(t)
	for _, path := range []string{"/health", "/"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: got %d, want 200", path, rec.Code)
		}
	}
}

func TestTileBySlotThenByTTY(t *testing.T) {
	h, store := newTestServer(t)

	rec := post(t, h, "/api/tile", `{"slot":2,"name":"BRYNTUM","tty":"/dev/ttys009","state":"working"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var tile state.Tile
	if err := json.Unmarshal(rec.Body.Bytes(), &tile); err != nil {
		t.Fatal(err)
	}
	if tile.Slot != 2 || tile.Color != config.DefaultPalette[1] {
		t.Fatalf("unexpected tile %+v", tile)
	}

	// A later update carrying only the tty must land on the same tile.
	rec = post(t, h, "/api/tile", `{"tty":"/dev/ttys009","state":"waiting","message":"approve?"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body)
	}
	got, ok := store.Get(2)
	if !ok || got.State != "waiting" || got.Message != "approve?" || got.Name != "BRYNTUM" {
		t.Fatalf("unexpected tile %+v (ok=%v)", got, ok)
	}
}

func TestTileWithNothingToMatchOnIsRejected(t *testing.T) {
	h, _ := newTestServer(t)
	rec := post(t, h, "/api/tile", `{"state":"working"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "register") {
		t.Errorf("the error should say how to fix it, got %s", rec.Body)
	}
}

func TestTileRegistrationPrefersTabPosition(t *testing.T) {
	h, store := newTestServer(t)
	rec := post(t, h, "/api/tile",
		`{"tty":"/dev/ttys009","tabTitle":"RITZA","tabIndex":6,"pid":1582,"state":"idle"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	tile, ok := store.Get(6)
	if !ok {
		t.Fatalf("tile should have landed on slot 6, snapshot: %+v", store.Snapshot().Tiles)
	}
	if tile.Name != "RITZA" || tile.TabTitle != "RITZA" || tile.PID != 1582 {
		t.Errorf("unexpected tile %+v", tile)
	}
}

func TestGridValidation(t *testing.T) {
	h, _ := newTestServer(t)
	if rec := post(t, h, "/api/grid", `{"cols":9,"rows":4}`); rec.Code != http.StatusBadRequest {
		t.Errorf("9 columns should be rejected, got %d", rec.Code)
	}
	rec := post(t, h, "/api/grid", `{"cols":3,"rows":8}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var snap state.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Cols != 3 || snap.Rows != 8 || len(snap.Tiles) != 24 {
		t.Errorf("unexpected grid %dx%d with %d tiles", snap.Cols, snap.Rows, len(snap.Tiles))
	}
}

func TestFocusOnEmptySlotIs404(t *testing.T) {
	h, _ := newTestServer(t)
	if rec := post(t, h, "/api/focus", `{"slot":4}`); rec.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", rec.Code)
	}
}

func TestEventsStreamsSnapshots(t *testing.T) {
	h, store := newTestServer(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/events?token=" + token)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}

	frames := make(chan string, 4)
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				frames <- string(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	select {
	case first := <-frames:
		if !strings.Contains(first, "event: state") {
			t.Fatalf("first frame was %q", first)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no initial snapshot")
	}

	name := "SPEAKEASY"
	if _, err := store.Apply(1, state.Patch{Name: &name}, "#fff"); err != nil {
		t.Fatal(err)
	}
	select {
	case update := <-frames:
		if !strings.Contains(update, "SPEAKEASY") {
			t.Fatalf("update frame was %q", update)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("change was not pushed")
	}
}

// rebuildFor exercises the refresh logic directly: driving it through the HTTP
// handler would need a live Ghostty.
func rebuildFor(t *testing.T, store *state.Store, tabs []string, pid int) []state.Tile {
	t.Helper()
	cfg := &config.Config{Token: token, Cols: 2, Rows: 6, Palette: config.DefaultPalette}
	s := &Server{cfg: cfg, store: store, log: log.New(io.Discard, "", 0)}
	return s.rebuild(ghostty.Window{PID: pid, Title: "MAIN", Tabs: tabs}, 1)
}

func TestRefreshKeepsOpenTabsAndDropsClosedOnes(t *testing.T) {
	_, store := newTestServer(t)
	rebuildFor(t, store, []string{"UNSILOED", "BRYNTUM", "RITZA"}, 42)

	// Give the middle tab some state and a bound shell.
	waiting, msg, tty := state.StateWaiting, "approve?", "/dev/ttys009"
	if _, err := store.Apply(2, state.Patch{State: &waiting, Message: &msg, TTY: &tty}, "#fff"); err != nil {
		t.Fatal(err)
	}

	// UNSILOED is closed, so BRYNTUM and RITZA shift up a slot.
	tiles := rebuildFor(t, store, []string{"BRYNTUM", "RITZA"}, 42)
	if len(tiles) != 2 {
		t.Fatalf("want 2 tiles, got %d", len(tiles))
	}
	if tiles[0].Name != "BRYNTUM" || tiles[0].Slot != 1 {
		t.Fatalf("BRYNTUM should have moved to slot 1: %+v", tiles[0])
	}
	if tiles[0].State != state.StateWaiting || tiles[0].Message != "approve?" || tiles[0].TTY != tty {
		t.Errorf("a tab that is still open should keep its state and shell: %+v", tiles[0])
	}
	if _, ok := store.Get(3); ok {
		t.Error("the closed tab's tile should be gone")
	}
}

func TestRefreshFollowsARename(t *testing.T) {
	_, store := newTestServer(t)
	rebuildFor(t, store, []string{"UNSILOED", "BRYNTUM"}, 42)
	tty := "/dev/ttys009"
	if _, err := store.Apply(2, state.Patch{TTY: &tty}, "#fff"); err != nil {
		t.Fatal(err)
	}

	tiles := rebuildFor(t, store, []string{"UNSILOED", "SCHEDULER"}, 42)
	if tiles[1].Name != "SCHEDULER" || tiles[1].TabTitle != "SCHEDULER" {
		t.Errorf("the tile should have picked up the new title: %+v", tiles[1])
	}
	if tiles[1].TTY != tty {
		t.Errorf("a rename should not break the shell binding: %+v", tiles[1])
	}
}

func TestRefreshKeepsACustomName(t *testing.T) {
	_, store := newTestServer(t)
	rebuildFor(t, store, []string{"UNSILOED"}, 42)
	custom := "CI box"
	if _, err := store.Apply(1, state.Patch{Name: &custom}, "#fff"); err != nil {
		t.Fatal(err)
	}
	tiles := rebuildFor(t, store, []string{"UNSILOED"}, 42)
	if tiles[0].Name != custom {
		t.Errorf("a name set by hand should survive a refresh, got %q", tiles[0].Name)
	}
}
