package repo

import (
	"errors"

	"go-backend/internal/store/model"
)

func (r *Repository) ListSubscriptionTemplates() ([]model.SubscriptionTemplate, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var out []model.SubscriptionTemplate
	err := r.db.Order("id ASC").Find(&out).Error
	return out, err
}

func (r *Repository) GetSubscriptionTemplate(id int64) (*model.SubscriptionTemplate, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var out model.SubscriptionTemplate
	res := r.db.Where("id = ?", id).Limit(1).Find(&out)
	if res.Error != nil || res.RowsAffected == 0 {
		return nil, res.Error
	}
	return &out, nil
}

func (r *Repository) SaveSubscriptionTemplate(t *model.SubscriptionTemplate) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Save(t).Error
}

func (r *Repository) DeleteSubscriptionTemplate(id int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Where("id = ?", id).Delete(&model.SubscriptionTemplate{}).Error
}
