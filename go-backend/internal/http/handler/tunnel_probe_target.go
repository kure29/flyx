package handler

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"go-backend/internal/store/model"
)

const (
	defaultTunnelProbeTargetHost = "www.bing.com"
	defaultTunnelProbeTargetPort = 443
)

type tunnelProbeTarget struct {
	Host string
	Port int
}

func defaultTunnelProbeTarget() tunnelProbeTarget {
	return tunnelProbeTarget{Host: defaultTunnelProbeTargetHost, Port: defaultTunnelProbeTargetPort}
}

func normalizeTunnelProbeTarget(host string, port int) (tunnelProbeTarget, bool, error) {
	host = strings.TrimSpace(host)
	if host == "" && port == 0 {
		return defaultTunnelProbeTarget(), false, nil
	}
	if host == "" {
		return tunnelProbeTarget{}, false, errors.New("测试目标 Host 不能为空")
	}
	if port <= 0 || port > 65535 {
		return tunnelProbeTarget{}, false, errors.New("测试目标端口必须是 1-65535")
	}
	if strings.Contains(host, "://") || strings.ContainsAny(host, "/?#") || strings.ContainsAny(host, " \t\r\n") || isTunnelProbeTargetSchemeLikeHost(host) {
		return tunnelProbeTarget{}, false, errors.New("测试目标 Host 不能包含协议或路径")
	}
	if normalized, ok := normalizeTunnelProbeTargetHost(host); ok {
		host = normalized
	} else {
		return tunnelProbeTarget{}, false, errors.New("测试目标 Host 格式无效")
	}

	return tunnelProbeTarget{Host: host, Port: port}, true, nil
}

func normalizeTunnelProbeTargetHost(host string) (string, bool) {
	if strings.HasPrefix(host, "[") || strings.HasSuffix(host, "]") {
		if !strings.HasPrefix(host, "[") || !strings.HasSuffix(host, "]") {
			return "", false
		}
		inner := strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
		addr, err := netip.ParseAddr(inner)
		if err != nil || !addr.Is6() {
			return "", false
		}
		return inner, true
	}

	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.String(), true
	}
	if strings.Contains(host, ":") || isTunnelProbeTargetIPv4Like(host) {
		return "", false
	}
	if !isValidTunnelProbeTargetHost(host) {
		return "", false
	}
	return host, true
}

func isValidTunnelProbeTargetHost(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !isASCIILetter(r) && !isASCIIDigit(r) && r != '-' {
				return false
			}
		}
	}
	return true
}

func isTunnelProbeTargetIPv4Like(host string) bool {
	if host == "" {
		return false
	}
	for _, r := range host {
		if !isASCIIDigit(r) && r != '.' {
			return false
		}
	}
	return strings.Contains(host, ".")
}

func isTunnelProbeTargetSchemeLikeHost(host string) bool {
	if _, err := netip.ParseAddr(host); err == nil {
		return false
	}

	colon := strings.IndexByte(host, ':')
	if colon <= 0 {
		return false
	}
	for i, r := range host[:colon] {
		if i == 0 {
			if !isASCIILetter(r) {
				return false
			}
			continue
		}
		if !isASCIILetter(r) && !isASCIIDigit(r) && r != '+' && r != '-' && r != '.' {
			return false
		}
	}
	return true
}

func isASCIILetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isASCIIDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

func parseTunnelProbeTargetFromRequest(req map[string]interface{}) (tunnelProbeTarget, bool, error) {
	if req == nil {
		return defaultTunnelProbeTarget(), false, nil
	}
	rawHost, hasHost := req["probeTargetHost"]
	rawPort, hasPort := req["probeTargetPort"]
	if !hasHost && !hasPort {
		return defaultTunnelProbeTarget(), false, nil
	}
	host, err := parseTunnelProbeTargetHostValue(rawHost)
	if err != nil {
		return tunnelProbeTarget{}, false, err
	}
	port, err := parseTunnelProbeTargetPortValue(rawPort)
	if err != nil {
		return tunnelProbeTarget{}, false, err
	}
	return normalizeTunnelProbeTarget(host, port)
}

func parseTunnelProbeTargetHostValue(raw interface{}) (string, error) {
	if raw == nil {
		return "", nil
	}
	host, ok := raw.(string)
	if !ok {
		return "", errors.New("测试目标 Host 格式无效")
	}
	if host != strings.TrimSpace(host) {
		return "", errors.New("测试目标 Host 不能包含协议或路径")
	}
	return host, nil
}

func parseTunnelProbeTargetPortValue(raw interface{}) (int, error) {
	if raw == nil {
		return 0, nil
	}
	switch v := raw.(type) {
	case float64:
		if v != float64(int64(v)) {
			return 0, errors.New("测试目标端口必须是整数")
		}
		return int(v), nil
	case string:
		if v == "" {
			return 0, nil
		}
		if v != strings.TrimSpace(v) {
			return 0, errors.New("测试目标端口必须是整数")
		}
		port, err := strconv.Atoi(v)
		if err != nil {
			return 0, errors.New("测试目标端口必须是整数")
		}
		return port, nil
	case int:
		return v, nil
	case int32:
		return int(v), nil
	case int64:
		return int(v), nil
	default:
		return 0, errors.New("测试目标端口必须是整数")
	}
}

func effectiveTunnelProbeTarget(tunnel *model.Tunnel) tunnelProbeTarget {
	if tunnel == nil {
		return defaultTunnelProbeTarget()
	}
	return effectiveTunnelProbeTargetValues(tunnel.ProbeTargetHost, tunnel.ProbeTargetPort)
}

func effectiveTunnelProbeTargetValues(host string, port int) tunnelProbeTarget {
	target, configured, err := normalizeTunnelProbeTarget(host, port)
	if err != nil || !configured {
		return defaultTunnelProbeTarget()
	}
	return target
}

func formatTunnelProbeTarget(target tunnelProbeTarget) string {
	if addr, err := netip.ParseAddr(target.Host); err == nil && addr.Is6() {
		return fmt.Sprintf("[%s]:%d", target.Host, target.Port)
	}
	return fmt.Sprintf("%s:%d", target.Host, target.Port)
}
