package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kylekampy/petcasts/internal/config"
	"github.com/kylekampy/petcasts/internal/db"
	"github.com/kylekampy/petcasts/internal/pipeline"
	"github.com/kylekampy/petcasts/internal/storage"
)

// WebApp is the interface the web dashboard must implement.
type WebApp interface {
	RegisterRoutes(mux *http.ServeMux)
}

type Server struct {
	Config      *config.Config
	DB          *db.DB
	Store       *storage.Local
	Pipeline    *pipeline.Pipeline
	PairingCode string
	Logger      *slog.Logger

	web        WebApp
	mu         sync.Mutex
	generating map[string]bool // frameID -> currently generating
}

// SetWeb attaches the web dashboard to the server.
func (s *Server) SetWeb(w WebApp) {
	s.web = w
}

func New(cfg *config.Config, database *db.DB, store *storage.Local, pipe *pipeline.Pipeline, pairingCode string, logger *slog.Logger) *Server {
	return &Server{
		Config:      cfg,
		DB:          database,
		Store:       store,
		Pipeline:    pipe,
		PairingCode: pairingCode,
		Logger:      logger,
		generating:  make(map[string]bool),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Frame API
	mux.HandleFunc("POST /api/v1/pair", s.handlePair)
	mux.HandleFunc("POST /api/v1/frame/{id}/generate", s.handleFrameGenerate)
	mux.HandleFunc("GET /api/v1/frame/{id}/image", s.handleFrameImage)

	// Status / debug
	mux.HandleFunc("GET /api/v1/status", s.handleStatus)
	mux.HandleFunc("GET /api/v1/generations", s.handleListGenerations)

	// Serve stored files (images)
	mux.Handle("GET /files/", http.StripPrefix("/files/", http.FileServer(http.Dir(s.Store.Root))))

	// Static files for web dashboard
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))

	// Web dashboard routes
	if s.web != nil {
		s.web.RegisterRoutes(mux)
	}

	return logMiddleware(mux, s.Logger)
}

func (s *Server) ListenAndServe(addr string) error {
	s.Logger.Info("server starting", "addr", addr, "pairing_code", s.PairingCode)
	srv := &http.Server{
		Addr:         addr,
		Handler:      s.Handler(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
	return srv.ListenAndServe()
}

// --- Handlers ---

type pairRequest struct {
	Code         string `json:"code"`
	HardwareType string `json:"hardware_type"`
	DisplayW     int    `json:"display_w"`
	DisplayH     int    `json:"display_h"`
}

type pairResponse struct {
	FrameID string `json:"frame_id"`
	Token   string `json:"token"`
}

func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	var req pairRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Code != s.PairingCode {
		jsonError(w, http.StatusUnauthorized, "invalid pairing code")
		return
	}

	if req.DisplayW == 0 {
		req.DisplayW = 800
	}
	if req.DisplayH == 0 {
		req.DisplayH = 480
	}

	frame, token, err := s.DB.CreateFrame(req.HardwareType, req.DisplayW, req.DisplayH)
	if err != nil {
		s.Logger.Error("failed to create frame", "error", err)
		jsonError(w, http.StatusInternalServerError, "failed to create frame")
		return
	}

	s.Logger.Info("frame paired", "frame_id", frame.ID, "hardware", req.HardwareType)
	jsonResponse(w, http.StatusOK, pairResponse{
		FrameID: frame.ID,
		Token:   token,
	})
}

// handleFrameGenerate triggers async generation. Returns immediately:
//   - 202: generation started (frame should wait ~2min then GET /image)
//   - 429: already generated today (image is ready, just GET /image)
//   - 409: generation already in progress
type generateRequest struct {
	Battery *float64 `json:"battery"`
	Force   bool     `json:"force"`
}

func (s *Server) handleFrameGenerate(w http.ResponseWriter, r *http.Request) {
	frame, ok := s.authenticateFrame(w, r)
	if !ok {
		return
	}

	var req generateRequest
	if r.ContentLength > 0 {
		json.NewDecoder(r.Body).Decode(&req)
	}

	// Update frame last seen
	if err := s.DB.UpdateFrameSeen(frame.ID, req.Battery); err != nil {
		s.Logger.Error("failed to update frame seen", "frame_id", frame.ID, "error", err)
	}

	// Check daily state
	today := time.Now().Format("2006-01-02")
	generated, forceUsed, _, err := s.DB.GetDailyState(frame.ID, today)
	if err != nil {
		s.Logger.Error("daily state check failed", "error", err)
	}

	needsGeneration := !generated || (req.Force && !forceUsed)

	if !needsGeneration {
		s.Logger.Info("rate limited: already generated today", "frame_id", frame.ID)
		jsonResponse(w, http.StatusTooManyRequests, map[string]string{
			"status":  "already_generated_today",
			"message": "Image is ready. GET /image to fetch it.",
		})
		return
	}

	if s.Pipeline == nil {
		jsonError(w, http.StatusServiceUnavailable, "generation pipeline not configured")
		return
	}

	// Check if already generating
	s.mu.Lock()
	if s.generating[frame.ID] {
		s.mu.Unlock()
		jsonResponse(w, http.StatusConflict, map[string]string{
			"status":  "already_generating",
			"message": "Generation already in progress.",
		})
		return
	}
	s.generating[frame.ID] = true
	s.mu.Unlock()

	// Return 202 immediately, run pipeline in background
	s.Logger.Info("generation accepted", "frame_id", frame.ID, "battery", req.Battery, "force", req.Force)
	jsonResponse(w, http.StatusAccepted, map[string]string{
		"status":  "accepted",
		"message": "Generation started. Wait ~2 minutes, then GET /image.",
	})

	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.generating, frame.ID)
			s.mu.Unlock()
		}()

		result, err := s.Pipeline.Run(frame.ID, req.Battery, req.Force)
		if err != nil {
			s.Logger.Error("generation failed", "frame_id", frame.ID, "error", err)
			return
		}

		if err := s.DB.SetDailyGenerated(frame.ID, today, result.DitheredPath, req.Force); err != nil {
			s.Logger.Error("failed to record daily state", "frame_id", frame.ID, "error", err)
		}
		s.Logger.Info("generation complete", "frame_id", frame.ID, "path", result.DitheredPath)
	}()
}

// handleFrameImage serves the latest dithered image for a frame.
// Returns the PNG if available, or an appropriate status if not.
func (s *Server) handleFrameImage(w http.ResponseWriter, r *http.Request) {
	frame, ok := s.authenticateFrame(w, r)
	if !ok {
		return
	}

	// Update frame last seen
	var batteryPct *float64
	if b := r.URL.Query().Get("battery"); b != "" {
		if v, err := strconv.ParseFloat(b, 64); err == nil {
			batteryPct = &v
		}
	}
	if err := s.DB.UpdateFrameSeen(frame.ID, batteryPct); err != nil {
		s.Logger.Error("failed to update frame seen", "frame_id", frame.ID, "error", err)
	}

	// Check if generating
	s.mu.Lock()
	isGenerating := s.generating[frame.ID]
	s.mu.Unlock()

	// Check daily state
	today := time.Now().Format("2006-01-02")
	_, _, ditheredPath, _ := s.DB.GetDailyState(frame.ID, today)

	if ditheredPath == "" {
		// No image for today — check for any previous image
		if gen, err := s.DB.LatestGeneration(frame.ID); err == nil && gen != nil {
			ditheredPath = gen.DitheredPath
		}
	}

	if ditheredPath == "" {
		status := http.StatusNotFound
		msg := "no image available"
		if isGenerating {
			status = http.StatusAccepted
			msg = "generation in progress, try again shortly"
		}
		jsonError(w, status, msg)
		return
	}

	s.serveDitheredImage(w, ditheredPath, isGenerating)
}

func (s *Server) serveDitheredImage(w http.ResponseWriter, relPath string, generating bool) {
	data, err := s.Store.Load(relPath)
	if err != nil {
		s.Logger.Error("failed to load image", "path", relPath, "error", err)
		jsonError(w, http.StatusInternalServerError, "failed to load image")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	if generating {
		w.Header().Set("X-Generating", "true")
	}
	w.Write(data)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	frames, err := s.DB.ListFrames()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to list frames")
		return
	}

	s.mu.Lock()
	genCopy := make(map[string]bool, len(s.generating))
	for k, v := range s.generating {
		genCopy[k] = v
	}
	s.mu.Unlock()

	type frameStatus struct {
		ID         string   `json:"id"`
		Name       string   `json:"name,omitempty"`
		Hardware   string   `json:"hardware_type,omitempty"`
		BatteryPct *float64 `json:"battery_pct,omitempty"`
		Generating bool     `json:"generating"`
	}

	statuses := make([]frameStatus, len(frames))
	for i, f := range frames {
		statuses[i] = frameStatus{
			ID:         f.ID,
			Name:       f.Name,
			Hardware:   f.HardwareType,
			BatteryPct: f.BatteryPct,
			Generating: genCopy[f.ID],
		}
	}

	jsonResponse(w, http.StatusOK, map[string]any{
		"frames": statuses,
	})
}

func (s *Server) handleListGenerations(w http.ResponseWriter, r *http.Request) {
	gens, err := s.DB.ListGenerations(50)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to list generations")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"generations": gens})
}

// --- Auth helper ---

func (s *Server) authenticateFrame(w http.ResponseWriter, r *http.Request) (*db.Frame, bool) {
	frameID := r.PathValue("id")
	token := extractBearerToken(r)
	if token == "" {
		jsonError(w, http.StatusUnauthorized, "missing bearer token")
		return nil, false
	}
	frame, err := s.DB.GetFrameByToken(token)
	if err != nil {
		jsonError(w, http.StatusUnauthorized, "invalid token")
		return nil, false
	}
	if frame.ID != frameID {
		jsonError(w, http.StatusForbidden, "token does not match frame")
		return nil, false
	}
	return frame, true
}

// --- Helpers ---

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return auth[7:]
	}
	return ""
}

func jsonResponse(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, code int, message string) {
	jsonResponse(w, code, map[string]string{"error": message})
}

func logMiddleware(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, statusCode: 200}
		next.ServeHTTP(rw, r)
		logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.statusCode,
			"duration", time.Since(start).Round(time.Millisecond),
		)
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
