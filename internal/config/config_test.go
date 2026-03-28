package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()

	// Write config.yaml
	configYAML := `
location:
  name: 'TestCity'
  latitude: 40.0
  longitude: -90.0
styles:
  - 'Pop art'
  - 'Pixel art'
gemini:
  image_model: 'gemini-test-image'
  chat_model: 'gemini-test-chat'
display:
  width: 800
  height: 480
cooldowns:
  photo_days: 7
  combo_days: 14
  style_uses: 7
`
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(configYAML), 0o644)

	// Write pets.yaml
	petsDir := filepath.Join(dir, "pets", "meta")
	os.MkdirAll(petsDir, 0o755)
	petsYAML := `
groups:
  - name: 'test group'
    pets: [Buddy, Max]
pets:
  - name: 'Buddy'
    description: 'A golden retriever'
    photos:
      - 'buddy_max.png'
  - name: 'Max'
    description: 'A tabby cat'
    photos:
      - 'buddy_max.png'
      - 'max_solo.png'
`
	os.WriteFile(filepath.Join(petsDir, "pets.yaml"), []byte(petsYAML), 0o644)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Location.Name != "TestCity" {
		t.Errorf("Location.Name = %q, want %q", cfg.Location.Name, "TestCity")
	}
	if cfg.Location.Latitude != 40.0 {
		t.Errorf("Location.Latitude = %f, want 40.0", cfg.Location.Latitude)
	}
	if len(cfg.Styles) != 2 {
		t.Errorf("len(Styles) = %d, want 2", len(cfg.Styles))
	}
	if cfg.Gemini.ImageModel != "gemini-test-image" {
		t.Errorf("Gemini.ImageModel = %q, want %q", cfg.Gemini.ImageModel, "gemini-test-image")
	}
	if cfg.Display.Width != 800 || cfg.Display.Height != 480 {
		t.Errorf("Display = %dx%d, want 800x480", cfg.Display.Width, cfg.Display.Height)
	}
	if len(cfg.Pets) != 2 {
		t.Fatalf("len(Pets) = %d, want 2", len(cfg.Pets))
	}
	if cfg.Pets[0].Name != "Buddy" {
		t.Errorf("Pets[0].Name = %q, want %q", cfg.Pets[0].Name, "Buddy")
	}
	if len(cfg.Groups) != 1 {
		t.Fatalf("len(Groups) = %d, want 1", len(cfg.Groups))
	}
	if cfg.Groups[0].Name != "test group" {
		t.Errorf("Groups[0].Name = %q, want %q", cfg.Groups[0].Name, "test group")
	}
	if cfg.DataDir != dir {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, dir)
	}
}

func TestPetByName(t *testing.T) {
	cfg := &Config{
		Pets: []Pet{
			{Name: "Buddy", Description: "golden retriever"},
			{Name: "Max", Description: "tabby cat"},
		},
	}

	if p := cfg.PetByName("Buddy"); p == nil || p.Name != "Buddy" {
		t.Errorf("PetByName(Buddy) failed")
	}
	if p := cfg.PetByName("Unknown"); p != nil {
		t.Errorf("PetByName(Unknown) = %v, want nil", p)
	}
}

func TestPhotoToPets(t *testing.T) {
	cfg := &Config{
		Pets: []Pet{
			{Name: "Buddy", Photos: []string{"both.png", "buddy.png"}},
			{Name: "Max", Photos: []string{"both.png", "max.png"}},
		},
	}

	m := cfg.PhotoToPets()
	if len(m["both.png"]) != 2 {
		t.Errorf("both.png has %d pets, want 2", len(m["both.png"]))
	}
	if len(m["buddy.png"]) != 1 {
		t.Errorf("buddy.png has %d pets, want 1", len(m["buddy.png"]))
	}
	if len(m["max.png"]) != 1 {
		t.Errorf("max.png has %d pets, want 1", len(m["max.png"]))
	}
}
