package handler

import (
	"strings"
	"testing"

	"go-backend/internal/kernel"
	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
)

func TestNetworksOverlap(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"tcp", "udp", false},
		{"udp", "tcp", false},
		{"tcp", "tcp", true},
		{"udp", "udp", true},
		{"tcp", "tcp,udp", true},
		{"udp", "tcp,udp", true},
		{"tcp,udp", "tcp,udp", true},
		{"", "tcp", true},
		{"", "udp", true},
		{"tcp", "", true},
	} {
		if got := networksOverlap(tc.a, tc.b); got != tc.want {
			t.Errorf("networksOverlap(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestInboundPortConflictL4(t *testing.T) {
	r, err := repo.Open(t.TempDir() + "/port-conflict.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	h := New(r, "unit-test-secret")

	if err := r.DB().Exec(`INSERT INTO node(id, name, secret, server_ip, port, status, created_time, updated_time) VALUES(1, 'n1', 's', '203.0.113.10', '30000-30100', 1, 1, 1)`).Error; err != nil {
		t.Fatal(err)
	}
	kernels := []model.NodeKernel{
		{NodeID: 1, KernelType: kernel.TypeXray, Version: "latest", Status: "running", Enabled: 1, CreatedTime: 1, UpdatedTime: 1},
		{NodeID: 1, KernelType: kernel.TypeSingbox, Version: "1.14.0", Status: "running", Enabled: 1, CreatedTime: 1, UpdatedTime: 1},
		{NodeID: 1, KernelType: kernel.TypeSnell, Version: "5.0.1", Status: "running", Enabled: 1, CreatedTime: 1, UpdatedTime: 1},
	}
	for i := range kernels {
		if err := r.CreateNodeKernel(&kernels[i]); err != nil {
			t.Fatal(err)
		}
	}
	xrayID, sbID, snellID := kernels[0].ID, kernels[1].ID, kernels[2].ID

	seedInbound := func(kernelID int64, kernelType, name string, in kernel.Inbound) {
		t.Helper()
		if err := kernel.Normalize(kernelType, &in); err != nil {
			t.Fatalf("normalize %s: %v", name, err)
		}
		row := model.NodeInbound{KernelID: kernelID, NodeID: 1, Name: name, Enabled: 1, CreatedTime: 1, UpdatedTime: 1}
		if err := applyInboundToModel(in, &row); err != nil {
			t.Fatal(err)
		}
		if err := r.CreateNodeInbound(&row); err != nil {
			t.Fatal(err)
		}
	}

	// Seed VLESS-Reality (TCP 443), sing-box Snell v5 (TCP 6160), snell-server v5 (TCP+UDP 6161), and a forward on 8443.
	seedInbound(xrayID, kernel.TypeXray, "vless-443", kernel.Inbound{
		Protocol: kernel.ProtocolVLESS, Port: 443, Security: kernel.Security{Type: kernel.SecurityReality},
	})
	seedInbound(sbID, kernel.TypeSingbox, "sb-snell-6160", kernel.Inbound{
		Protocol: kernel.ProtocolSnell, Port: 6160, Settings: kernel.Settings{SnellVersion: 5},
	})
	seedInbound(snellID, kernel.TypeSnell, "snell-6161", kernel.Inbound{
		Protocol: kernel.ProtocolSnell, Port: 6161, Settings: kernel.Settings{SnellVersion: 5},
	})
	if err := r.DB().Create(&model.ForwardPort{ForwardID: 99, NodeID: 1, Port: 8443}).Error; err != nil {
		t.Fatal(err)
	}

	hy2 := func(port int) kernel.Inbound {
		in := kernel.Inbound{Protocol: kernel.ProtocolHysteria2, Port: port, Transport: kernel.Transport{Type: kernel.TransportQUIC}, Security: kernel.Security{Type: kernel.SecurityTLS}}
		_ = kernel.Normalize(kernel.TypeSingbox, &in)
		return in
	}
	vless := func(port int) kernel.Inbound {
		in := kernel.Inbound{Protocol: kernel.ProtocolVLESS, Port: port, Security: kernel.Security{Type: kernel.SecurityReality}}
		_ = kernel.Normalize(kernel.TypeXray, &in)
		return in
	}
	ss := func(port int) kernel.Inbound {
		in := kernel.Inbound{Protocol: kernel.ProtocolShadowsocks, Port: port}
		_ = kernel.Normalize(kernel.TypeXray, &in)
		return in
	}

	// UDP Hysteria2 on 443 may coexist with TCP VLESS-Reality on 443.
	if msg, err := h.inboundPortConflict(1, kernel.TypeSingbox, hy2(443), 0); err != nil || msg != "" {
		t.Fatalf("UDP 443 vs TCP 443 must be allowed: msg=%q err=%v", msg, err)
	}
	// TCP VLESS on 443 must conflict with TCP VLESS on 443.
	if msg, err := h.inboundPortConflict(1, kernel.TypeXray, vless(443), 0); err != nil || !strings.Contains(msg, "vless-443") {
		t.Fatalf("TCP 443 vs TCP 443 must conflict: msg=%q err=%v", msg, err)
	}
	// TCP+UDP Shadowsocks on 443 must conflict with TCP VLESS on 443.
	if msg, err := h.inboundPortConflict(1, kernel.TypeXray, ss(443), 0); err != nil || !strings.Contains(msg, "vless-443") {
		t.Fatalf("TCP+UDP 443 vs TCP 443 must conflict: msg=%q err=%v", msg, err)
	}
	// sing-box built-in Snell is TCP-only, so UDP Hysteria2 on 6160 is allowed.
	if msg, err := h.inboundPortConflict(1, kernel.TypeSingbox, hy2(6160), 0); err != nil || msg != "" {
		t.Fatalf("UDP 6160 vs sing-box TCP-only Snell must be allowed: msg=%q err=%v", msg, err)
	}
	// Standalone snell-server v5 listens on TCP+UDP, so UDP Hysteria2 on 6161 must conflict.
	if msg, err := h.inboundPortConflict(1, kernel.TypeSingbox, hy2(6161), 0); err != nil || !strings.Contains(msg, "snell-6161") {
		t.Fatalf("UDP 6161 vs snell-server v5 (TCP+UDP) must conflict: msg=%q err=%v", msg, err)
	}
	// Forward ports carry both TCP and UDP, so both TCP and UDP inbounds conflict with them.
	if msg, err := h.inboundPortConflict(1, kernel.TypeXray, vless(8443), 0); err != nil || !strings.Contains(msg, "转发") {
		t.Fatalf("TCP 8443 vs forward must conflict: msg=%q err=%v", msg, err)
	}
	if msg, err := h.inboundPortConflict(1, kernel.TypeSingbox, hy2(8443), 0); err != nil || !strings.Contains(msg, "转发") {
		t.Fatalf("UDP 8443 vs forward must conflict: msg=%q err=%v", msg, err)
	}
}
