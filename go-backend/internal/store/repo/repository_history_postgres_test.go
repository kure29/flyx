package repo

import (
	"errors"
	"go-backend/internal/store/model"
	"testing"
)

func TestPostgresNodeTrafficBatchAcknowledgementIsIdempotent(t *testing.T) {
	r := openTrafficPostgresTest(t)
	initial := []*model.NodeMetric{{NodeID: 7, Timestamp: 1000, NetInBytes: 100, Uptime: 10}}
	batch := []*model.NodeMetric{{NodeID: 7, Timestamp: 2000, NetInBytes: 200, Uptime: 11}}
	if err := r.SaveNodeMetricBatch("stream", 1, initial); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := r.SaveNodeMetricBatch("stream", 2, batch); err != nil {
			t.Fatal(err)
		}
	}
	totals, err := r.NodeTrafficSince(0)
	if err != nil {
		t.Fatal(err)
	}
	if totals[7][0] != 100 {
		t.Fatalf("duplicate commit: %v", totals)
	}
	var count int64
	if err := r.DB().Model(&model.NodeMetric{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("duplicate raw metrics: %d", count)
	}
	batch[0].NetInBytes++
	if err := r.SaveNodeMetricBatch("stream", 2, batch); !errors.Is(err, ErrTrafficSequence) {
		t.Fatalf("reused identity accepted: %v", err)
	}
	if err := r.SaveNodeMetricBatch("stream", 4, batch); !errors.Is(err, ErrTrafficSequence) {
		t.Fatalf("sequence gap accepted: %v", err)
	}
}
