package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kylekampy/petcasts/internal/auth"
	"github.com/kylekampy/petcasts/internal/config"
	"github.com/kylekampy/petcasts/internal/db"
	"github.com/kylekampy/petcasts/internal/storage"
)

// Web serves the dashboard UI.
type Web struct {
	Config      *config.Config
	DB          *db.DB
	Store       *storage.Local
	Sessions    *auth.SessionManager
	Google      *auth.GoogleAuth
	PairingCode string
	Logger      *slog.Logger
	templates   map[string]*template.Template
}

func New(cfg *config.Config, database *db.DB, store *storage.Local, sessions *auth.SessionManager, google *auth.GoogleAuth, pairingCode string, logger *slog.Logger, templateDir string) (*Web, error) {
	w := &Web{
		Config:      cfg,
		DB:          database,
		Store:       store,
		Sessions:    sessions,
		Google:      google,
		PairingCode: pairingCode,
		Logger:      logger,
	}
	if err := w.loadTemplates(templateDir); err != nil {
		return nil, fmt.Errorf("load templates: %w", err)
	}
	return w, nil
}

func (w *Web) loadTemplates(dir string) error {
	funcMap := template.FuncMap{
		"deref": func(p *float64) float64 {
			if p == nil {
				return 0
			}
			return *p
		},
		"json": func(v any) string {
			b, _ := json.Marshal(v)
			return string(b)
		},
		"truncate": func(s string, n int) string {
			if len(s) <= n {
				return s
			}
			return s[:n] + "..."
		},
		"timeago": func(t time.Time) string {
			d := time.Since(t)
			switch {
			case d < time.Minute:
				return "just now"
			case d < time.Hour:
				return fmt.Sprintf("%dm ago", int(d.Minutes()))
			case d < 24*time.Hour:
				return fmt.Sprintf("%dh ago", int(d.Hours()))
			default:
				return fmt.Sprintf("%dd ago", int(d.Hours()/24))
			}
		},
		"formatDate": func(t time.Time) string {
			return t.Format("Jan 2, 2006 3:04 PM")
		},
	}

	basePath := filepath.Join(dir, "base.html")
	w.templates = make(map[string]*template.Template)

	// Login is standalone (no base)
	loginTmpl, err := template.New("login.html").Funcs(funcMap).ParseFiles(filepath.Join(dir, "login.html"))
	if err != nil {
		return fmt.Errorf("parse login.html: %w", err)
	}
	w.templates["login"] = loginTmpl

	// All other pages extend base
	pages := []string{"dashboard", "pets", "groups", "styles", "history", "frames", "claim", "settings"}
	for _, page := range pages {
		pagePath := filepath.Join(dir, page+".html")
		tmpl, err := template.New("base.html").Funcs(funcMap).ParseFiles(basePath, pagePath)
		if err != nil {
			return fmt.Errorf("parse %s.html: %w", page, err)
		}
		w.templates[page] = tmpl
	}
	return nil
}

func (w *Web) render(wr http.ResponseWriter, name string, data map[string]any) {
	tmpl, ok := w.templates[name]
	if !ok {
		http.Error(wr, "template not found", http.StatusInternalServerError)
		return
	}
	wr.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(wr, data); err != nil {
		w.Logger.Error("template render error", "template", name, "error", err)
	}
}

// RegisterRoutes adds all web routes to the mux.
func (w *Web) RegisterRoutes(mux *http.ServeMux) {
	// Public routes
	mux.HandleFunc("GET /login", w.handleLogin)
	mux.HandleFunc("GET /logout", w.handleLogout)
	mux.HandleFunc("GET /auth/google/login", w.Google.HandleLogin)
	mux.HandleFunc("GET /auth/google/callback", w.Google.HandleCallback)

	// Authenticated routes
	mux.Handle("GET /dashboard", w.Sessions.RequireAuth(http.HandlerFunc(w.handleDashboard)))
	mux.Handle("POST /dashboard/regenerate", w.Sessions.RequireAuth(http.HandlerFunc(w.handleRegenerate)))

	mux.Handle("GET /pets", w.Sessions.RequireAuth(http.HandlerFunc(w.handlePets)))
	mux.Handle("POST /pets", w.Sessions.RequireAuth(http.HandlerFunc(w.handleCreatePet)))
	mux.Handle("POST /pets/{id}/edit", w.Sessions.RequireAuth(http.HandlerFunc(w.handleUpdatePet)))
	mux.Handle("POST /pets/{id}/portrait", w.Sessions.RequireAuth(http.HandlerFunc(w.handleUploadPortrait)))
	mux.Handle("POST /pets/{id}/delete", w.Sessions.RequireAuth(http.HandlerFunc(w.handleDeletePet)))

	mux.Handle("GET /groups", w.Sessions.RequireAuth(http.HandlerFunc(w.handleGroups)))
	mux.Handle("POST /groups", w.Sessions.RequireAuth(http.HandlerFunc(w.handleCreateGroup)))
	mux.Handle("POST /groups/{id}/edit", w.Sessions.RequireAuth(http.HandlerFunc(w.handleUpdateGroup)))
	mux.Handle("POST /groups/{id}/delete", w.Sessions.RequireAuth(http.HandlerFunc(w.handleDeleteGroup)))

	mux.Handle("GET /styles", w.Sessions.RequireAuth(http.HandlerFunc(w.handleStyles)))
	mux.Handle("POST /styles/toggle", w.Sessions.RequireAuth(http.HandlerFunc(w.handleToggleStyle)))

	mux.Handle("GET /history", w.Sessions.RequireAuth(http.HandlerFunc(w.handleHistory)))

	mux.Handle("GET /frames", w.Sessions.RequireAuth(http.HandlerFunc(w.handleFrames)))
	mux.Handle("GET /frames/claim", w.Sessions.RequireAuth(http.HandlerFunc(w.handleFramesClaim)))
	mux.Handle("POST /frames/claim", w.Sessions.RequireAuth(http.HandlerFunc(w.handleClaimFrame)))

	mux.Handle("GET /settings", w.Sessions.RequireAuth(http.HandlerFunc(w.handleSettings)))
	mux.Handle("POST /settings", w.Sessions.RequireAuth(http.HandlerFunc(w.handleUpdateSettings)))

	// Root redirect
	mux.HandleFunc("GET /{$}", func(rw http.ResponseWriter, r *http.Request) {
		http.Redirect(rw, r, "/dashboard", http.StatusFound)
	})
}

// --- Page Handlers ---

func (w *Web) handleLogin(rw http.ResponseWriter, r *http.Request) {
	// Dev mode: auto-create session and redirect to dashboard
	if devID := w.Sessions.DevUserID(); devID != "" {
		w.Sessions.CreateSession(rw, devID)
		http.Redirect(rw, r, "/dashboard", http.StatusFound)
		return
	}
	w.render(rw, "login", nil)
}

func (w *Web) handleLogout(rw http.ResponseWriter, r *http.Request) {
	w.Sessions.Logout(rw, r)
	http.Redirect(rw, r, "/login", http.StatusFound)
}

func (w *Web) handleDashboard(rw http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	frames, _ := w.DB.ListUserFrames(user.ID)
	pets, _ := w.DB.ListPets(user.ID)
	gens, _ := w.DB.ListUserGenerations(user.ID, 1)

	var todayImage string
	if len(gens) > 0 {
		todayImage = gens[0].OriginalPath
	}

	totalGens, _ := w.DB.ListUserGenerations(user.ID, 9999)

	w.render(rw, "dashboard", map[string]any{
		"User":             user,
		"Frames":           frames,
		"TodayImage":       todayImage,
		"TotalPets":        len(pets),
		"TotalGenerations": len(totalGens),
	})
}

func (w *Web) handleRegenerate(rw http.ResponseWriter, r *http.Request) {
	// This just redirects back — actual regen is triggered by the frame
	http.Redirect(rw, r, "/dashboard", http.StatusFound)
}

func (w *Web) handlePets(rw http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	pets, _ := w.DB.ListPets(user.ID)
	w.render(rw, "pets", map[string]any{
		"User": user,
		"Pets": pets,
	})
}

func (w *Web) handleCreatePet(rw http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	name := strings.TrimSpace(r.FormValue("name"))
	desc := strings.TrimSpace(r.FormValue("description"))
	if name == "" {
		http.Redirect(rw, r, "/pets", http.StatusFound)
		return
	}
	if _, err := w.DB.CreatePet(user.ID, name, desc); err != nil {
		w.Logger.Error("create pet failed", "error", err)
	}
	http.Redirect(rw, r, "/pets", http.StatusFound)
}

func (w *Web) handleUpdatePet(rw http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	petID := r.PathValue("id")
	pet, err := w.DB.GetPet(petID)
	if err != nil || pet.UserID != user.ID {
		http.Error(rw, "not found", http.StatusNotFound)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	desc := strings.TrimSpace(r.FormValue("description"))
	if name != "" {
		w.DB.UpdatePet(petID, name, desc)
	}
	http.Redirect(rw, r, "/pets", http.StatusFound)
}

func (w *Web) handleUploadPortrait(rw http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	petID := r.PathValue("id")
	pet, err := w.DB.GetPet(petID)
	if err != nil || pet.UserID != user.ID {
		http.Error(rw, "not found", http.StatusNotFound)
		return
	}

	r.ParseMultipartForm(10 << 20) // 10MB
	file, header, err := r.FormFile("portrait")
	if err != nil {
		http.Redirect(rw, r, "/pets", http.StatusFound)
		return
	}
	defer file.Close()

	ext := filepath.Ext(header.Filename)
	relPath := filepath.Join("portraits", user.ID, petID+ext)
	fullPath, err := w.Store.SaveReader(relPath, file)
	if err != nil {
		w.Logger.Error("save portrait failed", "error", err)
		http.Redirect(rw, r, "/pets", http.StatusFound)
		return
	}
	_ = fullPath
	w.DB.UpdatePetPortrait(petID, relPath)
	http.Redirect(rw, r, "/pets", http.StatusFound)
}

func (w *Web) handleDeletePet(rw http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	petID := r.PathValue("id")
	pet, err := w.DB.GetPet(petID)
	if err != nil || pet.UserID != user.ID {
		http.Error(rw, "not found", http.StatusNotFound)
		return
	}
	// Delete portrait file if exists
	if pet.PortraitPath != "" {
		os.Remove(w.Store.FullPath(pet.PortraitPath))
	}
	w.DB.DeletePet(petID)
	http.Redirect(rw, r, "/pets", http.StatusFound)
}

func (w *Web) handleGroups(rw http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	groups, _ := w.DB.ListGroups(user.ID)
	pets, _ := w.DB.ListPets(user.ID)

	// Build pet name lookup
	petMap := make(map[string]string)
	for _, p := range pets {
		petMap[p.ID] = p.Name
	}

	type groupView struct {
		db.UserGroup
		PetNames []string
	}
	var views []groupView
	for _, g := range groups {
		var ids []string
		json.Unmarshal([]byte(g.PetIDs), &ids)
		var names []string
		for _, id := range ids {
			if name, ok := petMap[id]; ok {
				names = append(names, name)
			}
		}
		views = append(views, groupView{UserGroup: g, PetNames: names})
	}

	w.render(rw, "groups", map[string]any{
		"User":   user,
		"Groups": views,
		"Pets":   pets,
	})
}

func (w *Web) handleCreateGroup(rw http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	name := strings.TrimSpace(r.FormValue("name"))
	petIDs := r.Form["pet_ids"]
	weight, _ := strconv.ParseFloat(r.FormValue("weight"), 64)
	if weight <= 0 {
		weight = 1.0
	}
	if name == "" {
		http.Redirect(rw, r, "/groups", http.StatusFound)
		return
	}
	petIDsJSON, _ := json.Marshal(petIDs)
	w.DB.CreateGroup(user.ID, name, string(petIDsJSON), weight)
	http.Redirect(rw, r, "/groups", http.StatusFound)
}

func (w *Web) handleUpdateGroup(rw http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	groupID := r.PathValue("id")
	groups, _ := w.DB.ListGroups(user.ID)
	var found bool
	for _, g := range groups {
		if g.ID == groupID {
			found = true
			break
		}
	}
	if !found {
		http.Error(rw, "not found", http.StatusNotFound)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	petIDs := r.Form["pet_ids"]
	weight, _ := strconv.ParseFloat(r.FormValue("weight"), 64)
	if weight <= 0 {
		weight = 1.0
	}
	petIDsJSON, _ := json.Marshal(petIDs)
	w.DB.UpdateGroup(groupID, name, string(petIDsJSON), weight)
	http.Redirect(rw, r, "/groups", http.StatusFound)
}

func (w *Web) handleDeleteGroup(rw http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	groupID := r.PathValue("id")
	groups, _ := w.DB.ListGroups(user.ID)
	for _, g := range groups {
		if g.ID == groupID {
			w.DB.DeleteGroup(groupID)
			break
		}
	}
	_ = user
	http.Redirect(rw, r, "/groups", http.StatusFound)
}

func (w *Web) handleStyles(rw http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	enabled, _ := w.DB.GetEnabledStyles(user.ID, len(w.Config.Styles))

	type styleView struct {
		Index       int
		Description string
		Enabled     bool
	}
	var styles []styleView
	for i, desc := range w.Config.Styles {
		styles = append(styles, styleView{
			Index:       i,
			Description: desc,
			Enabled:     enabled[i],
		})
	}

	w.render(rw, "styles", map[string]any{
		"User":   user,
		"Styles": styles,
	})
}

func (w *Web) handleToggleStyle(rw http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	idx, err := strconv.Atoi(r.FormValue("index"))
	if err != nil || idx < 0 || idx >= len(w.Config.Styles) {
		http.Error(rw, "invalid style index", http.StatusBadRequest)
		return
	}
	enabled := r.FormValue("enabled") == "on" || r.FormValue("enabled") == "true"
	w.DB.SetStyleEnabled(user.ID, idx, enabled)
	http.Redirect(rw, r, "/styles", http.StatusFound)
}

func (w *Web) handleHistory(rw http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	gens, _ := w.DB.ListUserGenerations(user.ID, 100)
	w.render(rw, "history", map[string]any{
		"User":        user,
		"Generations": gens,
	})
}

func (w *Web) handleFrames(rw http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	frames, _ := w.DB.ListUserFrames(user.ID)
	w.render(rw, "frames", map[string]any{
		"User":   user,
		"Frames": frames,
	})
}

func (w *Web) handleFramesClaim(rw http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	w.render(rw, "claim", map[string]any{
		"User": user,
	})
}

func (w *Web) handleClaimFrame(rw http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	code := strings.TrimSpace(strings.ToUpper(r.FormValue("claim_code")))
	if code == "" {
		w.render(rw, "claim", map[string]any{
			"User":  user,
			"Error": "Please enter a claim code.",
		})
		return
	}

	// Look up pending frame by claim code
	pf, err := w.DB.GetPendingFrameByClaimCode(code)
	if err != nil {
		w.render(rw, "claim", map[string]any{
			"User":  user,
			"Error": "No frame found with that code. Make sure the code on your frame's screen matches.",
		})
		return
	}

	// Claim it
	_, _, err = w.DB.ClaimFrame(pf.MAC, user.ID)
	if err != nil {
		w.Logger.Error("claim frame failed", "mac", pf.MAC, "error", err)
		w.render(rw, "claim", map[string]any{
			"User":  user,
			"Error": "Failed to claim frame. It may have already been claimed.",
		})
		return
	}

	http.Redirect(rw, r, "/frames", http.StatusFound)
}

func (w *Web) handleSettings(rw http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	timezones := []string{
		"America/New_York", "America/Chicago", "America/Denver", "America/Los_Angeles",
		"America/Anchorage", "Pacific/Honolulu", "Europe/London", "Europe/Paris",
		"Europe/Berlin", "Asia/Tokyo", "Asia/Shanghai", "Australia/Sydney",
	}
	w.render(rw, "settings", map[string]any{
		"User":      user,
		"Timezones": timezones,
	})
}

func (w *Web) handleUpdateSettings(rw http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	locName := strings.TrimSpace(r.FormValue("location_name"))
	lat, _ := strconv.ParseFloat(r.FormValue("latitude"), 64)
	lon, _ := strconv.ParseFloat(r.FormValue("longitude"), 64)
	tz := r.FormValue("timezone")
	if tz == "" {
		tz = user.Timezone
	}
	w.DB.UpdateUserLocation(user.ID, locName, lat, lon, tz)
	http.Redirect(rw, r, "/settings", http.StatusFound)
}

// SaveReader helper on storage.Local for io.Reader
func init() {
	// Ensure storage.Local implements SaveReader (compile-time check done in storage package)
}

// saveUpload saves an uploaded file to storage and returns the relative path.
func (w *Web) saveUpload(userID, subdir, filename string, file io.Reader) (string, error) {
	ext := filepath.Ext(filename)
	relPath := filepath.Join(subdir, userID, db.GenerateToken()[:16]+ext)
	_, err := w.Store.SaveReader(relPath, file)
	return relPath, err
}
