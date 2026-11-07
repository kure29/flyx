package monitoring

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	ConfigMonitorRetentionDays  = "monitor_retention_days"
	DefaultMonitorRetentionDays = 7
	MinMonitorRetentionDays     = 1
	MaxMonitorRetentionDays     = 3650
)

func MonitoringRetentionDaysFromConfigMap(cfg map[string]string) int {
	if cfg == nil {
		return DefaultMonitorRetentionDays
	}
	days, err := parseMonitoringRetentionDays(cfg[ConfigMonitorRetentionDays])
	if err != nil {
		return DefaultMonitorRetentionDays
	}
	return days
}

func NormalizeMonitoringRetentionDays(value string) (string, error) {
	days, err := parseMonitoringRetentionDays(value)
	if err != nil {
		return "", err
	}
	return strconv.Itoa(days), nil
}

func parseMonitoringRetentionDays(value string) (int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, fmt.Errorf("监控数据保留天数不能为空")
	}
	days, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, fmt.Errorf("监控数据保留天数必须是整数")
	}
	if days < MinMonitorRetentionDays || days > MaxMonitorRetentionDays {
		return 0, fmt.Errorf("监控数据保留天数必须在 %d 到 %d 之间", MinMonitorRetentionDays, MaxMonitorRetentionDays)
	}
	return days, nil
}
