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

func TestQuotaAcknowledgedPausesRefreshHourlyAndAfterReconnect(t *testing.T) {
	fixture := setupNftablesHandler(t)
	h := fixture.handler
	manager := &fakeNftablesManager{}
	h.nftablesManager = manager
	seedNftablesSSHConfig(t, h, fixture.nodeID)
	tunnelID := seedTunnelForNftables(t, h, "quota-recheck", fixture.nodeID)
	f := seedForwardForNftables(t, h, tunnelID, fixture.nodeID, "203.0.113.9:8080")
	if err := h.syncNftablesNode(fixture.nodeID); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	if _, err := h.repo.AddUserQuotaUsage(f.UserID, bytesPerGB, now); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.DB().Model(&model.UserQuota{}).Where("user_id=?", f.UserID).Update("daily_limit_gb", 1).Error; err != nil {
		t.Fatal(err)
	}
	h.reconcileUserQuota(f.UserID, now)
	first := manager.blockCalls
	if first == 0 {
		t.Fatal("initial pause not sent")
	}
	for minute := 1; minute < 60; minute++ {
		h.resetUserQuotaWindows(now.Add(time.Duration(minute) * time.Minute))
	}
	if manager.blockCalls != first {
		t.Fatalf("acknowledged pause repeated %d times", manager.blockCalls-first)
	}
	h.resetUserQuotaWindows(now.Add(time.Hour))
	if manager.blockCalls != first+1 {
		t.Fatalf("hourly refresh missing: %d", manager.blockCalls)
	}
	h.invalidateNodeQuotaAcknowledgements(fixture.nodeID)
	manager.blockErr = errors.New("offline")
	h.resetUserQuotaWindows(now.Add(61 * time.Minute))
	failed := manager.blockCalls
	if failed != first+2 {
		t.Fatal("reconnect did not invalidate acknowledgement")
	}
	manager.blockErr = nil
	h.resetUserQuotaWindows(now.Add(62 * time.Minute))
	if manager.blockCalls != failed+1 {
		t.Fatal("failed refresh did not retry")
	}
	h.resetUserQuotaWindows(now.Add(63 * time.Minute))
	if manager.blockCalls != failed+1 {
		t.Fatal("new acknowledgement did not throttle")
	}
	h = New(h.repo, "test-secret")
	h.nftablesManager = manager
	h.resetUserQuotaWindows(now.Add(64 * time.Minute))
	if manager.blockCalls != failed+2 {
		t.Fatal("panel restart trusted stale acknowledgement")
	}
}
