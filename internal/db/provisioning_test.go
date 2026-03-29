package db

import (
	"testing"
	"time"
)

func TestRegisterPendingFrame(t *testing.T) {
	d := testDB(t)

	err := d.RegisterPendingFrame("AA:BB:CC:DD:EE:FF", "WREN-4827", "waveshare", 800, 480)
	if err != nil {
		t.Fatalf("RegisterPendingFrame: %v", err)
	}

	pf, err := d.GetPendingFrameByMAC("AA:BB:CC:DD:EE:FF")
	if err != nil {
		t.Fatalf("GetPendingFrameByMAC: %v", err)
	}
	if pf.ClaimCode != "WREN-4827" {
		t.Errorf("ClaimCode = %q, want WREN-4827", pf.ClaimCode)
	}
	if pf.Status != "pending" {
		t.Errorf("Status = %q, want pending", pf.Status)
	}
	if pf.HardwareType != "waveshare" {
		t.Errorf("HardwareType = %q, want waveshare", pf.HardwareType)
	}
}

func TestRegisterPendingFrame_Idempotent(t *testing.T) {
	d := testDB(t)

	d.RegisterPendingFrame("AA:BB:CC:DD:EE:FF", "WREN-4827", "waveshare", 800, 480)
	err := d.RegisterPendingFrame("AA:BB:CC:DD:EE:FF", "WREN-4827", "waveshare", 800, 480)
	if err != nil {
		t.Fatalf("second register should be idempotent: %v", err)
	}
}

func TestGetPendingFrameByClaimCode(t *testing.T) {
	d := testDB(t)

	d.RegisterPendingFrame("AA:BB:CC:DD:EE:FF", "HAWK-1234", "waveshare", 800, 480)

	pf, err := d.GetPendingFrameByClaimCode("HAWK-1234")
	if err != nil {
		t.Fatalf("GetPendingFrameByClaimCode: %v", err)
	}
	if pf.MAC != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("MAC = %q, want AA:BB:CC:DD:EE:FF", pf.MAC)
	}

	// Non-existent code
	_, err = d.GetPendingFrameByClaimCode("NOPE-0000")
	if err == nil {
		t.Error("expected error for non-existent code")
	}
}

func TestClaimFrame_Success(t *testing.T) {
	d := testDB(t)

	// Create a user
	user, _ := d.UpsertUserByGoogle("google-test", "test@test.com", "Test", "")

	// Register a pending frame
	d.RegisterPendingFrame("AA:BB:CC:DD:EE:FF", "DOVE-5678", "waveshare", 800, 480)

	// Claim it
	frameID, token, err := d.ClaimFrame("AA:BB:CC:DD:EE:FF", user.ID)
	if err != nil {
		t.Fatalf("ClaimFrame: %v", err)
	}
	if frameID == "" {
		t.Error("frameID is empty")
	}
	if token == "" {
		t.Error("token is empty")
	}

	// Verify pending frame is claimed
	pf, _ := d.GetPendingFrameByMAC("AA:BB:CC:DD:EE:FF")
	if pf.Status != "claimed" {
		t.Errorf("Status = %q, want claimed", pf.Status)
	}
	if pf.FrameID != frameID {
		t.Errorf("FrameID = %q, want %q", pf.FrameID, frameID)
	}
	if pf.UserID != user.ID {
		t.Errorf("UserID = %q, want %q", pf.UserID, user.ID)
	}

	// Verify real frame was created and linked to user
	frame, err := d.GetFrameByID(frameID)
	if err != nil {
		t.Fatalf("GetFrameByID: %v", err)
	}
	if frame.HardwareType != "waveshare" {
		t.Errorf("frame HardwareType = %q, want waveshare", frame.HardwareType)
	}

	// Verify frame appears in user's frames
	frames, _ := d.ListUserFrames(user.ID)
	if len(frames) != 1 {
		t.Errorf("user frame count = %d, want 1", len(frames))
	}
}

func TestClaimFrame_AlreadyClaimed(t *testing.T) {
	d := testDB(t)

	user, _ := d.UpsertUserByGoogle("google-test", "test@test.com", "Test", "")
	d.RegisterPendingFrame("AA:BB:CC:DD:EE:FF", "FERN-9999", "waveshare", 800, 480)

	// First claim succeeds
	_, _, err := d.ClaimFrame("AA:BB:CC:DD:EE:FF", user.ID)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}

	// Second claim fails
	_, _, err = d.ClaimFrame("AA:BB:CC:DD:EE:FF", user.ID)
	if err == nil {
		t.Error("second claim should fail")
	}
}

func TestGetProvisionCredentials(t *testing.T) {
	d := testDB(t)

	user, _ := d.UpsertUserByGoogle("google-test", "test@test.com", "Test", "")
	d.RegisterPendingFrame("AA:BB:CC:DD:EE:FF", "PINE-1111", "waveshare", 800, 480)

	// Before claim: empty
	fid, tok, err := d.GetProvisionCredentials("AA:BB:CC:DD:EE:FF")
	if err != nil {
		t.Fatalf("GetProvisionCredentials (pending): %v", err)
	}
	if fid != "" || tok != "" {
		t.Errorf("expected empty credentials before claim, got fid=%q tok=%q", fid, tok)
	}

	// Unknown MAC: empty, no error
	fid, tok, err = d.GetProvisionCredentials("00:00:00:00:00:00")
	if err != nil {
		t.Fatalf("GetProvisionCredentials (unknown): %v", err)
	}
	if fid != "" || tok != "" {
		t.Error("expected empty credentials for unknown MAC")
	}

	// After claim: credentials
	d.ClaimFrame("AA:BB:CC:DD:EE:FF", user.ID)
	fid, tok, err = d.GetProvisionCredentials("AA:BB:CC:DD:EE:FF")
	if err != nil {
		t.Fatalf("GetProvisionCredentials (claimed): %v", err)
	}
	if fid == "" || tok == "" {
		t.Error("expected credentials after claim")
	}
}

func TestMarkProvisioned(t *testing.T) {
	d := testDB(t)

	user, _ := d.UpsertUserByGoogle("google-test", "test@test.com", "Test", "")
	d.RegisterPendingFrame("AA:BB:CC:DD:EE:FF", "SAGE-2222", "waveshare", 800, 480)
	d.ClaimFrame("AA:BB:CC:DD:EE:FF", user.ID)

	err := d.MarkProvisioned("AA:BB:CC:DD:EE:FF")
	if err != nil {
		t.Fatalf("MarkProvisioned: %v", err)
	}

	pf, _ := d.GetPendingFrameByMAC("AA:BB:CC:DD:EE:FF")
	if pf.Status != "provisioned" {
		t.Errorf("Status = %q, want provisioned", pf.Status)
	}
	if pf.ProvisionToken != "" {
		t.Error("token should be cleared after provisioned")
	}
}

func TestCleanupExpiredPending(t *testing.T) {
	d := testDB(t)

	d.RegisterPendingFrame("AA:BB:CC:DD:EE:FF", "LUNA-3333", "waveshare", 800, 480)

	// Cleanup with very short duration — the row was just created, so use a negative duration
	// to force cleanup of everything
	removed, err := d.CleanupExpiredPending(-1 * time.Hour)
	if err != nil {
		t.Fatalf("CleanupExpiredPending: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}

	_, err = d.GetPendingFrameByMAC("AA:BB:CC:DD:EE:FF")
	if err == nil {
		t.Error("frame should have been cleaned up")
	}
}
