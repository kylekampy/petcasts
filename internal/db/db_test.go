package db

import (
	"testing"
	"time"
)

func testDB(t *testing.T) *DB {
	t.Helper()
	d, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestCreateAndGetFrame(t *testing.T) {
	d := testDB(t)

	frame, token, err := d.CreateFrame("waveshare", 800, 480)
	if err != nil {
		t.Fatalf("CreateFrame() error: %v", err)
	}
	if frame.ID == "" {
		t.Error("frame.ID is empty")
	}
	if token == "" {
		t.Error("token is empty")
	}
	if frame.HardwareType != "waveshare" {
		t.Errorf("HardwareType = %q, want %q", frame.HardwareType, "waveshare")
	}

	// Look up by token
	found, err := d.GetFrameByToken(token)
	if err != nil {
		t.Fatalf("GetFrameByToken() error: %v", err)
	}
	if found.ID != frame.ID {
		t.Errorf("GetFrameByToken ID = %q, want %q", found.ID, frame.ID)
	}

	// Look up by ID
	found2, err := d.GetFrameByID(frame.ID)
	if err != nil {
		t.Fatalf("GetFrameByID() error: %v", err)
	}
	if found2.HardwareType != "waveshare" {
		t.Errorf("GetFrameByID HardwareType = %q, want %q", found2.HardwareType, "waveshare")
	}

	// Invalid token
	_, err = d.GetFrameByToken("bogus")
	if err == nil {
		t.Error("GetFrameByToken(bogus) should have returned error")
	}
}

func TestUpdateFrameSeen(t *testing.T) {
	d := testDB(t)
	frame, _, err := d.CreateFrame("test", 800, 480)
	if err != nil {
		t.Fatalf("CreateFrame() error: %v", err)
	}

	battery := 85.0
	if err := d.UpdateFrameSeen(frame.ID, &battery); err != nil {
		t.Fatalf("UpdateFrameSeen() error: %v", err)
	}

	f, err := d.GetFrameByID(frame.ID)
	if err != nil {
		t.Fatalf("GetFrameByID() error: %v", err)
	}
	if f.BatteryPct == nil || *f.BatteryPct != 85.0 {
		t.Errorf("BatteryPct = %v, want 85.0", f.BatteryPct)
	}
	if f.LastSeenAt == nil {
		t.Error("LastSeenAt is nil after UpdateFrameSeen")
	}
}

func TestDailyState(t *testing.T) {
	d := testDB(t)
	frame, _, _ := d.CreateFrame("test", 800, 480)
	today := time.Now().Format("2006-01-02")

	// Initially not generated
	gen, force, path, err := d.GetDailyState(frame.ID, today)
	if err != nil {
		t.Fatalf("GetDailyState() error: %v", err)
	}
	if gen || force || path != "" {
		t.Errorf("initial state: generated=%v, forceUsed=%v, path=%q", gen, force, path)
	}

	// Mark as generated
	if err := d.SetDailyGenerated(frame.ID, today, "gen/test.png", false); err != nil {
		t.Fatalf("SetDailyGenerated() error: %v", err)
	}
	gen, force, path, _ = d.GetDailyState(frame.ID, today)
	if !gen || force || path != "gen/test.png" {
		t.Errorf("after generate: generated=%v, forceUsed=%v, path=%q", gen, force, path)
	}

	// Force regen
	if err := d.SetDailyGenerated(frame.ID, today, "gen/force.png", true); err != nil {
		t.Fatalf("SetDailyGenerated(force) error: %v", err)
	}
	gen, force, path, _ = d.GetDailyState(frame.ID, today)
	if !gen || !force || path != "gen/force.png" {
		t.Errorf("after force: generated=%v, forceUsed=%v, path=%q", gen, force, path)
	}
}

func TestRecordGeneration(t *testing.T) {
	d := testDB(t)
	frame, _, _ := d.CreateFrame("test", 800, 480)

	g := &Generation{
		FrameID:      frame.ID,
		CreatedAt:    time.Now(),
		Pets:         `["Buddy","Max"]`,
		Style:        "Pop art",
		SceneJSON:    `{"activity":"testing"}`,
		OriginalPath: "gen/orig.png",
		DitheredPath: "gen/dith.png",
		WeatherJSON:  `{"temp":72}`,
		ForceRegen:   false,
	}
	if err := d.RecordGeneration(g); err != nil {
		t.Fatalf("RecordGeneration() error: %v", err)
	}

	latest, err := d.LatestGeneration(frame.ID)
	if err != nil {
		t.Fatalf("LatestGeneration() error: %v", err)
	}
	if latest == nil {
		t.Fatal("LatestGeneration() returned nil")
	}
	if latest.Pets != `["Buddy","Max"]` {
		t.Errorf("Pets = %q, want %q", latest.Pets, `["Buddy","Max"]`)
	}
	if latest.Style != "Pop art" {
		t.Errorf("Style = %q, want %q", latest.Style, "Pop art")
	}
	if latest.OriginalPath != "gen/orig.png" {
		t.Errorf("OriginalPath = %q, want %q", latest.OriginalPath, "gen/orig.png")
	}
}

func TestSelectionHistory(t *testing.T) {
	d := testDB(t)

	// Record some selections
	d.RecordSelection("Buddy,Max", "both.png", "Pop art", "playing chess")
	d.RecordSelection("Buddy", "buddy.png", "Pixel art", "reading")

	history, err := d.GetSelectionHistory(10)
	if err != nil {
		t.Fatalf("GetSelectionHistory() error: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("len(history) = %d, want 2", len(history))
	}
	// Most recent first
	if history[0].SceneActivity != "reading" {
		t.Errorf("history[0].SceneActivity = %q, want %q", history[0].SceneActivity, "reading")
	}
	if history[1].Photo != "both.png" {
		t.Errorf("history[1].Photo = %q, want %q", history[1].Photo, "both.png")
	}
}

func TestListFrames(t *testing.T) {
	d := testDB(t)

	d.CreateFrame("waveshare", 800, 480)
	d.CreateFrame("seeed", 800, 480)

	frames, err := d.ListFrames()
	if err != nil {
		t.Fatalf("ListFrames() error: %v", err)
	}
	if len(frames) != 2 {
		t.Errorf("len(frames) = %d, want 2", len(frames))
	}
}

func TestListGenerations(t *testing.T) {
	d := testDB(t)
	frame, _, _ := d.CreateFrame("test", 800, 480)

	for i := 0; i < 3; i++ {
		d.RecordGeneration(&Generation{
			FrameID:   frame.ID,
			CreatedAt: time.Now(),
			Pets:      `["Buddy"]`,
			Style:     "test",
		})
	}

	gens, err := d.ListGenerations(10)
	if err != nil {
		t.Fatalf("ListGenerations() error: %v", err)
	}
	if len(gens) != 3 {
		t.Errorf("len(gens) = %d, want 3", len(gens))
	}
}

func TestHashToken(t *testing.T) {
	h1 := HashToken("abc")
	h2 := HashToken("abc")
	h3 := HashToken("xyz")

	if h1 != h2 {
		t.Error("same token should produce same hash")
	}
	if h1 == h3 {
		t.Error("different tokens should produce different hashes")
	}
	if len(h1) != 64 { // SHA-256 hex = 64 chars
		t.Errorf("hash length = %d, want 64", len(h1))
	}
}
