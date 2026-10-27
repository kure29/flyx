package metrics

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
)

func TestRecordNodeMetric(t *testing.T) {
	r, err := repo.Open(":memory:")
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	defer r.Close()

	svc := NewIngestionService(r)

	info := SystemInfo{
		Uptime:           86400,
		BytesReceived:    1024000,
		BytesTransmitted: 2048000,
		CPUUsage:         45.5,
		MemoryUsage:      60.2,
		DiskUsage:        30.1,
		Load1:            1.5,
		Load5:            1.2,
		Load15:           0.9,
		TCPConns:         100,
		UDPConns:         50,
		NetInSpeed:       51200,
		NetOutSpeed:      102400,
	}

	svc.RecordNodeMetric(1, info)
	svc.flushNodeMetrics()

	metrics, err := r.GetNodeMetrics(1, time.Now().UnixMilli()-60000, time.Now().UnixMilli()+1000)
	if err != nil {
		t.Fatalf("get metrics: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}

	m := metrics[0]
	if m.CPUUsage != 45.5 {
		t.Fatalf("expected CPUUsage 45.5, got %f", m.CPUUsage)
	}
	if m.MemUsage != 60.2 {
		t.Fatalf("expected MemUsage 60.2, got %f", m.MemUsage)
	}
	if m.DiskUsage != 30.1 {
		t.Fatalf("expected DiskUsage 30.1, got %f", m.DiskUsage)
	}
	if m.Load1 != 1.5 {
		t.Fatalf("expected Load1 1.5, got %f", m.Load1)
	}
	if m.TCPConns != 100 {
		t.Fatalf("expected TCPConns 100, got %d", m.TCPConns)
	}
	if m.UDPConns != 50 {
		t.Fatalf("expected UDPConns 50, got %d", m.UDPConns)
	}
}

func TestRecordNodeMetricAutoFlush(t *testing.T) {
	r, err := repo.Open(":memory:")
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	defer r.Close()

	svc := NewIngestionService(r)

	info := SystemInfo{
		CPUUsage:    50.0,
		MemoryUsage: 60.0,
		DiskUsage:   30.0,
	}

	for i := 0; i < 250; i++ {
		svc.RecordNodeMetric(1, info)
	}

	time.Sleep(100 * time.Millisecond)

	metrics, err := r.GetNodeMetrics(1, time.Now().UnixMilli()-60000, time.Now().UnixMilli()+1000)
	if err != nil {
		t.Fatalf("get metrics: %v", err)
	}
	if len(metrics) < 200 {
		t.Fatalf("expected at least 200 metrics after auto-flush, got %d", len(metrics))
	}
}

func TestIngestionServiceStart(t *testing.T) {
	r, err := repo.Open(":memory:")
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	defer r.Close()

	svc := NewIngestionService(r)
	svc.flushInterval = 100 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	info := SystemInfo{
		CPUUsage:    45.0,
		MemoryUsage: 55.0,
		DiskUsage:   35.0,
	}

	go svc.Start(ctx)

	for i := 0; i < 10; i++ {
		svc.RecordNodeMetric(1, info)
		time.Sleep(50 * time.Millisecond)
	}

	<-ctx.Done()

	metrics, err := r.GetNodeMetrics(1, time.Now().UnixMilli()-60000, time.Now().UnixMilli()+1000)
	if err != nil {
		t.Fatalf("get metrics: %v", err)
	}
	if len(metrics) == 0 {
		t.Fatalf("expected metrics after service run")
	}
}

func TestGetLatestMetric(t *testing.T) {
	r, err := repo.Open(":memory:")
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	defer r.Close()

	svc := NewIngestionService(r)

	now := time.Now().UnixMilli()

	info1 := SystemInfo{CPUUsage: 40.0, MemoryUsage: 50.0, DiskUsage: 30.0}
	svc.RecordNodeMetric(1, info1)

	time.Sleep(5 * time.Millisecond)

	info2 := SystemInfo{CPUUsage: 60.0, MemoryUsage: 70.0, DiskUsage: 40.0}
	svc.RecordNodeMetric(1, info2)

	svc.flushNodeMetrics()

	latest, err := svc.GetLatestMetric(1)
	if err != nil {
		t.Fatalf("get latest: %v", err)
	}
	if latest == nil {
		t.Fatalf("expected latest metric")
	}
	if latest.CPUUsage != 60.0 {
		t.Fatalf("expected latest CPUUsage 60.0, got %f", latest.CPUUsage)
	}

	_ = now

	latestNone, err := svc.GetLatestMetric(999)
	if err != nil {
		t.Fatalf("get latest for non-existent: %v", err)
	}
	if latestNone != nil {
		t.Fatalf("expected nil for non-existent node")
	}
}

func TestGetMetricsWithTimeRange(t *testing.T) {
	r, err := repo.Open(":memory:")
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	defer r.Close()

	svc := NewIngestionService(r)

	now := time.Now().UnixMilli()

	for i := 0; i < 5; i++ {
		info := SystemInfo{
			CPUUsage:    float64(40 + i*5),
			MemoryUsage: 50.0,
			DiskUsage:   30.0,
		}
		svc.RecordNodeMetric(1, info)
		time.Sleep(10 * time.Millisecond)
	}

	svc.flushNodeMetrics()

	metrics, err := svc.GetMetrics(1, now-60000, now+1000)
	if err != nil {
		t.Fatalf("get metrics: %v", err)
	}
	if len(metrics) != 5 {
		t.Fatalf("expected 5 metrics, got %d", len(metrics))
	}
}

func TestPruneMetrics(t *testing.T) {
	r, err := repo.Open(":memory:")
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	defer r.Close()

	svc := NewIngestionService(r)

	info := SystemInfo{CPUUsage: 50.0, MemoryUsage: 60.0, DiskUsage: 30.0}

	svc.RecordNodeMetric(1, info)
	svc.flushNodeMetrics()

	svc.pruneMetrics()

	metrics, err := r.GetNodeMetrics(1, time.Now().UnixMilli()-60000, time.Now().UnixMilli()+1000)
	if err != nil {
		t.Fatalf("get metrics: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric (not pruned), got %d", len(metrics))
	}
}

func TestPruneMetricsUsesConfiguredRetentionDays(t *testing.T) {
	r, err := repo.Open(":memory:")
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	defer r.Close()

	now := time.Now().UnixMilli()
	if err := r.UpsertConfig("monitor_retention_days", "2", now); err != nil {
		t.Fatalf("upsert retention config: %v", err)
	}

	oldMetric := &model.NodeMetric{NodeID: 1, Timestamp: now - int64(3*24*time.Hour/time.Millisecond), CPUUsage: 10}
	newMetric := &model.NodeMetric{NodeID: 1, Timestamp: now - int64(1*24*time.Hour/time.Millisecond), CPUUsage: 20}
	if err := r.InsertNodeMetric(oldMetric); err != nil {
		t.Fatalf("insert old metric: %v", err)
	}
	if err := r.InsertNodeMetric(newMetric); err != nil {
		t.Fatalf("insert new metric: %v", err)
	}

	svc := NewIngestionService(r)
	svc.pruneMetricsAt(time.UnixMilli(now))

	metrics, err := r.GetNodeMetrics(1, now-int64(4*24*time.Hour/time.Millisecond), now+1000)
	if err != nil {
		t.Fatalf("get node metrics: %v", err)
	}
	if len(metrics) != 1 || metrics[0].CPUUsage != 20 {
		t.Fatalf("expected only newer metric to remain, got %#v", metrics)
	}
}

func TestMultipleNodes(t *testing.T) {
	r, err := repo.Open(":memory:")
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	defer r.Close()

	svc := NewIngestionService(r)

	info := SystemInfo{
		CPUUsage:    50.0,
		MemoryUsage: 60.0,
		DiskUsage:   30.0,
	}

	svc.RecordNodeMetric(1, info)
	svc.RecordNodeMetric(2, info)
	svc.RecordNodeMetric(3, info)

	svc.flushNodeMetrics()

	for nodeID := int64(1); nodeID <= 3; nodeID++ {
		metrics, err := r.GetNodeMetrics(nodeID, time.Now().UnixMilli()-60000, time.Now().UnixMilli()+1000)
		if err != nil {
			t.Fatalf("get metrics for node %d: %v", nodeID, err)
		}
		if len(metrics) != 1 {
			t.Fatalf("expected 1 metric for node %d, got %d", nodeID, len(metrics))
		}
	}
}

func TestZeroValues(t *testing.T) {
	r, err := repo.Open(":memory:")
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	defer r.Close()

	svc := NewIngestionService(r)

	info := SystemInfo{}

	svc.RecordNodeMetric(1, info)
	svc.flushNodeMetrics()

	metrics, err := r.GetNodeMetrics(1, time.Now().UnixMilli()-60000, time.Now().UnixMilli()+1000)
	if err != nil {
		t.Fatalf("get metrics: %v", err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}

	m := metrics[0]
	if m.CPUUsage != 0 || m.MemUsage != 0 || m.DiskUsage != 0 {
		t.Fatalf("expected zero values, got CPU=%f Mem=%f Disk=%f", m.CPUUsage, m.MemUsage, m.DiskUsage)
	}
}

// Traffic is what the byte counters grew by; the first report is only a
// baseline and a smaller counter (a reboot) counts in full.
func TestNodeTrafficFromCounters(t *testing.T) {
	r, err := repo.Open(filepath.Join(t.TempDir(), "traffic.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	s := NewIngestionService(r)
	s.RecordNodeMetric(7, SystemInfo{BytesReceived: 1000, BytesTransmitted: 500})
	s.RecordNodeMetric(7, SystemInfo{BytesReceived: 1600, BytesTransmitted: 900})
	s.RecordNodeMetric(7, SystemInfo{BytesReceived: 100, BytesTransmitted: 50})
	s.flushNodeMetrics()
	totals, err := r.NodeTrafficSince(0)
	if err != nil {
		t.Fatal(err)
	}
	in, out := totals[7][0], totals[7][1]
	if in != 700 || out != 450 {
		t.Fatalf("traffic in=%d out=%d", in, out)
	}
}
