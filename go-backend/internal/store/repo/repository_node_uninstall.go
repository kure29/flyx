package repo

import (
	"errors"
	"strings"
	"time"

	"go-backend/internal/store/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Agents that stay offline this long are presumed gone with their machines.
const nodeUninstallRetention = 180 * 24 * time.Hour

func (r *Repository) AddNodeUninstall(secret, nodeName string, now time.Time) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return errors.New("secret is required")
	}
	if err := r.db.Where("created_time < ?", now.Add(-nodeUninstallRetention).UnixMilli()).Delete(&model.NodeUninstall{}).Error; err != nil {
		return err
	}
	return r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.NodeUninstall{
		Secret: secret, NodeName: nodeName, CreatedTime: now.UnixMilli(),
	}).Error
}

// GetNodeUninstallBySecret returns nil when the secret has nothing pending.
func (r *Repository) GetNodeUninstallBySecret(secret string) (*model.NodeUninstall, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	if strings.TrimSpace(secret) == "" {
		return nil, nil
	}
	var row model.NodeUninstall
	err := r.db.Where("secret = ?", secret).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *Repository) DeleteNodeUninstall(secret string) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Where("secret = ?", secret).Delete(&model.NodeUninstall{}).Error
}
