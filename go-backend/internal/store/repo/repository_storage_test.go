package repo

import (
	"path/filepath"
	"testing"

	"go-backend/internal/store/model"
)

func TestDatabaseStorageSummarySQLiteIncludesSize(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "storage.db")
	r, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	defer r.Close()

	if err := r.InsertNodeMetric(&model.NodeMetric{NodeID: 1, Timestamp: 123, CPUUsage: 1}); err != nil {
		t.Fatalf("insert metric: %v", err)
	}

	summary, err := r.DatabaseStorageSummary()
	if err != nil {
		t.Fatalf("storage summary: %v", err)
	}
	if summary.DBType != "sqlite" {
		t.Fatalf("expected sqlite db type, got %q", summary.DBType)
	}
	if summary.DatabaseSizeBytes <= 0 {
		t.Fatalf("expected database size > 0, got %d", summary.DatabaseSizeBytes)
	}
	if summary.DatabaseSizeText == "" {
		t.Fatalf("expected formatted size")
	}
}

func TestFormatDatabaseSize(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{bytes: 0, want: "0 B"},
		{bytes: 512, want: "512 B"},
		{bytes: 1024, want: "1.0 KB"},
		{bytes: 1024 * 1024, want: "1.0 MB"},
	}
	for _, tc := range tests {
		if got := formatDatabaseSize(tc.bytes); got != tc.want {
			t.Fatalf("formatDatabaseSize(%d) = %q, want %q", tc.bytes, got, tc.want)
		}
	}
}
