package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	stateCookieName = "oauth_state"
	stateCookieTTL  = 10 * time.Minute
	userInfoURL     = "https://www.googleapis.com/oauth2/v2/userinfo"
)

// GoogleAuth handles the Google OAuth2 login and callback flow.
type GoogleAuth struct {
	config   *oauth2.Config
	sessions *SessionManager
}

// NewGoogleAuth creates a GoogleAuth with the provided OAuth2 credentials and session manager.
func NewGoogleAuth(clientID, clientSecret, redirectURL string, sessions *SessionManager) *GoogleAuth {
	cfg := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint:     google.Endpoint,
	}
	return &GoogleAuth{
		config:   cfg,
		sessions: sessions,
	}
}

// HandleLogin generates a random state token, stores it in a short-lived cookie,
// and redirects the user to Google's OAuth2 consent page.
func (g *GoogleAuth) HandleLogin(w http.ResponseWriter, r *http.Request) {
	state, err := generateState()
	if err != nil {
		http.Error(w, "failed to generate state", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/",
		Expires:  time.Now().Add(stateCookieTTL),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	url := g.config.AuthCodeURL(state, oauth2.AccessTypeOnline)
	http.Redirect(w, r, url, http.StatusFound)
}

// HandleCallback validates the OAuth2 state, exchanges the code for a token,
// fetches the user's info from Google, upserts the user in the DB,
// creates a session, and redirects to /dashboard.
func (g *GoogleAuth) HandleCallback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie(stateCookieName)
	if err != nil || stateCookie.Value != r.FormValue("state") {
		http.Error(w, "invalid oauth state", http.StatusBadRequest)
		return
	}

	// Clear the state cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})

	code := r.FormValue("code")
	token, err := g.config.Exchange(context.Background(), code)
	if err != nil {
		http.Error(w, "failed to exchange code", http.StatusInternalServerError)
		return
	}

	userInfo, err := fetchUserInfo(g.config.Client(context.Background(), token))
	if err != nil {
		http.Error(w, "failed to fetch user info", http.StatusInternalServerError)
		return
	}

	user, err := g.sessions.db.UpsertUserByGoogle(userInfo.ID, userInfo.Email, userInfo.Name, userInfo.Picture)
	if err != nil {
		http.Error(w, "failed to upsert user", http.StatusInternalServerError)
		return
	}

	if err := g.sessions.CreateSession(w, user.ID); err != nil {
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/dashboard", http.StatusFound)
}

// googleUserInfo is the subset of fields returned by the Google userinfo endpoint.
type googleUserInfo struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

func fetchUserInfo(client *http.Client) (*googleUserInfo, error) {
	resp, err := client.Get(userInfoURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo: unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var info googleUserInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
