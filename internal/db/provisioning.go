package db

import (
	"database/sql"
	"fmt"
	"time"
)

type PendingFrame struct {
	MAC            string
	ClaimCode      string
	HardwareType   string
	DisplayW       int
	DisplayH       int
	FrameID        string
	ProvisionToken string
	UserID         string
	Status         string // "pending", "claimed", "provisioned"
	CreatedAt      time.Time
	ClaimedAt      *time.Time
}

// RegisterPendingFrame creates or updates a pending frame record.
// Idempotent — safe to call on every boot.
func (d *DB) RegisterPendingFrame(mac, claimCode, hardwareType string, displayW, displayH int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.db.Exec(`
		INSERT INTO pending_frames (mac, claim_code, hardware_type, display_w, display_h, status, created_at)
		VALUES (?, ?, ?, ?, ?, 'pending', ?)
		ON CONFLICT(mac) DO UPDATE SET
			claim_code = excluded.claim_code,
			hardware_type = excluded.hardware_type,
			display_w = excluded.display_w,
			display_h = excluded.display_h
	`, mac, claimCode, hardwareType, displayW, displayH, now)
	return err
}

// GetPendingFrameByMAC looks up a pending frame by its MAC address.
func (d *DB) GetPendingFrameByMAC(mac string) (*PendingFrame, error) {
	return d.scanPendingFrame(`SELECT mac, claim_code, hardware_type, display_w, display_h, frame_id, provision_token, user_id, status, created_at, claimed_at FROM pending_frames WHERE mac = ?`, mac)
}

// GetPendingFrameByClaimCode looks up a pending frame by its claim code.
func (d *DB) GetPendingFrameByClaimCode(code string) (*PendingFrame, error) {
	return d.scanPendingFrame(`SELECT mac, claim_code, hardware_type, display_w, display_h, frame_id, provision_token, user_id, status, created_at, claimed_at FROM pending_frames WHERE claim_code = ? AND status = 'pending'`, code)
}

func (d *DB) scanPendingFrame(query string, args ...any) (*PendingFrame, error) {
	var pf PendingFrame
	var frameID, provToken, userID, claimedAt sql.NullString
	var ts string
	err := d.db.QueryRow(query, args...).Scan(
		&pf.MAC, &pf.ClaimCode, &pf.HardwareType, &pf.DisplayW, &pf.DisplayH,
		&frameID, &provToken, &userID, &pf.Status, &ts, &claimedAt,
	)
	if err != nil {
		return nil, err
	}
	pf.FrameID = frameID.String
	pf.ProvisionToken = provToken.String
	pf.UserID = userID.String
	pf.CreatedAt, _ = time.Parse(time.RFC3339, ts)
	if claimedAt.Valid {
		t, _ := time.Parse(time.RFC3339, claimedAt.String)
		pf.ClaimedAt = &t
	}
	return &pf, nil
}

// ClaimFrame links a pending frame to a user. Creates the real frame record,
// generates credentials, and updates the pending record.
// Returns the frame ID and plaintext API token.
func (d *DB) ClaimFrame(mac, userID string) (string, string, error) {
	pf, err := d.GetPendingFrameByMAC(mac)
	if err != nil {
		return "", "", fmt.Errorf("pending frame not found: %w", err)
	}
	if pf.Status != "pending" {
		return "", "", fmt.Errorf("frame already claimed (status: %s)", pf.Status)
	}

	// Create the real frame
	frame, token, err := d.CreateFrame(pf.HardwareType, pf.DisplayW, pf.DisplayH)
	if err != nil {
		return "", "", fmt.Errorf("create frame: %w", err)
	}

	// Link frame to user
	if err := d.SetFrameUser(frame.ID, userID); err != nil {
		return "", "", fmt.Errorf("set frame user: %w", err)
	}

	// Update pending record with credentials
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = d.db.Exec(`
		UPDATE pending_frames SET
			frame_id = ?, provision_token = ?, user_id = ?, status = 'claimed', claimed_at = ?
		WHERE mac = ?
	`, frame.ID, token, userID, now, mac)
	if err != nil {
		return "", "", fmt.Errorf("update pending: %w", err)
	}

	return frame.ID, token, nil
}

// GetProvisionCredentials returns credentials for a claimed frame.
// Returns empty strings and no error if the frame is still pending or unknown.
func (d *DB) GetProvisionCredentials(mac string) (frameID, token string, err error) {
	pf, err := d.GetPendingFrameByMAC(mac)
	if err == sql.ErrNoRows {
		return "", "", nil // unknown MAC — return empty, no error
	}
	if err != nil {
		return "", "", err
	}
	if pf.Status == "pending" {
		return "", "", nil // not yet claimed
	}
	return pf.FrameID, pf.ProvisionToken, nil
}

// MarkProvisioned flips a pending frame to "provisioned" and clears the plaintext token.
func (d *DB) MarkProvisioned(mac string) error {
	_, err := d.db.Exec(`
		UPDATE pending_frames SET status = 'provisioned', provision_token = NULL WHERE mac = ?
	`, mac)
	return err
}

// CleanupExpiredPending removes pending frames older than maxAge.
func (d *DB) CleanupExpiredPending(maxAge time.Duration) (int64, error) {
	cutoff := time.Now().Add(-maxAge).UTC().Format(time.RFC3339)
	result, err := d.db.Exec(`DELETE FROM pending_frames WHERE status = 'pending' AND created_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
