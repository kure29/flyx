package handler

import (
	nft "go-backend/internal/runtime/nftables"
	"testing"
	"time"
)

func TestCounterSnapshotsRejectLateRepliesAndCreditNewEpoch(t *testing.T) {
	f := setupNftablesCollectionFixture(t)
	h := f.handler
	t.Cleanup(func() { _ = h.repo.Close() })
	next := func() int64 {
		n, err := h.repo.NextCounterSnapshot(f.nodeID, "kernel")
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	apply := func(seq int64, epoch string, bytes uint64) {
		h.accountForwardCounterSamplesOrdered(f.nodeID, []nft.CounterSample{{ForwardID: f.forwardID, Protocol: "tcp", Direction: nft.CounterDirectionToTarget, Bytes: bytes}}, map[int64]string{f.forwardID: "realm:" + epoch}, time.Now(), "kernel", seq, epoch)
	}
	apply(next(), "e1", 100)
	older, newer := next(), next()
	apply(newer, "e1", 200)
	apply(older, "e1", 150)
	apply(next(), "e2", 80)
	apply(next(), "e1", 900) // A retired epoch cannot return even with a new request ID.
	apply(next(), "e2", 60)
	apply(next(), "e2", 90)
	for _, query := range []string{"SELECT in_flow FROM forward", "SELECT in_flow FROM user WHERE id=1", "SELECT daily_used_bytes FROM user_quota WHERE user_id=1"} {
		if got := mustHandlerCount(t, h, query); got != 190 {
			t.Fatalf("%s=%d, want 190", query, got)
		}
	}
	states, err := h.repo.GetNftCounterStatesByNode(f.nodeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].RuleHash != "realm:e2" || states[0].Bytes != 90 {
		t.Fatalf("baseline regressed: %+v", states)
	}
}
