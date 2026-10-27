package repo

import (
	"errors"
	"fmt"
	"sort"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go-backend/internal/store/model"
)

func (r *Repository) InsertNodeMetric(m *model.NodeMetric) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Create(m).Error
}

func (r *Repository) InsertNodeMetricBatch(metrics []*model.NodeMetric) error {
	if r == nil || r.db == nil || len(metrics) == 0 {
		return nil
	}
	return r.db.CreateInBatches(metrics, 100).Error
}

func (r *Repository) GetNodeMetrics(nodeID int64, startMs, endMs int64) ([]model.NodeMetric, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}

	rangeMs := endMs - startMs
	const maxRawRangeMs = int64(60 * 60 * 1000) // 1 hour — return raw data for short ranges
	const targetPoints = 500                    // target number of chart points for downsampled data

	// For short ranges, return raw data (full resolution).
	if rangeMs <= maxRawRangeMs {
		var metrics []model.NodeMetric
		err := r.db.Where("node_id = ? AND timestamp >= ? AND timestamp <= ?", nodeID, startMs, endMs).
			Order("timestamp ASC").
			Limit(5000).
			Find(&metrics).Error
		return metrics, err
	}

	// For longer ranges, downsample via SQL aggregation to keep the response small and fast.
	bucketMs := rangeMs / targetPoints
	if bucketMs < 1000 {
		bucketMs = 1000 // minimum 1-second buckets
	}

	bucketExpr := fmt.Sprintf("(timestamp / %d * %d)", bucketMs, bucketMs)
	groupExpr := fmt.Sprintf("timestamp / %d", bucketMs)

	var metrics []model.NodeMetric
	err := r.db.Model(&model.NodeMetric{}).
		Select(
			fmt.Sprintf(
				"%d AS node_id, "+
					"CAST(%s AS BIGINT) AS timestamp, "+
					"AVG(cpu_usage) AS cpu_usage, "+
					"AVG(mem_usage) AS mem_usage, "+
					"AVG(disk_usage) AS disk_usage, "+
					"CAST(AVG(net_in_bytes) AS BIGINT) AS net_in_bytes, "+
					"CAST(AVG(net_out_bytes) AS BIGINT) AS net_out_bytes, "+
					"CAST(AVG(net_in_speed) AS BIGINT) AS net_in_speed, "+
					"CAST(AVG(net_out_speed) AS BIGINT) AS net_out_speed, "+
					"AVG(load1) AS load1, "+
					"AVG(load5) AS load5, "+
					"AVG(load15) AS load15, "+
					"CAST(AVG(tcp_conns) AS BIGINT) AS tcp_conns, "+
					"CAST(AVG(udp_conns) AS BIGINT) AS udp_conns, "+
					"CAST(MAX(uptime) AS BIGINT) AS uptime",
				nodeID, bucketExpr,
			),
		).
		Where("node_id = ? AND timestamp >= ? AND timestamp <= ?", nodeID, startMs, endMs).
		Group(groupExpr).
		Order("timestamp ASC").
		Limit(targetPoints + 100). // safety margin
		Scan(&metrics).Error

	if metrics == nil {
		metrics = make([]model.NodeMetric, 0)
	}
	return metrics, err
}

func (r *Repository) GetLatestNodeMetric(nodeID int64) (*model.NodeMetric, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	var m model.NodeMetric
	err := r.db.Where("node_id = ?", nodeID).Order("timestamp DESC").First(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &m, nil
}

func (r *Repository) PruneNodeMetrics(olderThanMs int64) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Where("timestamp < ?", olderThanMs).Delete(&model.NodeMetric{}).Error
}

func (r *Repository) InsertTunnelMetric(m *model.TunnelMetric) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Create(m).Error
}

func (r *Repository) InsertTunnelMetricBatch(metrics []*model.TunnelMetric) error {
	if r == nil || r.db == nil || len(metrics) == 0 {
		return nil
	}
	return r.db.CreateInBatches(metrics, 100).Error
}

// UpsertTunnelMetricBuckets adds the provided metric deltas into per-minute buckets.
// Requires a unique index on (tunnel_id, node_id, timestamp) for safe upserts.
func (r *Repository) UpsertTunnelMetricBuckets(metrics []*model.TunnelMetric) error {
	if r == nil || r.db == nil || len(metrics) == 0 {
		return nil
	}

	// Postgres rejects a single INSERT ... ON CONFLICT when the input contains
	// duplicate conflict keys. Pre-aggregate within this batch to keep inserts safe.
	type bucketKey struct {
		tunnelID  int64
		nodeID    int64
		timestamp int64
	}

	agg := make(map[bucketKey]*model.TunnelMetric, len(metrics))
	for _, m := range metrics {
		if m == nil {
			continue
		}
		if m.TunnelID <= 0 || m.NodeID <= 0 || m.Timestamp <= 0 {
			continue
		}
		if m.BytesIn == 0 && m.BytesOut == 0 && m.Connections == 0 && m.Errors == 0 {
			continue
		}

		k := bucketKey{tunnelID: m.TunnelID, nodeID: m.NodeID, timestamp: m.Timestamp}
		if existing, ok := agg[k]; ok {
			existing.BytesIn += m.BytesIn
			existing.BytesOut += m.BytesOut
			existing.Connections += m.Connections
			existing.Errors += m.Errors
			if existing.AvgLatencyMs == 0 && m.AvgLatencyMs != 0 {
				existing.AvgLatencyMs = m.AvgLatencyMs
			}
			continue
		}
		cp := *m
		agg[k] = &cp
	}
	if len(agg) == 0 {
		return nil
	}

	rows := make([]*model.TunnelMetric, 0, len(agg))
	for _, v := range agg {
		rows = append(rows, v)
	}

	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "tunnel_id"}, {Name: "node_id"}, {Name: "timestamp"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"bytes_in":    gorm.Expr("tunnel_metric.bytes_in + excluded.bytes_in"),
			"bytes_out":   gorm.Expr("tunnel_metric.bytes_out + excluded.bytes_out"),
			"connections": gorm.Expr("tunnel_metric.connections + excluded.connections"),
			"errors":      gorm.Expr("tunnel_metric.errors + excluded.errors"),
			// avg_latency_ms is not additive; keep the existing bucket value.
		}),
	}).CreateInBatches(rows, 100).Error
}

func (r *Repository) GetTunnelMetrics(tunnelID int64, startMs, endMs int64) ([]model.TunnelMetric, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	var metrics []model.TunnelMetric
	err := r.db.Where("tunnel_id = ? AND timestamp >= ? AND timestamp <= ?", tunnelID, startMs, endMs).
		Order("timestamp DESC").
		Limit(5000).
		Find(&metrics).Error
	if len(metrics) > 1 {
		for i, j := 0, len(metrics)-1; i < j; i, j = i+1, j-1 {
			metrics[i], metrics[j] = metrics[j], metrics[i]
		}
	}
	return metrics, err
}

// GetTunnelMetricsAggregated returns tunnel-level aggregated series (one point per timestamp).
// Storage remains per (tunnel_id, node_id, timestamp) for future drill-down.
func (r *Repository) GetTunnelMetricsAggregated(tunnelID int64, startMs, endMs int64) ([]model.TunnelMetric, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}

	var metrics []model.TunnelMetric
	err := r.db.Model(&model.TunnelMetric{}).
		Select(
			"tunnel_id, 0 AS node_id, timestamp, "+
				"SUM(bytes_in) AS bytes_in, "+
				"SUM(bytes_out) AS bytes_out, "+
				"SUM(connections) AS connections, "+
				"SUM(errors) AS errors, "+
				"AVG(avg_latency_ms) AS avg_latency_ms",
		).
		Where("tunnel_id = ? AND timestamp >= ? AND timestamp <= ?", tunnelID, startMs, endMs).
		Group("tunnel_id, timestamp").
		Order("timestamp ASC").
		Limit(5000).
		Scan(&metrics).Error
	if metrics == nil {
		metrics = make([]model.TunnelMetric, 0)
	}
	return metrics, err
}

func (r *Repository) PruneTunnelMetrics(olderThanMs int64) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Where("timestamp < ?", olderThanMs).Delete(&model.TunnelMetric{}).Error
}

func (r *Repository) ListServiceMonitors() ([]model.ServiceMonitor, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	var monitors []model.ServiceMonitor
	err := r.db.Order("id ASC").Find(&monitors).Error
	return monitors, err
}

func (r *Repository) ListEnabledServiceMonitors() ([]model.ServiceMonitor, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	var monitors []model.ServiceMonitor
	err := r.db.Where("enabled = 1 AND type IN (?)", []string{"tcp", "icmp"}).Order("id ASC").Find(&monitors).Error
	return monitors, err
}

func (r *Repository) GetServiceMonitor(id int64) (*model.ServiceMonitor, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	var m model.ServiceMonitor
	err := r.db.First(&m, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &m, nil
}

func (r *Repository) CreateServiceMonitor(m *model.ServiceMonitor) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Create(m).Error
}

func (r *Repository) UpdateServiceMonitor(m *model.ServiceMonitor) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Save(m).Error
}

func (r *Repository) DeleteServiceMonitor(id int64) error {
	if r == nil || r.db == nil {
		return nil
	}
	if id <= 0 {
		return nil
	}

	// Keep API/UI semantics simple: deleting a monitor also deletes its history.
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("monitor_id = ?", id).Delete(&model.ServiceMonitorResult{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.ServiceMonitor{}, id).Error
	})
}

func (r *Repository) InsertServiceMonitorResult(result *model.ServiceMonitorResult) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Create(result).Error
}

func (r *Repository) GetServiceMonitorResults(monitorID int64, limit int) ([]model.ServiceMonitorResult, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}
	var results []model.ServiceMonitorResult
	err := r.db.Where("monitor_id = ?", monitorID).
		Order("timestamp DESC").
		Limit(limit).
		Find(&results).Error
	return results, err
}

// GetServiceMonitorResultsByTimeRange returns results for a monitor within [startMs, endMs].
// Mirrors GetNodeMetrics / GetTunnelMetrics pattern for time-range based charting.
func (r *Repository) GetServiceMonitorResultsByTimeRange(monitorID int64, startMs, endMs int64) ([]model.ServiceMonitorResult, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	var results []model.ServiceMonitorResult
	err := r.db.Where("monitor_id = ? AND timestamp >= ? AND timestamp <= ?", monitorID, startMs, endMs).
		Order("timestamp DESC").
		Limit(5000).
		Find(&results).Error
	if len(results) > 1 {
		for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
			results[i], results[j] = results[j], results[i]
		}
	}
	return results, err
}

// GetLatestServiceMonitorResults returns the newest result per monitor_id.
// This is intended for list rendering (avoid N+1 queries).
func (r *Repository) GetLatestServiceMonitorResults() ([]model.ServiceMonitorResult, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}

	var results []model.ServiceMonitorResult

	// Prefer a window-function query (works on modern SQLite + Postgres).
	q1 := `
		SELECT id, monitor_id, node_id, timestamp, success, latency_ms, status_code, error_message
		FROM (
			SELECT *, ROW_NUMBER() OVER (PARTITION BY monitor_id ORDER BY timestamp DESC, id DESC) AS rn
			FROM service_monitor_result
		) t
		WHERE rn = 1
		ORDER BY monitor_id ASC
	`
	if err := r.db.Raw(q1).Scan(&results).Error; err == nil {
		return results, nil
	}

	// Fallback: just return newest rows (best-effort). This avoids hard failure on older SQLite builds.
	// Note: This may not include all monitors if the table is extremely large and skewed.
	results = nil
	q2 := `
		SELECT id, monitor_id, node_id, timestamp, success, latency_ms, status_code, error_message
		FROM service_monitor_result
		ORDER BY timestamp DESC, id DESC
		LIMIT 5000
	`
	err := r.db.Raw(q2).Scan(&results).Error
	if err != nil {
		return nil, err
	}

	seen := make(map[int64]struct{}, len(results))
	out := make([]model.ServiceMonitorResult, 0, len(results))
	for _, row := range results {
		if row.MonitorID <= 0 {
			continue
		}
		if _, ok := seen[row.MonitorID]; ok {
			continue
		}
		seen[row.MonitorID] = struct{}{}
		out = append(out, row)
	}
	// Keep response stable for the frontend.
	sort.Slice(out, func(i, j int) bool { return out[i].MonitorID < out[j].MonitorID })
	return out, nil
}

func (r *Repository) PruneServiceMonitorResults(olderThanMs int64) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Where("timestamp < ?", olderThanMs).Delete(&model.ServiceMonitorResult{}).Error
}

// AddNodeTraffic adds hourly traffic to the nodes' rows.
func (r *Repository) AddNodeTraffic(rows []model.NodeTraffic) error {
	if r == nil || r.db == nil || len(rows) == 0 {
		return nil
	}
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "node_id"}, {Name: "hour"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"bucket_minutes": gorm.Expr("CASE WHEN node_traffic.bucket_minutes > excluded.bucket_minutes THEN node_traffic.bucket_minutes ELSE excluded.bucket_minutes END"),
			"in_bytes":       gorm.Expr("node_traffic.in_bytes + excluded.in_bytes"),
			"out_bytes":      gorm.Expr("node_traffic.out_bytes + excluded.out_bytes"),
		}),
	}).Create(&rows).Error
}

// NodeTrafficSince sums each node's traffic from fromMs on.
func (r *Repository) NodeTrafficSince(fromMs int64) (map[int64][2]int64, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var rows []struct {
		NodeID   int64
		InBytes  int64
		OutBytes int64
	}
	err := r.db.Model(&model.NodeTraffic{}).Select("node_id, SUM(in_bytes) AS in_bytes, SUM(out_bytes) AS out_bytes").
		Where("hour >= ?", fromMs).Group("node_id").Scan(&rows).Error
	out := make(map[int64][2]int64, len(rows))
	for _, row := range rows {
		out[row.NodeID] = [2]int64{row.InBytes, row.OutBytes}
	}
	return out, err
}

// PruneNodeTraffic drops the hours before beforeMs.
func (r *Repository) PruneNodeTraffic(beforeMs int64) error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Where("hour < ?", beforeMs).Delete(&model.NodeTraffic{}).Error
}
