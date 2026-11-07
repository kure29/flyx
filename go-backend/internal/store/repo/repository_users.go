package repo

import (
	"errors"

	"gorm.io/gorm"

	"go-backend/internal/store/model"
)

// ─── User Queries ────────────────────────────────────────────────────

func (r *Repository) GetUserByUsername(username string) (*model.User, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var user model.User
	err := r.db.Where(`"user" = ?`, username).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *Repository) GetUserByID(id int64) (*model.User, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var user model.User
	err := r.db.Where("id = ?", id).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *Repository) UsernameExistsExceptID(username string, exceptID int64) (bool, error) {
	if r == nil || r.db == nil {
		return false, errors.New("repository not initialized")
	}
	var count int64
	err := r.db.Model(&model.User{}).Where(`"user" = ? AND id != ?`, username, exceptID).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *Repository) UpdateUserNameAndPassword(userID int64, username, passwordMD5 string, now int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Model(&model.User{}).Where("id = ?", userID).Updates(map[string]interface{}{
		"user":                username,
		"pwd":                 passwordMD5,
		"password_changed_at": now,
		"updated_time":        now,
	}).Error
}

// ─── User Package Queries ────────────────────────────────────────────

func (r *Repository) GetUserPackageTunnels(userID int64) ([]model.UserTunnelDetail, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var items []model.UserTunnelDetail
	err := r.db.Model(&model.UserTunnel{}).
		Select("user_tunnel.id, user_tunnel.user_id, user_tunnel.tunnel_id, tunnel.name AS tunnel_name, user_tunnel.status, tunnel.flow AS tunnel_flow, user_tunnel.flow, user_tunnel.flow_mib, user_tunnel.in_flow, user_tunnel.out_flow, user_tunnel.num, user_tunnel.flow_reset_time, user_tunnel.exp_time, user_tunnel.speed_id, speed_limit.name AS speed_limit, speed_limit.speed").
		Joins("LEFT JOIN tunnel ON tunnel.id = user_tunnel.tunnel_id").
		Joins("LEFT JOIN speed_limit ON speed_limit.id = user_tunnel.speed_id").
		Where("user_tunnel.user_id = ?", userID).
		Order("user_tunnel.id ASC").
		Find(&items).Error
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = make([]model.UserTunnelDetail, 0)
	}
	return items, nil
}

func (r *Repository) GetUserTunnelByID(id int64) (*model.UserTunnel, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var ut model.UserTunnel
	err := r.db.Where("id = ?", id).First(&ut).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ut, nil
}
