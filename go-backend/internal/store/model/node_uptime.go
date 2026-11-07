package model

// NodeUptime counts, per node and 10-minute bucket, how many of the
// once-a-minute samples found the node online. The public status page
// derives availability from it.
type NodeUptime struct {
	NodeID  int64 `gorm:"column:node_id;primaryKey;autoIncrement:false"`
	Bucket  int64 `gorm:"column:bucket;primaryKey;autoIncrement:false;index:idx_node_uptime_bucket"`
	Samples int   `gorm:"column:samples;not null;default:0"`
	Up      int   `gorm:"column:up;not null;default:0"`
}

func (NodeUptime) TableName() string { return "node_uptime" }
