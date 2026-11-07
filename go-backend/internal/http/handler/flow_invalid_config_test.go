package handler

import (
	nft "go-backend/internal/runtime/nftables"
	"testing"
	"time"
)

// Tunnels saved by an older version may carry a billing ratio and direction,
// even invalid ones. They no longer change what a forward is credited with.
func TestCounterAccountingIgnoresLegacyTunnelBilling(t *testing.T) {
	f := setupNftablesCollectionFixture(t)
	h := f.handler
	t.Cleanup(func() { _ = h.repo.Close() })
	tunnel := seedTunnelForNftables(t, h, "legacy-billing", f.nodeID)
	legacy := seedForwardForNftables(t, h, tunnel, f.nodeID, "203.0.113.10:8080")
	if err := h.repo.DB().Exec("UPDATE tunnel SET traffic_ratio=-1, flow=1 WHERE id=?", tunnel).Error; err != nil {
		t.Fatal(err)
	}
	hashes := map[int64]string{f.forwardID: "stable", legacy.ID: "stable"}
	sample := func(bytes uint64) []nft.CounterSample {
		return []nft.CounterSample{
			{ForwardID: f.forwardID, Protocol: "tcp", Direction: nft.CounterDirectionToTarget, Bytes: bytes},
			{ForwardID: legacy.ID, Protocol: "tcp", Direction: nft.CounterDirectionFromTarget, Bytes: bytes},
		}
	}
	h.accountForwardCounterSamples(f.nodeID, sample(100), hashes, time.Now())
	h.accountForwardCounterSamples(f.nodeID, sample(150), hashes, time.Now())
	if got := mustHandlerCount(t, h, "SELECT in_flow FROM forward WHERE id=?", f.forwardID); got != 50 {
		t.Fatalf("forward credited %d, want 50", got)
	}
	// Download-only billing (flow=1) used to drop this direction.
	if got := mustHandlerCount(t, h, "SELECT out_flow FROM forward WHERE id=?", legacy.ID); got != 50 {
		t.Fatalf("legacy-billing forward credited %d, want 50", got)
	}
}
