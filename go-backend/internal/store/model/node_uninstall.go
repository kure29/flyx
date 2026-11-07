package model

// NodeUninstall remembers a server deleted while its agent was offline. When
// an agent connects again with this secret it is told to uninstall itself.
type NodeUninstall struct {
	ID          int64  `gorm:"primaryKey;autoIncrement"`
	Secret      string `gorm:"type:varchar(100);not null;uniqueIndex"`
	NodeName    string `gorm:"column:node_name;type:varchar(100);not null;default:''"`
	CreatedTime int64  `gorm:"column:created_time;not null"`
}

func (NodeUninstall) TableName() string { return "node_uninstall" }
