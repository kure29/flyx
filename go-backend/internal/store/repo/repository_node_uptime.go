package repo

import (
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go-backend/internal/store/model"
)

// RecordNodeUptime adds one sample per node to the bucket: up when online.
func (r *Repository) RecordNodeUptime(bucket int64, online map[int64]bool) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	if len(online) == 0 {
		return nil
	}
	rows := make([]model.NodeUptime, 0, len(online))
	for id, up := range online {
		row := model.NodeUptime{NodeID: id, Bucket: bucket, Samples: 1}
		if up {
			row.Up = 1
		}
		rows = append(rows, row)
	}
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "node_id"}, {Name: "bucket"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"samples": gorm.Expr("node_uptime.samples + excluded.samples"),
			"up":      gorm.Expr("node_uptime.up + excluded.up"),
		}),
	}).Create(&rows).Error
}

// NodeUptimeSince returns every bucket from fromBucket on.
func (r *Repository) NodeUptimeSince(fromBucket int64) ([]model.NodeUptime, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var rows []model.NodeUptime
	err := r.db.Where("bucket >= ?", fromBucket).Find(&rows).Error
	return rows, err
}

// PruneNodeUptime drops the buckets before beforeBucket.
func (r *Repository) PruneNodeUptime(beforeBucket int64) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Where("bucket < ?", beforeBucket).Delete(&model.NodeUptime{}).Error
}

// ListStatusPageNodes returns the local nodes with only the fields the
// public status page may use: never addresses, ports or secrets.
func (r *Repository) ListStatusPageNodes() ([]model.Node, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var nodes []model.Node
	err := r.db.Select("id", "inx", "name", "status", "expiry_time",
		"host_os", "host_arch", "host_virtualization", "host_cpu_name", "region", "region_detected").
		Where("is_remote = ?", 0).
		Order("inx ASC, id ASC").
		Find(&nodes).Error
	return nodes, err
}
