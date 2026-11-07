package model

type CounterStream struct {
	NodeID  int64  `gorm:"primaryKey;autoIncrement:false"`
	Source  string `gorm:"primaryKey;size:32"`
	Issued  int64  `gorm:"not null;default:0"`
	Applied int64  `gorm:"not null;default:0"`
	Epoch   string `gorm:"size:128;not null;default:''"`
}

func (CounterStream) TableName() string { return "counter_stream" }

type CounterEpoch struct {
	NodeID int64  `gorm:"primaryKey;autoIncrement:false"`
	Source string `gorm:"primaryKey;size:32"`
	Epoch  string `gorm:"primaryKey;size:128"`
}

func (CounterEpoch) TableName() string { return "counter_epoch" }
