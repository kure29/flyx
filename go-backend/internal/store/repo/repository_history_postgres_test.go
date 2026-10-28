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
	var gap *TrafficSequenceGap
	if err := r.SaveNodeMetricBatch("stream", 4, batch); !errors.As(err, &gap) || gap.Expected != 3 {
		t.Fatalf("sequence gap did not expose expected sequence: %v", err)
	}
	if err := r.DB().Where("scope=?", "node-history:stream").Delete(&model.TrafficCursor{}).Error; err != nil {
		t.Fatal(err)
	}
	next := []*model.NodeMetric{{NodeID: 7, Timestamp: 3000, NetInBytes: 300, Uptime: 12}}
	if err := r.SaveNodeMetricBatch("stream", 3, next); !errors.As(err, &gap) || gap.Expected != 1 {
		t.Fatalf("missing cursor did not expose expected sequence: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := r.SaveNodeMetricBatch("recovered-stream", 1, next); err != nil {
			t.Fatal(err)
		}
	}
	totals, err = r.NodeTrafficSince(0)
	if err != nil {
		t.Fatal(err)
	}
	if totals[7][0] != 200 {
		t.Fatalf("stream recovery lost or duplicated increments: %v", totals)
	}
	if err := r.DB().Model(&model.NodeMetric{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("stream recovery duplicated raw metrics: %d", count)
	}
}
