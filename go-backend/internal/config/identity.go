package config

import "os"

// IdentityEnv keeps existing installations working while new configuration
// uses FLYX names. An explicitly empty new value also overrides the old one.
func IdentityEnv(name, legacy string) string {
	if value, ok := os.LookupEnv(name); ok {
		return value
	}
	return os.Getenv(legacy)
}
