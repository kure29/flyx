package repo

import (
	"errors"

	"go-backend/internal/store/model"
)

// InsertNodeLatencySamples stores one round of latency measurements.
func (r *Repository) InsertNodeLatencySamples(samples []model.NodeLatencySample) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	if len(samples) == 0 {
		return nil
	}
	return r.db.CreateInBatches(samples, 200).Error
}

// ListNodeLatencySamples returns a node's samples since the given time,
// oldest first.
func (r *Repository) ListNodeLatencySamples(nodeID, sinceMs int64) ([]model.NodeLatencySample, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var out []model.NodeLatencySample
	err := r.db.Where("node_id = ? AND created_time >= ?", nodeID, sinceMs).
		Order("created_time ASC, id ASC").Find(&out).Error
	return out, err
}

// ListLatencySamplesSince returns every node's samples since the given
// time, oldest first.
func (r *Repository) ListLatencySamplesSince(sinceMs int64) ([]model.NodeLatencySample, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var out []model.NodeLatencySample
	err := r.db.Where("created_time >= ?", sinceMs).
		Order("node_id ASC, created_time ASC, id ASC").Find(&out).Error
	return out, err
}

// PruneNodeLatencySamples deletes samples older than the cutoff.
func (r *Repository) PruneNodeLatencySamples(olderThanMs int64) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Where("created_time < ?", olderThanMs).Delete(&model.NodeLatencySample{}).Error
}
