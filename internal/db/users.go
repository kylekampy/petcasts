package db

import (
	"database/sql"
	"time"
)

type User struct {
	ID           string
	Email        string
	Name         string
	PictureURL   string
	GoogleID     string
	Timezone     string
	LocationName string
	Latitude     float64
	Longitude    float64
	CreatedAt    time.Time
}

type Session struct {
	TokenHash string
	UserID    string
	CreatedAt time.Time
	ExpiresAt time.Time
}

type UserPet struct {
	ID           string
	UserID       string
	Name         string
	Description  string
	PortraitPath string
	SortOrder    int
	CreatedAt    time.Time
}

type UserPhoto struct {
	ID        string
	UserID    string
	Filename  string
	Path      string
	PetIDs    string // JSON array
	CreatedAt time.Time
}

type UserGroup struct {
	ID     string
	UserID string
	Name   string
	PetIDs string // JSON array
	Weight float64
}

type UserStyle struct {
	UserID     string
	StyleIndex int
	Enabled    bool
}

// --- User CRUD ---

func (d *DB) UpsertUserByGoogle(googleID, email, name, pictureURL string) (*User, error) {
	var u User
	err := d.db.QueryRow(`SELECT id FROM users WHERE google_id = ?`, googleID).Scan(&u.ID)
	if err == sql.ErrNoRows {
		id := generateID()
		now := time.Now().UTC().Format(time.RFC3339)
		_, err = d.db.Exec(
			`INSERT INTO users (id, email, name, picture_url, google_id, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			id, email, name, pictureURL, googleID, now,
		)
		if err != nil {
			return nil, err
		}
		u.ID = id
	} else if err != nil {
		return nil, err
	} else {
		_, err = d.db.Exec(
			`UPDATE users SET email = ?, name = ?, picture_url = ? WHERE google_id = ?`,
			email, name, pictureURL, googleID,
		)
		if err != nil {
			return nil, err
		}
	}
	return d.GetUser(u.ID)
}

func (d *DB) GetUser(id string) (*User, error) {
	var u User
	var googleID sql.NullString
	var ts string
	err := d.db.QueryRow(
		`SELECT id, email, name, picture_url, google_id, timezone, location_name, latitude, longitude, created_at FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.Email, &u.Name, &u.PictureURL, &googleID, &u.Timezone, &u.LocationName, &u.Latitude, &u.Longitude, &ts)
	if err != nil {
		return nil, err
	}
	if googleID.Valid {
		u.GoogleID = googleID.String
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339, ts)
	return &u, nil
}

func (d *DB) UpdateUserLocation(userID, locationName string, lat, lon float64, timezone string) error {
	_, err := d.db.Exec(
		`UPDATE users SET location_name = ?, latitude = ?, longitude = ?, timezone = ? WHERE id = ?`,
		locationName, lat, lon, timezone, userID,
	)
	return err
}

// --- Sessions ---

func (d *DB) CreateSession(userID string, ttl time.Duration) (string, error) {
	token := GenerateToken()
	hash := HashToken(token)
	now := time.Now().UTC()
	_, err := d.db.Exec(
		`INSERT INTO sessions (token_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		hash, userID, now.Format(time.RFC3339), now.Add(ttl).Format(time.RFC3339),
	)
	if err != nil {
		return "", err
	}
	return token, nil
}

func (d *DB) GetSession(token string) (*Session, error) {
	hash := HashToken(token)
	var s Session
	var created, expires string
	err := d.db.QueryRow(
		`SELECT token_hash, user_id, created_at, expires_at FROM sessions WHERE token_hash = ?`, hash,
	).Scan(&s.TokenHash, &s.UserID, &created, &expires)
	if err != nil {
		return nil, err
	}
	s.CreatedAt, _ = time.Parse(time.RFC3339, created)
	s.ExpiresAt, _ = time.Parse(time.RFC3339, expires)
	if time.Now().After(s.ExpiresAt) {
		d.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, hash)
		return nil, sql.ErrNoRows
	}
	return &s, nil
}

func (d *DB) DeleteSession(token string) error {
	hash := HashToken(token)
	_, err := d.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, hash)
	return err
}

func (d *DB) DeleteExpiredSessions() error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, now)
	return err
}

// --- User Pets ---

func (d *DB) CreatePet(userID, name, description string) (*UserPet, error) {
	id := generateID()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.db.Exec(
		`INSERT INTO user_pets (id, user_id, name, description, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, userID, name, description, now,
	)
	if err != nil {
		return nil, err
	}
	return d.GetPet(id)
}

func (d *DB) GetPet(id string) (*UserPet, error) {
	var p UserPet
	var ts string
	err := d.db.QueryRow(
		`SELECT id, user_id, name, description, portrait_path, sort_order, created_at FROM user_pets WHERE id = ?`, id,
	).Scan(&p.ID, &p.UserID, &p.Name, &p.Description, &p.PortraitPath, &p.SortOrder, &ts)
	if err != nil {
		return nil, err
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339, ts)
	return &p, nil
}

func (d *DB) ListPets(userID string) ([]UserPet, error) {
	rows, err := d.db.Query(
		`SELECT id, user_id, name, description, portrait_path, sort_order, created_at FROM user_pets WHERE user_id = ? ORDER BY sort_order, name`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var pets []UserPet
	for rows.Next() {
		var p UserPet
		var ts string
		if err := rows.Scan(&p.ID, &p.UserID, &p.Name, &p.Description, &p.PortraitPath, &p.SortOrder, &ts); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339, ts)
		pets = append(pets, p)
	}
	return pets, rows.Err()
}

func (d *DB) UpdatePet(id, name, description string) error {
	_, err := d.db.Exec(`UPDATE user_pets SET name = ?, description = ? WHERE id = ?`, name, description, id)
	return err
}

func (d *DB) UpdatePetPortrait(id, portraitPath string) error {
	_, err := d.db.Exec(`UPDATE user_pets SET portrait_path = ? WHERE id = ?`, portraitPath, id)
	return err
}

func (d *DB) DeletePet(id string) error {
	_, err := d.db.Exec(`DELETE FROM user_pets WHERE id = ?`, id)
	return err
}

// --- User Photos ---

func (d *DB) CreatePhoto(userID, filename, path, petIDs string) (*UserPhoto, error) {
	id := generateID()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.db.Exec(
		`INSERT INTO user_photos (id, user_id, filename, path, pet_ids, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, userID, filename, path, petIDs, now,
	)
	if err != nil {
		return nil, err
	}
	return &UserPhoto{ID: id, UserID: userID, Filename: filename, Path: path, PetIDs: petIDs}, nil
}

func (d *DB) ListPhotos(userID string) ([]UserPhoto, error) {
	rows, err := d.db.Query(
		`SELECT id, user_id, filename, path, pet_ids, created_at FROM user_photos WHERE user_id = ? ORDER BY created_at DESC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var photos []UserPhoto
	for rows.Next() {
		var p UserPhoto
		var ts string
		if err := rows.Scan(&p.ID, &p.UserID, &p.Filename, &p.Path, &p.PetIDs, &ts); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339, ts)
		photos = append(photos, p)
	}
	return photos, rows.Err()
}

func (d *DB) DeletePhoto(id string) error {
	_, err := d.db.Exec(`DELETE FROM user_photos WHERE id = ?`, id)
	return err
}

// --- User Groups ---

func (d *DB) CreateGroup(userID, name, petIDs string, weight float64) (*UserGroup, error) {
	id := generateID()
	_, err := d.db.Exec(
		`INSERT INTO user_groups (id, user_id, name, pet_ids, weight) VALUES (?, ?, ?, ?, ?)`,
		id, userID, name, petIDs, weight,
	)
	if err != nil {
		return nil, err
	}
	return &UserGroup{ID: id, UserID: userID, Name: name, PetIDs: petIDs, Weight: weight}, nil
}

func (d *DB) ListGroups(userID string) ([]UserGroup, error) {
	rows, err := d.db.Query(
		`SELECT id, user_id, name, pet_ids, weight FROM user_groups WHERE user_id = ? ORDER BY name`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var groups []UserGroup
	for rows.Next() {
		var g UserGroup
		if err := rows.Scan(&g.ID, &g.UserID, &g.Name, &g.PetIDs, &g.Weight); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

func (d *DB) UpdateGroup(id, name, petIDs string, weight float64) error {
	_, err := d.db.Exec(`UPDATE user_groups SET name = ?, pet_ids = ?, weight = ? WHERE id = ?`, name, petIDs, weight, id)
	return err
}

func (d *DB) DeleteGroup(id string) error {
	_, err := d.db.Exec(`DELETE FROM user_groups WHERE id = ?`, id)
	return err
}

// --- User Styles ---

func (d *DB) GetEnabledStyles(userID string, totalStyles int) ([]bool, error) {
	enabled := make([]bool, totalStyles)
	for i := range enabled {
		enabled[i] = true // default all enabled
	}

	rows, err := d.db.Query(`SELECT style_index, enabled FROM user_styles WHERE user_id = ?`, userID)
	if err != nil {
		return enabled, nil // return defaults on error
	}
	defer rows.Close()
	for rows.Next() {
		var idx, en int
		if err := rows.Scan(&idx, &en); err != nil {
			continue
		}
		if idx >= 0 && idx < totalStyles {
			enabled[idx] = en == 1
		}
	}
	return enabled, nil
}

func (d *DB) SetStyleEnabled(userID string, styleIndex int, enabled bool) error {
	en := 0
	if enabled {
		en = 1
	}
	_, err := d.db.Exec(`
		INSERT INTO user_styles (user_id, style_index, enabled) VALUES (?, ?, ?)
		ON CONFLICT(user_id, style_index) DO UPDATE SET enabled = ?
	`, userID, styleIndex, en, en)
	return err
}

// --- Frame user association ---

func (d *DB) SetFrameUser(frameID, userID string) error {
	_, err := d.db.Exec(`UPDATE frames SET user_id = ? WHERE id = ?`, userID, frameID)
	return err
}

func (d *DB) ListUserFrames(userID string) ([]Frame, error) {
	rows, err := d.db.Query(
		`SELECT id, name, api_token_hash, hardware_type, display_w, display_h, last_seen_at, battery_pct, firmware_version
		 FROM frames WHERE user_id = ?`, userID,
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

// ListUserGenerations returns recent generations for a specific user.
func (d *DB) ListUserGenerations(userID string, limit int) ([]Generation, error) {
	rows, err := d.db.Query(`
		SELECT g.id, g.frame_id, g.created_at, g.pets, g.style, g.scene_json, g.original_path, g.dithered_path, g.weather_json, g.force_regen
		FROM generations g
		JOIN frames f ON g.frame_id = f.id
		WHERE f.user_id = ?
		ORDER BY g.id DESC LIMIT ?
	`, userID, limit)
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
