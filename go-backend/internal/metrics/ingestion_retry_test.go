package metrics

import (
	"errors"
	"go-backend/internal/store/repo"
	"gorm.io/gorm"
	"path/filepath"
	"testing"
)

func TestNodeTrafficFlushRetainsFailedBatchAndRestartBaseline(t *testing.T) {
	r, err := repo.Open(filepath.Join(t.TempDir(), "traffic.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	s := NewIngestionService(r)
	s.RecordNodeMetric(7, SystemInfo{BytesReceived: 1000, Uptime: 10})
	s.flushNodeMetrics()
	s.RecordNodeMetric(7, SystemInfo{BytesReceived: 1200, Uptime: 11})
	if err := r.DB().Callback().Create().Before("gorm:create").Register("test:fail_traffic", func(db *gorm.DB) {
		if db.Statement.Table == "node_traffic" {
			db.AddError(errors.New("injected failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	s.flushNodeMetrics()
	if len(s.pending) != 1 {
		t.Fatal("failed batch discarded")
	}
	totals, err := r.NodeTrafficSince(0)
	if err != nil {
		t.Fatal(err)
	}
	if totals[7][0] != 0 {
		t.Fatal("partial traffic commit")
	}
	if err := r.DB().Callback().Create().Remove("test:fail_traffic"); err != nil {
		t.Fatal(err)
	}
	s.RecordNodeMetric(7, SystemInfo{BytesReceived: 1300, Uptime: 12})
	s.flushNodeMetrics()
	s.flushNodeMetrics()
	totals, err = r.NodeTrafficSince(0)
	if err != nil {
		t.Fatal(err)
	}
	if totals[7][0] != 300 {
		t.Fatalf("retry lost new or pending data: %v", totals)
	}
	restarted := NewIngestionService(r)
	restarted.RecordNodeMetric(7, SystemInfo{BytesReceived: 1400, Uptime: 13})
	restarted.flushNodeMetrics()
	totals, err = r.NodeTrafficSince(0)
	if err != nil {
		t.Fatal(err)
	}
	if totals[7][0] != 400 {
		t.Fatalf("restart lost baseline: %v", totals)
	}
}
