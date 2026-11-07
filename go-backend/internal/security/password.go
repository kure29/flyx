package security

import (
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func HashPassword(plain string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func VerifyPassword(storedHash, plain string) (bool, bool) {
	if strings.HasPrefix(storedHash, "$2") {
		return bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(plain)) == nil, false
	}
	if MD5(plain) == storedHash {
		return true, true
	}
	return false, false
}

func IsLegacyPasswordHash(storedHash string) bool {
	storedHash = strings.TrimSpace(storedHash)
	if len(storedHash) != 32 {
		return false
	}
	for _, r := range storedHash {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			continue
		}
		return false
	}
	return true
}
