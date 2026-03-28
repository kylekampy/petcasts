package server

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kylekampy/petcasts/internal/config"
	"github.com/kylekampy/petcasts/internal/db"
	"github.com/kylekampy/petcasts/internal/gemini"
	"github.com/kylekampy/petcasts/internal/pipeline"
	"github.com/kylekampy/petcasts/internal/storage"
)

// mockGemini returns canned responses for the full e2e flow.
type mockGemini struct{}

func (m *mockGemini) GenerateText(model string, prompt string) (string, error) {
	scene := map[string]string{
		"activity":            "Buddy and Max sipping cocoa on the porch",
		"foreground":          "A small table with steaming mugs",
		"background":          "Bare spring trees under gray sky",
		"mood":                "Cozy afternoon light",
		"constraints":         "Keep pets centered",
		"weather_integration": "Temperature on a wooden sign",
	}
	data, _ := json.Marshal(scene)
	return string(data), nil
}

func (m *mockGemini) GenerateImage(model string, prompt string, refImage []byte, refMimeType string, aspectRatio string) (*gemini.GenerateImageResponse, error) {
	img := image.NewNRGBA(image.Rect(0, 0, 1200, 800))
	for y := range 800 {
		for x := range 1200 {
			img.SetNRGBA(x, y, color.NRGBA{uint8(x % 256), uint8(y % 256), 100, 255})
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return &gemini.GenerateImageResponse{
		Text:      "Generated image",
		ImageData: buf.Bytes(),
		MimeType:  "image/png",
	}, nil
}

// setupE2EServer creates a full server with real pipeline + mock Gemini.
func setupE2EServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()

	// Config
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(`
location:
  name: 'TestCity'
  latitude: 40.0
  longitude: -90.0
styles:
  - 'Pop art style'
  - 'Watercolor style'
gemini:
  image_model: 'test-image'
  chat_model: 'test-chat'
display:
  width: 800
  height: 480
cooldowns:
  photo_days: 7
  combo_days: 14
  style_uses: 7
`), 0o644)

	// Pets
	petsDir := filepath.Join(dir, "pets", "meta")
	os.MkdirAll(petsDir, 0o755)
	os.WriteFile(filepath.Join(petsDir, "pets.yaml"), []byte(`
groups:
  - name: 'pals'
    pets: [Buddy, Max]
pets:
  - name: 'Buddy'
    description: 'A golden retriever'
    photos: ['buddy_max.png']
  - name: 'Max'
    description: 'A tabby cat'
    photos: ['buddy_max.png']
`), 0o644)

	// Reference photo
	inputDir := filepath.Join(dir, "pets", "input")
	os.MkdirAll(inputDir, 0o755)
	refImg := image.NewNRGBA(image.Rect(0, 0, 100, 100))
	var refBuf bytes.Buffer
	png.Encode(&refBuf, refImg)
	os.WriteFile(filepath.Join(inputDir, "buddy_max.png"), refBuf.Bytes(), 0o644)

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
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	pipe := &pipeline.Pipeline{
		Config: cfg,
		DB:     database,
		Store:  store,
		Gemini: &mockGemini{},
		Logger: logger.With("component", "pipeline"),
	}

	srv := New(cfg, database, store, pipe, "E2E-CODE", logger.With("component", "server"))
	return srv, dir
}

// TestE2E_FullFrameBootSequence simulates the exact sequence a real frame performs:
// 1. Pair with the server
// 2. POST /generate to trigger image generation (gets 202)
// 3. Wait for generation to complete
// 4. GET /image to fetch the dithered PNG
// 5. Verify the image is a valid 800x480 PNG with only palette colors
func TestE2E_FullFrameBootSequence(t *testing.T) {
	srv, _ := setupE2EServer(t)
	handler := srv.Handler()

	// === Step 1: Pair ===
	pairBody := `{"code":"E2E-CODE","hardware_type":"waveshare","display_w":800,"display_h":480}`
	pairReq := httptest.NewRequest("POST", "/api/v1/pair", bytes.NewBufferString(pairBody))
	pairW := httptest.NewRecorder()
	handler.ServeHTTP(pairW, pairReq)

	if pairW.Code != http.StatusOK {
		t.Fatalf("pair: status %d, body: %s", pairW.Code, pairW.Body.String())
	}
	var pairResp pairResponse
	json.Unmarshal(pairW.Body.Bytes(), &pairResp)
	frameID := pairResp.FrameID
	token := pairResp.Token
	t.Logf("paired frame: id=%s", frameID)

	// === Step 2: Trigger generation ===
	genBody := `{"battery": 78}`
	genReq := httptest.NewRequest("POST", "/api/v1/frame/"+frameID+"/generate",
		bytes.NewBufferString(genBody))
	genReq.Header.Set("Authorization", "Bearer "+token)
	genW := httptest.NewRecorder()
	handler.ServeHTTP(genW, genReq)

	if genW.Code != http.StatusAccepted {
		t.Fatalf("generate: status %d, want 202, body: %s", genW.Code, genW.Body.String())
	}
	t.Logf("generation accepted (202)")

	// === Step 3: Wait for background generation ===
	// Poll until the generation flag clears (max 30s)
	deadline := time.Now().Add(30 * time.Second)
	for {
		srv.mu.Lock()
		generating := srv.generating[frameID]
		srv.mu.Unlock()
		if !generating {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("generation did not complete within 30s")
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Logf("generation complete")

	// === Step 4: Fetch image ===
	imgReq := httptest.NewRequest("GET", "/api/v1/frame/"+frameID+"/image?battery=78", nil)
	imgReq.Header.Set("Authorization", "Bearer "+token)
	imgW := httptest.NewRecorder()
	handler.ServeHTTP(imgW, imgReq)

	if imgW.Code != http.StatusOK {
		t.Fatalf("image: status %d, want 200, body: %s", imgW.Code, imgW.Body.String())
	}
	if imgW.Header().Get("Content-Type") != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", imgW.Header().Get("Content-Type"))
	}
	t.Logf("image fetched: %d bytes", imgW.Body.Len())

	// === Step 5: Validate the image ===
	img, err := png.Decode(bytes.NewReader(imgW.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode image: %v", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != 800 || bounds.Dy() != 480 {
		t.Errorf("image size = %dx%d, want 800x480", bounds.Dx(), bounds.Dy())
	}

	// Every pixel must be a Spectra 6 palette color
	paletteSet := map[color.NRGBA]bool{
		{0, 0, 0, 255}:       true, // Black
		{255, 255, 255, 255}: true, // White
		{200, 0, 0, 255}:     true, // Red
		{0, 150, 0, 255}:     true, // Green
		{0, 0, 200, 255}:     true, // Blue
		{255, 230, 0, 255}:   true, // Yellow
	}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			c := color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
			if !paletteSet[c] {
				t.Fatalf("pixel (%d,%d) = %v not in Spectra 6 palette", x, y, c)
			}
		}
	}
	t.Logf("all pixels are valid Spectra 6 palette colors")

	// === Verify DB state ===
	frame, err := srv.DB.GetFrameByID(frameID)
	if err != nil {
		t.Fatalf("GetFrameByID: %v", err)
	}
	if frame.BatteryPct == nil || *frame.BatteryPct != 78.0 {
		t.Errorf("battery = %v, want 78.0", frame.BatteryPct)
	}
	if frame.LastSeenAt == nil {
		t.Error("frame LastSeenAt is nil")
	}

	gen, err := srv.DB.LatestGeneration(frameID)
	if err != nil {
		t.Fatalf("LatestGeneration: %v", err)
	}
	if gen == nil {
		t.Fatal("no generation record")
	}
	if gen.DitheredPath == "" {
		t.Error("generation has no dithered path")
	}

	today := time.Now().Format("2006-01-02")
	generated, _, dithPath, _ := srv.DB.GetDailyState(frameID, today)
	if !generated {
		t.Error("daily state not marked as generated")
	}
	if dithPath == "" {
		t.Error("daily state has no dithered path")
	}
}

// TestE2E_SecondRequestSameDay verifies that a second generate request
// on the same day returns 429 and the image endpoint still works.
func TestE2E_SecondRequestSameDay(t *testing.T) {
	srv, _ := setupE2EServer(t)
	handler := srv.Handler()

	frameID, token := e2ePair(t, handler)
	e2eGenerateAndWait(t, srv, handler, frameID, token)

	// Second generate → 429
	genReq := httptest.NewRequest("POST", "/api/v1/frame/"+frameID+"/generate", nil)
	genReq.Header.Set("Authorization", "Bearer "+token)
	genW := httptest.NewRecorder()
	handler.ServeHTTP(genW, genReq)

	if genW.Code != http.StatusTooManyRequests {
		t.Errorf("second generate: status %d, want 429", genW.Code)
	}

	// Image still available
	imgReq := httptest.NewRequest("GET", "/api/v1/frame/"+frameID+"/image", nil)
	imgReq.Header.Set("Authorization", "Bearer "+token)
	imgW := httptest.NewRecorder()
	handler.ServeHTTP(imgW, imgReq)

	if imgW.Code != http.StatusOK {
		t.Errorf("image after 429: status %d, want 200", imgW.Code)
	}
}

// TestE2E_ImageDuringGeneration verifies that fetching the image while
// generation is in progress returns 202 (no prior image) or the old image.
func TestE2E_ImageDuringGeneration(t *testing.T) {
	srv, _ := setupE2EServer(t)
	handler := srv.Handler()

	frameID, token := e2ePair(t, handler)

	// Simulate generation in progress without actually running the pipeline
	srv.mu.Lock()
	srv.generating[frameID] = true
	srv.mu.Unlock()

	imgReq := httptest.NewRequest("GET", "/api/v1/frame/"+frameID+"/image", nil)
	imgReq.Header.Set("Authorization", "Bearer "+token)
	imgW := httptest.NewRecorder()
	handler.ServeHTTP(imgW, imgReq)

	// No previous image exists, so should be 202
	if imgW.Code != http.StatusAccepted {
		t.Errorf("image during generation (no prior): status %d, want 202", imgW.Code)
	}

	// Clean up
	srv.mu.Lock()
	delete(srv.generating, frameID)
	srv.mu.Unlock()
}

// TestE2E_MultipleFrames verifies two frames can pair and generate independently.
func TestE2E_MultipleFrames(t *testing.T) {
	srv, _ := setupE2EServer(t)
	handler := srv.Handler()

	frameID1, token1 := e2ePair(t, handler)
	frameID2, token2 := e2ePair(t, handler)

	if frameID1 == frameID2 {
		t.Fatal("two frames got the same ID")
	}

	// Generate for frame 1
	e2eGenerateAndWait(t, srv, handler, frameID1, token1)

	// Frame 2 should have no image
	imgReq := httptest.NewRequest("GET", "/api/v1/frame/"+frameID2+"/image", nil)
	imgReq.Header.Set("Authorization", "Bearer "+token2)
	imgW := httptest.NewRecorder()
	handler.ServeHTTP(imgW, imgReq)

	if imgW.Code != http.StatusNotFound {
		t.Errorf("frame2 image before generate: status %d, want 404", imgW.Code)
	}

	// Generate for frame 2
	e2eGenerateAndWait(t, srv, handler, frameID2, token2)

	// Both frames should now have images
	for i, tc := range []struct {
		id    string
		token string
	}{{frameID1, token1}, {frameID2, token2}} {
		req := httptest.NewRequest("GET", "/api/v1/frame/"+tc.id+"/image", nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("frame %d image: status %d, want 200", i+1, w.Code)
		}
	}

	// Tokens shouldn't cross-authenticate
	crossReq := httptest.NewRequest("GET", "/api/v1/frame/"+frameID1+"/image", nil)
	crossReq.Header.Set("Authorization", "Bearer "+token2)
	crossW := httptest.NewRecorder()
	handler.ServeHTTP(crossW, crossReq)

	if crossW.Code != http.StatusForbidden {
		t.Errorf("cross-auth: status %d, want 403", crossW.Code)
	}
}

// TestE2E_StatusReflectsGenerating verifies the status endpoint shows
// generating state accurately.
func TestE2E_StatusReflectsGenerating(t *testing.T) {
	srv, _ := setupE2EServer(t)
	handler := srv.Handler()

	frameID, _ := e2ePair(t, handler)

	// Not generating
	status1 := e2eGetStatus(t, handler)
	frames1 := status1["frames"].([]any)
	frame1 := frames1[0].(map[string]any)
	if frame1["generating"].(bool) {
		t.Error("frame should not be generating initially")
	}

	// Simulate generating
	srv.mu.Lock()
	srv.generating[frameID] = true
	srv.mu.Unlock()

	status2 := e2eGetStatus(t, handler)
	frames2 := status2["frames"].([]any)
	frame2 := frames2[0].(map[string]any)
	if !frame2["generating"].(bool) {
		t.Error("frame should show as generating")
	}

	srv.mu.Lock()
	delete(srv.generating, frameID)
	srv.mu.Unlock()
}

// --- e2e helpers ---

func e2ePair(t *testing.T, handler http.Handler) (string, string) {
	t.Helper()
	body := `{"code":"E2E-CODE","hardware_type":"waveshare"}`
	req := httptest.NewRequest("POST", "/api/v1/pair", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("e2e pair: status %d, body: %s", w.Code, w.Body.String())
	}
	var resp pairResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	return resp.FrameID, resp.Token
}

func e2eGenerateAndWait(t *testing.T, srv *Server, handler http.Handler, frameID, token string) {
	t.Helper()
	genReq := httptest.NewRequest("POST", "/api/v1/frame/"+frameID+"/generate",
		bytes.NewBufferString(`{"battery":90}`))
	genReq.Header.Set("Authorization", "Bearer "+token)
	genW := httptest.NewRecorder()
	handler.ServeHTTP(genW, genReq)

	if genW.Code != http.StatusAccepted {
		t.Fatalf("e2e generate: status %d, want 202, body: %s", genW.Code, genW.Body.String())
	}

	// Wait for completion
	deadline := time.Now().Add(30 * time.Second)
	for {
		srv.mu.Lock()
		generating := srv.generating[frameID]
		srv.mu.Unlock()
		if !generating {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("e2e generate: did not complete within 30s")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func e2eGetStatus(t *testing.T, handler http.Handler) map[string]any {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	return resp
}
