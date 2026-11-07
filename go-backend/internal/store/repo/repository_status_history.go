package repo

import (
	"database/sql"
	"errors"
	"fmt"
)

// The public status page's history: node_metric and node_latency_sample
// averaged over fixed time buckets, so a chart gets a few hundred points
// whatever the range. The bucket size is formatted into the SQL (an int64,
// never user text) because PostgreSQL only accepts a GROUP BY expression
// that matches the selected one exactly, which two placeholders do not.

// NodeMetricBucket is one bucket of a node's metrics.
type NodeMetricBucket struct {
	T      int64   `gorm:"column:t"`
	CPU    float64 `gorm:"column:cpu"`
	Mem    float64 `gorm:"column:mem"`
	Swap   float64 `gorm:"column:swap"`
	Disk   float64 `gorm:"column:disk"`
	Load   float64 `gorm:"column:load1"`
	TCP    float64 `gorm:"column:tcp"`
	UDP    float64 `gorm:"column:udp"`
	Procs  float64 `gorm:"column:procs"`
	NetIn  float64 `gorm:"column:net_in"`
	NetOut float64 `gorm:"column:net_out"`
}

// NodeMetricBuckets averages a node's metrics in [startMs, endMs) per
// bucketMs, oldest first; T is the bucket's start.
func (r *Repository) NodeMetricBuckets(nodeID, startMs, endMs, bucketMs int64) ([]NodeMetricBucket, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	if bucketMs <= 0 {
		return nil, errors.New("invalid bucket")
	}
	avg := func(col string) string { return "CAST(AVG(" + col + ") AS DOUBLE PRECISION)" }
	query := fmt.Sprintf(`SELECT ("timestamp" / %[1]d) * %[1]d AS t,
		%[2]s AS cpu, %[3]s AS mem, %[4]s AS swap, %[5]s AS disk, %[6]s AS load1,
		%[7]s AS tcp, %[8]s AS udp, %[9]s AS procs, %[10]s AS net_in, %[11]s AS net_out
		FROM node_metric
		WHERE node_id = ? AND "timestamp" >= ? AND "timestamp" < ?
		GROUP BY ("timestamp" / %[1]d)
		ORDER BY t`,
		bucketMs, avg("cpu_usage"), avg("mem_usage"), avg("swap_usage"), avg("disk_usage"), avg("load1"),
		avg("tcp_conns"), avg("udp_conns"), avg("process_count"), avg("net_in_speed"), avg("net_out_speed"))
	var out []NodeMetricBucket
	err := r.db.Raw(query, nodeID, startMs, endMs).Scan(&out).Error
	return out, err
}

// LatencyBucket is one bucket of one reference point's latency.
type LatencyBucket struct {
	Point string `gorm:"column:point"`
	T     int64  `gorm:"column:t"`
	// Average over the successful rounds; NULL when every round failed.
	Ms   sql.NullFloat64 `gorm:"column:ms"`
	Loss float64         `gorm:"column:loss"`
}

// NodeLatencyBuckets averages a node's latency samples in [startMs, endMs)
// per point and bucketMs, ordered by point then time.
func (r *Repository) NodeLatencyBuckets(nodeID, startMs, endMs, bucketMs int64) ([]LatencyBucket, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	if bucketMs <= 0 {
		return nil, errors.New("invalid bucket")
	}
	query := fmt.Sprintf(`SELECT point, (created_time / %[1]d) * %[1]d AS t,
		CAST(AVG(CASE WHEN ok = 1 THEN latency_ms END) AS DOUBLE PRECISION) AS ms,
		CAST(AVG(loss_pct) AS DOUBLE PRECISION) AS loss
		FROM node_latency_sample
		WHERE node_id = ? AND created_time >= ? AND created_time < ?
		GROUP BY point, (created_time / %[1]d)
		ORDER BY point, t`, bucketMs)
	var out []LatencyBucket
	err := r.db.Raw(query, nodeID, startMs, endMs).Scan(&out).Error
	return out, err
}
