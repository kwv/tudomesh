package mesh

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// NewStateTracker
// ---------------------------------------------------------------------------

func TestNewStateTracker(t *testing.T) {
	st := NewStateTracker()
	if st == nil {
		t.Fatal("NewStateTracker returned nil")
	}
	if len(st.GetPositions()) != 0 {
		t.Error("new tracker should have zero positions")
	}
	if len(st.GetMaps()) != 0 {
		t.Error("new tracker should have zero maps")
	}
	if st.HasMaps() {
		t.Error("new tracker HasMaps should be false")
	}
}

// ---------------------------------------------------------------------------
// SetColor / UpdatePosition
// ---------------------------------------------------------------------------

func TestStateTracker_SetColor(t *testing.T) {
	st := NewStateTracker()

	st.SetColor("vac-a", "#00FF00")
	st.UpdatePosition("vac-a", 10, 20, 45)

	positions := st.GetPositions()
	pos, ok := positions["vac-a"]
	if !ok {
		t.Fatal("vac-a not found in positions")
	}
	if pos.Color != "#00FF00" {
		t.Errorf("Color = %q, want %q", pos.Color, "#00FF00")
	}
}

func TestStateTracker_UpdatePosition(t *testing.T) {
	st := NewStateTracker()

	t.Run("default color when none set", func(t *testing.T) {
		st.UpdatePosition("vac-x", 1.5, 2.5, 90)
		pos := st.GetPositions()["vac-x"]
		if pos == nil {
			t.Fatal("vac-x not found")
		}
		if pos.Color != "#FF0000" {
			t.Errorf("default Color = %q, want %q", pos.Color, "#FF0000")
		}
		if pos.X != 1.5 || pos.Y != 2.5 || pos.Angle != 90 {
			t.Errorf("position = (%g,%g,%g), want (1.5,2.5,90)", pos.X, pos.Y, pos.Angle)
		}
		if pos.VacuumID != "vac-x" {
			t.Errorf("VacuumID = %q, want %q", pos.VacuumID, "vac-x")
		}
	})

	t.Run("custom color after SetColor", func(t *testing.T) {
		st.SetColor("vac-x", "#0000FF")
		st.UpdatePosition("vac-x", 3, 4, 180)
		pos := st.GetPositions()["vac-x"]
		if pos.Color != "#0000FF" {
			t.Errorf("Color after SetColor = %q, want %q", pos.Color, "#0000FF")
		}
		if pos.X != 3 || pos.Y != 4 {
			t.Errorf("updated position = (%g,%g), want (3,4)", pos.X, pos.Y)
		}
	})

	t.Run("timestamp is set", func(t *testing.T) {
		before := time.Now()
		st.UpdatePosition("vac-ts", 0, 0, 0)
		after := time.Now()
		pos := st.GetPositions()["vac-ts"]
		if pos.Timestamp.Before(before) || pos.Timestamp.After(after) {
			t.Errorf("Timestamp = %v, want between %v and %v", pos.Timestamp, before, after)
		}
	})

	t.Run("overwrite replaces previous position", func(t *testing.T) {
		st.UpdatePosition("vac-ow", 10, 20, 0)
		st.UpdatePosition("vac-ow", 30, 40, 270)
		pos := st.GetPositions()["vac-ow"]
		if pos.X != 30 || pos.Y != 40 || pos.Angle != 270 {
			t.Errorf("overwritten position = (%g,%g,%g), want (30,40,270)", pos.X, pos.Y, pos.Angle)
		}
	})
}

// ---------------------------------------------------------------------------
// UpdateMap / GetMaps / HasMaps
// ---------------------------------------------------------------------------

func TestStateTracker_UpdateMap(t *testing.T) {
	st := NewStateTracker()

	m := &ValetudoMap{
		Size:      Size{X: 100, Y: 200},
		PixelSize: 5,
	}
	st.UpdateMap("vac-a", m)

	maps := st.GetMaps()
	if len(maps) != 1 {
		t.Fatalf("len(maps) = %d, want 1", len(maps))
	}
	got, ok := maps["vac-a"]
	if !ok {
		t.Fatal("vac-a not in maps")
	}
	if got.Size.X != 100 || got.Size.Y != 200 {
		t.Errorf("Size = %+v, want {100 200}", got.Size)
	}
}

func TestStateTracker_GetMaps(t *testing.T) {
	st := NewStateTracker()

	st.UpdateMap("vac-a", &ValetudoMap{PixelSize: 1})
	st.UpdateMap("vac-b", &ValetudoMap{PixelSize: 2})

	maps := st.GetMaps()
	if len(maps) != 2 {
		t.Errorf("len(maps) = %d, want 2", len(maps))
	}
	if maps["vac-a"].PixelSize != 1 {
		t.Errorf("vac-a.PixelSize = %d, want 1", maps["vac-a"].PixelSize)
	}
	if maps["vac-b"].PixelSize != 2 {
		t.Errorf("vac-b.PixelSize = %d, want 2", maps["vac-b"].PixelSize)
	}
}

func TestStateTracker_HasMaps(t *testing.T) {
	st := NewStateTracker()

	if st.HasMaps() {
		t.Error("HasMaps should be false on empty tracker")
	}

	st.UpdateMap("vac-a", &ValetudoMap{})
	if !st.HasMaps() {
		t.Error("HasMaps should be true after UpdateMap")
	}
}

// ---------------------------------------------------------------------------
// GetPositions returns copies, not references
// ---------------------------------------------------------------------------

func TestStateTracker_GetPositions(t *testing.T) {
	st := NewStateTracker()
	st.SetColor("vac-a", "#AABBCC")
	st.UpdatePosition("vac-a", 5, 10, 45)

	snapshot := st.GetPositions()
	// Mutate the snapshot copy
	snapshot["vac-a"].X = 999

	// Original must be unchanged
	fresh := st.GetPositions()
	if fresh["vac-a"].X != 5 {
		t.Errorf("original X mutated to %g; GetPositions must return copies", fresh["vac-a"].X)
	}

	// Adding a key to the snapshot must not appear in a fresh read
	snapshot["injected"] = &LivePosition{VacuumID: "injected"}
	fresh = st.GetPositions()
	if _, ok := fresh["injected"]; ok {
		t.Error("injected key visible in fresh snapshot; map must be a copy")
	}
}

// ---------------------------------------------------------------------------
// Concurrency: hammer all methods under -race
// ---------------------------------------------------------------------------

func TestStateTracker_Concurrency(t *testing.T) {
	st := NewStateTracker()

	const (
		goroutines = 50
		iterations = 200
	)

	var wg sync.WaitGroup
	wg.Add(goroutines * 4) // writers: SetColor, UpdatePosition, UpdateMap; readers: GetPositions/GetMaps/HasMaps

	// Writers: SetColor
	for g := 0; g < goroutines; g++ {
		g := g
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				id := fmt.Sprintf("vac-%d", g)
				st.SetColor(id, fmt.Sprintf("#%06X", i))
			}
		}()
	}

	// Writers: UpdatePosition
	for g := 0; g < goroutines; g++ {
		g := g
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				id := fmt.Sprintf("vac-%d", g)
				st.UpdatePosition(id, float64(i), float64(g), float64(i*90%360))
			}
		}()
	}

	// Writers: UpdateMap
	for g := 0; g < goroutines; g++ {
		g := g
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				id := fmt.Sprintf("vac-%d", g)
				st.UpdateMap(id, &ValetudoMap{
					Size:      Size{X: i, Y: g},
					PixelSize: i + 1,
				})
			}
		}()
	}

	// Readers: GetPositions, GetMaps, HasMaps interleaved
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_ = st.GetPositions()
				_ = st.GetMaps()
				_ = st.HasMaps()
			}
		}()
	}

	wg.Wait()

	// After all goroutines complete, sanity-check we have data
	if len(st.GetPositions()) == 0 {
		t.Error("expected positions after concurrent writes")
	}
	if !st.HasMaps() {
		t.Error("expected maps after concurrent writes")
	}
}
