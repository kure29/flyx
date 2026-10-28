package handler

import (
	"fmt"
	"math"
	"strings"

	"go-backend/internal/store/repo"
)

// Validate the entire upload before aggregating it or producing any side effects.
// A bound on the batch total also bounds every per-forward, user and share sum.
func validateFlowUploadItems(items []flowItem, metas map[int64]repo.FlowUploadForwardMeta) error {
	var rawTotal, scaledTotal int64
	for i, item := range items {
		for _, value := range []int64{item.U, item.D} {
			if value < 0 || value > math.MaxInt64-rawTotal {
				return fmt.Errorf("item %d: invalid traffic counter or overflowing batch", i)
			}
			rawTotal += value
		}
		forwardID, _, _, ok := parseFlowServiceIDs(strings.TrimSpace(item.N))
		meta, exists := metas[forwardID]
		if !ok || !exists {
			continue
		}
		if !validTunnelBilling(meta.TunnelFlow, meta.TrafficRatio) {
			// Stored configuration is not an upload error. The batch builder
			// logs and skips local billing for this forward, preserving peers.
			continue
		}
		values := []int64{item.D}
		if meta.TunnelFlow == 2 {
			values = append(values, item.U)
		}
		for _, value := range values {
			scaled := float64(value) * meta.TrafficRatio
			// float64(MaxInt64) rounds up to 2^63. Reject that boundary before
			// converting, as an out-of-range float-to-int conversion can wrap.
			if math.IsInf(scaled, 0) || scaled >= float64(math.MaxInt64) {
				return fmt.Errorf("item %d: scaled traffic exceeds int64", i)
			}
			bytes := int64(scaled)
			if bytes > math.MaxInt64-scaledTotal {
				return fmt.Errorf("item %d: billing batch exceeds int64", i)
			}
			scaledTotal += bytes
		}
	}
	return nil
}
