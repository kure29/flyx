package metrics

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"go-backend/internal/monitoring"
	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
)

type SystemInfo struct {
	Uptime           uint64  `json:"uptime"`
	BytesReceived    uint64  `json:"bytes_received"`
	BytesTransmitted uint64  `json:"bytes_transmitted"`
	CPUUsage         float64 `json:"cpu_usage"`
	MemoryUsage      float64 `json:"memory_usage"`
	DiskUsage        float64 `json:"disk_usage"`
	Load1            float64 `json:"load1"`
	Load5            float64 `json:"load5"`
	Load15           float64 `json:"load15"`
	TCPConns         int64   `json:"tcp_conns"`
	UDPConns         int64   `json:"udp_conns"`
	NetInSpeed       int64   `json:"net_in_speed"`
	NetOutSpeed      int64   `json:"net_out_speed"`
}

// NodeTrafficRetention keeps a little over a year of node traffic.
const NodeTrafficRetention = 400 * 24 * time.Hour

type IngestionService struct {
	repo          *repo.Repository
	nodeBuffer    []*model.NodeMetric
	nodeBufferMu  sync.Mutex
	flushInterval time.Duration

	flushMu  sync.Mutex
	stream   string
	sequence int64
	pending  []*model.NodeMetric
}

func NewIngestionService(repo *repo.Repository) *IngestionService {
	return &IngestionService{
		repo:          repo,
		nodeBuffer:    make([]*model.NodeMetric, 0, 500),
		flushInterval: 30 * time.Second,
		stream:        uuid.NewString(),
		sequence:      1,
	}
}

func (s *IngestionService) Start(ctx context.Context) {
	flushTicker := time.NewTicker(s.flushInterval)
	defer flushTicker.Stop()

	pruneTicker := time.NewTicker(1 * time.Hour)
	defer pruneTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.flushNodeMetrics()
			return
		case <-flushTicker.C:
			s.flushNodeMetrics()
		case <-pruneTicker.C:
			s.pruneMetrics()
		}
	}
}

func (s *IngestionService) RecordNodeMetric(nodeID int64, info SystemInfo) {
	m := &model.NodeMetric{
		NodeID:      nodeID,
		Timestamp:   time.Now().UnixMilli(),
		CPUUsage:    info.CPUUsage,
		MemUsage:    info.MemoryUsage,
		DiskUsage:   info.DiskUsage,
		NetInBytes:  int64(info.BytesReceived),
		NetOutBytes: int64(info.BytesTransmitted),
		NetInSpeed:  info.NetInSpeed,
		NetOutSpeed: info.NetOutSpeed,
		Load1:       info.Load1,
		Load5:       info.Load5,
		Load15:      info.Load15,
		TCPConns:    info.TCPConns,
		UDPConns:    info.UDPConns,
		Uptime:      int64(info.Uptime),
	}

	s.nodeBufferMu.Lock()
	s.nodeBuffer = append(s.nodeBuffer, m)
	shouldFlush := len(s.nodeBuffer) == 200

	s.nodeBufferMu.Unlock()

	if shouldFlush {
		go s.flushNodeMetrics()
	}
}

func (s *IngestionService) flushNodeMetrics() {
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	if s.repo == nil {
		return
	}
	for {
		if len(s.pending) == 0 {
			s.nodeBufferMu.Lock()
			s.pending, s.nodeBuffer = s.nodeBuffer, make([]*model.NodeMetric, 0, 500)
			s.nodeBufferMu.Unlock()
		}
		if len(s.pending) == 0 {
			return
		}
		err := s.repo.SaveNodeMetricBatch(s.stream, s.sequence, s.pending)
		var gap *repo.TrafficSequenceGap
		if errors.As(err, &gap) {
			// This producer lives in the panel, not the Agent upload queue.
			// A lost/rolled-back receipt cannot be repaired by retrying the
			// old sequence. Keep pending intact and retry once with a fresh
			// stream; further write failures retain that new identity.
			log.Printf("node history cursor gap stream=%s sequence=%d expected=%d; starting new stream", s.stream, s.sequence, gap.Expected)
			s.stream, s.sequence = uuid.NewString(), 1
			err = s.repo.SaveNodeMetricBatch(s.stream, s.sequence, s.pending)
		}
		if err != nil {
			log.Printf("monitoring write failed op=node_metric.flush count=%d err=%v", len(s.pending), err)
			return
		}
		s.pending = nil
		s.sequence++
	}

}

func (s *IngestionService) pruneMetrics() {
	s.pruneMetricsAt(time.Now())
}

func (s *IngestionService) retentionDaysFromConfig() int {
	if s == nil || s.repo == nil {
		return monitoring.DefaultMonitorRetentionDays
	}
	cfg, err := s.repo.GetConfigsByNames([]string{monitoring.ConfigMonitorRetentionDays})
	if err != nil {
		return monitoring.DefaultMonitorRetentionDays
	}
	return monitoring.MonitoringRetentionDaysFromConfigMap(cfg)
}

func (s *IngestionService) pruneMetricsAt(now time.Time) {
	cutoff := now.Add(-time.Duration(s.retentionDaysFromConfig()) * 24 * time.Hour).UnixMilli()
	if s.repo == nil {
		return
	}
	if err := s.repo.PruneNodeMetrics(cutoff); err != nil {
		log.Printf("monitoring prune failed op=node_metric cutoff=%d err=%v", cutoff, err)
	}
	if err := s.repo.PruneTunnelMetrics(cutoff); err != nil {
		log.Printf("monitoring prune failed op=tunnel_metric cutoff=%d err=%v", cutoff, err)
	}
	if err := s.repo.PruneServiceMonitorResults(cutoff); err != nil {
		log.Printf("monitoring prune failed op=service_monitor_result cutoff=%d err=%v", cutoff, err)
	}
	if err := s.repo.PruneNodeLatencySamples(cutoff); err != nil {
		log.Printf("monitoring prune failed op=node_latency_sample cutoff=%d err=%v", cutoff, err)
	}
	if err := s.repo.PruneNodeTraffic(now.Add(-NodeTrafficRetention).UnixMilli()); err != nil {
		log.Printf("monitoring prune failed op=node_traffic err=%v", err)
	}
}

func (s *IngestionService) GetLatestMetric(nodeID int64) (*model.NodeMetric, error) {
	return s.repo.GetLatestNodeMetric(nodeID)
}

func (s *IngestionService) GetMetrics(nodeID int64, startMs, endMs int64) ([]model.NodeMetric, error) {
	return s.repo.GetNodeMetrics(nodeID, startMs, endMs)
}
