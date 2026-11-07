package config

import (
	"os"
	"path/filepath"
)

type Config struct {
	Addr        string
	DBType      string
	DBPath      string
	DatabaseURL string
	JWTSecret   string
	LogDir      string
	// StatusThemesDir holds the status page themes the admin installs;
	// next to the database by default, so it is kept with the data volume.
	StatusThemesDir string
	// GeoIPCountryDB is the country database used to detect where nodes are.
	// Empty: the panel downloads DB-IP's free database next to the database
	// and refreshes it monthly. A path: the admin's own file, used as is.
	// "off": no detection.
	GeoIPCountryDB string
}

func FromEnv() Config {
	cfg := Config{
		Addr:        getEnv("SERVER_ADDR", ":6365"),
		DBType:      getEnv("DB_TYPE", "sqlite"),
		DBPath:      getEnv("DB_PATH", "/app/data/gost.db"),
		DatabaseURL: getEnv("DATABASE_URL", ""),
		JWTSecret:   getEnv("JWT_SECRET", ""),
		LogDir:      getEnv("LOG_DIR", "/app/logs"),
	}
	cfg.StatusThemesDir = getEnv("STATUS_THEMES_DIR", filepath.Join(filepath.Dir(cfg.DBPath), "themes"))
	cfg.GeoIPCountryDB = getEnv("GEOIP_COUNTRY_DB", "")

	return cfg
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
