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
	"testing"
	"time"

	"github.com/kylekampy/petcasts/internal/config"
	"github.com/kylekampy/petcasts/internal/db"
	"github.com/kylekampy/petcasts/internal/storage"
)

func testServer(t *testing.T) (*Server, *db.DB) {
	t.Helper()
	dir := t.TempDir()

	database, err := db.Open(dir)
	if err != nil {
		t.Fatalf("db.Open() error: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	store := storage.NewLocal(dir)
	cfg := &config.Config{
		Display: config.Display{Width: 800, Height: 480},
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	srv := New(cfg, database, store, nil, "TEST-CODE", logger)
	return srv, database
}

// helper to create a frame and return its ID + token
func pairFrame(t *testing.T, handler http.Handler) (string, string) {
	t.Helper()
	body := `{"code":"TEST-CODE","hardware_type":"waveshare"}`
	req := httptest.NewRequest("POST", "/api/v1/pair", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("pair failed: status %d, body: %s", w.Code, w.Body.String())
	}
	var resp pairResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	return resp.FrameID, resp.Token
}

// helper to save a fake PNG in storage and mark it generated today
func prepareImage(t *testing.T, srv *Server, database *db.DB, frameID, relPath string) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	for y := range 10 {
		for x := range 10 {
			img.SetNRGBA(x, y, color.NRGBA{255, 0, 0, 255})
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	srv.Store.Save(relPath, buf.Bytes())
	today := time.Now().Format("2006-01-02")
	database.SetDailyGenerated(frameID, today, relPath, false)
}

// --- Pair tests ---

func TestPairHandler_Success(t *testing.T) {
	srv, _ := testServer(t)
	handler := srv.Handler()

	body := `{"code":"TEST-CODE","hardware_type":"waveshare","display_w":800,"display_h":480}`
	req := httptest.NewRequest("POST", "/api/v1/pair", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp pairResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.FrameID == "" {
		t.Error("FrameID is empty")
	}
	if resp.Token == "" {
		t.Error("Token is empty")
	}
}

func TestPairHandler_WrongCode(t *testing.T) {
	srv, _ := testServer(t)
	handler := srv.Handler()

	body := `{"code":"WRONG"}`
	req := httptest.NewRequest("POST", "/api/v1/pair", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestPairHandler_DefaultDisplaySize(t *testing.T) {
	srv, _ := testServer(t)
	handler := srv.Handler()

	body := `{"code":"TEST-CODE","hardware_type":"unknown"}`
	req := httptest.NewRequest("POST", "/api/v1/pair", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp pairResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	frame, _ := srv.DB.GetFrameByID(resp.FrameID)
	if frame.DisplayW != 800 || frame.DisplayH != 480 {
		t.Errorf("default display size = %dx%d, want 800x480", frame.DisplayW, frame.DisplayH)
	}
}

// --- Generate tests ---

func TestGenerate_NoPipeline_Returns503(t *testing.T) {
	srv, _ := testServer(t)
	handler := srv.Handler()
	frameID, token := pairFrame(t, handler)

	req := httptest.NewRequest("POST", "/api/v1/frame/"+frameID+"/generate", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusServiceUnavailable, w.Body.String())
	}
}

func TestGenerate_AlreadyGeneratedToday_Returns429(t *testing.T) {
	srv, database := testServer(t)
	handler := srv.Handler()
	frameID, token := pairFrame(t, handler)

	// Mark as already generated today
	today := time.Now().Format("2006-01-02")
	database.SetDailyGenerated(frameID, today, "gen/test.png", false)

	req := httptest.NewRequest("POST", "/api/v1/frame/"+frameID+"/generate",
		bytes.NewBufferString(`{"battery": 85}`))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusTooManyRequests, w.Body.String())
	}
}

func TestGenerate_NoAuth(t *testing.T) {
	srv, _ := testServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest("POST", "/api/v1/frame/abc/generate", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestGenerate_TokenFrameMismatch(t *testing.T) {
	srv, _ := testServer(t)
	handler := srv.Handler()
	_, token := pairFrame(t, handler)

	req := httptest.NewRequest("POST", "/api/v1/frame/wrong-id/generate", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

// --- Image tests ---

func TestFrameImage_NoToken(t *testing.T) {
	srv, _ := testServer(t)
	handler := srv.Handler()

	req := httptest.NewRequest("GET", "/api/v1/frame/abc/image", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestFrameImage_ServesExistingImage(t *testing.T) {
	srv, database := testServer(t)
	handler := srv.Handler()
	frameID, token := pairFrame(t, handler)

	prepareImage(t, srv, database, frameID, "gen/test_dithered.png")

	req := httptest.NewRequest("GET", "/api/v1/frame/"+frameID+"/image", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want %q", ct, "image/png")
	}
	if w.Body.Len() == 0 {
		t.Error("response body is empty")
	}
}

func TestFrameImage_NoImage_Returns404(t *testing.T) {
	srv, _ := testServer(t)
	handler := srv.Handler()
	frameID, token := pairFrame(t, handler)

	req := httptest.NewRequest("GET", "/api/v1/frame/"+frameID+"/image", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestFrameImage_Generating_Returns202(t *testing.T) {
	srv, _ := testServer(t)
	handler := srv.Handler()
	frameID, token := pairFrame(t, handler)

	// Simulate generation in progress
	srv.mu.Lock()
	srv.generating[frameID] = true
	srv.mu.Unlock()

	req := httptest.NewRequest("GET", "/api/v1/frame/"+frameID+"/image", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d", w.Code, http.StatusAccepted)
	}
}

func TestFrameImage_GeneratingWithOldImage_ServesOldImage(t *testing.T) {
	srv, database := testServer(t)
	handler := srv.Handler()
	frameID, token := pairFrame(t, handler)

	// Save an old image (yesterday) as a previous generation
	img := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	var buf bytes.Buffer
	png.Encode(&buf, img)
	srv.Store.Save("gen/old.png", buf.Bytes())
	database.RecordGeneration(&db.Generation{
		FrameID:      frameID,
		CreatedAt:    time.Now().Add(-24 * time.Hour),
		Pets:         `["Buddy"]`,
		Style:        "test",
		DitheredPath: "gen/old.png",
	})

	// Simulate generation in progress
	srv.mu.Lock()
	srv.generating[frameID] = true
	srv.mu.Unlock()

	req := httptest.NewRequest("GET", "/api/v1/frame/"+frameID+"/image", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should serve the old image with X-Generating header
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if w.Header().Get("X-Generating") != "true" {
		t.Error("expected X-Generating: true header")
	}
}

// --- Status tests ---

func TestStatusHandler(t *testing.T) {
	srv, _ := testServer(t)
	handler := srv.Handler()
	pairFrame(t, handler)

	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	frames, ok := resp["frames"].([]any)
	if !ok || len(frames) != 1 {
		t.Errorf("frames count = %v, want 1", resp["frames"])
	}
}

// --- End-to-end flow ---

func TestEndToEnd_PairGenerateServe(t *testing.T) {
	srv, database := testServer(t)
	handler := srv.Handler()

	// Step 1: Pair
	frameID, token := pairFrame(t, handler)

	// Step 2: Trigger generate (will return 503 since pipeline is nil, but that's ok)
	genReq := httptest.NewRequest("POST", "/api/v1/frame/"+frameID+"/generate",
		bytes.NewBufferString(`{"battery": 92}`))
	genReq.Header.Set("Authorization", "Bearer "+token)
	genW := httptest.NewRecorder()
	handler.ServeHTTP(genW, genReq)
	// 503 expected since pipeline is nil in test
	if genW.Code != http.StatusServiceUnavailable {
		t.Fatalf("generate: status = %d, want %d", genW.Code, http.StatusServiceUnavailable)
	}

	// Step 3: Manually prepare an image (simulating pipeline completion)
	prepareImage(t, srv, database, frameID, "gen/today.png")

	// Step 4: Fetch image
	imgReq := httptest.NewRequest("GET", "/api/v1/frame/"+frameID+"/image?battery=92", nil)
	imgReq.Header.Set("Authorization", "Bearer "+token)
	imgW := httptest.NewRecorder()
	handler.ServeHTTP(imgW, imgReq)

	if imgW.Code != http.StatusOK {
		t.Fatalf("image: status = %d, body: %s", imgW.Code, imgW.Body.String())
	}
	if imgW.Header().Get("Content-Type") != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", imgW.Header().Get("Content-Type"))
	}

	// Step 5: Verify battery was recorded
	frame, _ := database.GetFrameByID(frameID)
	if frame.BatteryPct == nil || *frame.BatteryPct != 92.0 {
		t.Errorf("battery = %v, want 92.0", frame.BatteryPct)
	}

	// Step 6: Generate again should return 429
	gen2Req := httptest.NewRequest("POST", "/api/v1/frame/"+frameID+"/generate", nil)
	gen2Req.Header.Set("Authorization", "Bearer "+token)
	gen2W := httptest.NewRecorder()
	handler.ServeHTTP(gen2W, gen2Req)

	if gen2W.Code != http.StatusTooManyRequests {
		t.Errorf("second generate: status = %d, want %d", gen2W.Code, http.StatusTooManyRequests)
	}
}
