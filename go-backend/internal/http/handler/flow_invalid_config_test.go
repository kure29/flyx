package handler

import (
	nft "go-backend/internal/runtime/nftables"
	"testing"
	"time"
)

func TestCounterAccountingIsolatesInvalidConfigAndRetainsItsBaseline(t *testing.T) {
	f := setupNftablesCollectionFixture(t)
	h := f.handler
	t.Cleanup(func() { _ = h.repo.Close() })
	tunnel := seedTunnelForNftables(t, h, "invalid-counter-config", f.nodeID)
	bad := seedForwardForNftables(t, h, tunnel, f.nodeID, "203.0.113.10:8080")
	hashes := map[int64]string{f.forwardID: "stable", bad.ID: "stable"}
	sample := func(bytes uint64) []nft.CounterSample {
		return []nft.CounterSample{
			{ForwardID: f.forwardID, Protocol: "tcp", Direction: nft.CounterDirectionToTarget, Bytes: bytes},
			{ForwardID: bad.ID, Protocol: "tcp", Direction: nft.CounterDirectionToTarget, Bytes: bytes},
		}
	}
	h.accountForwardCounterSamples(f.nodeID, sample(100), hashes, time.Now())
	if err := h.repo.DB().Exec("UPDATE tunnel SET traffic_ratio=-1 WHERE id=?", tunnel).Error; err != nil {
		t.Fatal(err)
	}
	h.accountForwardCounterSamples(f.nodeID, sample(150), hashes, time.Now())
	if got := mustHandlerCount(t, h, "SELECT in_flow FROM forward WHERE id=?", f.forwardID); got != 50 {
		t.Fatalf("healthy forward blocked: %d", got)
	}
	if got := mustHandlerCount(t, h, "SELECT in_flow FROM forward WHERE id=?", bad.ID); got != 0 {
		t.Fatalf("invalid config charged: %d", got)
	}
	if got := mustHandlerCount(t, h, "SELECT bytes FROM nft_counter_state WHERE forward_id=?", bad.ID); got != 100 {
		t.Fatalf("invalid baseline advanced: %d", got)
	}
	if err := h.repo.DB().Exec("UPDATE tunnel SET traffic_ratio=1 WHERE id=?", tunnel).Error; err != nil {
		t.Fatal(err)
	}
	h.accountForwardCounterSamples(f.nodeID, sample(160), hashes, time.Now())
	for _, id := range []int64{f.forwardID, bad.ID} {
		if got := mustHandlerCount(t, h, "SELECT in_flow FROM forward WHERE id=?", id); got != 60 {
			t.Fatalf("repaired forward %d usage=%d", id, got)
		}
	}
}
