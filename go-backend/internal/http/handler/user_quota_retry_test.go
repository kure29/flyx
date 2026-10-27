package handler

import (
	"errors"
	"testing"
	"time"

	"go-backend/internal/store/model"
)

func TestQuotaControlRetriesAcrossRestartAndPreservesManualPause(t *testing.T) {
	fixture := setupNftablesHandler(t)
	h := fixture.handler
	manager := &fakeNftablesManager{}
	h.nftablesManager = manager
	seedNftablesSSHConfig(t, h, fixture.nodeID)
	tunnelID := seedTunnelForNftables(t, h, "quota", fixture.nodeID)
	f := seedForwardForNftables(t, h, tunnelID, fixture.nodeID, "203.0.113.9:8080")
	if err := h.syncNftablesNode(fixture.nodeID); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	q, err := h.repo.AddUserQuotaUsage(f.UserID, bytesPerGB, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.repo.DB().Model(&model.UserQuota{}).Where("user_id = ?", f.UserID).Update("daily_limit_gb", 1).Error; err != nil {
		t.Fatal(err)
	}
	q.DailyLimitGB = 1
	manager.blockErr = errors.New("SSH offline")
	h.enforceUserQuotaIfNeeded(f.UserID, q)
	assertQuota := func(status, disabled int, pending bool) {
		t.Helper()
		f, err := h.getForwardRecord(f.ID)
		if err != nil {
			t.Fatal(err)
		}
		q, err := h.repo.GetUserQuotaView(f.UserID, now)
		if err != nil {
			t.Fatal(err)
		}
		if f.Status != status || q.DisabledByQuota != disabled || (q.PausedForwardIDs != "") != pending {
			t.Fatalf("status=%d quota=%+v", f.Status, q)
		}
	}
	assertQuota(1, 1, true) // Failure must not pretend the node paused.
	h = New(h.repo, "test-secret")
	h.nftablesManager = manager
	manager.blockErr = nil
	h.resetUserQuotaWindows(now)
	assertQuota(0, 1, true)
	if !manager.blocked[20000] {
		t.Fatal("nftables traffic was not blocked")
	}
	if manager.reconcileHit != 1 {
		t.Fatal("pause rebuilt NAT counters")
	}

	release, err := h.repo.ResetUserQuotaUsage(f.UserID, "all", now)
	if err != nil {
		t.Fatal(err)
	}
	manager.blockErr = errors.New("SSH offline")
	h.applyUserQuotaRelease(release, now.UnixMilli())
	assertQuota(0, 0, true)
	manager.blockErr = nil
	h.resetUserQuotaWindows(now) // No further reset/traffic event is needed.
	assertQuota(1, 0, false)
	if manager.blocked[20000] {
		t.Fatal("nftables traffic stayed blocked")
	}
	if manager.reconcileHit != 1 {
		t.Fatal("resume rebuilt NAT counters")
	}

	q, err = h.repo.AddUserQuotaUsage(f.UserID, bytesPerGB, now)
	if err != nil {
		t.Fatal(err)
	}
	h.enforceUserQuotaIfNeeded(f.UserID, q)
	assertQuota(0, 1, true)
	if err := h.repo.SetManualForwardStatus(f.UserID, f.ID, 0, now.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	release, err = h.repo.ResetUserQuotaUsage(f.UserID, "all", now)
	if err != nil {
		t.Fatal(err)
	}
	h.applyUserQuotaRelease(release, now.UnixMilli())
	assertQuota(0, 0, false)
	if !manager.blocked[20000] {
		t.Fatal("manual pause was undone")
	}
	manager.blocked = map[int]bool{} // Node restart: rebuild both intent and NAT.
	if err := h.syncNftablesNode(fixture.nodeID); err != nil {
		t.Fatal(err)
	}
	if !manager.blocked[20000] || len(manager.lastPlan.Rules) != 1 || !manager.lastPlan.Rules[0].Paused {
		t.Fatal("paused rule not restored safely")
	}
}
