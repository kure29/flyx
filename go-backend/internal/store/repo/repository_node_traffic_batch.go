package repo

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go-backend/internal/store/model"
	"gorm.io/gorm/clause"
	"sort"
	"time"
)

// SaveNodeMetricBatch keeps samples, counter baselines and history increments
// atomic. Retrying an uncertain commit cannot add the same bytes twice.
func (r *Repository) SaveNodeMetricBatch(stream string, sequence int64, samples []*model.NodeMetric) error {
	data, err := json.Marshal(samples)
	if err != nil {
		return err
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	_, err = r.ApplyTrafficBatch("node-history:"+stream, sequence, digest, func(tx *Repository) error {
		byNode := map[int64][]*model.NodeMetric{}
		for _, sample := range samples {
			if sample.NodeID > 0 && sample.NetInBytes >= 0 && sample.NetOutBytes >= 0 {
				byNode[sample.NodeID] = append(byNode[sample.NodeID], sample)
			}
		}
		ids := make([]int64, 0, len(byNode))
		for id := range byNode {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			baseline := model.NodeTrafficBaseline{NodeID: id}
			if err := tx.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&baseline).Error; err != nil {
				return err
			}
			if err := tx.db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&baseline, "node_id = ?", id).Error; err != nil {
				return err
			}
			if baseline.Timestamp == 0 {
				// Upgrade from the old in-memory accumulator using its last
				// successfully saved raw sample, when one is available.
				latest, err := tx.GetLatestNodeMetric(id)
				if err != nil {
					return err
				}
				if latest != nil && latest.NetInBytes >= 0 && latest.NetOutBytes >= 0 {
					baseline.InBytes, baseline.OutBytes, baseline.Uptime, baseline.Timestamp = latest.NetInBytes, latest.NetOutBytes, latest.Uptime, latest.Timestamp
				}
			}
			traffic := map[int64][2]int64{}
			for _, sample := range byNode[id] {
				if sample.Timestamp < baseline.Timestamp {
					continue
				}
				if baseline.Timestamp > 0 {
					in, out := sample.NetInBytes-baseline.InBytes, sample.NetOutBytes-baseline.OutBytes
					if in < 0 || (sample.Uptime > 0 && baseline.Uptime > sample.Uptime) {
						in = sample.NetInBytes
					}
					if out < 0 || (sample.Uptime > 0 && baseline.Uptime > sample.Uptime) {
						out = sample.NetOutBytes
					}
					hour := sample.Timestamp - sample.Timestamp%int64(15*time.Minute/time.Millisecond)
					total := traffic[hour]
					total[0] += in
					total[1] += out
					traffic[hour] = total
				}
				baseline.InBytes, baseline.OutBytes, baseline.Uptime, baseline.Timestamp = sample.NetInBytes, sample.NetOutBytes, sample.Uptime, sample.Timestamp
			}
			rows := make([]model.NodeTraffic, 0, len(traffic))
			for hour, total := range traffic {
				rows = append(rows, model.NodeTraffic{NodeID: id, Hour: hour, BucketMinutes: 15, InBytes: total[0], OutBytes: total[1]})
			}
			if err := tx.AddNodeTraffic(rows); err != nil {
				return err
			}
			if err := tx.db.Save(&baseline).Error; err != nil {
				return err
			}
		}
		// GORM assigns IDs even when a transaction later rolls back. Keep the
		// pending input immutable so its digest remains stable on retries.
		copies := make([]*model.NodeMetric, len(samples))
		for i, sample := range samples {
			copy := *sample
			copy.ID = 0
			copies[i] = &copy
		}
		return tx.InsertNodeMetricBatch(copies)
	})
	return err
}
