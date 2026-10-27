package handler

import "go-backend/internal/store/model"

func (h *Handler) enforceFlowUploadBatch(nodeID int64, batch flowUploadBatch, quotas map[int64]*model.UserQuotaView) {
	for id, q := range quotas {
		h.enforceUserQuotaIfNeeded(id, q)
	}
	for _, target := range batch.policyTargets {
		if target.UserID > 0 && target.UserTunnelID > 0 {
			h.enforceFlowPolicies(target.UserID, target.UserTunnelID)
		}
	}
	if len(batch.orphanServices) > 0 {
		names := make([]string, 0, len(batch.orphanServices))
		for name := range batch.orphanServices {
			names = append(names, name)
		}
		h.sendDeleteOrphanedForwardServices(nodeID, names)
	}
	for id := range batch.peerShareUsage {
		share, err := h.repo.GetPeerShare(id)
		if err == nil && isPeerShareFlowExceeded(share) {
			h.enforcePeerShareFlowLimit(id)
		}
	}
}
