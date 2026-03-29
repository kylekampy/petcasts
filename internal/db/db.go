package db

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	db *sql.DB
}

type Frame struct {
	ID              string
	Name            string
	APITokenHash    string
	HardwareType    string
	DisplayW        int
	DisplayH        int
	LastSeenAt      *time.Time
	BatteryPct      *float64
	FirmwareVersion string
}

type Generation struct {
	ID           int64
	FrameID      string
	CreatedAt    time.Time
	Pets         string // JSON array of pet names
	Style        string
	SceneJSON    string
	OriginalPath string
	DitheredPath string
	WeatherJSON  string
	ForceRegen   bool
}

type SelectionEntry struct {
	CreatedAt     time.Time
	PetNames      string
	Photo         string
	Style         string
	SceneActivity string
}

func Open(dataDir string) (*DB, error) {
	dbPath := filepath.Join(dataDir, "petcast.db")
	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	if _, err := sqlDB.Exec("PRAGMA journal_mode=WAL"); err != nil {
		sqlDB.Close()
		return nil, err
	}
	d := &DB{db: sqlDB}
	if err := d.migrate(); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) Close() error {
	return d.db.Close()
}

func (d *DB) migrate() error {
	_, err := d.db.Exec(`
		CREATE TABLE IF NOT EXISTS frames (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			api_token_hash TEXT NOT NULL UNIQUE,
			hardware_type TEXT NOT NULL DEFAULT '',
			display_w INTEGER NOT NULL DEFAULT 800,
			display_h INTEGER NOT NULL DEFAULT 480,
			last_seen_at TEXT,
			battery_pct REAL,
			firmware_version TEXT NOT NULL DEFAULT ''
		);

		CREATE TABLE IF NOT EXISTS generations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			frame_id TEXT NOT NULL,
			created_at TEXT NOT NULL,
			pets TEXT NOT NULL,
			style TEXT NOT NULL,
			scene_json TEXT NOT NULL DEFAULT '{}',
			original_path TEXT NOT NULL DEFAULT '',
			dithered_path TEXT NOT NULL DEFAULT '',
			weather_json TEXT NOT NULL DEFAULT '{}',
			force_regen INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY (frame_id) REFERENCES frames(id)
		);

		CREATE TABLE IF NOT EXISTS daily_state (
			frame_id TEXT NOT NULL,
			date TEXT NOT NULL,
			generated INTEGER NOT NULL DEFAULT 0,
			force_regen_used INTEGER NOT NULL DEFAULT 0,
			dithered_path TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (frame_id, date)
		);

		CREATE TABLE IF NOT EXISTS selection_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at TEXT NOT NULL,
			pet_names TEXT NOT NULL,
			photo TEXT NOT NULL DEFAULT '',
			style TEXT NOT NULL DEFAULT '',
			scene_activity TEXT NOT NULL DEFAULT ''
		);

		CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			email TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL DEFAULT '',
			picture_url TEXT NOT NULL DEFAULT '',
			google_id TEXT UNIQUE,
			timezone TEXT NOT NULL DEFAULT 'America/Chicago',
			location_name TEXT NOT NULL DEFAULT '',
			latitude REAL NOT NULL DEFAULT 0,
			longitude REAL NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS sessions (
			token_hash TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			created_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			FOREIGN KEY (user_id) REFERENCES users(id)
		);

		CREATE TABLE IF NOT EXISTS user_pets (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			portrait_path TEXT NOT NULL DEFAULT '',
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			FOREIGN KEY (user_id) REFERENCES users(id)
		);

		CREATE TABLE IF NOT EXISTS user_photos (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			filename TEXT NOT NULL,
			path TEXT NOT NULL,
			pet_ids TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL,
			FOREIGN KEY (user_id) REFERENCES users(id)
		);

		CREATE TABLE IF NOT EXISTS user_groups (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			name TEXT NOT NULL,
			pet_ids TEXT NOT NULL DEFAULT '[]',
			weight REAL NOT NULL DEFAULT 1.0,
			FOREIGN KEY (user_id) REFERENCES users(id)
		);

		CREATE TABLE IF NOT EXISTS user_styles (
			user_id TEXT NOT NULL,
			style_index INTEGER NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			PRIMARY KEY (user_id, style_index),
			FOREIGN KEY (user_id) REFERENCES users(id)
		);
	`)
	if err != nil {
		return err
	}

	_, err = d.db.Exec(`
		CREATE TABLE IF NOT EXISTS pending_frames (
			mac TEXT PRIMARY KEY,
			claim_code TEXT NOT NULL,
			hardware_type TEXT NOT NULL DEFAULT '',
			display_w INTEGER NOT NULL DEFAULT 800,
			display_h INTEGER NOT NULL DEFAULT 480,
			frame_id TEXT,
			provision_token TEXT,
			user_id TEXT,
			status TEXT NOT NULL DEFAULT 'pending',
			created_at TEXT NOT NULL,
			claimed_at TEXT,
			FOREIGN KEY (user_id) REFERENCES users(id)
		);
	`)
	if err != nil {
		return err
	}

	// Add user_id to frames if not already present (migration)
	d.db.Exec(`ALTER TABLE frames ADD COLUMN user_id TEXT REFERENCES users(id)`)
	// Add user_id to selection_history if not present
	d.db.Exec(`ALTER TABLE selection_history ADD COLUMN user_id TEXT`)

	return nil
}

// GenerateToken creates a cryptographically random API token.
func GenerateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// HashToken produces a SHA-256 hex digest of a token.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func generateID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// CreateFrame registers a new frame. Returns the frame and the plaintext token (only shown once).
func (d *DB) CreateFrame(hardwareType string, displayW, displayH int) (*Frame, string, error) {
	id := generateID()
	token := GenerateToken()
	hash := HashToken(token)
	_, err := d.db.Exec(
		`INSERT INTO frames (id, api_token_hash, hardware_type, display_w, display_h) VALUES (?, ?, ?, ?, ?)`,
		id, hash, hardwareType, displayW, displayH,
	)
	if err != nil {
		return nil, "", err
	}
	return &Frame{ID: id, APITokenHash: hash, HardwareType: hardwareType, DisplayW: displayW, DisplayH: displayH}, token, nil
}

// GetFrameByToken looks up a frame by its plaintext API token.
func (d *DB) GetFrameByToken(token string) (*Frame, error) {
	hash := HashToken(token)
	return d.getFrameByHash(hash)
}

func (d *DB) getFrameByHash(hash string) (*Frame, error) {
	var f Frame
	var lastSeen sql.NullString
	var batteryPct sql.NullFloat64
	err := d.db.QueryRow(
		`SELECT id, name, api_token_hash, hardware_type, display_w, display_h, last_seen_at, battery_pct, firmware_version
		 FROM frames WHERE api_token_hash = ?`, hash,
	).Scan(&f.ID, &f.Name, &f.APITokenHash, &f.HardwareType, &f.DisplayW, &f.DisplayH, &lastSeen, &batteryPct, &f.FirmwareVersion)
	if err != nil {
		return nil, err
	}
	if lastSeen.Valid {
		t, _ := time.Parse(time.RFC3339, lastSeen.String)
		f.LastSeenAt = &t
	}
	if batteryPct.Valid {
		f.BatteryPct = &batteryPct.Float64
	}
	return &f, nil
}

// GetFrameByID looks up a frame by ID.
func (d *DB) GetFrameByID(id string) (*Frame, error) {
	var f Frame
	var lastSeen sql.NullString
	var batteryPct sql.NullFloat64
	err := d.db.QueryRow(
		`SELECT id, name, api_token_hash, hardware_type, display_w, display_h, last_seen_at, battery_pct, firmware_version
		 FROM frames WHERE id = ?`, id,
	).Scan(&f.ID, &f.Name, &f.APITokenHash, &f.HardwareType, &f.DisplayW, &f.DisplayH, &lastSeen, &batteryPct, &f.FirmwareVersion)
	if err != nil {
		return nil, err
	}
	if lastSeen.Valid {
		t, _ := time.Parse(time.RFC3339, lastSeen.String)
		f.LastSeenAt = &t
	}
	if batteryPct.Valid {
		f.BatteryPct = &batteryPct.Float64
	}
	return &f, nil
}

// UpdateFrameSeen updates last_seen_at and optionally battery_pct.
func (d *DB) UpdateFrameSeen(frameID string, batteryPct *float64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if batteryPct != nil {
		_, err := d.db.Exec(`UPDATE frames SET last_seen_at = ?, battery_pct = ? WHERE id = ?`, now, *batteryPct, frameID)
		return err
	}
	_, err := d.db.Exec(`UPDATE frames SET last_seen_at = ? WHERE id = ?`, now, frameID)
	return err
}

// GetDailyState checks generation state for a frame on a given date (YYYY-MM-DD).
func (d *DB) GetDailyState(frameID, date string) (generated bool, forceUsed bool, ditheredPath string, err error) {
	err = d.db.QueryRow(
		`SELECT generated, force_regen_used, dithered_path FROM daily_state WHERE frame_id = ? AND date = ?`,
		frameID, date,
	).Scan(&generated, &forceUsed, &ditheredPath)
	if err == sql.ErrNoRows {
		return false, false, "", nil
	}
	return
}

// SetDailyGenerated marks a frame as having generated for a date.
func (d *DB) SetDailyGenerated(frameID, date, ditheredPath string, forceRegen bool) error {
	_, err := d.db.Exec(`
		INSERT INTO daily_state (frame_id, date, generated, force_regen_used, dithered_path)
		VALUES (?, ?, 1, ?, ?)
		ON CONFLICT(frame_id, date) DO UPDATE SET
			generated = 1,
			dithered_path = ?,
			force_regen_used = CASE WHEN ? THEN 1 ELSE force_regen_used END
	`, frameID, date, boolToInt(forceRegen), ditheredPath, ditheredPath, forceRegen)
	return err
}

// RecordGeneration stores a generation record.
func (d *DB) RecordGeneration(g *Generation) error {
	_, err := d.db.Exec(`
		INSERT INTO generations (frame_id, created_at, pets, style, scene_json, original_path, dithered_path, weather_json, force_regen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, g.FrameID, g.CreatedAt.UTC().Format(time.RFC3339), g.Pets, g.Style, g.SceneJSON, g.OriginalPath, g.DitheredPath, g.WeatherJSON, boolToInt(g.ForceRegen))
	return err
}

// GetSelectionHistory returns the N most recent selection entries (newest first).
func (d *DB) GetSelectionHistory(limit int) ([]SelectionEntry, error) {
	rows, err := d.db.Query(
		`SELECT created_at, pet_names, photo, style, scene_activity FROM selection_history ORDER BY id DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []SelectionEntry
	for rows.Next() {
		var e SelectionEntry
		var ts string
		if err := rows.Scan(&ts, &e.PetNames, &e.Photo, &e.Style, &e.SceneActivity); err != nil {
			return nil, err
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339, ts)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// RecordSelection adds a selection to history and prunes old entries.
func (d *DB) RecordSelection(petNames, photo, style, sceneActivity string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := d.db.Exec(
		`INSERT INTO selection_history (created_at, pet_names, photo, style, scene_activity) VALUES (?, ?, ?, ?, ?)`,
		now, petNames, photo, style, sceneActivity,
	); err != nil {
		return err
	}
	cutoff := time.Now().AddDate(0, 0, -90).UTC().Format(time.RFC3339)
	_, err := d.db.Exec(`DELETE FROM selection_history WHERE created_at < ?`, cutoff)
	return err
}

// LatestGeneration returns the most recent generation for a frame, or nil.
func (d *DB) LatestGeneration(frameID string) (*Generation, error) {
	var g Generation
	var ts string
	var fr int
	err := d.db.QueryRow(
		`SELECT id, frame_id, created_at, pets, style, scene_json, original_path, dithered_path, weather_json, force_regen
		 FROM generations WHERE frame_id = ? ORDER BY id DESC LIMIT 1`, frameID,
	).Scan(&g.ID, &g.FrameID, &ts, &g.Pets, &g.Style, &g.SceneJSON, &g.OriginalPath, &g.DitheredPath, &g.WeatherJSON, &fr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	g.CreatedAt, _ = time.Parse(time.RFC3339, ts)
	g.ForceRegen = fr == 1
	return &g, nil
}

// ListGenerations returns recent generations across all frames.
func (d *DB) ListGenerations(limit int) ([]Generation, error) {
	rows, err := d.db.Query(
		`SELECT id, frame_id, created_at, pets, style, scene_json, original_path, dithered_path, weather_json, force_regen
		 FROM generations ORDER BY created_at DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var gens []Generation
	for rows.Next() {
		var g Generation
		var ts string
		var fr int
		if err := rows.Scan(&g.ID, &g.FrameID, &ts, &g.Pets, &g.Style, &g.SceneJSON, &g.OriginalPath, &g.DitheredPath, &g.WeatherJSON, &fr); err != nil {
			return nil, err
		}
		g.CreatedAt, _ = time.Parse(time.RFC3339, ts)
		g.ForceRegen = fr == 1
		gens = append(gens, g)
	}
	return gens, rows.Err()
}

// ListFrames returns all registered frames.
func (d *DB) ListFrames() ([]Frame, error) {
	rows, err := d.db.Query(
		`SELECT id, name, api_token_hash, hardware_type, display_w, display_h, last_seen_at, battery_pct, firmware_version FROM frames`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var frames []Frame
	for rows.Next() {
		var f Frame
		var lastSeen sql.NullString
		var batteryPct sql.NullFloat64
		if err := rows.Scan(&f.ID, &f.Name, &f.APITokenHash, &f.HardwareType, &f.DisplayW, &f.DisplayH, &lastSeen, &batteryPct, &f.FirmwareVersion); err != nil {
			return nil, err
		}
		if lastSeen.Valid {
			t, _ := time.Parse(time.RFC3339, lastSeen.String)
			f.LastSeenAt = &t
		}
		if batteryPct.Valid {
			f.BatteryPct = &batteryPct.Float64
		}
		frames = append(frames, f)
	}
	return frames, rows.Err()
}
