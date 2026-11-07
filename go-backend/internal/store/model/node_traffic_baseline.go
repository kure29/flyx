package model

// NodeTrafficBaseline survives panel restarts and advances with traffic totals.
type NodeTrafficBaseline struct {
	NodeID    int64 `gorm:"primaryKey;autoIncrement:false"`
	InBytes   int64 `gorm:"not null;default:0"`
	OutBytes  int64 `gorm:"not null;default:0"`
	Uptime    int64 `gorm:"not null;default:0"`
	Timestamp int64 `gorm:"not null;default:0"`
}

func (NodeTrafficBaseline) TableName() string { return "node_traffic_baseline" }
