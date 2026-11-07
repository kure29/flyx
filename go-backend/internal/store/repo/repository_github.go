package repo

import (
	"errors"

	"go-backend/internal/store/model"
	"gorm.io/gorm"
)

func (r *Repository) GetUserGithub(userID int64) (*model.UserGithub, error) {
	var row model.UserGithub
	err := r.db.Where("user_id = ?", userID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, err
}

func (r *Repository) GetUserGithubByGithubID(githubID int64) (*model.UserGithub, error) {
	var row model.UserGithub
	err := r.db.Where("github_id = ?", githubID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, err
}

func (r *Repository) CreateUserGithub(row *model.UserGithub) error {
	return r.db.Create(row).Error
}

func (r *Repository) DeleteUserGithub(userID int64) (bool, error) {
	result := r.db.Where("user_id = ?", userID).Delete(&model.UserGithub{})
	return result.RowsAffected > 0, result.Error
}

// ListAdminsWithoutGithub names the enabled admins with no GitHub account
// bound, who could not log in once admin password login is off.
func (r *Repository) ListAdminsWithoutGithub() ([]string, error) {
	var names []string
	err := r.db.Model(&model.User{}).
		Where("role_id = 0 AND status = 1").
		Where("id NOT IN (?)", r.db.Model(&model.UserGithub{}).Select("user_id")).
		Order("id").Pluck("user", &names).Error
	return names, err
}
