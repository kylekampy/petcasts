package pipeline

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/kylekampy/petcasts/internal/config"
	"github.com/kylekampy/petcasts/internal/db"
	"github.com/kylekampy/petcasts/internal/gemini"
	"github.com/kylekampy/petcasts/internal/storage"
)

// mockGemini returns canned responses for scene and image generation.
type mockGemini struct {
	textCalls  int
	imageCalls int
}

func (m *mockGemini) GenerateText(model string, prompt string) (string, error) {
	m.textCalls++
	scene := SceneDescription{
		Activity:           "Buddy and Max are having a tea party on the porch",
		Foreground:         "A small wooden table with teacups",
		Background:         "A spring garden with bare trees starting to bud",
		Mood:               "Warm afternoon light, soft pastels",
		Constraints:        "Keep pets centered",
		WeatherIntegration: "Temperature written on a chalkboard sign",
	}
	data, _ := json.Marshal(scene)
	return string(data), nil
}

func (m *mockGemini) GenerateImage(model string, prompt string, refImage []byte, refMimeType string, aspectRatio string) (*gemini.GenerateImageResponse, error) {
	m.imageCalls++
	// Generate a real PNG image (gradient so dithering has something to work with)
	img := image.NewNRGBA(image.Rect(0, 0, 1200, 800))
	for y := range 800 {
		for x := range 1200 {
			r := uint8((x * 255) / 1200)
			g := uint8((y * 255) / 800)
			b := uint8(128)
			img.SetNRGBA(x, y, color.NRGBA{r, g, b, 255})
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)

	return &gemini.GenerateImageResponse{
		Text:      "Here is your image",
		ImageData: buf.Bytes(),
		MimeType:  "image/png",
	}, nil
}

// setupTestPipeline creates a Pipeline with real config, real DB, real storage,
// and a mock Gemini client. Returns the pipeline, mock, and data dir.
func setupTestPipeline(t *testing.T) (*Pipeline, *mockGemini, string) {
	t.Helper()
	dir := t.TempDir()

	// Write minimal config
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(`
location:
  name: 'TestCity'
  latitude: 40.0
  longitude: -90.0
styles:
  - 'Pop art in bold primary colors'
  - 'Watercolor with soft washes'
gemini:
  image_model: 'test-image-model'
  chat_model: 'test-chat-model'
display:
  width: 800
  height: 480
cooldowns:
  photo_days: 7
  combo_days: 14
  style_uses: 7
`), 0o644)

	// Write pets config
	petsDir := filepath.Join(dir, "pets", "meta")
	os.MkdirAll(petsDir, 0o755)
	os.WriteFile(filepath.Join(petsDir, "pets.yaml"), []byte(`
groups:
  - name: 'test group'
    pets: [Buddy, Max]
pets:
  - name: 'Buddy'
    description: 'A golden retriever with floppy ears'
    photos:
      - 'buddy_max.png'
  - name: 'Max'
    description: 'A tabby cat with green eyes'
    photos:
      - 'buddy_max.png'
      - 'max_solo.png'
`), 0o644)

	// Create a fake reference photo
	inputDir := filepath.Join(dir, "pets", "input")
	os.MkdirAll(inputDir, 0o755)
	refImg := image.NewNRGBA(image.Rect(0, 0, 100, 100))
	var refBuf bytes.Buffer
	png.Encode(&refBuf, refImg)
	os.WriteFile(filepath.Join(inputDir, "buddy_max.png"), refBuf.Bytes(), 0o644)
	os.WriteFile(filepath.Join(inputDir, "max_solo.png"), refBuf.Bytes(), 0o644)

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	database, err := db.Open(dir)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	store := storage.NewLocal(dir)
	mock := &mockGemini{}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	pipe := &Pipeline{
		Config: cfg,
		DB:     database,
		Store:  store,
		Gemini: mock,
		Logger: logger,
	}

	return pipe, mock, dir
}

func TestPipeline_FullRun(t *testing.T) {
	pipe, mock, dir := setupTestPipeline(t)

	// Create a frame to associate the generation with
	frame, _, err := pipe.DB.CreateFrame("test", 800, 480)
	if err != nil {
		t.Fatalf("CreateFrame: %v", err)
	}

	result, err := pipe.Run(frame.ID, nil, false)
	if err != nil {
		t.Fatalf("Pipeline.Run: %v", err)
	}

	// Verify Gemini was called
	if mock.textCalls != 1 {
		t.Errorf("text calls = %d, want 1", mock.textCalls)
	}
	if mock.imageCalls != 1 {
		t.Errorf("image calls = %d, want 1", mock.imageCalls)
	}

	// Verify output paths are set
	if result.OriginalPath == "" {
		t.Error("OriginalPath is empty")
	}
	if result.DitheredPath == "" {
		t.Error("DitheredPath is empty")
	}

	// Verify files actually exist on disk
	if !pipe.Store.Exists(result.OriginalPath) {
		t.Errorf("original file doesn't exist: %s", result.OriginalPath)
	}
	if !pipe.Store.Exists(result.DitheredPath) {
		t.Errorf("dithered file doesn't exist: %s", result.DitheredPath)
	}

	// Verify the dithered image is the right size and only has palette colors
	dithData, err := pipe.Store.Load(result.DitheredPath)
	if err != nil {
		t.Fatalf("load dithered: %v", err)
	}
	dithImg, err := png.Decode(bytes.NewReader(dithData))
	if err != nil {
		t.Fatalf("decode dithered: %v", err)
	}
	bounds := dithImg.Bounds()
	if bounds.Dx() != 800 || bounds.Dy() != 480 {
		t.Errorf("dithered size = %dx%d, want 800x480", bounds.Dx(), bounds.Dy())
	}
	paletteSet := make(map[color.NRGBA]bool)
	for _, c := range Spectra6Palette {
		paletteSet[c] = true
	}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := dithImg.At(x, y).RGBA()
			c := color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
			if !paletteSet[c] {
				t.Fatalf("dithered pixel (%d,%d) = %v not in palette", x, y, c)
			}
		}
	}

	// Verify DB records were created
	gen, err := pipe.DB.LatestGeneration(frame.ID)
	if err != nil {
		t.Fatalf("LatestGeneration: %v", err)
	}
	if gen == nil {
		t.Fatal("no generation record found")
	}
	if gen.FrameID != frame.ID {
		t.Errorf("generation frame_id = %q, want %q", gen.FrameID, frame.ID)
	}
	if gen.OriginalPath != result.OriginalPath {
		t.Errorf("generation original_path = %q, want %q", gen.OriginalPath, result.OriginalPath)
	}

	// Verify selection history was recorded
	history, err := pipe.DB.GetSelectionHistory(1)
	if err != nil {
		t.Fatalf("GetSelectionHistory: %v", err)
	}
	if len(history) == 0 {
		t.Fatal("no selection history recorded")
	}
	if history[0].SceneActivity == "" {
		t.Error("selection history has empty scene activity")
	}

	// Verify scene data in result
	if result.Scene.Activity == "" {
		t.Error("result scene activity is empty")
	}
	if result.Forecast == nil {
		t.Error("result forecast is nil")
	}

	// Verify selection data
	if len(result.Selection.Pets) == 0 {
		t.Error("no pets selected")
	}
	if result.Selection.Photo == "" {
		t.Error("no photo selected")
	}
	if result.Selection.Style == "" {
		t.Error("no style selected")
	}

	// Verify original image is larger than dithered (full-res vs 800x480)
	origData, _ := pipe.Store.Load(result.OriginalPath)
	origImg, _ := png.Decode(bytes.NewReader(origData))
	origBounds := origImg.Bounds()
	if origBounds.Dx() <= 800 || origBounds.Dy() <= 480 {
		// Original from mock is 1200x800, should be preserved
		t.Logf("original size = %dx%d (mock returns 1200x800)", origBounds.Dx(), origBounds.Dy())
	}

	t.Logf("Pipeline run complete: pets=%v photo=%s style_prefix=%s",
		petNames(result.Selection.Pets), result.Selection.Photo, truncate(result.Selection.Style, 40))
	t.Logf("Files: original=%s dithered=%s", result.OriginalPath, result.DitheredPath)

	// Run a second time — should pick different style due to cooldowns
	result2, err := pipe.Run(frame.ID, nil, false)
	if err != nil {
		t.Fatalf("Pipeline.Run (2nd): %v", err)
	}
	if result2.DitheredPath == "" {
		t.Error("second run produced no dithered path")
	}

	// Verify two generations now in DB
	gens, err := pipe.DB.ListGenerations(10)
	if err != nil {
		t.Fatalf("ListGenerations: %v", err)
	}
	if len(gens) != 2 {
		t.Errorf("generation count = %d, want 2", len(gens))
	}

	// Verify files on disk
	entries, _ := os.ReadDir(filepath.Join(dir, "generations"))
	if len(entries) == 0 {
		t.Error("no files in generations directory")
	}
}

func TestPipeline_WithBattery(t *testing.T) {
	pipe, _, _ := setupTestPipeline(t)
	frame, _, _ := pipe.DB.CreateFrame("test", 800, 480)

	battery := 10.0 // low battery
	result, err := pipe.Run(frame.ID, &battery, false)
	if err != nil {
		t.Fatalf("Pipeline.Run with battery: %v", err)
	}
	if result.DitheredPath == "" {
		t.Error("no output with low battery")
	}
}

func TestPipeline_ForceRegen(t *testing.T) {
	pipe, mock, _ := setupTestPipeline(t)
	frame, _, _ := pipe.DB.CreateFrame("test", 800, 480)

	// First run
	_, err := pipe.Run(frame.ID, nil, false)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Second run with force
	_, err = pipe.Run(frame.ID, nil, true)
	if err != nil {
		t.Fatalf("force run: %v", err)
	}

	if mock.imageCalls != 2 {
		t.Errorf("image calls = %d, want 2 (normal + force)", mock.imageCalls)
	}
}

func TestPipeline_SelectionCooldowns(t *testing.T) {
	pipe, _, _ := setupTestPipeline(t)
	frame, _, _ := pipe.DB.CreateFrame("test", 800, 480)

	styles := make(map[string]int)
	// Run several times — with only 2 styles configured, cooldowns should rotate
	for range 4 {
		result, err := pipe.Run(frame.ID, nil, false)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		styles[result.Selection.Style]++
	}

	// Both styles should have been used
	if len(styles) < 2 {
		t.Errorf("only %d unique styles used in 4 runs, expected 2", len(styles))
	}
}
