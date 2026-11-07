package monitoring

import (
	"strconv"
	"strings"
)

type ServiceMonitorLimits struct {
	CheckerScanIntervalSec int `json:"checkerScanIntervalSec"`
	WorkerLimit            int `json:"workerLimit"`

	MinIntervalSec     int `json:"minIntervalSec"`
	DefaultIntervalSec int `json:"defaultIntervalSec"`

	MinTimeoutSec     int `json:"minTimeoutSec"`
	DefaultTimeoutSec int `json:"defaultTimeoutSec"`
	MaxTimeoutSec     int `json:"maxTimeoutSec"`
}

const (
	ConfigServiceMonitorCheckerScanIntervalSec = "service_monitor_checker_scan_interval_sec"
	ConfigServiceMonitorWorkerLimit            = "service_monitor_worker_limit"
	ConfigServiceMonitorMinIntervalSec         = "service_monitor_min_interval_sec"
	ConfigServiceMonitorDefaultIntervalSec     = "service_monitor_default_interval_sec"
	ConfigServiceMonitorMinTimeoutSec          = "service_monitor_min_timeout_sec"
	ConfigServiceMonitorDefaultTimeoutSec      = "service_monitor_default_timeout_sec"
	ConfigServiceMonitorMaxTimeoutSec          = "service_monitor_max_timeout_sec"
)

func DefaultServiceMonitorLimits() ServiceMonitorLimits {
	return ServiceMonitorLimits{
		CheckerScanIntervalSec: 1,
		WorkerLimit:            20,
		MinIntervalSec:         1,
		DefaultIntervalSec:     1,
		MinTimeoutSec:          1,
		DefaultTimeoutSec:      5,
		MaxTimeoutSec:          60,
	}
}

// ServiceMonitorLimitsFromConfigMap parses limits from vite_config values.
// Missing/invalid values fall back to defaults.
func ServiceMonitorLimitsFromConfigMap(cfg map[string]string) ServiceMonitorLimits {
	limits := DefaultServiceMonitorLimits()
	if cfg == nil {
		return limits
	}

	limits.CheckerScanIntervalSec = parseConfigInt(cfg, ConfigServiceMonitorCheckerScanIntervalSec, limits.CheckerScanIntervalSec)
	limits.WorkerLimit = parseConfigInt(cfg, ConfigServiceMonitorWorkerLimit, limits.WorkerLimit)
	limits.MinIntervalSec = parseConfigInt(cfg, ConfigServiceMonitorMinIntervalSec, limits.MinIntervalSec)
	limits.DefaultIntervalSec = parseConfigInt(cfg, ConfigServiceMonitorDefaultIntervalSec, limits.DefaultIntervalSec)
	limits.MinTimeoutSec = parseConfigInt(cfg, ConfigServiceMonitorMinTimeoutSec, limits.MinTimeoutSec)
	limits.DefaultTimeoutSec = parseConfigInt(cfg, ConfigServiceMonitorDefaultTimeoutSec, limits.DefaultTimeoutSec)
	limits.MaxTimeoutSec = parseConfigInt(cfg, ConfigServiceMonitorMaxTimeoutSec, limits.MaxTimeoutSec)

	return normalizeServiceMonitorLimits(limits)
}

func normalizeServiceMonitorLimits(limits ServiceMonitorLimits) ServiceMonitorLimits {
	if limits.CheckerScanIntervalSec <= 0 {
		limits.CheckerScanIntervalSec = 30
	}
	if limits.WorkerLimit <= 0 {
		limits.WorkerLimit = 5
	}
	if limits.WorkerLimit > 50 {
		limits.WorkerLimit = 50
	}

	if limits.MinIntervalSec <= 0 {
		limits.MinIntervalSec = limits.CheckerScanIntervalSec
	}
	if limits.MinIntervalSec < limits.CheckerScanIntervalSec {
		limits.MinIntervalSec = limits.CheckerScanIntervalSec
	}
	if limits.DefaultIntervalSec <= 0 {
		limits.DefaultIntervalSec = 60
	}
	if limits.DefaultIntervalSec < limits.MinIntervalSec {
		limits.DefaultIntervalSec = limits.MinIntervalSec
	}

	if limits.MinTimeoutSec <= 0 {
		limits.MinTimeoutSec = 1
	}
	if limits.DefaultTimeoutSec <= 0 {
		limits.DefaultTimeoutSec = 5
	}
	if limits.DefaultTimeoutSec < limits.MinTimeoutSec {
		limits.DefaultTimeoutSec = limits.MinTimeoutSec
	}
	if limits.MaxTimeoutSec <= 0 {
		limits.MaxTimeoutSec = 60
	}
	if limits.MaxTimeoutSec < limits.DefaultTimeoutSec {
		limits.MaxTimeoutSec = limits.DefaultTimeoutSec
	}

	return limits
}

func parseConfigInt(cfg map[string]string, key string, fallback int) int {
	v := strings.TrimSpace(cfg[key])
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
