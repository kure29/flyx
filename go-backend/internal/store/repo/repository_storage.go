package repo

import (
	"errors"
	"fmt"
	"os"
)

type DatabaseStorageSummary struct {
	DBType            string `json:"dbType"`
	DatabaseSizeBytes int64  `json:"databaseSizeBytes"`
	DatabaseSizeText  string `json:"databaseSizeText"`
}

func (r *Repository) DatabaseStorageSummary() (DatabaseStorageSummary, error) {
	if r == nil || r.db == nil {
		return DatabaseStorageSummary{}, errors.New("repository not initialized")
	}

	switch r.db.Dialector.Name() {
	case "sqlite":
		size, err := sqliteDatabaseFileSize(r.dbPath)
		if err != nil {
			return DatabaseStorageSummary{}, err
		}
		return DatabaseStorageSummary{DBType: "sqlite", DatabaseSizeBytes: size, DatabaseSizeText: formatDatabaseSize(size)}, nil
	case "postgres":
		var size int64
		if err := r.db.Raw("SELECT pg_database_size(current_database())").Scan(&size).Error; err != nil {
			return DatabaseStorageSummary{}, err
		}
		return DatabaseStorageSummary{DBType: "postgres", DatabaseSizeBytes: size, DatabaseSizeText: formatDatabaseSize(size)}, nil
	default:
		return DatabaseStorageSummary{}, fmt.Errorf("unsupported database dialect %q", r.db.Dialector.Name())
	}
}

func sqliteDatabaseFileSize(path string) (int64, error) {
	if path == "" || path == ":memory:" {
		return 0, nil
	}

	var total int64
	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		info, err := os.Stat(candidate)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return 0, err
		}
		if !info.IsDir() {
			total += info.Size()
		}
	}
	return total, nil
}

func formatDatabaseSize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	value := float64(bytes) / 1024
	for _, unit := range units {
		if value < 1024 || unit == "TB" {
			return fmt.Sprintf("%.1f %s", value, unit)
		}
		value /= 1024
	}
	return fmt.Sprintf("%d B", bytes)
}
