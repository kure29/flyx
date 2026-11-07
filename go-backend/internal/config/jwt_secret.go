package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const jwtSecretFileName = "jwt_secret"

// EnsureJWTSecret fills cfg.JWTSecret when JWT_SECRET is unset. Signing
// tokens with an empty key would let anyone forge an admin token, so the
// secret is read from (or generated into) a file next to the SQLite
// database, which keeps sessions valid across restarts.
func EnsureJWTSecret(cfg *Config) (generated bool, err error) {
	if strings.TrimSpace(cfg.JWTSecret) != "" {
		return false, nil
	}

	path := filepath.Join(filepath.Dir(cfg.DBPath), jwtSecretFileName)
	data, err := os.ReadFile(path)
	if err == nil {
		if secret := strings.TrimSpace(string(data)); secret != "" {
			cfg.JWTSecret = secret
			return false, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("read %s: %w", path, err)
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return false, fmt.Errorf("generate jwt secret: %w", err)
	}
	secret := hex.EncodeToString(buf)

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(secret+"\n"), 0o600); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	cfg.JWTSecret = secret
	return true, nil
}
