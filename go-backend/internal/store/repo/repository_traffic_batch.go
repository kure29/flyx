package repo

import (
	"errors"
	"fmt"
	"go-backend/internal/store/model"
	"gorm.io/gorm/clause"
)

var ErrTrafficSequence = errors.New("unexpected traffic batch sequence or content")

// ApplyTrafficBatch commits the accounting and its acknowledgement together.
// Producers send one batch at a time and retain it until the matching ACK.
func (r *Repository) ApplyTrafficBatch(scope string, sequence int64, digest string, apply func(*Repository) error) (bool, error) {
	if scope == "" || len(scope) > 200 || sequence <= 0 || len(digest) != 64 {
		return false, ErrTrafficSequence
	}
	applied := false
	err := r.Transaction(func(tx *Repository) error {
		cursor := model.TrafficCursor{Scope: scope}
		if err := tx.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&cursor).Error; err != nil {
			return err
		}
		if err := tx.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("scope = ?", scope).First(&cursor).Error; err != nil {
			return err
		}
		if sequence <= cursor.Sequence {
			if sequence == cursor.Sequence && digest != cursor.Digest {
				return ErrTrafficSequence
			}
			return nil
		}
		if sequence != cursor.Sequence+1 {
			return fmt.Errorf("%w: got %d after %d", ErrTrafficSequence, sequence, cursor.Sequence)
		}
		if err := apply(tx); err != nil {
			return err
		}
		if err := tx.db.Model(&cursor).Updates(map[string]any{"sequence": sequence, "digest": digest}).Error; err != nil {
			return err
		}
		applied = true
		return nil
	})
	return applied && err == nil, err
}
