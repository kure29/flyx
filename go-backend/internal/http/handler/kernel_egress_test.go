package handler

import (
	"strings"
	"testing"

	"go-backend/internal/kernel"
	"go-backend/internal/store/repo"
)

func TestOutboundIPVersionAndChain(t *testing.T) {
	for _, version := range []string{"1.13.0", "1.14.3", "latest"} {
		if !kernelCaps(version).SingboxPreferGoDNS {
			t.Fatalf("%s must prefer Go DNS", version)
		}
	}
	if kernelCaps("1.12.0").SingboxPreferGoDNS {
		t.Fatal("sing-box 1.12 does not support prefer_go")
	}
	for _, policy := range []string{kernel.OutboundIPv4Only, kernel.OutboundIPv6Only, kernel.OutboundPreferIPv4, kernel.OutboundPreferIPv6} {
		for _, version := range []string{"1.11.15", "v1.10.0"} {
			if err := validateOutboundIPVersion(kernel.TypeSingbox, version, policy); err == nil {
				t.Fatalf("old sing-box %s accepted", version)
			}
		}
		for _, version := range []string{"1.12.0", "1.14.3", "latest"} {
			if err := validateOutboundIPVersion(kernel.TypeSingbox, version, policy); err != nil {
				t.Fatal(err)
			}
		}
	}
	r, err := repo.Open(t.TempDir() + "/egress.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	h := New(r, "unit-test-secret")
	front := seedRelayTargetNode(t, r, "front", "198.51.100.1")
	landing := seedRelayTargetNode(t, r, "v6-only", "2001:db8::2")
	box := seedChainKernel(t, r, front, kernel.TypeSingbox)
	exit := seedChainKernel(t, r, landing, kernel.TypeXray)
	target := seedChainInbound(t, r, exit, "target", kernel.Inbound{Protocol: kernel.ProtocolShadowsocks, Port: 8388}, 0)
	entry := seedChainInbound(t, r, box, "entry", kernel.Inbound{Protocol: kernel.ProtocolShadowsocks, Port: 8389, OutboundIPStrategy: kernel.OutboundIPv4Only}, target.ID)
	if err := h.validateChain(*entry, kernel.TypeSingbox); err == nil || !strings.Contains(err.Error(), "IPv6") {
		t.Fatalf("IPv6 next hop must fail before saving: %v", err)
	}
	if err := h.checkKernelVersionFits(box.ID, kernel.TypeSingbox, "1.11.15"); err == nil || !strings.Contains(err.Error(), "1.12") {
		t.Fatalf("downgrade must not silently remove IPv4 policy: %v", err)
	}
	entry.OutboundID = 0
	if err := r.SaveNodeInbound(entry); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"1.12.0", "1.13.0", "latest"} {
		box.Version = version
		payload, _, err := h.buildKernelApplyPayload(box)
		if err != nil {
			t.Fatal(err)
		}
		cfg := payload["files"].([]kernel.File)[0].Content
		if got := strings.Contains(cfg, `"prefer_go": true`); got != (version != "1.12.0") {
			t.Fatalf("local DNS compatibility for %s: prefer_go=%v", version, got)
		}
	}
}

func TestIPv6OnlyChainValidation(t *testing.T) {
	r, err := repo.Open(t.TempDir() + "/ipv6-egress.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	h := New(r, "unit-test-secret")
	front := seedRelayTargetNode(t, r, "front", "198.51.100.1")
	box := seedChainKernel(t, r, front, kernel.TypeSingbox)
	for _, address := range []string{"192.0.2.2", "::ffff:192.0.2.2", "2001:db8::2", "exit.example.com"} {
		landing := seedRelayTargetNode(t, r, address, address)
		exit := seedChainKernel(t, r, landing, kernel.TypeXray)
		target := seedChainInbound(t, r, exit, "target", kernel.Inbound{Protocol: kernel.ProtocolShadowsocks, Port: 8388}, 0)
		entry := seedChainInbound(t, r, box, "entry", kernel.Inbound{Protocol: kernel.ProtocolShadowsocks, Port: 8389, OutboundIPStrategy: kernel.OutboundIPv6Only}, target.ID)
		err := h.validateChain(*entry, kernel.TypeSingbox)
		wantError := address == "192.0.2.2" || address == "::ffff:192.0.2.2"
		if (err != nil) != wantError {
			t.Fatalf("next hop %s: %v", address, err)
		}
	}
	if err := h.checkKernelVersionFits(box.ID, kernel.TypeSingbox, "1.11.15"); err == nil {
		t.Fatal("downgrade must not bypass IPv6-only policy")
	}
}

func TestXrayPreferredEgressVersion(t *testing.T) {
	for _, policy := range []string{kernel.OutboundPreferIPv4, kernel.OutboundPreferIPv6} {
		for _, version := range []string{"24.12.31", "25.5.16", "25.6", "v25.6.7"} {
			if err := validateOutboundIPVersion(kernel.TypeXray, version, policy); err == nil {
				t.Fatalf("%s accepted Happy Eyeballs policy %s", version, policy)
			}
		}
		for _, version := range []string{"25.6.8", "25.7.1", "26.3.27", "latest"} {
			if err := validateOutboundIPVersion(kernel.TypeXray, version, policy); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := validateOutboundIPVersion(kernel.TypeXray, "24.12.31", kernel.OutboundIPv4Only); err != nil {
		t.Fatalf("strict policies must keep supporting older Xray: %v", err)
	}
}
