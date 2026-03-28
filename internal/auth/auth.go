package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/kylekampy/petcasts/internal/db"
)

const (
	cookieName     = "petcast_session"
	sessionTTL     = 30 * 24 * time.Hour
)

// contextKey is a private type for context values set by this package.
type contextKey int

const userContextKey contextKey = 0

// SessionManager handles session creation, lookup, and cookie management.
type SessionManager struct {
	db *db.DB
}

// NewSessionManager creates a SessionManager backed by the given DB.
func NewSessionManager(database *db.DB) *SessionManager {
	return &SessionManager{db: database}
}

// CreateSession creates a session in the DB and sets a secure httpOnly cookie on the response.
func (s *SessionManager) CreateSession(w http.ResponseWriter, userID string) error {
	token, err := s.db.CreateSession(userID, sessionTTL)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(sessionTTL),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// GetCurrentUser reads the session cookie, validates the session, and returns the associated user.
// Returns an error if the cookie is missing, the session is expired or missing, or the user cannot be found.
func (s *SessionManager) GetCurrentUser(r *http.Request) (*db.User, error) {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return nil, err
	}
	session, err := s.db.GetSession(cookie.Value)
	if err != nil {
		return nil, err
	}
	return s.db.GetUser(session.UserID)
}

// Logout deletes the session from the DB and clears the session cookie.
func (s *SessionManager) Logout(w http.ResponseWriter, r *http.Request) error {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		// No cookie — nothing to clear.
		return nil
	}
	if err := s.db.DeleteSession(cookie.Value); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// RequireAuth is middleware that ensures the request has a valid session.
// If not authenticated, it redirects to /login.
// On success, it adds the user to the request context.
func (s *SessionManager) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := s.GetCurrentUser(r)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// UserFromContext retrieves the authenticated user from the request context.
// Returns nil if no user is present.
func UserFromContext(ctx context.Context) *db.User {
	user, _ := ctx.Value(userContextKey).(*db.User)
	return user
}

// ErrNoSession is returned when there is no active session for the request.
var ErrNoSession = errors.New("auth: no active session")
