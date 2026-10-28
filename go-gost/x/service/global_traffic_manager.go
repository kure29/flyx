package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-gost/x/registry"
)

// GlobalTrafficManager 全局流量管理器（所有服务共享）
type GlobalTrafficManager struct {
	mu             sync.RWMutex
	serviceTraffic map[string]*ServiceTraffic // key: 服务名, value: 流量数据
	ctx            context.Context
	cancel         context.CancelFunc
	reportTicker   *time.Ticker
}

// ServiceTraffic 单个服务的流量累积
type ServiceTraffic struct {
	mu          sync.Mutex
	ServiceName string
	UpBytes     int64 // 上行流量（累积）
	DownBytes   int64 // 下行流量（累积）
}

var (
	globalManager     *GlobalTrafficManager
	globalManagerOnce sync.Once
)

// GetGlobalTrafficManager 获取全局流量管理器单例
func GetGlobalTrafficManager() *GlobalTrafficManager {
	globalManagerOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		globalManager = &GlobalTrafficManager{
			serviceTraffic: make(map[string]*ServiceTraffic),
			ctx:            ctx,
			cancel:         cancel,
			reportTicker:   time.NewTicker(5 * time.Second),
		}
		// 启动定时上报协程
		go globalManager.startReporting()
	})
	return globalManager
}

// AddTraffic 添加流量到指定服务（由各服务调用）
func (m *GlobalTrafficManager) AddTraffic(serviceName string, upBytes, downBytes int64) {
	if !registry.ServiceRegistry().IsRegistered(serviceName) {
		return
	}
	m.addServiceTraffic(serviceName, upBytes, downBytes)
}

// addServiceTraffic accepts samples from a service's own lifetime, including
// connections draining after its listener has been unregistered.
func (m *GlobalTrafficManager) addServiceTraffic(serviceName string, upBytes, downBytes int64) {
	if upBytes == 0 && downBytes == 0 {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// 获取或创建服务流量记录
	traffic, exists := m.serviceTraffic[serviceName]
	if !exists {
		traffic = &ServiceTraffic{
			ServiceName: serviceName,
		}
		m.serviceTraffic[serviceName] = traffic
	}

	// 累加流量
	traffic.mu.Lock()
	traffic.UpBytes += upBytes
	traffic.DownBytes += downBytes
	traffic.mu.Unlock()
}

// RemoveServices drops cached traffic counters for services that no longer exist.
func (m *GlobalTrafficManager) RemoveServices(serviceNames ...string) {
	if m == nil || len(serviceNames) == 0 {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, name := range serviceNames {
		if m.isTrafficEmptyLocked(name) {
			delete(m.serviceTraffic, name)
		}
	}
}

// RetainServices removes traffic counters for every service not in activeNames.
func (m *GlobalTrafficManager) RetainServices(activeNames map[string]struct{}) {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for name := range m.serviceTraffic {
		if _, ok := activeNames[name]; !ok {
			if m.isTrafficEmptyLocked(name) {
				delete(m.serviceTraffic, name)
			}
		}
	}
}

func (m *GlobalTrafficManager) isTrafficEmptyLocked(name string) bool {
	traffic, ok := m.serviceTraffic[name]
	if !ok {
		return true
	}

	traffic.mu.Lock()
	defer traffic.mu.Unlock()
	return traffic.UpBytes == 0 && traffic.DownBytes == 0
}

// startReporting 启动定时上报协程（每5秒执行一次）
func (m *GlobalTrafficManager) startReporting() {

	for {
		select {
		case <-m.reportTicker.C:
			m.collectAndReport()

		case <-m.ctx.Done():
			fmt.Printf("⏹️ 全局流量上报器已停止\n")
			return
		}
	}
}

// collectAndReport 收集所有服务流量并合并上报
func (m *GlobalTrafficManager) collectAndReport() {
	m.mu.Lock()

	if len(m.serviceTraffic) == 0 {
		m.mu.Unlock()
		return
	}

	reportItems := make([]TrafficReportItem, 0, len(m.serviceTraffic))

	for name, traffic := range m.serviceTraffic {
		traffic.mu.Lock()
		up := traffic.UpBytes
		down := traffic.DownBytes
		if up > 0 || down > 0 {
			traffic.UpBytes = 0
			traffic.DownBytes = 0
		}
		traffic.mu.Unlock()
		isStale := !registry.ServiceRegistry().IsRegistered(name)

		if up > 0 || down > 0 {
			reportItems = append(reportItems, TrafficReportItem{
				N: name,
				U: up,
				D: down,
			})
		}
		if isStale {
			delete(m.serviceTraffic, name)
		}
	}

	m.mu.Unlock()

	if len(reportItems) == 0 {
		return
	}

	var totalUp, totalDown int64
	for _, item := range reportItems {
		totalUp += item.U
		totalDown += item.D
	}

	success, err := sendBatchTrafficReport(m.ctx, reportItems)
	if err != nil {
		fmt.Printf("❌ 全局流量上报失败: %v (总流量: ↑%d ↓%d, %d个服务)\n", err, totalUp, totalDown, len(reportItems))
		return
	}

	if !success {
		fmt.Printf("⚠️ 全局流量上报未成功 (总流量: ↑%d ↓%d, %d个服务)\n", totalUp, totalDown, len(reportItems))
	}
}

// Stop 停止全局流量管理器
func (m *GlobalTrafficManager) Stop() {
	if m.reportTicker != nil {
		m.reportTicker.Stop()
	}
	if m.cancel != nil {
		m.cancel()
	}
	fmt.Printf("🛑 全局流量管理器已停止\n")
}

// GetServiceTraffic 获取指定服务的当前流量（用于调试）
func (m *GlobalTrafficManager) GetServiceTraffic(serviceName string) (upBytes, downBytes int64) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if traffic, exists := m.serviceTraffic[serviceName]; exists {
		traffic.mu.Lock()
		upBytes = traffic.UpBytes
		downBytes = traffic.DownBytes
		traffic.mu.Unlock()
	}
	return
}
