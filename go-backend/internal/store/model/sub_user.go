package model

// SubUser is someone the admin shares inbounds with: an own subscription
// link and own credentials, cut off when disabled or expired.
type SubUser struct {
	ID       int64  `gorm:"primaryKey;autoIncrement"`
	Name     string `gorm:"type:varchar(100);not null"`
	Token    string `gorm:"type:varchar(64);not null;uniqueIndex"`
	UUID     string `gorm:"column:uuid;type:varchar(36);not null"`
	Password string `gorm:"type:varchar(64);not null"`
	// InboundIDs is a comma separated list of shared inbounds.
	InboundIDs string `gorm:"column:inbound_ids;type:text;not null;default:''"`
	// ExpireTime is unix ms; 0 never expires.
	ExpireTime int64 `gorm:"column:expire_time;not null;default:0"`
	Enabled    int   `gorm:"not null;default:1"`
	// AppliedActive is the active state last pushed to the kernels, so the
	// expiry loop knows whose credentials still have to be removed.
	AppliedActive int    `gorm:"column:applied_active;not null;default:0"`
	Remark        string `gorm:"type:text;not null;default:''"`
	CreatedTime   int64  `gorm:"column:created_time;not null"`
	UpdatedTime   int64  `gorm:"column:updated_time;not null"`
}

func (SubUser) TableName() string { return "sub_user" }
