package metrics

import (
	"errors"
	"go-backend/internal/store/model"
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

func TestNodeHistoryCursorGapPreservesPendingAndBufferedSamples(t *testing.T) {
	for _, mode := range []string{"missing", "rollback"} {
		t.Run(mode, func(t *testing.T) {
			r, err := repo.Open(filepath.Join(t.TempDir(), "history.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			s := NewIngestionService(r)
			for _, bytes := range []int64{100, 200} {
				s.nodeBuffer = append(s.nodeBuffer, &model.NodeMetric{NodeID: 7, Timestamp: bytes * 10, NetInBytes: bytes, Uptime: bytes})
				s.flushNodeMetrics()
			}
			oldStream := s.stream
			if s.sequence != 3 {
				t.Fatalf("unexpected starting sequence %d", s.sequence)
			}
			scope := "node-history:" + oldStream
			if mode == "missing" {
				err = r.DB().Where("scope=?", scope).Delete(&model.TrafficCursor{}).Error
			} else {
				err = r.DB().Model(&model.TrafficCursor{}).Where("scope=?", scope).Update("sequence", 1).Error
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := r.DB().Callback().Create().Before("gorm:create").Register("test:history_recovery", func(tx *gorm.DB) {
				if tx.Statement.Table == "node_traffic" {
					tx.AddError(errors.New("database unavailable after stream rotation"))
				}
			}); err != nil {
				t.Fatal(err)
			}
			s.nodeBuffer = []*model.NodeMetric{{NodeID: 7, Timestamp: 3000, NetInBytes: 300, Uptime: 300}}
			s.flushNodeMetrics()
			if s.stream == oldStream || s.sequence != 1 || len(s.pending) != 1 || s.pending[0].NetInBytes != 300 {
				t.Fatalf("failed recovery lost identity/data: %+v", s)
			}
			recoveredStream := s.stream
			s.nodeBuffer = []*model.NodeMetric{{NodeID: 7, Timestamp: 4000, NetInBytes: 400, Uptime: 400}}
			s.flushNodeMetrics()
			if s.stream != recoveredStream || len(s.pending) != 1 || len(s.nodeBuffer) != 1 {
				t.Fatal("transient failure changed retry identity or mixed new samples")
			}
			if err := r.DB().Callback().Create().Remove("test:history_recovery"); err != nil {
				t.Fatal(err)
			}
			s.flushNodeMetrics()
			s.flushNodeMetrics()
			if s.stream != recoveredStream || s.sequence != 3 || len(s.pending) != 0 || len(s.nodeBuffer) != 0 {
				t.Fatal("recovered queue did not drain")
			}
			totals, err := r.NodeTrafficSince(0)
			if err != nil {
				t.Fatal(err)
			}
			if totals[7][0] != 300 {
				t.Fatalf("lost or duplicate increments: %v", totals)
			}
			var count int64
			if err := r.DB().Model(&model.NodeMetric{}).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != 4 {
				t.Fatalf("raw samples=%d, want 4", count)
			}
			var baseline model.NodeTrafficBaseline
			if err := r.DB().First(&baseline, "node_id=?", 7).Error; err != nil {
				t.Fatal(err)
			}
			if baseline.InBytes != 400 || baseline.Timestamp != 4000 {
				t.Fatalf("wrong recovered baseline: %+v", baseline)
			}
		})
	}
}

func TestNodeHistoryIdentityConflictDoesNotRotateOrDiscard(t *testing.T) {
	r, err := repo.Open(filepath.Join(t.TempDir(), "conflict.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	s := NewIngestionService(r)
	committed := []*model.NodeMetric{{NodeID: 7, Timestamp: 1000, NetInBytes: 100, Uptime: 10}}
	if err := r.SaveNodeMetricBatch(s.stream, 1, committed); err != nil {
		t.Fatal(err)
	}
	s.pending = []*model.NodeMetric{{NodeID: 7, Timestamp: 2000, NetInBytes: 200, Uptime: 20}}
	stream := s.stream
	s.flushNodeMetrics()
	if s.stream != stream || s.sequence != 1 || len(s.pending) != 1 {
		t.Fatal("identity conflict was replayed or discarded")
	}
	var count int64
	if err := r.DB().Model(&model.NodeMetric{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("conflict wrote raw samples: %d", count)
	}
}
