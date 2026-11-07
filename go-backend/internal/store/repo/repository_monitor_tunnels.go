package repo

import (
	"errors"

	"go-backend/internal/store/model"
)

func (r *Repository) ListMonitorTunnels() ([]model.Tunnel, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var tunnels []model.Tunnel
	err := r.db.Select("id", "inx", "name", "status", "updated_time").
		Order("inx ASC, id ASC").
		Find(&tunnels).Error
	return tunnels, err
}
