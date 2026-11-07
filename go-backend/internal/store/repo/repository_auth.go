package repo

import (
	"errors"

	"go-backend/internal/auth"
	"go-backend/internal/store/model"
)

func (r *Repository) GetUserAuthState(userID int64) (*auth.UserAuthState, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var user struct {
		ID                int64 `gorm:"column:id"`
		RoleID            int   `gorm:"column:role_id"`
		Status            int   `gorm:"column:status"`
		PasswordChangedAt int64 `gorm:"column:password_changed_at"`
	}
	if err := r.db.Model(&model.User{}).Select("id", "role_id", "status", "password_changed_at").Where("id = ?", userID).First(&user).Error; err != nil {
		return nil, normalizeNotFoundErr(err)
	}
	return &auth.UserAuthState{
		ID:                user.ID,
		RoleID:            user.RoleID,
		Status:            user.Status,
		PasswordChangedAt: user.PasswordChangedAt,
	}, nil
}
