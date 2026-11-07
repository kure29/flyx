package handler

import (
	"fmt"
	"math"
)

// Validate the entire upload before aggregating it or producing any side effects.
// A bound on the batch total also bounds every per-forward and share sum.
func validateFlowUploadItems(items []flowItem) error {
	var total int64
	for i, item := range items {
		for _, value := range []int64{item.U, item.D} {
			if value < 0 || value > math.MaxInt64-total {
				return fmt.Errorf("item %d: invalid traffic counter or overflowing batch", i)
			}
			total += value
		}
	}
	return nil
}
