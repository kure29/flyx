package nftables

import "testing"

func TestParseSingleTargetAcceptsHostPortAndIPv6(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		host string
		port int
	}{
		{name: "hostname", raw: "example.com:443", host: "example.com", port: 443},
		{name: "ipv4", raw: "198.51.100.20:8443", host: "198.51.100.20", port: 8443},
		{name: "ipv6", raw: "[2001:db8::1]:443", host: "2001:db8::1", port: 443},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, err := ParseSingleTarget(tt.raw)
			if err != nil {
				t.Fatalf("ParseSingleTarget: %v", err)
			}
			if target.Host != tt.host || target.Port != tt.port {
				t.Fatalf("expected %s/%d, got %+v", tt.host, tt.port, target)
			}
		})
	}
}

func TestParseSingleTargetRejectsUnsupportedValues(t *testing.T) {
	for _, raw := range []string{
		"",
		"example.com",
		"example.com:0",
		"example.com:65536",
		"a:1,b:2",
		"http://example.com:443",
		"https:443",
		"mailto:443",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := ParseSingleTarget(raw); err == nil {
				t.Fatalf("expected error for %q", raw)
			}
		})
	}
}
