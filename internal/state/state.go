// Package state holds the tile grid that the phone mirrors.
package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Tile states. Anything else is rejected by Normalise.
const (
	StateEmpty   = "empty"
	StateIdle    = "idle"
	StateWorking = "working"
	StateWaiting = "waiting"
	StateDone    = "done"
	StateError   = "error"
)

// States lists every valid tile state, in rough lifecycle order.
var States = []string{StateEmpty, StateIdle, StateWorking, StateWaiting, StateDone, StateError}

// ValidState reports whether s is a known tile state.
func ValidState(s string) bool {
	for _, v := range States {
		if v == s {
			return true
		}
	}
	return false
}

// Tile is one square on the phone: one Ghostty tab.
type Tile struct {
	Slot    int    `json:"slot"`
	Name    string `json:"name"`
	Color   string `json:"color"`
	State   string `json:"state"`
	Message string `json:"message"`

	// TTY is the terminal device of the shell running in this tab, e.g.
	// "/dev/ttys024". It is how a status update finds its own tile.
	TTY string `json:"tty,omitempty"`
	// TabTitle is the tab's title in Ghostty, which is what focusing matches
	// on. It is tracked separately from Name so the tile can be relabelled
	// without breaking the link to the tab.
	TabTitle string `json:"tabTitle,omitempty"`
	// PID is the Ghostty application instance owning the tab; several can run
	// at once. Window and TabIndex are the last known position, used when the
	// title no longer matches.
	PID      int    `json:"pid,omitempty"`
	Window   string `json:"window,omitempty"`
	TabIndex int    `json:"tabIndex,omitempty"`

	UpdatedAt time.Time `json:"updatedAt"`
}

// Snapshot is the full grid as sent to clients.
type Snapshot struct {
	Rev   int64  `json:"rev"`
	Cols  int    `json:"cols"`
	Rows  int    `json:"rows"`
	Host  string `json:"host"`
	Tiles []Tile `json:"tiles"`
}

// Patch is a partial tile update. Nil fields are left unchanged.
type Patch struct {
	Slot     *int    `json:"slot,omitempty"`
	Name     *string `json:"name,omitempty"`
	Color    *string `json:"color,omitempty"`
	State    *string `json:"state,omitempty"`
	Message  *string `json:"message,omitempty"`
	TTY      *string `json:"tty,omitempty"`
	TabTitle *string `json:"tabTitle,omitempty"`
	PID      *int    `json:"pid,omitempty"`
	Window   *string `json:"window,omitempty"`
	TabIndex *int    `json:"tabIndex,omitempty"`
}

type persisted struct {
	Cols  int    `json:"cols"`
	Rows  int    `json:"rows"`
	Rev   int64  `json:"rev"`
	Tiles []Tile `json:"tiles"`
}

// Store is a concurrency-safe tile grid with change notification.
type Store struct {
	mu    sync.RWMutex
	path  string
	host  string
	cols  int
	rows  int
	rev   int64
	tiles map[int]Tile
	subs  map[chan Snapshot]struct{}
}

// New loads a store from path, or starts empty if the file does not exist.
func New(path, host string, cols, rows int) (*Store, error) {
	s := &Store{
		path:  path,
		host:  host,
		cols:  cols,
		rows:  rows,
		tiles: map[int]Tile{},
		subs:  map[chan Snapshot]struct{}{},
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var p persisted
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	if p.Cols > 0 {
		s.cols = p.Cols
	}
	if p.Rows > 0 {
		s.rows = p.Rows
	}
	// Revisions carry across restarts. A client that reconnects after one would
	// otherwise be holding a higher number than the daemon it is talking to.
	s.rev = p.Rev
	for _, t := range p.Tiles {
		if t.Slot > 0 {
			s.tiles[t.Slot] = t
		}
	}
	return s, nil
}

// Grid returns the current grid dimensions.
func (s *Store) Grid() (cols, rows int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cols, s.rows
}

// SetGrid changes the grid dimensions. Tiles outside the new grid are kept but
// not rendered, so shrinking and re-growing the grid is non-destructive.
func (s *Store) SetGrid(cols, rows int) Snapshot {
	s.mu.Lock()
	if cols > 0 {
		s.cols = cols
	}
	if rows > 0 {
		s.rows = rows
	}
	snap := s.snapshotLocked()
	// A failed write should not stop connected clients from seeing the change.
	_ = s.saveLocked()
	s.mu.Unlock()
	s.publish(snap)
	return snap
}

// Snapshot returns the current grid.
func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotLocked()
}

func (s *Store) snapshotLocked() Snapshot {
	n := s.cols * s.rows
	snap := Snapshot{Rev: s.rev, Cols: s.cols, Rows: s.rows, Host: s.host}
	for i := 1; i <= n; i++ {
		if t, ok := s.tiles[i]; ok {
			snap.Tiles = append(snap.Tiles, t)
			continue
		}
		snap.Tiles = append(snap.Tiles, Tile{Slot: i, State: StateEmpty})
	}
	// Tiles parked outside the visible grid still round-trip through the API so
	// that growing the grid restores them.
	var extra []Tile
	for slot, t := range s.tiles {
		if slot > n {
			extra = append(extra, t)
		}
	}
	sort.Slice(extra, func(i, j int) bool { return extra[i].Slot < extra[j].Slot })
	snap.Tiles = append(snap.Tiles, extra...)
	return snap
}

// Get returns the tile in a slot.
func (s *Store) Get(slot int) (Tile, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tiles[slot]
	return t, ok
}

// FindByTTY returns the tile bound to a terminal device.
func (s *Store) FindByTTY(tty string) (Tile, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.tiles {
		if t.TTY != "" && t.TTY == tty {
			return t, true
		}
	}
	return Tile{}, false
}

// FindByName returns the tile with a matching (case-insensitive) name.
func (s *Store) FindByName(name string) (Tile, bool) {
	if name == "" {
		return Tile{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.tiles {
		if strings.EqualFold(t.Name, name) {
			return t, true
		}
	}
	return Tile{}, false
}

// FindByTabTitle returns the tile bound to a Ghostty tab title.
func (s *Store) FindByTabTitle(title string) (Tile, bool) {
	if title == "" {
		return Tile{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.tiles {
		if t.TabTitle == title || (t.TabTitle == "" && t.Name == title) {
			return t, true
		}
	}
	return Tile{}, false
}

// FreeSlot returns preferred if it is unused, otherwise the lowest free slot.
// It never returns 0: if the grid is full it returns the next slot past the end.
func (s *Store) FreeSlot(preferred int) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := s.cols * s.rows
	if preferred >= 1 && preferred <= n {
		if _, taken := s.tiles[preferred]; !taken {
			return preferred
		}
	}
	for i := 1; i <= n; i++ {
		if _, taken := s.tiles[i]; !taken {
			return i
		}
	}
	max := n
	for slot := range s.tiles {
		if slot > max {
			max = slot
		}
	}
	return max + 1
}

// Apply merges a patch into a slot, creating the tile if needed.
func (s *Store) Apply(slot int, p Patch, defaultColor string) (Tile, error) {
	if slot < 1 {
		return Tile{}, errors.New("slot must be >= 1")
	}
	if p.State != nil && !ValidState(*p.State) {
		return Tile{}, errors.New("unknown state " + *p.State)
	}

	s.mu.Lock()
	t, existed := s.tiles[slot]
	t.Slot = slot
	if !existed {
		t.Color = defaultColor
		t.State = StateIdle
	}
	if p.Name != nil {
		t.Name = strings.TrimSpace(*p.Name)
	}
	if p.Color != nil && *p.Color != "" {
		t.Color = *p.Color
	}
	if p.State != nil {
		t.State = *p.State
	}
	if p.Message != nil {
		t.Message = strings.TrimSpace(*p.Message)
	}
	if p.TTY != nil {
		t.TTY = *p.TTY
		// A tty belongs to exactly one tab, so drop any stale binding.
		if t.TTY != "" {
			for other, ot := range s.tiles {
				if other != slot && ot.TTY == t.TTY {
					ot.TTY = ""
					s.tiles[other] = ot
				}
			}
		}
	}
	if p.TabTitle != nil {
		t.TabTitle = *p.TabTitle
	}
	if p.PID != nil {
		t.PID = *p.PID
	}
	if p.Window != nil {
		t.Window = *p.Window
	}
	if p.TabIndex != nil {
		t.TabIndex = *p.TabIndex
	}
	if t.Name == "" && t.TabTitle != "" {
		t.Name = t.TabTitle
	}
	if t.Name == "" {
		t.Name = "tab " + itoa(slot)
	}
	if t.Color == "" {
		t.Color = defaultColor
	}
	if t.State == "" {
		t.State = StateIdle
	}
	t.UpdatedAt = time.Now()
	s.tiles[slot] = t
	s.rev++
	snap := s.snapshotLocked()
	err := s.saveLocked()
	s.mu.Unlock()

	s.publish(snap)
	return t, err
}

// Tiles returns every tile that exists, in slot order.
func (s *Store) Tiles() []Tile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Tile, 0, len(s.tiles))
	for _, t := range s.tiles {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slot < out[j].Slot })
	return out
}

// Replace swaps the whole tile set atomically, so a rebuild is never visible
// half-applied to a client watching the stream.
func (s *Store) Replace(tiles []Tile) error {
	now := time.Now()
	s.mu.Lock()
	s.tiles = make(map[int]Tile, len(tiles))
	for _, t := range tiles {
		if t.Slot < 1 {
			continue
		}
		if t.UpdatedAt.IsZero() {
			t.UpdatedAt = now
		}
		s.tiles[t.Slot] = t
	}
	s.rev++
	snap := s.snapshotLocked()
	err := s.saveLocked()
	s.mu.Unlock()
	s.publish(snap)
	return err
}

// Clear removes a tile.
func (s *Store) Clear(slot int) error {
	s.mu.Lock()
	delete(s.tiles, slot)
	s.rev++
	snap := s.snapshotLocked()
	err := s.saveLocked()
	s.mu.Unlock()
	s.publish(snap)
	return err
}

// ClearAll removes every tile.
func (s *Store) ClearAll() error {
	s.mu.Lock()
	s.tiles = map[int]Tile{}
	s.rev++
	snap := s.snapshotLocked()
	err := s.saveLocked()
	s.mu.Unlock()
	s.publish(snap)
	return err
}

func (s *Store) saveLocked() error {
	if s.path == "" {
		return nil
	}
	p := persisted{Cols: s.cols, Rows: s.rows, Rev: s.rev}
	for _, t := range s.tiles {
		p.Tiles = append(p.Tiles, t)
	}
	sort.Slice(p.Tiles, func(i, j int) bool { return p.Tiles[i].Slot < p.Tiles[j].Slot })
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Subscribe returns a channel of snapshots and a function to unsubscribe.
func (s *Store) Subscribe() (<-chan Snapshot, func()) {
	ch := make(chan Snapshot, 4)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		if _, ok := s.subs[ch]; ok {
			delete(s.subs, ch)
			close(ch)
		}
		s.mu.Unlock()
	}
}

func (s *Store) publish(snap Snapshot) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for ch := range s.subs {
		select {
		case ch <- snap:
		default: // slow client: it will catch up on the next event
		}
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}
