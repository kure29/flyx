package repo

import (
	"path/filepath"
	"testing"

	"go-backend/internal/store/model"
)

func checkStatusHistoryBuckets(t *testing.T, r *Repository) {
	t.Helper()
	const bucket = 10_000
	base := int64(1_800_000_000_000) // a multiple of bucket
	rows := []*model.NodeMetric{
		{NodeID: 1, Timestamp: base + 1_000, CPUUsage: 10, MemUsage: 40, SwapUsage: 5, DiskUsage: 50, Load1: 1, TCPConns: 10, UDPConns: 2, Procs: 100, NetInSpeed: 1000, NetOutSpeed: 2000},
		{NodeID: 1, Timestamp: base + 6_000, CPUUsage: 30, MemUsage: 60, SwapUsage: 15, DiskUsage: 50, Load1: 3, TCPConns: 20, UDPConns: 4, Procs: 102, NetInSpeed: 3000, NetOutSpeed: 4000},
		{NodeID: 1, Timestamp: base + 12_000, CPUUsage: 50, MemUsage: 50, DiskUsage: 51, Load1: 2, TCPConns: 30, Procs: 101},
		// Outside the range, or another node.
		{NodeID: 1, Timestamp: base + 40_000, CPUUsage: 99},
		{NodeID: 2, Timestamp: base + 2_000, CPUUsage: 77},
	}
	if err := r.InsertNodeMetricBatch(rows); err != nil {
		t.Fatal(err)
	}
	got, err := r.NodeMetricBuckets(1, base, base+30_000, bucket)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("buckets = %+v", got)
	}
	b := got[0]
	if b.T != base || b.CPU != 20 || b.Mem != 50 || b.Swap != 10 || b.Load != 2 || b.TCP != 15 || b.UDP != 3 || b.Procs != 101 || b.NetIn != 2000 || b.NetOut != 3000 {
		t.Fatalf("first bucket = %+v", b)
	}
	if got[1].T != base+bucket || got[1].CPU != 50 {
		t.Fatalf("second bucket = %+v", got[1])
	}

	samples := []model.NodeLatencySample{
		{NodeID: 1, CreatedTime: base + 1_000, Point: "电信", OK: 1, LatencyMs: 40},
		{NodeID: 1, CreatedTime: base + 5_000, Point: "电信", OK: 1, LatencyMs: 60, LossPct: 10},
		{NodeID: 1, CreatedTime: base + 15_000, Point: "电信", OK: 0, LossPct: 100},
		{NodeID: 1, CreatedTime: base + 2_000, Point: "联通", OK: 1, LatencyMs: 80},
		{NodeID: 2, CreatedTime: base + 2_000, Point: "电信", OK: 1, LatencyMs: 1},
	}
	if err := r.InsertNodeLatencySamples(samples); err != nil {
		t.Fatal(err)
	}
	pings, err := r.NodeLatencyBuckets(1, base, base+30_000, bucket)
	if err != nil {
		t.Fatal(err)
	}
	if len(pings) != 3 {
		t.Fatalf("latency buckets = %+v", pings)
	}
	first := pings[0]
	if first.Point != "电信" || first.T != base || !first.Ms.Valid || first.Ms.Float64 != 50 || first.Loss != 5 {
		t.Fatalf("first latency bucket = %+v", first)
	}
	if failed := pings[1]; failed.Point != "电信" || failed.Ms.Valid || failed.Loss != 100 {
		t.Fatalf("a bucket of failed rounds has no latency: %+v", failed)
	}
	if other := pings[2]; other.Point != "联通" || other.Ms.Float64 != 80 {
		t.Fatalf("second point = %+v", other)
	}

	if _, err := r.NodeMetricBuckets(1, base, base+1, 0); err == nil {
		t.Fatal("a zero bucket must be refused")
	}
}

func TestStatusHistoryBucketsSQLite(t *testing.T) {
	r, err := Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	checkStatusHistoryBuckets(t, r)
}

func TestStatusHistoryBucketsPostgres(t *testing.T) {
	checkStatusHistoryBuckets(t, openTrafficPostgresTest(t))
}
