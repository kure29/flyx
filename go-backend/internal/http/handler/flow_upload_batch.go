package handler

import (
	"errors"
	"log"
	"sort"
	"strings"
	"time"

	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
)

type flowPolicyTarget struct {
	UserID       int64
	UserTunnelID int64
}

type flowUploadBatch struct {
	err                   error
	flowDeltas            []repo.FlowUploadCounterDelta
	quotaUsage            map[int64]int64
	policyTargets         []flowPolicyTarget
	forwardTraffic        map[int64]tunnelTrafficDelta
	orphanServices        map[string]struct{}
	peerShareForwardItems map[string]flowItem
	peerShareRuntimeItems map[int64]flowItem
	peerShareUsage        map[int64]int64
}

func (h *Handler) buildFlowUploadBatch(items []flowItem, metas map[int64]repo.FlowUploadForwardMeta) flowUploadBatch {
	batch := flowUploadBatch{
		quotaUsage:            make(map[int64]int64),
		forwardTraffic:        make(map[int64]tunnelTrafficDelta),
		orphanServices:        make(map[string]struct{}),
		peerShareForwardItems: make(map[string]flowItem),
		peerShareRuntimeItems: make(map[int64]flowItem),
	}
	policySeen := map[flowPolicyTarget]struct{}{}
	flowSeen := map[int64]int{}

	for _, item := range items {
		serviceName := strings.TrimSpace(item.N)
		if serviceName == "" || serviceName == "web_api" {
			continue
		}
		if runtimeID, ok := parsePeerShareRuntimeServiceID(serviceName); ok {
			merged := batch.peerShareRuntimeItems[runtimeID]
			merged.N = serviceName
			merged.U += item.U
			merged.D += item.D
			batch.peerShareRuntimeItems[runtimeID] = merged
			continue
		}
		forwardID, userID, userTunnelID, ok := parseFlowServiceIDs(serviceName)
		if !ok {
			continue
		}
		normalized := normalizeForwardRuntimeServiceName(serviceName)
		merged := batch.peerShareForwardItems[normalized]
		merged.N = normalized
		merged.U += item.U
		merged.D += item.D
		batch.peerShareForwardItems[normalized] = merged

		meta, exists := metas[forwardID]
		if !exists {
			batch.orphanServices[serviceName] = struct{}{}
			continue
		}
		// Local accounting uses database ownership, not foreign IDs embedded in
		// a service name (including stale user-tunnel IDs after reassignment).
		userID, userTunnelID = meta.UserID, meta.UserTunnelID

		raw := batch.forwardTraffic[forwardID]
		raw.bytesIn += item.D
		raw.bytesOut += item.U
		batch.forwardTraffic[forwardID] = raw

		if !validTunnelBilling(meta.TunnelFlow, meta.TrafficRatio) {
			log.Printf("flow billing skipped forward_id=%d: invalid tunnel billing configuration", forwardID)
			continue
		}

		// flow selects the billable directions; traffic_ratio is the multiplier.
		scaledIn := int64(float64(item.D) * meta.TrafficRatio)
		var scaledOut int64
		if meta.TunnelFlow != 1 {
			scaledOut = int64(float64(item.U) * meta.TrafficRatio)
		}
		if idx, ok := flowSeen[forwardID]; ok {
			batch.flowDeltas[idx].InFlow += scaledIn
			batch.flowDeltas[idx].OutFlow += scaledOut
		} else {
			flowSeen[forwardID] = len(batch.flowDeltas)
			batch.flowDeltas = append(batch.flowDeltas, repo.FlowUploadCounterDelta{
				ForwardID:    forwardID,
				UserID:       userID,
				UserTunnelID: userTunnelID,
				InFlow:       scaledIn,
				OutFlow:      scaledOut,
			})
		}
		batch.quotaUsage[userID] += scaledIn + scaledOut

		target := flowPolicyTarget{UserID: userID, UserTunnelID: userTunnelID}
		if _, seen := policySeen[target]; !seen {
			policySeen[target] = struct{}{}
			batch.policyTargets = append(batch.policyTargets, target)
		}

	}

	sort.Slice(batch.policyTargets, func(i, j int) bool {
		if batch.policyTargets[i].UserID == batch.policyTargets[j].UserID {
			return batch.policyTargets[i].UserTunnelID < batch.policyTargets[j].UserTunnelID
		}
		return batch.policyTargets[i].UserID < batch.policyTargets[j].UserID
	})

	return batch
}

// commitFlowUploadBatch must run inside the caller's accounting transaction.
func (h *Handler) commitFlowUploadBatch(nodeID int64, batch *flowUploadBatch, now time.Time) (map[int64]*model.UserQuotaView, error) {
	if batch.err != nil {
		return nil, batch.err
	}
	shares, err := h.resolveFlowShareUsage(nodeID, *batch)
	if err != nil {
		return nil, err
	}
	batch.peerShareUsage = shares
	if err := h.repo.ApplyFlowUploadDeltasBatchAt(batch.flowDeltas, now); err != nil {
		return nil, err
	}
	quotas, err := h.repo.AddUserQuotaUsageBatch(batch.quotaUsage, now)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(shares))
	for id := range shares {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if err := h.repo.AddPeerShareCurrentFlow(id, shares[id]); err != nil {
			return nil, err
		}
	}
	return quotas, nil
}

func (h *Handler) applyFlowUploadBatch(nodeID int64, batch flowUploadBatch, now time.Time) error {
	if h == nil || h.repo == nil {
		return errors.New("repository unavailable")
	}
	var quotas map[int64]*model.UserQuotaView
	err := h.repo.Transaction(func(tx *repo.Repository) error {
		var err error
		quotas, err = (&Handler{repo: tx}).commitFlowUploadBatch(nodeID, &batch, now)
		return err
	})
	if err != nil {
		return err
	}
	h.enforceFlowUploadBatch(nodeID, batch, quotas)
	return nil
}

func (h *Handler) resolveFlowShareUsage(nodeID int64, batch flowUploadBatch) (map[int64]int64, error) {
	usages := make(map[int64]int64)
	add := func(id, delta int64) error {
		share, err := h.repo.GetPeerShare(id)
		if err != nil {
			return err
		}
		if share != nil && share.NodeID == nodeID && delta > 0 {
			usages[id] += delta
		}
		return nil
	}
	for id, delta := range batch.peerShareUsage {
		if err := add(id, delta); err != nil {
			return nil, err
		}
	}
	for id, item := range batch.peerShareRuntimeItems {
		runtime, err := h.repo.GetPeerShareRuntimeByID(id)
		if err != nil {
			return nil, err
		}
		if runtime != nil && runtime.NodeID == nodeID && runtime.Status == 1 {
			if err := add(runtime.ShareID, item.U+item.D); err != nil {
				return nil, err
			}
		}
	}
	if len(batch.peerShareForwardItems) == 0 {
		return usages, nil
	}
	runtimes, err := h.repo.ListActiveForwardPeerShareRuntimesByNode(nodeID)
	if err != nil {
		return nil, err
	}
	for name, item := range batch.peerShareForwardItems {
		var owner int64
		ambiguous := false
		for _, rt := range runtimes {
			if normalizeForwardRuntimeServiceName(rt.ServiceName) == name {
				if owner != 0 {
					ambiguous = true
				}
				owner = rt.ShareID
			}
		}
		if ambiguous {
			continue
		}
		if owner == 0 {
			fid, uid, _, ok := parseFlowServiceIDs(name)
			if !ok {
				continue
			}
			f, err := h.repo.GetForwardRecord(fid)
			if err != nil {
				return nil, err
			}
			if f == nil || f.UserID != uid {
				continue
			}
			tunnel, err := h.repo.GetTunnelRecord(f.TunnelID)
			if err != nil {
				return nil, err
			}
			if tunnel == nil {
				continue
			}
			name, err := h.repo.GetTunnelName(f.TunnelID)
			if err != nil {
				return nil, err
			}
			owner, _ = parsePeerShareIDFromFederationTunnelName(name)
		}
		if owner > 0 {
			if err := add(owner, item.U+item.D); err != nil {
				return nil, err
			}
		}
	}
	return usages, nil
}
