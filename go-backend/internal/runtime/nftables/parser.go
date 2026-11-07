package nftables

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

func ParseSingleTarget(raw string) (Target, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return Target{}, fmt.Errorf("目标地址不能为空")
	}
	if strings.Contains(value, ",") || strings.Contains(value, "\n") {
		return Target{}, fmt.Errorf("nftables 纯转发第一阶段仅支持单目标")
	}
	if hasScheme(value) {
		return Target{}, fmt.Errorf("目标地址必须是 host:port，不能包含 URL scheme")
	}
	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		return Target{}, fmt.Errorf("目标地址必须是 host:port")
	}
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" {
		return Target{}, fmt.Errorf("目标主机不能为空")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return Target{}, fmt.Errorf("目标端口必须在 1-65535 之间")
	}
	return Target{Host: host, Port: port}, nil
}

func hasScheme(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" {
		return false
	}
	if strings.Contains(value, "://") {
		return true
	}
	colon := strings.IndexByte(value, ':')
	if colon <= 0 || strings.Contains(parsed.Scheme, ".") {
		return false
	}
	suffix := value[colon+1:]
	return strings.IndexByte(suffix, ':') == -1
}
