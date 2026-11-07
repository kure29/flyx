package model

// TrafficCursor bounds deduplication storage to one row per reporting stream.
type TrafficCursor struct {
	Scope    string `gorm:"primaryKey;size:200"`
	Sequence int64  `gorm:"not null;default:0"`
	Digest   string `gorm:"size:64;not null;default:''"`
}

func (TrafficCursor) TableName() string { return "traffic_cursor" }
