package repo

import (
	"errors"
	"go-backend/internal/store/model"
	"path/filepath"
	"testing"
)

func TestNodeTrafficBatchAcknowledgementIsIdempotent(t *testing.T) {
	r, err := Open(filepath.Join(t.TempDir(), "batch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
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

func TestNodeTrafficUpgradeUsesLastSavedRawSample(t *testing.T) {
	r, err := Open(filepath.Join(t.TempDir(), "upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	old := []*model.NodeMetric{{NodeID: 1, Timestamp: 1000, Uptime: 10, NetInBytes: 1000}}
	if err := r.InsertNodeMetricBatch(old); err != nil {
		t.Fatal(err)
	}
	next := []*model.NodeMetric{{NodeID: 1, Timestamp: 2000, Uptime: 11, NetInBytes: 1200}}
	if err := r.SaveNodeMetricBatch("after-upgrade", 1, next); err != nil {
		t.Fatal(err)
	}
	totals, err := r.NodeTrafficSince(0)
	if err != nil {
		t.Fatal(err)
	}
	if totals[1][0] != 200 {
		t.Fatalf("upgrade lost the saved baseline: %v", totals)
	}
}

// A counter that drops while the node stays up (a container's interface
// went away) adds nothing; a reboot counts what was sent since boot.
func TestNodeTrafficCounterDropsAndReboots(t *testing.T) {
	r, err := Open(filepath.Join(t.TempDir(), "drops.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	const day = int64(86400)
	up := 239 * day // seconds the node has been up at the first sample
	t0 := int64(1_800_000_000_000)
	sample := func(sec, uptime, in, out int64) *model.NodeMetric {
		return &model.NodeMetric{NodeID: 3, Timestamp: t0 + sec*1000, Uptime: uptime, NetInBytes: in, NetOutBytes: out}
	}
	const tb = int64(1) << 40
	steps := [][]*model.NodeMetric{
		{sample(0, up, 10*tb, 20*tb)},
		// +1000 / +2000 bytes.
		{sample(60, up+60, 10*tb+1000, 20*tb+2000)},
		// A veth with 1 TB went away: the sum drops, the node is still up.
		{sample(120, up+120, 9*tb+1500, 19*tb+2500)},
		// +500 / +500 after the drop.
		{sample(180, up+180, 9*tb+2000, 19*tb+3000)},
		// Rebooted, then offline a day: uptime is larger than before the
		// first sample would allow, but the boot time moved.
		{sample(180+2*day, day, 7000, 8000)},
	}
	for i, batch := range steps {
		if err := r.SaveNodeMetricBatch("drops", int64(i+1), batch); err != nil {
			t.Fatal(err)
		}
	}
	totals, err := r.NodeTrafficSince(0)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := totals[3], [2]int64{1000 + 500 + 7000, 2000 + 500 + 8000}; got != want {
		t.Fatalf("traffic %v, want %v (a dropped interface must not add its total again)", got, want)
	}
}
