package repo

import (
	"errors"

	"go-backend/internal/store/model"
)

func (r *Repository) ListMonitorNodes() ([]model.Node, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var nodes []model.Node
	err := r.db.Select("id", "inx", "name", "status", "version", "updated_time", "latency_targets").
		Where("is_remote = ?", 0).
		Order("inx ASC, id ASC").
		Find(&nodes).Error
	return nodes, err
}

// SetNodesLatencyTargets stores the nodes' own latency reference point
// choice ("" follows the defaults).
func (r *Repository) SetNodesLatencyTargets(ids []int64, value string) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	if len(ids) == 0 {
		return nil
	}
	return r.db.Model(&model.Node{}).Where("id IN ? AND is_remote = ?", ids, 0).
		Update("latency_targets", value).Error
}

// ListNodesWithExpiry returns local nodes that have an expiry time set.
func (r *Repository) ListNodesWithExpiry() ([]model.Node, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var nodes []model.Node
	err := r.db.Select("id", "name", "expiry_time", "renewal_cycle", "expiry_reminder_dismissed").
		Where("is_remote = ? AND expiry_time IS NOT NULL AND expiry_time > 0", 0).
		Order("id ASC").
		Find(&nodes).Error
	return nodes, err
}

// SumStatisticsFlowSince adds up the hourly forward traffic increments
// recorded since the given time (unix ms).
func (r *Repository) SumStatisticsFlowSince(sinceMs int64) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("repository not initialized")
	}
	var total int64
	err := r.db.Model(&model.StatisticsFlow{}).Where("created_time >= ?", sinceMs).
		Select("COALESCE(SUM(flow), 0)").Scan(&total).Error
	return total, err
}

// SumInboundTraffic returns the cumulative traffic of all inbounds.
func (r *Repository) SumInboundTraffic() (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("repository not initialized")
	}
	var total int64
	err := r.db.Model(&model.NodeInbound{}).Select("COALESCE(SUM(upload_bytes + download_bytes), 0)").Scan(&total).Error
	return total, err
}

// CountNodesOnline returns (online, total) for local nodes.
func (r *Repository) CountNodesOnline() (int64, int64, error) {
	if r == nil || r.db == nil {
		return 0, 0, errors.New("repository not initialized")
	}
	var total, online int64
	if err := r.db.Model(&model.Node{}).Where("is_remote = ?", 0).Count(&total).Error; err != nil {
		return 0, 0, err
	}
	if err := r.db.Model(&model.Node{}).Where("is_remote = ? AND status = ?", 0, 1).Count(&online).Error; err != nil {
		return 0, 0, err
	}
	return online, total, nil
}
