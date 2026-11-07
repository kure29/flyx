package repo

import (
	"path/filepath"
	"testing"
	"time"
)

func TestGetUserAuthStateReturnsPasswordChangedAt(t *testing.T) {
	r, err := Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer r.Close()

	now := time.Now().UnixMilli()
	userID, err := r.CreateUser("admin_user", "pwd", 0, 2727251700000, 99999, 1, 99999, 1, 0, now)
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	state, err := r.GetUserAuthState(userID)
	if err != nil {
		t.Fatalf("GetUserAuthState() error = %v", err)
	}
	if state == nil || state.PasswordChangedAt != now || state.Status != 1 || state.RoleID != 0 {
		t.Fatalf("unexpected auth state: %+v", state)
	}
}
