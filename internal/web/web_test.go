package web

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kylekampy/petcasts/internal/auth"
	"github.com/kylekampy/petcasts/internal/config"
	"github.com/kylekampy/petcasts/internal/db"
	"github.com/kylekampy/petcasts/internal/storage"
)

func setupTestWeb(t *testing.T) (*Web, *db.DB, *db.User) {
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
	petsDir := filepath.Join(dir, "pets", "meta")
	os.MkdirAll(petsDir, 0o755)
	os.WriteFile(filepath.Join(petsDir, "pets.yaml"), []byte(`
pets: []
groups: []
`), 0o644)

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
	sessions := auth.NewSessionManager(database)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Use the real templates from the repo
	templateDir := filepath.Join(findRepoRoot(t), "web", "templates")

	w, err := New(cfg, database, store, sessions, nil, "TEST-CODE", logger, templateDir)
	if err != nil {
		t.Fatalf("web.New: %v", err)
	}

	// Create a test user
	user, err := database.UpsertUserByGoogle("google-123", "test@example.com", "Test User", "")
	if err != nil {
		t.Fatalf("UpsertUserByGoogle: %v", err)
	}

	return w, database, user
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root")
		}
		dir = parent
	}
}

func authenticatedRequest(t *testing.T, database *db.DB, userID, method, path string) *http.Request {
	t.Helper()
	token, err := database.CreateSession(userID, 24*time.Hour)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	req := httptest.NewRequest(method, path, nil)
	req.AddCookie(&http.Cookie{Name: "petcast_session", Value: token})
	return req
}

func authenticatedFormRequest(t *testing.T, database *db.DB, userID, path string, form url.Values) *http.Request {
	t.Helper()
	token, err := database.CreateSession(userID, 24*time.Hour)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	body := form.Encode()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "petcast_session", Value: token})
	return req
}

func TestLoginPage(t *testing.T) {
	w, _, _ := setupTestWeb(t)
	mux := http.NewServeMux()
	w.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/login", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("login: status %d, body: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "Google") {
		t.Error("login page should contain Google sign-in")
	}
}

func TestDashboard_Unauthenticated_Redirects(t *testing.T) {
	w, _, _ := setupTestWeb(t)
	mux := http.NewServeMux()
	w.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/dashboard", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Errorf("unauthenticated dashboard: status %d, want 302", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/login" {
		t.Errorf("redirect to %q, want /login", loc)
	}
}

func TestDashboard_Authenticated(t *testing.T) {
	w, database, user := setupTestWeb(t)
	mux := http.NewServeMux()
	w.RegisterRoutes(mux)

	req := authenticatedRequest(t, database, user.ID, "GET", "/dashboard")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("dashboard: status %d, body: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Dashboard") {
		t.Error("dashboard should contain 'Dashboard'")
	}
	if !strings.Contains(body, "Test User") {
		t.Error("dashboard should contain user name")
	}
}

func TestPets_CRUD(t *testing.T) {
	w, database, user := setupTestWeb(t)
	mux := http.NewServeMux()
	w.RegisterRoutes(mux)

	// Create pet
	form := url.Values{"name": {"Buddy"}, "description": {"A golden retriever"}}
	req := authenticatedFormRequest(t, database, user.ID, "/pets", form)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("create pet: status %d", rr.Code)
	}

	// Verify pet in DB
	pets, err := database.ListPets(user.ID)
	if err != nil {
		t.Fatalf("ListPets: %v", err)
	}
	if len(pets) != 1 {
		t.Fatalf("pet count = %d, want 1", len(pets))
	}
	if pets[0].Name != "Buddy" {
		t.Errorf("pet name = %q, want Buddy", pets[0].Name)
	}

	// View pets page
	req = authenticatedRequest(t, database, user.ID, "GET", "/pets")
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("pets page: status %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Buddy") {
		t.Error("pets page should contain 'Buddy'")
	}

	// Update pet
	form = url.Values{"name": {"Buddy Boy"}, "description": {"A very good golden retriever"}}
	req = authenticatedFormRequest(t, database, user.ID, "/pets/"+pets[0].ID+"/edit", form)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("update pet: status %d", rr.Code)
	}

	updated, _ := database.GetPet(pets[0].ID)
	if updated.Name != "Buddy Boy" {
		t.Errorf("updated name = %q, want 'Buddy Boy'", updated.Name)
	}

	// Delete pet
	req = authenticatedFormRequest(t, database, user.ID, "/pets/"+pets[0].ID+"/delete", nil)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("delete pet: status %d", rr.Code)
	}

	pets, _ = database.ListPets(user.ID)
	if len(pets) != 0 {
		t.Errorf("pet count after delete = %d, want 0", len(pets))
	}
}

func TestGroups_CRUD(t *testing.T) {
	w, database, user := setupTestWeb(t)
	mux := http.NewServeMux()
	w.RegisterRoutes(mux)

	// Create two pets first
	database.CreatePet(user.ID, "Buddy", "golden retriever")
	database.CreatePet(user.ID, "Max", "tabby cat")
	pets, _ := database.ListPets(user.ID)

	// Create group
	form := url.Values{
		"name":    {"Test Group"},
		"pet_ids": {pets[0].ID, pets[1].ID},
		"weight":  {"1.5"},
	}
	req := authenticatedFormRequest(t, database, user.ID, "/groups", form)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("create group: status %d", rr.Code)
	}

	groups, _ := database.ListGroups(user.ID)
	if len(groups) != 1 {
		t.Fatalf("group count = %d, want 1", len(groups))
	}
	if groups[0].Name != "Test Group" {
		t.Errorf("group name = %q, want 'Test Group'", groups[0].Name)
	}
	if groups[0].Weight != 1.5 {
		t.Errorf("group weight = %f, want 1.5", groups[0].Weight)
	}

	// View groups page
	req = authenticatedRequest(t, database, user.ID, "GET", "/groups")
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("groups page: status %d", rr.Code)
	}

	// Delete group
	req = authenticatedFormRequest(t, database, user.ID, "/groups/"+groups[0].ID+"/delete", nil)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	groups, _ = database.ListGroups(user.ID)
	if len(groups) != 0 {
		t.Errorf("group count after delete = %d, want 0", len(groups))
	}
}

func TestStyles_ToggleAndView(t *testing.T) {
	w, database, user := setupTestWeb(t)
	mux := http.NewServeMux()
	w.RegisterRoutes(mux)

	// View styles page
	req := authenticatedRequest(t, database, user.ID, "GET", "/styles")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("styles: status %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Pop art") {
		t.Error("styles page should contain style description")
	}

	// Toggle style off
	form := url.Values{"index": {"0"}, "enabled": {"false"}}
	req = authenticatedFormRequest(t, database, user.ID, "/styles/toggle", form)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("toggle style: status %d", rr.Code)
	}

	enabled, _ := database.GetEnabledStyles(user.ID, 2)
	if enabled[0] {
		t.Error("style 0 should be disabled after toggle")
	}
	if !enabled[1] {
		t.Error("style 1 should still be enabled")
	}
}

func TestSettings_UpdateLocation(t *testing.T) {
	w, database, user := setupTestWeb(t)
	mux := http.NewServeMux()
	w.RegisterRoutes(mux)

	// View settings
	req := authenticatedRequest(t, database, user.ID, "GET", "/settings")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("settings: status %d", rr.Code)
	}

	// Update location
	form := url.Values{
		"location_name": {"La Crosse"},
		"latitude":      {"43.8"},
		"longitude":     {"-91.2"},
		"timezone":      {"America/Chicago"},
	}
	req = authenticatedFormRequest(t, database, user.ID, "/settings", form)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("update settings: status %d", rr.Code)
	}

	updated, _ := database.GetUser(user.ID)
	if updated.LocationName != "La Crosse" {
		t.Errorf("location = %q, want 'La Crosse'", updated.LocationName)
	}
	if updated.Latitude != 43.8 {
		t.Errorf("lat = %f, want 43.8", updated.Latitude)
	}
}

func TestHistory_EmptyAndPopulated(t *testing.T) {
	w, database, user := setupTestWeb(t)
	mux := http.NewServeMux()
	w.RegisterRoutes(mux)

	// Empty history
	req := authenticatedRequest(t, database, user.ID, "GET", "/history")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("history: status %d", rr.Code)
	}
}

func TestFrames_Page(t *testing.T) {
	w, database, user := setupTestWeb(t)
	mux := http.NewServeMux()
	w.RegisterRoutes(mux)

	req := authenticatedRequest(t, database, user.ID, "GET", "/frames")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("frames: status %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Set Up a New Frame") {
		t.Error("frames page should show setup wizard")
	}
}

func TestClaimPage_Renders(t *testing.T) {
	w, database, user := setupTestWeb(t)
	mux := http.NewServeMux()
	w.RegisterRoutes(mux)

	req := authenticatedRequest(t, database, user.ID, "GET", "/frames/claim")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("claim page: status %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Claim Your Frame") {
		t.Error("claim page should contain 'Claim Your Frame'")
	}
}

func TestClaimFrame_Success(t *testing.T) {
	w, database, user := setupTestWeb(t)
	mux := http.NewServeMux()
	w.RegisterRoutes(mux)

	// Register a pending frame
	database.RegisterPendingFrame("AA:BB:CC:DD:EE:FF", "DOVE-5678", "waveshare", 800, 480)

	// Submit claim
	form := url.Values{"claim_code": {"DOVE-5678"}}
	req := authenticatedFormRequest(t, database, user.ID, "/frames/claim", form)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("claim: status %d, body: %s", rr.Code, rr.Body.String())
	}
	if loc := rr.Header().Get("Location"); loc != "/frames" {
		t.Errorf("redirect to %q, want /frames", loc)
	}

	// Verify frame is in user's frames
	frames, _ := database.ListUserFrames(user.ID)
	if len(frames) != 1 {
		t.Fatalf("user frame count = %d, want 1", len(frames))
	}
}

func TestClaimFrame_InvalidCode(t *testing.T) {
	w, database, user := setupTestWeb(t)
	mux := http.NewServeMux()
	w.RegisterRoutes(mux)

	form := url.Values{"claim_code": {"NOPE-0000"}}
	req := authenticatedFormRequest(t, database, user.ID, "/frames/claim", form)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("invalid claim: status %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "No frame found") {
		t.Error("should show error for invalid code")
	}
}

func TestClaimFrame_EmptyCode(t *testing.T) {
	w, database, user := setupTestWeb(t)
	mux := http.NewServeMux()
	w.RegisterRoutes(mux)

	form := url.Values{"claim_code": {""}}
	req := authenticatedFormRequest(t, database, user.ID, "/frames/claim", form)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("empty claim: status %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Please enter") {
		t.Error("should show error for empty code")
	}
}

func TestFramesPage_ShowsSetupWizard(t *testing.T) {
	w, database, user := setupTestWeb(t)
	mux := http.NewServeMux()
	w.RegisterRoutes(mux)

	req := authenticatedRequest(t, database, user.ID, "GET", "/frames")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("frames: status %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Set Up a New Frame") {
		t.Error("frames page should contain setup wizard")
	}
	if !strings.Contains(body, "esp-web-install-button") {
		t.Error("frames page should contain ESP Web Tools button")
	}
	if !strings.Contains(body, "Enter Claim Code") {
		t.Error("frames page should link to claim page")
	}
}

func TestRootRedirectsToDashboard(t *testing.T) {
	w, _, _ := setupTestWeb(t)
	mux := http.NewServeMux()
	w.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Errorf("root redirect: status %d, want 302", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/dashboard" {
		t.Errorf("redirect to %q, want /dashboard", loc)
	}
}
