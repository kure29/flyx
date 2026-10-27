package handler

import (
	"context"
	"log"
	"time"
)

var nftablesTrafficCollectInterval = 30 * time.Second

func (h *Handler) StartBackgroundJobs() {
	if h == nil || h.repo == nil {
		return
	}

	h.jobsMu.Lock()
	if h.jobsStarted {
		h.jobsMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	queue := newNodeReconnectProbeQueue()
	h.reconnectProbes = queue
	h.jobsWG.Add(1)
	h.jobsCancel = cancel
	h.jobsCtx = ctx
	h.jobsStarted = true
	h.jobsWG.Add(15)
	h.jobsMu.Unlock()

	go h.runHourlyStatsLoop(ctx)
	go h.runDailyMaintenanceLoop(ctx)
	go h.runQuotaControlLoop(ctx)
	go h.runNodeRenewalCycleLoop(ctx)
	go h.runMetricsIngestion(ctx)
	go h.runHealthChecks(ctx)
	go h.runTunnelQualityProber(ctx)
	go h.runNftablesTrafficCollectLoop(ctx)
	go h.runFederationCleanupRetryLoop(ctx)
	go h.runKernelStatusLoop(ctx)
	go h.runReachProbeLoop(ctx)
	go h.runNotifyLoop(ctx)
	go h.runSubscriptionLoop(ctx)
	go h.runRemoteSourceLoop(ctx)
	go h.runGistBackupLoop(ctx)
	go func() {
		defer h.jobsWG.Done()
		h.runNodeReconnectProbes(ctx, queue, 5*time.Second)
	}()
}

func (h *Handler) runFederationCleanupRetryLoop(ctx context.Context) {
	defer h.jobsWG.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if err := h.retryPendingFederationRuntimeCleanup(); err != nil {
			log.Printf("federation cleanup remains pending: %v", err)
		}
		h.retryPendingPeerShareOperations()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (h *Handler) retryPendingPeerShareOperations() {
	nodeIDs, err := h.repo.ListPendingPeerShareNodeIDs()
	if err != nil {
		log.Printf("peer share pending operation lookup failed: %v", err)
		return
	}
	for _, nodeID := range nodeIDs {
		node, err := h.repo.GetNodeByID(nodeID)
		if err != nil || node == nil || node.Status != 1 {
			continue
		}
		if err := h.retryPendingPeerShareResourcesOnNode(nodeID); err != nil {
			log.Printf("peer share resource retry failed node_id=%d err=%v", nodeID, err)
		}
		if err := h.retryPendingPeerShareRoleRuntimesOnNode(nodeID); err != nil {
			log.Printf("peer share role retry failed node_id=%d err=%v", nodeID, err)
		}
	}
}

func (h *Handler) StopBackgroundJobs() {
	if h == nil {
		return
	}

	h.jobsMu.Lock()
	if !h.jobsStarted {
		h.jobsMu.Unlock()
		return
	}
	cancel := h.jobsCancel
	h.jobsCancel = nil
	h.jobsStarted = false
	h.jobsMu.Unlock()

	if cancel != nil {
		cancel()
	}
	h.jobsWG.Wait()
}

func (h *Handler) runMetricsIngestion(ctx context.Context) {
	defer h.jobsWG.Done()
	if h.metrics != nil {
		h.metrics.Start(ctx)
	}
}

func (h *Handler) runHealthChecks(ctx context.Context) {
	defer h.jobsWG.Done()
	if h.healthCheck != nil {
		h.healthCheck.Start(ctx)
	}
}

func (h *Handler) runTunnelQualityProber(ctx context.Context) {
	defer h.jobsWG.Done()
	if h == nil || h.qualityProber == nil {
		return
	}

	h.qualityProber.Start(ctx)
}

func (h *Handler) runNftablesTrafficCollectLoop(ctx context.Context) {
	defer h.jobsWG.Done()
	h.runNftablesStartupReconcile(ctx)
	select {
	case <-ctx.Done():
		return
	default:
		h.runNftablesTrafficCollectJob(time.Now())
	}

	interval := nftablesTrafficCollectInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.runNftablesTrafficCollectJob(time.Now())
		}
	}
}

func (h *Handler) runNftablesStartupReconcile(ctx context.Context) {
	if h == nil || h.repo == nil {
		return
	}
	nodes, err := h.repo.ListNftablesNodesForCollection()
	if err != nil {
		log.Printf("nftables startup reconcile failed op=list_nodes err=%v", err)
		return
	}
	for _, node := range nodes {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if err := h.syncNftablesNode(node.NodeID); err != nil {
			log.Printf("nftables startup reconcile failed node_id=%d err=%v", node.NodeID, err)
		}
	}
}

func (h *Handler) runHourlyStatsLoop(ctx context.Context) {
	defer h.jobsWG.Done()

	for {
		wait := durationUntilNextHour(time.Now())
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
			h.runStatisticsFlowJob(time.Now())
		}
	}
}

func (h *Handler) runDailyMaintenanceLoop(ctx context.Context) {
	defer h.jobsWG.Done()

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		h.runResetAndExpiryJob(time.Now())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func durationUntilNextHour(now time.Time) time.Duration {
	next := now.Truncate(time.Hour).Add(time.Hour)
	return next.Sub(now)
}

func durationUntilNextDailyMaintenance(now time.Time) time.Duration {
	next := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 5, 0, now.Location())
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next.Sub(now)
}

func (h *Handler) runStatisticsFlowJob(now time.Time) {
	if h == nil || h.repo == nil {
		return
	}

	nowMs := now.UnixMilli()
	cutoffMs := nowMs - int64((48*time.Hour)/time.Millisecond)
	_ = h.repo.PurgeSubscriptionTraffic(now.Add(-subTrafficRetention).UnixMilli())

	hourMark := now.Truncate(time.Hour)
	hourText := hourMark.Format("15:04")
	createdTime := hourMark.UnixMilli()

	users, err := h.repo.ListAllUserFlowSnapshots()
	if err != nil {
		return
	}

	allSaved := true
	for _, user := range users {
		currentTotal := user.InFlow + user.OutFlow
		increment := currentTotal

		lastTotal, err := h.repo.GetLastStatisticsFlowTotal(user.UserID)
		if err != nil {
			log.Printf("statistics flow baseline lookup failed user_id=%d err=%v", user.UserID, err)
			allSaved = false
			continue
		}
		if lastTotal.Valid {
			increment = currentTotal - lastTotal.Int64
			if increment < 0 {
				increment = currentTotal
			}
		}

		if err := h.repo.CreateStatisticsFlow(user.UserID, increment, currentTotal, hourText, createdTime); err != nil {
			log.Printf("statistics flow write failed user_id=%d err=%v", user.UserID, err)
			allSaved = false
		}
	}
	// Old rows may still be the only cumulative baseline after downtime. Do
	// not remove them until every user has a successfully saved replacement.
	if allSaved {
		if err := h.repo.PurgeOldStatisticsFlows(cutoffMs); err != nil {
			log.Printf("statistics flow cleanup failed err=%v", err)
		}
	}
}

func (h *Handler) runResetAndExpiryJob(now time.Time) {
	if h == nil || h.repo == nil {
		return
	}

	h.resetMonthlyFlow(now)
	h.disableExpiredUsers(now.UnixMilli())
	h.disableExpiredUserTunnels(now.UnixMilli())
}

func (h *Handler) resetMonthlyFlow(now time.Time) {
	if err := h.repo.RollMonthlyFlows(now); err != nil {
		log.Printf("monthly flow reset failed: %v", err)
	}
}

func (h *Handler) disableExpiredUsers(nowMs int64) {
	userIDs, err := h.repo.ListExpiredActiveUserIDs(nowMs)
	if err != nil {
		return
	}

	for _, userID := range userIDs {
		forwards, err := h.listActiveForwardsByUser(userID)
		if err == nil {
			h.pauseForwardRecords(forwards, nowMs)
		}
		_ = h.repo.DisableUser(userID)
	}
}

func (h *Handler) disableExpiredUserTunnels(nowMs int64) {
	items, err := h.repo.ListExpiredActiveUserTunnels(nowMs)
	if err != nil {
		return
	}

	for _, item := range items {
		forwards, err := h.listActiveForwardsByUserTunnel(item.UserID, item.TunnelID)
		if err == nil {
			h.pauseForwardRecords(forwards, nowMs)
		}
		_ = h.repo.DisableUserTunnel(item.ID)
	}
}

func (h *Handler) runNodeRenewalCycleLoop(ctx context.Context) {
	defer h.jobsWG.Done()

	for {
		wait := durationUntilNextNodeRenewalCycle(time.Now())
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
			h.runNodeRenewalCycleJob(time.Now())
		}
	}
}

func durationUntilNextNodeRenewalCycle(now time.Time) time.Duration {
	next := now.Truncate(6 * time.Hour).Add(6 * time.Hour)
	return next.Sub(now)
}

func (h *Handler) runNodeRenewalCycleJob(now time.Time) {
	if h == nil || h.repo == nil {
		return
	}

	advanced, err := h.repo.AdvanceNodeRenewalCycles(now.UnixMilli())
	if err != nil {
		return
	}

	_ = advanced
}

func (h *Handler) runQuotaControlLoop(ctx context.Context) {
	defer h.jobsWG.Done()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		h.resetUserQuotaWindows(time.Now())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
