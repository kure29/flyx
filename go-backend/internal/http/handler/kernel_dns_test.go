package handler

import (
	"strings"
	"testing"

	"go-backend/internal/kernel"
	"go-backend/internal/store/repo"
)

func TestAutomaticSingboxDNSVersionCompatibility(t *testing.T) {
	r, err := repo.Open(t.TempDir() + "/dns.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	h := New(r, "unit-test-secret")
	node := seedRelayTargetNode(t, r, "automatic-dns", "198.51.100.1")
	k := seedChainKernel(t, r, node, kernel.TypeSingbox)
	seedChainInbound(t, r, k, "direct", kernel.Inbound{Protocol: kernel.ProtocolShadowsocks, Port: 8388}, 0)
	for _, version := range []string{"1.12.0", "1.13.0", "v1.14.3", "latest"} {
		k.Version = version
		payload, _, err := h.buildKernelApplyPayload(k)
		if err != nil {
			t.Fatal(err)
		}
		cfg := payload["files"].([]kernel.File)[0].Content
		want := version != "1.12.0"
		if strings.Contains(cfg, `"prefer_go": true`) != want || strings.Contains(cfg, `"default_domain_resolver"`) != want {
			t.Fatalf("wrong local DNS compatibility for %s: %s", version, cfg)
		}
	}
}
