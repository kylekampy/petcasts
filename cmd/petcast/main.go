package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"

	// Register image decoders
	_ "image/jpeg"
	_ "image/png"

	"github.com/kylekampy/petcasts/internal/config"
	"github.com/kylekampy/petcasts/internal/db"
	"github.com/kylekampy/petcasts/internal/gemini"
	"github.com/kylekampy/petcasts/internal/pipeline"
	"github.com/kylekampy/petcasts/internal/server"
	"github.com/kylekampy/petcasts/internal/storage"
)

func main() {
	var (
		port     int
		dataDir  string
		pairCode string
	)
	flag.IntVar(&port, "port", 7777, "HTTP server port")
	flag.StringVar(&dataDir, "data", ".", "Data directory (contains config.yaml, pets/)")
	flag.StringVar(&pairCode, "pairing-code", "", "Frame pairing code (auto-generated if empty)")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Load .env (won't override existing env vars)
	envPath := filepath.Join(dataDir, ".env")
	if err := godotenv.Load(envPath); err != nil {
		logger.Debug("no .env file loaded", "path", envPath, "error", err)
	}

	// Load config
	cfg, err := config.Load(dataDir)
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	logger.Info("config loaded",
		"location", cfg.Location.Name,
		"pets", len(cfg.Pets),
		"styles", len(cfg.Styles),
	)

	// Gemini API key
	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey == "" {
		logger.Error("GOOGLE_API_KEY environment variable is required")
		os.Exit(1)
	}

	// Open database
	database, err := db.Open(dataDir)
	if err != nil {
		logger.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	// Storage
	store := storage.NewLocal(dataDir)

	// Gemini client
	geminiClient := gemini.NewClient(apiKey)

	// Pipeline
	pipe := &pipeline.Pipeline{
		Config: cfg,
		DB:     database,
		Store:  store,
		Gemini: geminiClient,
		Logger: logger.With("component", "pipeline"),
	}

	// Pairing code
	if pairCode == "" {
		pairCode = generatePairingCode()
	}

	// Server
	srv := server.New(cfg, database, store, pipe, pairCode, logger.With("component", "server"))
	addr := fmt.Sprintf(":%d", port)
	logger.Info("petcast server starting",
		"addr", addr,
		"pairing_code", pairCode,
		"data_dir", dataDir,
	)
	if err := srv.ListenAndServe(addr); err != nil {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}

func generatePairingCode() string {
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("PC-%s", hex.EncodeToString(b))
}
