package contract_test

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"go-backend/internal/auth"
)

func TestNodePortMuxAndSharePortContract(t *testing.T) {
	secret := "contract-port-mux-secret"
	router, r := setupContractRouter(t, secret)
	admin, err := auth.GenerateToken(1, "admin_user", 0, secret)
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	nodeID := seedKernelNode(t, r, "mux-node")

	// Install Xray and sing-box (node is offline; DB records are created).
	_ = kernelContractPost(t, router, admin, "/api/v1/kernel/install",
		`{"nodeId":`+jsonInt64(nodeID)+`,"kernelType":"xray","version":"latest"}`)
	_ = kernelContractPost(t, router, admin, "/api/v1/kernel/install",
		`{"nodeId":`+jsonInt64(nodeID)+`,"kernelType":"singbox","version":"latest"}`)

	// Default port-mux config is disabled on port 443.
	got := kernelContractPost(t, router, admin, "/api/v1/node/port-mux/get",
		`{"nodeId":`+jsonInt64(nodeID)+`}`)
	if got.Code != 0 {
		t.Fatalf("port-mux/get failed: %+v", got)
	}
	if !strings.Contains(mustJSON(t, got.Data), `"port":443`) || !strings.Contains(mustJSON(t, got.Data), `"enabled":false`) {
		t.Fatalf("unexpected default port-mux: %s", mustJSON(t, got.Data))
	}

	// Enable 443 port mux with panel HTTPS rule + fallback target.
	save := kernelContractPost(t, router, admin, "/api/v1/node/port-mux/save", `{
		"nodeId":`+jsonInt64(nodeID)+`,
		"enabled":true,
		"listen":"::",
		"port":443,
		"rules":[{"sni":"panel.example.com","target":"127.0.0.1:8443","proxyProtocol":true,"remark":"面板 HTTPS"}],
		"fallbackTarget":"127.0.0.1:8443",
		"fallbackProxy":false
	}`)
	if save.Code != 0 {
		t.Fatalf("port-mux/save failed: %+v", save)
	}

	// Create Xray VLESS + Reality on port 443 with SNI www.microsoft.com and xver=1.
	vlessRes := kernelContractPost(t, router, admin, "/api/v1/inbound/create", `{
		"nodeId":`+jsonInt64(nodeID)+`,
		"kernelType":"xray",
		"name":"VLESS 443",
		"protocol":"vless",
		"port":443,
		"transport":{"type":"tcp"},
		"security":{"type":"reality","sni":"www.microsoft.com","realityXver":1}
	}`)
	if vlessRes.Code != 0 {
		t.Fatalf("create VLESS 443 failed: %+v", vlessRes)
	}
	vlessID := mustExtractID(t, vlessRes.Data)

	// Create sing-box Trojan + TLS on port 443 with a different SNI.
	trojanRes := kernelContractPost(t, router, admin, "/api/v1/inbound/create", `{
		"nodeId":`+jsonInt64(nodeID)+`,
		"kernelType":"singbox",
		"name":"Trojan 443",
		"protocol":"trojan",
		"port":443,
		"transport":{"type":"tcp"},
		"security":{"type":"tls","sni":"trojan.example.com","certMode":"self"}
	}`)
	if trojanRes.Code != 0 {
		t.Fatalf("create Trojan 443 failed: %+v", trojanRes)
	}
	trojanID := mustExtractID(t, trojanRes.Data)

	// Create sing-box Hysteria2 on UDP 443: coexists with TCP 443 mux without being muxed.
	hy2Res := kernelContractPost(t, router, admin, "/api/v1/inbound/create", `{
		"nodeId":`+jsonInt64(nodeID)+`,
		"kernelType":"singbox",
		"name":"Hysteria2 443",
		"protocol":"hysteria2",
		"port":443,
		"transport":{"type":"quic"},
		"security":{"type":"tls","sni":"hy2.example.com","certMode":"self"}
	}`)
	if hy2Res.Code != 0 {
		t.Fatalf("create Hysteria2 UDP 443 failed: %+v", hy2Res)
	}
	hy2ID := mustExtractID(t, hy2Res.Data)

	// Reject duplicate SNI against existing inbound.
	dupInbound := kernelContractPost(t, router, admin, "/api/v1/inbound/create", `{
		"nodeId":`+jsonInt64(nodeID)+`,
		"kernelType":"xray",
		"name":"Dup SNI",
		"protocol":"vless",
		"port":443,
		"transport":{"type":"tcp"},
		"security":{"type":"reality","sni":"WWW.MICROSOFT.COM"}
	}`)
	if dupInbound.Code == 0 || !strings.Contains(dupInbound.Msg, "冲突") {
		t.Fatalf("expected duplicate SNI rejection, got %+v", dupInbound)
	}

	// Reject duplicate SNI against external panel rule.
	dupExt := kernelContractPost(t, router, admin, "/api/v1/inbound/create", `{
		"nodeId":`+jsonInt64(nodeID)+`,
		"kernelType":"singbox",
		"name":"Dup Panel SNI",
		"protocol":"trojan",
		"port":443,
		"transport":{"type":"tcp"},
		"security":{"type":"tls","sni":"panel.example.com","certMode":"self"}
	}`)
	if dupExt.Code == 0 || !strings.Contains(dupExt.Msg, "外部规则冲突") {
		t.Fatalf("expected external rule SNI conflict, got %+v", dupExt)
	}

	// Reject plain TCP (SecurityNone) on muxed port 443.
	plainTCP := kernelContractPost(t, router, admin, "/api/v1/inbound/create", `{
		"nodeId":`+jsonInt64(nodeID)+`,
		"kernelType":"xray",
		"name":"Plain SS 443",
		"protocol":"shadowsocks",
		"port":443,
		"transport":{"type":"tcp"},
		"security":{"type":"none"}
	}`)
	if plainTCP.Code == 0 || !strings.Contains(plainTCP.Msg, "SNI") {
		t.Fatalf("expected plain TCP on muxed port rejection, got %+v", plainTCP)
	}

	// Verify inbound/list reports muxPort > 0 for VLESS and Trojan, and 0 for Hysteria2.
	listRes := kernelContractPost(t, router, admin, "/api/v1/inbound/list", `{}`)
	if listRes.Code != 0 {
		t.Fatalf("inbound/list failed: %+v", listRes)
	}
	var items []struct {
		ID        int64 `json:"id"`
		Port      int   `json:"port"`
		SharePort int   `json:"sharePort"`
		MuxPort   int   `json:"muxPort"`
		Security  struct {
			RealityXver int `json:"realityXver"`
		} `json:"security"`
	}
	if err := json.Unmarshal([]byte(mustJSON(t, listRes.Data)), &items); err != nil {
		t.Fatalf("decode inbound/list: %v", err)
	}
	byID := map[int64]int{}
	for _, it := range items {
		byID[it.ID] = it.MuxPort
		if it.ID == vlessID && it.Security.RealityXver != 1 {
			t.Fatalf("expected vless realityXver=1, got %d", it.Security.RealityXver)
		}
	}
	if byID[vlessID] <= 0 || byID[trojanID] <= 0 || byID[vlessID] == byID[trojanID] {
		t.Fatalf("expected distinct muxPorts for VLESS & Trojan, got %+v", byID)
	}
	if byID[hy2ID] != 0 {
		t.Fatalf("expected UDP Hysteria2 muxPort=0, got %d", byID[hy2ID])
	}

	// Verify share links advertise port 443 for both muxed inbounds.
	for _, id := range []int64{vlessID, trojanID} {
		sh := kernelContractPost(t, router, admin, "/api/v1/inbound/share", `{"id":`+jsonInt64(id)+`}`)
		if sh.Code != 0 {
			t.Fatalf("share %d failed: %+v", id, sh)
		}
		if !strings.Contains(mustJSON(t, sh.Data), `"port":443`) {
			t.Fatalf("expected share port 443 for inbound %d, got %s", id, mustJSON(t, sh.Data))
		}
	}

	// Verify loopback inbound with SharePort=443 emits direct share endpoint on port 443.
	loopbackRes := kernelContractPost(t, router, admin, "/api/v1/inbound/create", `{
		"nodeId":`+jsonInt64(nodeID)+`,
		"kernelType":"xray",
		"name":"Behind External Caddy",
		"protocol":"vless",
		"listen":"127.0.0.1",
		"port":10443,
		"sharePort":443,
		"transport":{"type":"tcp"},
		"security":{"type":"reality","sni":"www.apple.com"}
	}`)
	if loopbackRes.Code != 0 {
		t.Fatalf("create loopback sharePort inbound failed: %+v", loopbackRes)
	}
	loopbackID := mustExtractID(t, loopbackRes.Data)
	loopbackShare := kernelContractPost(t, router, admin, "/api/v1/inbound/share", `{"id":`+jsonInt64(loopbackID)+`}`)
	if loopbackShare.Code != 0 || !strings.Contains(mustJSON(t, loopbackShare.Data), `"port":443`) {
		t.Fatalf("expected loopback inbound with sharePort=443 to advertise port 443, got %+v", loopbackShare)
	}

	// Disabling port mux while 2 TCP inbounds share 443 must be rejected.
	disableBlocked := kernelContractPost(t, router, admin, "/api/v1/node/port-mux/save", `{
		"nodeId":`+jsonInt64(nodeID)+`,
		"enabled":false,
		"listen":"::",
		"port":443,
		"rules":[]
	}`)
	if disableBlocked.Code == 0 || !strings.Contains(disableBlocked.Msg, "共用端口 443") {
		t.Fatalf("expected disable rejection while 2 TCP inbounds share 443, got %+v", disableBlocked)
	}

	// Delete Trojan, then disabling port mux succeeds and clears VLESS muxPort to 0.
	del := kernelContractPost(t, router, admin, "/api/v1/inbound/delete", `{"id":`+jsonInt64(trojanID)+`}`)
	if del.Code != 0 {
		t.Fatalf("delete trojan failed: %+v", del)
	}
	disableOK := kernelContractPost(t, router, admin, "/api/v1/node/port-mux/save", `{
		"nodeId":`+jsonInt64(nodeID)+`,
		"enabled":false,
		"listen":"::",
		"port":443,
		"rules":[]
	}`)
	if disableOK.Code != 0 {
		t.Fatalf("disable mux after deleting trojan failed: %+v", disableOK)
	}
	vlessRow, err := r.GetNodeInbound(vlessID)
	if err != nil || vlessRow == nil || vlessRow.MuxPort != 0 {
		t.Fatalf("expected vless muxPort cleared to 0 after disabling mux, got %+v err=%v", vlessRow, err)
	}
}

func mustExtractID(t *testing.T, data interface{}) int64 {
	t.Helper()
	raw := mustJSON(t, data)
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("decode id map: %v", err)
	}
	switch v := m["id"].(type) {
	case float64:
		return int64(v)
	case string:
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			t.Fatalf("parse id %q: %v", v, err)
		}
		return id
	default:
		t.Fatalf("missing id in %s", raw)
		return 0
	}
}
