package handler

import "testing"

func TestNormalizeTunnelProbeTargetDefaultsWhenEmpty(t *testing.T) {
	target, configured, err := normalizeTunnelProbeTarget("", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if configured {
		t.Fatalf("expected empty input to be default, not configured")
	}
	if target.Host != defaultTunnelProbeTargetHost || target.Port != defaultTunnelProbeTargetPort {
		t.Fatalf("unexpected default target: %+v", target)
	}
}

func TestNormalizeTunnelProbeTargetAcceptsHostPortAndIPv6(t *testing.T) {
	target, configured, err := normalizeTunnelProbeTarget(" [2001:db8::1] ", 8443)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !configured {
		t.Fatalf("expected explicit target")
	}
	if target.Host != "2001:db8::1" || target.Port != 8443 {
		t.Fatalf("unexpected normalized target: %+v", target)
	}
	if got := formatTunnelProbeTarget(target); got != "[2001:db8::1]:8443" {
		t.Fatalf("unexpected formatted target: %s", got)
	}
}

func TestNormalizeTunnelProbeTargetRejectsPartialAndInvalidInputs(t *testing.T) {
	tests := []struct {
		name string
		host string
		port int
	}{
		{name: "missing host", host: "", port: 443},
		{name: "missing port", host: "example.com", port: 0},
		{name: "port too high", host: "example.com", port: 70000},
		{name: "scheme", host: "https://example.com", port: 443},
		{name: "path", host: "example.com/ping", port: 443},
		{name: "space", host: "example .com", port: 443},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := normalizeTunnelProbeTarget(tt.host, tt.port); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func TestNormalizeTunnelProbeTargetRejectsSchemePrefixButAllowsIPv6(t *testing.T) {
	for _, host := range []string{"https:example.com", "mailto:ops@example.com"} {
		if _, _, err := normalizeTunnelProbeTarget(host, 443); err == nil {
			t.Fatalf("expected scheme-like host %q to be rejected", host)
		}
	}

	for _, host := range []string{"2001:db8::1", "[2001:db8::1]"} {
		target, configured, err := normalizeTunnelProbeTarget(host, 443)
		if err != nil {
			t.Fatalf("expected IPv6 host %q to be accepted: %v", host, err)
		}
		if !configured || target.Host != "2001:db8::1" {
			t.Fatalf("unexpected IPv6 normalization for %q: %+v configured=%v", host, target, configured)
		}
	}
}

func TestNormalizeTunnelProbeTargetValidatesHostShape(t *testing.T) {
	validHosts := []string{
		"example.com",
		"localhost",
		"api-1.example.co.uk",
		"192.0.2.10",
		"2001:db8::1",
		"[2001:db8::1]",
	}
	for _, host := range validHosts {
		if _, _, err := normalizeTunnelProbeTarget(host, 443); err != nil {
			t.Fatalf("expected valid host %q: %v", host, err)
		}
	}

	invalidHosts := []string{
		"1:2:3",
		"[2001:db8::1",
		"2001:db8::1]",
		"[example.com]",
		"example..com",
		"-example.com",
		"example-.com",
		"exa_mple.com",
		"999.1.1.1",
	}
	for _, host := range invalidHosts {
		if _, _, err := normalizeTunnelProbeTarget(host, 443); err == nil {
			t.Fatalf("expected invalid host %q to be rejected", host)
		}
	}
}

func TestParseTunnelProbeTargetFromRequest(t *testing.T) {
	req := map[string]interface{}{
		"probeTargetHost": "speed.example.com",
		"probeTargetPort": float64(1443),
	}
	target, configured, err := parseTunnelProbeTargetFromRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !configured || target.Host != "speed.example.com" || target.Port != 1443 {
		t.Fatalf("unexpected request target: %+v configured=%v", target, configured)
	}
}
