package model

// UserGithub links a panel user to the GitHub account they log in with.
// One GitHub account per user and one user per GitHub account.
type UserGithub struct {
	ID        int64  `gorm:"primaryKey;autoIncrement"`
	UserID    int64  `gorm:"column:user_id;not null;uniqueIndex"`
	GithubID  int64  `gorm:"column:github_id;not null;uniqueIndex"`
	Login     string `gorm:"type:varchar(100);not null;default:''"`
	CreatedAt int64  `gorm:"column:created_at;not null"`
}

func (UserGithub) TableName() string { return "user_github" }
