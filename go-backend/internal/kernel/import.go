package kernel

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// ParseImportHostPort validates the address of an imported node: a public
// IP or a hostname, plus a port.
func ParseImportHostPort(hostport string) (string, int, error) {
	host, portStr, err := net.SplitHostPort(strings.TrimSpace(hostport))
	if err != nil {
		return "", 0, errors.New("地址格式应为 主机:端口")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, errors.New("端口不正确")
	}
	if err := ValidateImportHost(host); err != nil {
		return "", 0, err
	}
	return host, port, nil
}

// ValidateImportHost accepts a routable IP or a hostname.
func ValidateImportHost(host string) error {
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsUnspecified() || ip.IsLoopback() {
			return errors.New("地址不能是本机或 0.0.0.0")
		}
		return nil
	}
	if host == "" || len(host) > 253 || !hostPattern.MatchString(host) || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return fmt.Errorf("地址 %q 不正确", host)
	}
	return nil
}

func decodeBase64Loose(s string) ([]byte, error) {
	s = strings.TrimRight(strings.TrimSpace(s), "=")
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(s)
}
