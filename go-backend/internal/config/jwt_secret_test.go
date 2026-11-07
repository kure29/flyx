package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureJWTSecretKeepsEnvValue(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{DBPath: filepath.Join(dir, "gost.db"), JWTSecret: "from-env"}

	generated, err := EnsureJWTSecret(&cfg)
	if err != nil {
		t.Fatalf("EnsureJWTSecret: %v", err)
	}
	if generated || cfg.JWTSecret != "from-env" {
		t.Fatalf("got generated=%v secret=%q, want env value kept", generated, cfg.JWTSecret)
	}
	if _, err := os.Stat(filepath.Join(dir, jwtSecretFileName)); !os.IsNotExist(err) {
		t.Fatalf("secret file should not be written when JWT_SECRET is set")
	}
}

func TestEnsureJWTSecretGeneratesAndReuses(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	cfg := Config{DBPath: filepath.Join(dir, "gost.db")}

	generated, err := EnsureJWTSecret(&cfg)
	if err != nil {
		t.Fatalf("EnsureJWTSecret: %v", err)
	}
	if !generated || len(cfg.JWTSecret) != 64 {
		t.Fatalf("got generated=%v secret=%q, want a new 64-char secret", generated, cfg.JWTSecret)
	}
	info, err := os.Stat(filepath.Join(dir, jwtSecretFileName))
	if err != nil {
		t.Fatalf("stat secret file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("secret file perm = %o, want 600", perm)
	}

	again := Config{DBPath: cfg.DBPath}
	generated, err = EnsureJWTSecret(&again)
	if err != nil {
		t.Fatalf("EnsureJWTSecret (reuse): %v", err)
	}
	if generated || again.JWTSecret != cfg.JWTSecret {
		t.Fatalf("got generated=%v secret=%q, want the stored secret %q", generated, again.JWTSecret, cfg.JWTSecret)
	}
}
