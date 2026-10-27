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
		if meta.TunnelFlow <= 0 || meta.TrafficRatio <= 0 || math.IsNaN(meta.TrafficRatio) || math.IsInf(meta.TrafficRatio, 0) {
			return fmt.Errorf("item %d: invalid billing configuration", i)
		}
		for _, value := range []int64{item.U, item.D} {
			scaled := float64(value) * meta.TrafficRatio
			// float64(MaxInt64) rounds up to 2^63. Reject that boundary before
			// converting, as an out-of-range float-to-int conversion can wrap.
			if math.IsInf(scaled, 0) || scaled >= float64(math.MaxInt64) {
				return fmt.Errorf("item %d: scaled traffic exceeds int64", i)
			}
			bytes := int64(scaled)
			if bytes > math.MaxInt64/meta.TunnelFlow {
				return fmt.Errorf("item %d: billing traffic exceeds int64", i)
			}
			bytes *= meta.TunnelFlow
			if bytes > math.MaxInt64-scaledTotal {
				return fmt.Errorf("item %d: billing batch exceeds int64", i)
			}
			scaledTotal += bytes
		}
	}
	return nil
}
