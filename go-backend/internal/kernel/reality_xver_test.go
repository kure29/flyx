package kernel

import (
	"encoding/json"
	"testing"
)

func TestRealityXverNormalizeAndRender(t *testing.T) {
	for _, bad := range []int{-1, 3, 99} {
		in := Inbound{
			ID:       1,
			Protocol: ProtocolVLESS,
			Port:     443,
			Security: Security{Type: SecurityReality, SNI: "www.microsoft.com", RealityXver: bad},
		}
		if err := Normalize(TypeXray, &in); err == nil {
			t.Fatalf("expected error for RealityXver=%d", bad)
		}
	}

	for _, good := range []int{0, 1, 2} {
		in := Inbound{
			ID:       1,
			Protocol: ProtocolVLESS,
			Port:     443,
			Security: Security{Type: SecurityReality, SNI: "www.microsoft.com", RealityXver: good},
		}
		if err := Normalize(TypeXray, &in); err != nil {
			t.Fatalf("Normalize(xver=%d): %v", good, err)
		}
		if in.Security.RealityXver != good {
			t.Fatalf("got RealityXver=%d, want %d", in.Security.RealityXver, good)
		}
		rendered, err := Render(TypeXray, []Inbound{in})
		if err != nil {
			t.Fatalf("Render(xver=%d): %v", good, err)
		}
		var cfg struct {
			Inbounds []struct {
				StreamSettings struct {
					RealitySettings struct {
						Xver int `json:"xver"`
					} `json:"realitySettings"`
				} `json:"streamSettings"`
			} `json:"inbounds"`
		}
		if err := json.Unmarshal([]byte(rendered.Files[0].Content), &cfg); err != nil {
			t.Fatalf("unmarshal xray config: %v", err)
		}
		if len(cfg.Inbounds) != 1 || cfg.Inbounds[0].StreamSettings.RealitySettings.Xver != good {
			t.Fatalf("rendered xver=%d, want %d", cfg.Inbounds[0].StreamSettings.RealitySettings.Xver, good)
		}
	}

	// TLS clears any stray RealityXver
	tlsIn := Inbound{
		ID:       2,
		Protocol: ProtocolTrojan,
		Port:     443,
		Security: Security{Type: SecurityTLS, SNI: "trojan.example.com", RealityXver: 2},
	}
	if err := Normalize(TypeXray, &tlsIn); err != nil {
		t.Fatalf("Normalize TLS: %v", err)
	}
	if tlsIn.Security.RealityXver != 0 {
		t.Fatalf("expected TLS to clear RealityXver, got %d", tlsIn.Security.RealityXver)
	}
}
