package repo

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

func monthlyFlowBoundary(now time.Time, day int) time.Time {
	if day < 1 {
		return time.Time{}
	}
	boundary := func(year int, month time.Month) time.Time {
		last := time.Date(year, month+1, 0, 0, 0, 0, 0, now.Location()).Day()
		d := day
		if d > last {
			d = last
		}
		return time.Date(year, month, d, 0, 0, 0, 0, now.Location())
	}
	current := boundary(now.Year(), now.Month())
	if current.After(now) {
		return boundary(now.Year(), now.Month()-1)
	}
	return current
}

// Unknown legacy cycles are initialized without clearing existing usage. From
// that point onward the persisted boundary makes catch-up and retries safe.
// It returns the IDs whose in_flow/out_flow counters were actually zeroed.
func rollMonthlyFlowTx(tx *gorm.DB, table string, ids []int64, now time.Time) ([]int64, error) {
	var rows []struct {
		ID            int64
		FlowResetTime int64
		FlowResetAt   int64
	}
	if ids == nil {
		// Minute maintenance only discovers candidates here; locking every
		// account would serialize unrelated traffic uploads with this scan.
		if err := tx.Table(table).Select("id, flow_reset_time, flow_reset_at").Where("flow_reset_time > 0").Order("id ASC").Find(&rows).Error; err != nil {
			return nil, err
		}
		ids = make([]int64, 0, len(rows))
		for _, row := range rows {
			if row.FlowResetAt == 0 || row.FlowResetAt < monthlyFlowBoundary(now, int(row.FlowResetTime)).UnixMilli() {
				ids = append(ids, row.ID)
			}
		}
	}
	var resetIDs []int64
	for start := 0; start < len(ids); start += 500 {
		end := start + 500
		if end > len(ids) {
			end = len(ids)
		}
		rows = nil
		// Billing may have rolled a candidate while we were scanning. Read
		// fresh values under the billing row locks and recheck before resetting.
		if err := tx.Table(table).Select("id, flow_reset_time, flow_reset_at").Where("flow_reset_time > 0 AND id IN ?", ids[start:end]).Order("id ASC").Clauses(clause.Locking{Strength: "UPDATE"}).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			boundary := monthlyFlowBoundary(now, int(row.FlowResetTime)).UnixMilli()
			updates := map[string]any{}
			if row.FlowResetAt == 0 {
				updates["flow_reset_at"] = now.UnixMilli()
			} else if row.FlowResetAt < boundary {
				updates["flow_reset_at"], updates["in_flow"], updates["out_flow"] = boundary, 0, 0
				resetIDs = append(resetIDs, row.ID)
			}
			if len(updates) > 0 {
				if err := tx.Table(table).Where("id = ?", row.ID).Updates(updates).Error; err != nil {
					return nil, err
				}
			}
		}
	}
	return resetIDs, nil
}

func (r *Repository) RollMonthlyFlows(now time.Time) error {
	_, err := r.RollMonthlyFlowsWithResetUsers(now)
	return err
}

func (r *Repository) RollMonthlyFlowsWithResetUsers(now time.Time) ([]int64, error) {
	var resetUsers []int64
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var err error
		resetUsers, err = rollMonthlyFlowTx(tx, "user", nil, now)
		if err != nil {
			return err
		}
		_, err = rollMonthlyFlowTx(tx, "user_tunnel", nil, now)
		return err
	})
	return resetUsers, err
}
