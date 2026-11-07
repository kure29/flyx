package handler

// enforceFlowUploadBatch acts on an upload once it is counted: services no
// forward owns go, and panels sharing a node that used up their share stop.
func (h *Handler) enforceFlowUploadBatch(nodeID int64, batch flowUploadBatch) {
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
