package security

import (
	"strings"
	"testing"
)

func TestHashPasswordProducesBcrypt(t *testing.T) {
	hash, err := HashPassword("admin_user")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if len(hash) < 50 || !strings.HasPrefix(hash, "$2") {
		t.Fatalf("expected bcrypt hash, got %q", hash)
	}
	if ok, legacy := VerifyPassword(hash, "admin_user"); !ok || legacy {
		t.Fatalf("VerifyPassword() = (%v,%v), want (true,false)", ok, legacy)
	}
}

func TestVerifyPasswordAcceptsLegacyMD5(t *testing.T) {
	if ok, legacy := VerifyPassword("3c85cdebade1c51cf64ca9f3c09d182d", "admin_user"); !ok || !legacy {
		t.Fatalf("VerifyPassword() = (%v,%v), want (true,true)", ok, legacy)
	}
}

func TestIsLegacyPasswordHash(t *testing.T) {
	if !IsLegacyPasswordHash("3c85cdebade1c51cf64ca9f3c09d182d") {
		t.Fatal("expected 32-char hex MD5 hash to be legacy")
	}
	hash, err := HashPassword("admin_user")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if IsLegacyPasswordHash(hash) {
		t.Fatalf("expected bcrypt hash not to be legacy: %q", hash)
	}
}
