package handler

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"go-backend/internal/kernel"
	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
)

func TestBuildKernelAndPortMuxPayloads(t *testing.T) {
	r, err := repo.Open(t.TempDir() + "/mux.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer r.Close()

	now := time.Now().UnixMilli()
	if err := r.DB().Exec(`
		INSERT INTO node(name, secret, server_ip, server_ip_v4, server_ip_v6, port, interface_name, version, http, tls, socks, created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx)
		VALUES('n1', 'sec', '203.0.113.10', '203.0.113.10', '', '30000-30010', '', 'v1', 0, 0, 0, ?, ?, 1, '[::]', '[::]', 0)
	`, now, now).Error; err != nil {
		t.Fatalf("insert node: %v", err)
	}
	var nodeID int64
	if err := r.DB().Raw(`SELECT id FROM node WHERE name = 'n1'`).Scan(&nodeID).Error; err != nil {
		t.Fatalf("select node: %v", err)
	}

	k := &model.NodeKernel{
		NodeID:      nodeID,
		KernelType:  kernel.TypeXray,
		Version:     "latest",
		Status:      kernelStatusRunning,
		Enabled:     1,
		CreatedTime: now,
		UpdatedTime: now,
	}
	if err := r.CreateNodeKernel(k); err != nil {
		t.Fatalf("create kernel: %v", err)
	}

	in := kernel.Inbound{
		Protocol: kernel.ProtocolVLESS,
		Listen:   "::",
		Port:     443,
		Security: kernel.Security{Type: kernel.SecurityReality, SNI: "www.microsoft.com", RealityXver: 1},
	}
	if err := kernel.Normalize(kernel.TypeXray, &in); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	row := model.NodeInbound{
		KernelID:       k.ID,
		NodeID:         nodeID,
		Name:           "VLESS 443",
		Protocol:       in.Protocol,
		Listen:         in.Listen,
		Port:           in.Port,
		MuxPort:        45443,
		RelayWhitelist: 1,
		WhitelistExtra: "198.51.100.1/32",
		Enabled:        1,
		CreatedTime:    now,
		UpdatedTime:    now,
	}
	if err := applyInboundToModel(in, &row); err != nil {
		t.Fatalf("applyInboundToModel: %v", err)
	}
	if err := r.CreateNodeInbound(&row); err != nil {
		t.Fatalf("create inbound: %v", err)
	}

	rulesJSON, _ := json.Marshal([]muxExternalRule{
		{SNI: "panel.example.com", Target: "127.0.0.1:8443", ProxyProtocol: true, Remark: "panel"},
	})
	if err := r.UpsertNodePortMux(&model.NodePortMux{
		NodeID:         nodeID,
		Enabled:        1,
		Listen:         "::",
		Port:           443,
		Rules:          string(rulesJSON),
		FallbackTarget: "127.0.0.1:8443",
		FallbackProxy:  0,
		Status:         kernelStatusPending,
		CreatedTime:    now,
		UpdatedTime:    now,
	}); err != nil {
		t.Fatalf("upsert mux: %v", err)
	}

	h := New(r, "test-jwt-secret")

	// Kernel payload must listen on 127.0.0.1:45443 and omit nftables allowlist.
	kPayload, _, err := h.buildKernelApplyPayload(k)
	if err != nil {
		t.Fatalf("buildKernelApplyPayload: %v", err)
	}
	files := kPayload["files"].([]kernel.File)
	var xrayCfg struct {
		Inbounds []struct {
			Listen string `json:"listen"`
			Port   int    `json:"port"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal([]byte(files[0].Content), &xrayCfg); err != nil {
		t.Fatalf("decode xray config: %v", err)
	}
	if len(xrayCfg.Inbounds) != 1 || xrayCfg.Inbounds[0].Listen != "127.0.0.1" || xrayCfg.Inbounds[0].Port != 45443 {
		t.Fatalf("unexpected xray inbound listen/port: %+v", xrayCfg.Inbounds)
	}
	ports := kPayload["ports"].([]map[string]interface{})
	if len(ports) != 1 || ports[0]["port"] != 45443 {
		t.Fatalf("unexpected nft ports: %+v", ports)
	}
	if _, hasAllow := ports[0]["allow"]; hasAllow {
		t.Fatalf("muxed inbound must not set nftables allowlist; SNI mux enforces it: %+v", ports[0])
	}

	// Port mux payload must include both panel.example.com and www.microsoft.com (with whitelist!).
	mPayload, mHash, enabled, err := h.buildNodePortMuxPayload(nodeID)
	if err != nil || !enabled || mHash == "" {
		t.Fatalf("buildNodePortMuxPayload: enabled=%v hash=%q err=%v", enabled, mHash, err)
	}
	routes := mPayload["routes"].([]agentMuxRoutePayload)
	if len(routes) != 2 {
		t.Fatalf("expected 2 routes, got %+v", routes)
	}
	var gotPanel, gotVless *agentMuxRoutePayload
	for i := range routes {
		switch routes[i].SNI {
		case "panel.example.com":
			gotPanel = &routes[i]
		case "www.microsoft.com":
			gotVless = &routes[i]
		}
	}
	if gotPanel == nil || gotPanel.Target != "127.0.0.1:8443" || !gotPanel.ProxyProtocol {
		t.Fatalf("unexpected panel route: %+v", gotPanel)
	}
	if gotVless == nil || gotVless.Target != fmt.Sprintf("127.0.0.1:%d", 45443) {
		t.Fatalf("unexpected vless route: %+v", gotVless)
	}
	if len(gotVless.Allow) == 0 {
		t.Fatalf("expected whitelist Allow entries on muxed vless route, got %+v", gotVless)
	}
}
