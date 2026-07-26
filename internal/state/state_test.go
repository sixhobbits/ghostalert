package state

import (
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "state.json"), "testhost", 2, 5)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func str(s string) *string { return &s }
func num(i int) *int       { return &i }

func TestSnapshotFillsEmptySlots(t *testing.T) {
	s := newTestStore(t)
	snap := s.Snapshot()
	if len(snap.Tiles) != 10 {
		t.Fatalf("want 10 tiles, got %d", len(snap.Tiles))
	}
	for i, tile := range snap.Tiles {
		if tile.Slot != i+1 {
			t.Errorf("tile %d has slot %d", i, tile.Slot)
		}
		if tile.State != StateEmpty {
			t.Errorf("slot %d should start empty, got %q", tile.Slot, tile.State)
		}
	}
}

func TestApplyCreatesAndPatches(t *testing.T) {
	s := newTestStore(t)
	tile, err := s.Apply(3, Patch{Name: str("RITZA"), State: str(StateWaiting)}, "#abcdef")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if tile.Name != "RITZA" || tile.State != StateWaiting || tile.Color != "#abcdef" {
		t.Fatalf("unexpected tile %+v", tile)
	}

	// A patch that only carries a state must not wipe the name or colour.
	tile, err = s.Apply(3, Patch{State: str(StateDone)}, "#000000")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if tile.Name != "RITZA" || tile.Color != "#abcdef" || tile.State != StateDone {
		t.Fatalf("patch overwrote fields it should not have: %+v", tile)
	}
}

func TestApplyRejectsBadInput(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Apply(0, Patch{}, "#fff"); err == nil {
		t.Error("slot 0 should be rejected")
	}
	if _, err := s.Apply(1, Patch{State: str("banana")}, "#fff"); err == nil {
		t.Error("unknown state should be rejected")
	}
}

func TestTTYBindingIsExclusive(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Apply(1, Patch{TTY: str("/dev/ttys001")}, "#fff"); err != nil {
		t.Fatal(err)
	}
	// The same shell cannot be two tabs, so rebinding must clear the old tile.
	if _, err := s.Apply(2, Patch{TTY: str("/dev/ttys001")}, "#fff"); err != nil {
		t.Fatal(err)
	}
	if tile, _ := s.Get(1); tile.TTY != "" {
		t.Errorf("slot 1 kept a stale tty %q", tile.TTY)
	}
	found, ok := s.FindByTTY("/dev/ttys001")
	if !ok || found.Slot != 2 {
		t.Errorf("FindByTTY returned %+v (ok=%v), want slot 2", found, ok)
	}
}

func TestFreeSlot(t *testing.T) {
	s := newTestStore(t)
	if got := s.FreeSlot(4); got != 4 {
		t.Errorf("free preferred slot: got %d want 4", got)
	}
	if _, err := s.Apply(4, Patch{}, "#fff"); err != nil {
		t.Fatal(err)
	}
	if got := s.FreeSlot(4); got != 1 {
		t.Errorf("taken preferred slot should fall back to the lowest free one, got %d", got)
	}
	for i := 1; i <= 10; i++ {
		if _, err := s.Apply(i, Patch{}, "#fff"); err != nil {
			t.Fatal(err)
		}
	}
	if got := s.FreeSlot(2); got != 11 {
		t.Errorf("full grid should spill past the end, got %d", got)
	}
}

func TestFindByTabTitleAndName(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Apply(2, Patch{Name: str("Bryntum"), TabTitle: str("BRYNTUM")}, "#fff"); err != nil {
		t.Fatal(err)
	}
	if tile, ok := s.FindByTabTitle("BRYNTUM"); !ok || tile.Slot != 2 {
		t.Errorf("FindByTabTitle: %+v %v", tile, ok)
	}
	if tile, ok := s.FindByName("bryntum"); !ok || tile.Slot != 2 {
		t.Errorf("FindByName should ignore case: %+v %v", tile, ok)
	}
	if _, ok := s.FindByTabTitle(""); ok {
		t.Error("an empty title should never match")
	}
}

func TestTilesOutsideGridSurviveResize(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Apply(12, Patch{Name: str("twelve")}, "#fff"); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	if len(snap.Tiles) != 11 { // 10 in the grid, plus the parked one
		t.Fatalf("want 11 tiles, got %d", len(snap.Tiles))
	}
	snap = s.SetGrid(2, 6)
	if len(snap.Tiles) != 12 {
		t.Fatalf("after growing, want 12 tiles, got %d", len(snap.Tiles))
	}
	if snap.Tiles[11].Name != "twelve" {
		t.Errorf("slot 12 lost its tile: %+v", snap.Tiles[11])
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s, err := New(path, "h", 2, 5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(1, Patch{Name: str("A"), TabIndex: num(3), PID: num(42)}, "#fff"); err != nil {
		t.Fatal(err)
	}
	s.SetGrid(3, 4)
	if _, err := s.Apply(1, Patch{State: str(StateWorking)}, "#fff"); err != nil {
		t.Fatal(err)
	}

	reloaded, err := New(path, "h", 2, 5)
	if err != nil {
		t.Fatal(err)
	}
	cols, rows := reloaded.Grid()
	if cols != 3 || rows != 4 {
		t.Errorf("grid not persisted: got %dx%d", cols, rows)
	}
	tile, ok := reloaded.Get(1)
	if !ok || tile.Name != "A" || tile.TabIndex != 3 || tile.PID != 42 || tile.State != StateWorking {
		t.Errorf("tile not persisted: %+v (ok=%v)", tile, ok)
	}
}

func TestSubscribeReceivesUpdates(t *testing.T) {
	s := newTestStore(t)
	ch, cancel := s.Subscribe()
	defer cancel()

	if _, err := s.Apply(1, Patch{State: str(StateWaiting)}, "#fff"); err != nil {
		t.Fatal(err)
	}
	snap := <-ch
	if snap.Tiles[0].State != StateWaiting {
		t.Errorf("subscriber saw %+v", snap.Tiles[0])
	}

	cancel()
	// A cancelled subscription must not be published to again.
	if _, err := s.Apply(1, Patch{State: str(StateDone)}, "#fff"); err != nil {
		t.Fatal(err)
	}
}

func TestRevisionSurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := New(path, "h", 2, 5)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := s.Apply(1, Patch{Name: str("A")}, "#fff"); err != nil {
			t.Fatal(err)
		}
	}
	before := s.Snapshot().Rev

	// A restart that reset the revision would make every connected client
	// discard the daemon's state as older than what it already had.
	reloaded, err := New(path, "h", 2, 5)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Snapshot().Rev; got != before {
		t.Errorf("revision after restart = %d, want %d", got, before)
	}
}
