package repo

import (
	"cmp"
	"errors"
	"slices"

	"go-backend/internal/store/model"

	"gorm.io/gorm"
)

var ErrRemoteSourceNodeOrder = errors.New("订阅源节点已变化，请重新打开预览后排序")

func applyRemoteSourceOrder(rows []model.NodeInbound) {
	bySource := map[int64][]model.NodeInbound{}
	for _, row := range rows {
		if row.SourceID != 0 && row.SourceOrder > 0 {
			bySource[row.SourceID] = nil
		}
	}
	for _, row := range rows {
		if nodes, sorted := bySource[row.SourceID]; sorted {
			bySource[row.SourceID] = append(nodes, row)
		}
	}
	for _, nodes := range bySource {
		slices.SortStableFunc(nodes, func(first, second model.NodeInbound) int {
			if first.SourceOrder == 0 || second.SourceOrder == 0 {
				return cmp.Compare(second.SourceOrder, first.SourceOrder)
			}
			return cmp.Compare(first.SourceOrder, second.SourceOrder)
		})
	}
	for index, row := range rows {
		if nodes, sorted := bySource[row.SourceID]; sorted {
			rows[index] = nodes[0]
			bySource[row.SourceID] = nodes[1:]
		}
	}
}

func (r *Repository) ListRemoteSources() ([]model.RemoteSource, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var out []model.RemoteSource
	err := r.db.Order("id ASC").Find(&out).Error
	return out, err
}

func (r *Repository) GetRemoteSource(id int64) (*model.RemoteSource, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var out model.RemoteSource
	res := r.db.Where("id = ?", id).Limit(1).Find(&out)
	if res.Error != nil || res.RowsAffected == 0 {
		return nil, res.Error
	}
	return &out, nil
}

// SaveRemoteSource stores the user-editable fields (the status fields are
// written by UpdateRemoteSourceStatus).
func (r *Repository) SaveRemoteSource(s *model.RemoteSource, nodeOrder []int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	if s.ID == 0 {
		if len(nodeOrder) > 0 {
			return ErrRemoteSourceNodeOrder
		}
		return r.db.Create(s).Error
	}
	fields := map[string]interface{}{
		"name": s.Name, "url": s.URL, "interval_hours": s.IntervalHours, "relay_only": s.RelayOnly,
		"kind": s.Kind, "role": s.Role, "user_agent": s.UserAgent, "enabled": s.Enabled, "updated_time": s.UpdatedTime,
	}
	if nodeOrder == nil {
		return r.db.Model(&model.RemoteSource{}).Where("id = ?", s.ID).Updates(fields).Error
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.RemoteSource{}).Where("id = ?", s.ID).Updates(fields)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrRemoteSourceNodeOrder
		}
		if err := tx.Model(&model.NodeInbound{}).Where("source_id = ? AND source_order <> 0", s.ID).Update("source_order", 0).Error; err != nil {
			return err
		}
		seen := make(map[int64]bool, len(nodeOrder))
		for index, id := range nodeOrder {
			if id <= 0 || seen[id] {
				return ErrRemoteSourceNodeOrder
			}
			seen[id] = true
			result := tx.Model(&model.NodeInbound{}).Where("id = ? AND source_id = ?", id, s.ID).Update("source_order", index+1)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return ErrRemoteSourceNodeOrder
			}
		}
		return nil
	})
}

func (r *Repository) UpdateRemoteSourceStatus(id int64, fields map[string]interface{}) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Model(&model.RemoteSource{}).Where("id = ?", id).Updates(fields).Error
}

func (r *Repository) DeleteRemoteSource(id int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Where("id = ?", id).Delete(&model.RemoteSource{}).Error
}

// DetachSourceInbounds keeps a deleted source's nodes as plain imports.
func (r *Repository) DetachSourceInbounds(sourceID int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Model(&model.NodeInbound{}).Where("source_id = ?", sourceID).Update("source_id", 0).Error
}
