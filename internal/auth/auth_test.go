package auth

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kylekampy/petcasts/internal/db"
)

// testSetup opens a temp DB and returns a SessionManager and a test user.
func testSetup(t *testing.T) (*db.DB, *SessionManager, *db.User) {
	t.Helper()
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open() error: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	user, err := database.UpsertUserByGoogle("google-001", "test@example.com", "Test User", "https://example.com/pic.jpg")
	if err != nil {
		t.Fatalf("UpsertUserByGoogle() error: %v", err)
	}

	sessions := NewSessionManager(database)
	return database, sessions, user
}

// TestSessionRoundtrip creates a session and verifies GetCurrentUser returns the correct user.
func TestSessionRoundtrip(t *testing.T) {
	_, sessions, user := testSetup(t)

	w := httptest.NewRecorder()
	if err := sessions.CreateSession(w, user.ID); err != nil {
		t.Fatalf("CreateSession() error: %v", err)
	}

	// Build a request with the session cookie.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range w.Result().Cookies() {
		req.AddCookie(c)
	}

	got, err := sessions.GetCurrentUser(req)
	if err != nil {
		t.Fatalf("GetCurrentUser() error: %v", err)
	}
	if got.ID != user.ID {
		t.Errorf("user ID = %q, want %q", got.ID, user.ID)
	}
	if got.Email != user.Email {
		t.Errorf("user email = %q, want %q", got.Email, user.Email)
	}
}

// TestExpiredSession verifies that GetCurrentUser returns an error for an expired session.
func TestExpiredSession(t *testing.T) {
	database, sessions, user := testSetup(t)

	// Create a session that expires in the past.
	token, err := database.CreateSession(user.ID, -1*time.Second)
	if err != nil {
		t.Fatalf("CreateSession() error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: token})

	_, err = sessions.GetCurrentUser(req)
	if err == nil {
		t.Fatal("GetCurrentUser() should return error for expired session, got nil")
	}
	if err != sql.ErrNoRows {
		t.Errorf("GetCurrentUser() error = %v, want sql.ErrNoRows", err)
	}
}

// TestRequireAuthRedirectsWhenNoSession verifies that the middleware redirects unauthenticated requests.
func TestRequireAuthRedirectsWhenNoSession(t *testing.T) {
	_, sessions, _ := testSetup(t)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := sessions.RequireAuth(next)

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusFound)
	}
	if loc := w.Header().Get("Location"); loc != "/login" {
		t.Errorf("Location = %q, want %q", loc, "/login")
	}
}

// TestRequireAuthPassesThroughWhenAuthenticated verifies the middleware calls next and sets context.
func TestRequireAuthPassesThroughWhenAuthenticated(t *testing.T) {
	_, sessions, user := testSetup(t)

	// Create a session cookie.
	setCookieW := httptest.NewRecorder()
	if err := sessions.CreateSession(setCookieW, user.ID); err != nil {
		t.Fatalf("CreateSession() error: %v", err)
	}

	var ctxUser *db.User
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxUser = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	handler := sessions.RequireAuth(next)

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	for _, c := range setCookieW.Result().Cookies() {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ctxUser == nil {
		t.Fatal("UserFromContext() returned nil inside authenticated handler")
	}
	if ctxUser.ID != user.ID {
		t.Errorf("context user ID = %q, want %q", ctxUser.ID, user.ID)
	}
}

// TestLogoutClearsSession verifies that after logout, the session is invalid.
func TestLogoutClearsSession(t *testing.T) {
	_, sessions, user := testSetup(t)

	// Create session.
	createW := httptest.NewRecorder()
	if err := sessions.CreateSession(createW, user.ID); err != nil {
		t.Fatalf("CreateSession() error: %v", err)
	}
	cookies := createW.Result().Cookies()

	// Logout using a request carrying that cookie.
	logoutReq := httptest.NewRequest(http.MethodPost, "/logout", nil)
	for _, c := range cookies {
		logoutReq.AddCookie(c)
	}
	logoutW := httptest.NewRecorder()
	if err := sessions.Logout(logoutW, logoutReq); err != nil {
		t.Fatalf("Logout() error: %v", err)
	}

	// Subsequent request with the same cookie should fail.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	_, err := sessions.GetCurrentUser(req)
	if err == nil {
		t.Fatal("GetCurrentUser() should return error after logout, got nil")
	}
}

// TestUserFromContextNilWhenAbsent verifies UserFromContext returns nil for an empty context.
func TestUserFromContextNilWhenAbsent(t *testing.T) {
	got := UserFromContext(context.Background())
	if got != nil {
		t.Errorf("UserFromContext() = %v, want nil", got)
	}
}
