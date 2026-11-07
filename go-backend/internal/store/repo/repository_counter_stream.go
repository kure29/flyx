package repo

import (
	"go-backend/internal/store/model"
	"gorm.io/gorm/clause"
)

func (r *Repository) lockCounterStream(nodeID int64, source string) (*model.CounterStream, error) {
	row := model.CounterStream{NodeID: nodeID, Source: source}
	if err := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return nil, err
	}
	err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ? AND source = ?", nodeID, source).First(&row).Error
	return &row, err
}

// Reserve before issuing the RPC, so late replies cannot overtake a newer
// accepted request. Database sequencing also covers multiple panel instances.
func (r *Repository) NextCounterSnapshot(nodeID int64, source string) (int64, error) {
	var sequence int64
	err := r.Transaction(func(tx *Repository) error {
		row, err := tx.lockCounterStream(nodeID, source)
		if err != nil {
			return err
		}
		sequence = row.Issued + 1
		return tx.db.Model(row).Update("issued", sequence).Error
	})
	return sequence, err
}

func (r *Repository) ApplyCounterSnapshot(nodeID int64, source string, sequence int64, epoch string, apply func(*Repository) error) (bool, error) {
	applied := false
	err := r.Transaction(func(tx *Repository) error {
		row, err := tx.lockCounterStream(nodeID, source)
		if err != nil {
			return err
		}
		if sequence <= row.Applied || sequence > row.Issued {
			return nil
		}
		if epoch != "" && epoch != row.Epoch {
			var count int64
			if err := tx.db.Model(&model.CounterEpoch{}).Where("node_id = ? AND source = ? AND epoch = ?", nodeID, source, epoch).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return nil
			} // A retired generation must never become current again.
			if row.Epoch != "" {
				if err := tx.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.CounterEpoch{NodeID: nodeID, Source: source, Epoch: row.Epoch}).Error; err != nil {
					return err
				}
			}
		}
		if err := apply(tx); err != nil {
			return err
		}
		if err := tx.db.Model(row).Updates(map[string]any{"applied": sequence, "epoch": epoch}).Error; err != nil {
			return err
		}
		applied = true
		return nil
	})
	return applied && err == nil, err
}
