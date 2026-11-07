package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"go-backend/internal/store/repo"
)

func TestTunnelCreatePersistsProbeTargetAndListReturnsConfiguredValue(t *testing.T) {
	h := setupProbeTargetTunnelHandler(t)
	body := bytes.NewReader([]byte(`{
		"name":"custom-target",
		"type":1,
		"flow":1,
		"trafficRatio":1,
		"status":1,
		"inNodeId":[{"nodeId":10,"protocol":"tls"}],
		"probeTargetHost":"speed.example.com",
		"probeTargetPort":8443
	}`))

	res := httptest.NewRecorder()
	h.tunnelCreate(res, httptest.NewRequest(http.MethodPost, "/api/v1/tunnel/create", body))
	assertProbeTargetSuccess(t, res)

	listRes := httptest.NewRecorder()
	h.tunnelList(listRes, httptest.NewRequest(http.MethodPost, "/api/v1/tunnel/list", nil))
	var payload struct {
		Code int              `json:"code"`
		Data []map[string]any `json:"data"`
	}
	decodeProbeTargetResponse(t, listRes, &payload)
	if payload.Code != 0 {
		t.Fatalf("expected success, got code %d", payload.Code)
	}
	item := payload.Data[0]
	if item["probeTargetHost"] != "speed.example.com" || item["probeTargetPort"] != float64(8443) {
		t.Fatalf("unexpected probe target in list response: %+v", item)
	}
}

func TestTunnelUpdatePersistsDefaultProbeTargetAsEmpty(t *testing.T) {
	h := setupProbeTargetTunnelHandler(t)
	seedProbeTargetTunnel(t, h, 77, "existing", "old.example.com", 9443)
	body := bytes.NewReader([]byte(`{
		"id":77,
		"name":"existing",
		"type":1,
		"flow":1,
		"trafficRatio":1,
		"status":1,
		"inNodeId":[{"nodeId":10,"protocol":"tls"}],
		"probeTargetHost":"",
		"probeTargetPort":0
	}`))

	res := httptest.NewRecorder()
	h.tunnelUpdate(res, httptest.NewRequest(http.MethodPost, "/api/v1/tunnel/update", body))
	assertProbeTargetSuccess(t, res)

	items, err := h.repo.ListTunnels()
	if err != nil {
		t.Fatalf("list tunnels: %v", err)
	}
	item := findProbeTargetTunnelItem(t, items, 77)
	if item["probeTargetHost"] != "" || item["probeTargetPort"] != 0 {
		t.Fatalf("expected default target to round-trip as empty/0, got %+v", item)
	}
}

func TestTunnelUpdateWithoutProbeTargetFieldsPreservesExistingTarget(t *testing.T) {
	h := setupProbeTargetTunnelHandler(t)
	seedProbeTargetTunnel(t, h, 79, "existing", "old.example.com", 9443)
	body := bytes.NewReader([]byte(`{
		"id":79,
		"name":"existing",
		"type":1,
		"flow":1,
		"trafficRatio":1,
		"status":1,
		"inNodeId":[{"nodeId":10,"protocol":"tls"}]
	}`))

	res := httptest.NewRecorder()
	h.tunnelUpdate(res, httptest.NewRequest(http.MethodPost, "/api/v1/tunnel/update", body))
	assertProbeTargetSuccess(t, res)

	items, err := h.repo.ListTunnels()
	if err != nil {
		t.Fatalf("list tunnels: %v", err)
	}
	item := findProbeTargetTunnelItem(t, items, 79)
	if item["probeTargetHost"] != "old.example.com" || item["probeTargetPort"] != 9443 {
		t.Fatalf("expected omitted probe target fields to preserve existing target, got %+v", item)
	}
}

func TestTunnelUpdateRejectsInvalidProbeTargetWithoutClearingExistingTarget(t *testing.T) {
	tests := []struct {
		name        string
		probeFields string
	}{
		{name: "non numeric port", probeFields: `,"probeTargetPort":"abc"`},
		{name: "fractional port", probeFields: `,"probeTargetPort":443.5`},
		{name: "whitespace host", probeFields: `,"probeTargetHost":"   "`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := setupProbeTargetTunnelHandler(t)
			seedProbeTargetTunnel(t, h, 80, "existing", "old.example.com", 9443)
			body := bytes.NewReader([]byte(`{
				"id":80,
				"name":"existing",
				"type":1,
				"flow":1,
				"trafficRatio":1,
				"status":1,
				"inNodeId":[{"nodeId":10,"protocol":"tls"}]
			` + tt.probeFields + `}`))

			res := httptest.NewRecorder()
			h.tunnelUpdate(res, httptest.NewRequest(http.MethodPost, "/api/v1/tunnel/update", body))
			var payload struct {
				Code int    `json:"code"`
				Msg  string `json:"msg"`
			}
			decodeProbeTargetResponse(t, res, &payload)
			if payload.Code == 0 || payload.Msg == "" {
				t.Fatalf("expected validation failure, got %+v", payload)
			}

			items, err := h.repo.ListTunnels()
			if err != nil {
				t.Fatalf("list tunnels: %v", err)
			}
			item := findProbeTargetTunnelItem(t, items, 80)
			if item["probeTargetHost"] != "old.example.com" || item["probeTargetPort"] != 9443 {
				t.Fatalf("expected invalid probe target to preserve existing target, got %+v", item)
			}
		})
	}
}

func TestTunnelCreateRejectsInvalidProbeTarget(t *testing.T) {
	h := setupProbeTargetTunnelHandler(t)
	body := bytes.NewReader([]byte(`{
		"name":"bad-target",
		"type":1,
		"flow":1,
		"trafficRatio":1,
		"status":1,
		"inNodeId":[{"nodeId":10,"protocol":"tls"}],
		"probeTargetHost":"https://example.com",
		"probeTargetPort":443
	}`))

	res := httptest.NewRecorder()
	h.tunnelCreate(res, httptest.NewRequest(http.MethodPost, "/api/v1/tunnel/create", body))
	var payload struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	decodeProbeTargetResponse(t, res, &payload)
	if payload.Code == 0 || payload.Msg == "" {
		t.Fatalf("expected validation failure, got %+v", payload)
	}
}

func TestTunnelUpdateInvalidProbeTargetDoesNotCleanFederationBindings(t *testing.T) {
	h := setupProbeTargetTunnelHandler(t)
	seedProbeTargetTunnel(t, h, 88, "existing", "old.example.com", 9443)
	seedProbeTargetFederationBinding(t, h, 88)
	body := bytes.NewReader([]byte(`{
		"id":88,
		"name":"existing",
		"type":1,
		"flow":1,
		"trafficRatio":1,
		"status":1,
		"inNodeId":[{"nodeId":10,"protocol":"tls"}],
		"probeTargetHost":"https://example.com",
		"probeTargetPort":443
	}`))

	res := httptest.NewRecorder()
	h.tunnelUpdate(res, httptest.NewRequest(http.MethodPost, "/api/v1/tunnel/update", body))
	var payload struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	decodeProbeTargetResponse(t, res, &payload)
	if payload.Code == 0 || payload.Msg == "" {
		t.Fatalf("expected validation failure, got %+v", payload)
	}

	bindings, err := h.repo.ListActiveFederationTunnelBindingsByTunnel(88)
	if err != nil {
		t.Fatalf("list federation bindings: %v", err)
	}
	if len(bindings) != 1 {
		t.Fatalf("expected federation binding to remain after invalid update, got %d", len(bindings))
	}
}

func TestTunnelDiagnosisUsesConfiguredProbeTarget(t *testing.T) {
	h := setupProbeTargetTunnelHandler(t)
	seedProbeTargetTunnel(t, h, 90, "diagnosis-target", "speed.example.com", 8443)

	_, _, workItems, err := h.prepareTunnelDiagnosis(90)
	if err != nil {
		t.Fatalf("prepare tunnel diagnosis: %v", err)
	}
	if len(workItems) != 1 {
		t.Fatalf("expected one diagnosis item, got %d", len(workItems))
	}
	if workItems[0].targetIP != "speed.example.com" || workItems[0].targetPort != 8443 {
		t.Fatalf("expected custom diagnosis target speed.example.com:8443, got %s:%d", workItems[0].targetIP, workItems[0].targetPort)
	}
}

func setupProbeTargetTunnelHandler(t *testing.T) *Handler {
	t.Helper()
	r, err := repo.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	h := New(r, "secret")
	now := time.Now().UnixMilli()
	if err := r.DB().Exec(`
		INSERT INTO node(id, name, secret, server_ip, server_ip_v4, server_ip_v6, port, interface_name, version, http, tls, socks, created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx)
		VALUES(10, 'entry-a', 'entry-secret', '10.0.0.1', '10.0.0.1', '', '30000-30010', '', 'v1', 1, 1, 1, ?, ?, 1, '[::]', '[::]', 0)
	`, now, now).Error; err != nil {
		t.Fatalf("insert node: %v", err)
	}
	return h
}

func seedProbeTargetTunnel(t *testing.T, h *Handler, id int64, name string, host string, port int) {
	t.Helper()
	now := time.Now().UnixMilli()
	if err := h.repo.DB().Exec(`
		INSERT INTO tunnel(id, name, traffic_ratio, type, protocol, flow, created_time, updated_time, status, inx, ip_preference, probe_target_host, probe_target_port)
		VALUES(?, ?, 1, 1, 'tls', 1, ?, ?, 1, ?, '', ?, ?)
	`, id, name, now, now, id, host, port).Error; err != nil {
		t.Fatalf("insert tunnel: %v", err)
	}
	if err := h.repo.DB().Exec(`
		INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol)
		VALUES(?, '1', 10, 30001, 'round', 1, 'tls')
	`, id).Error; err != nil {
		t.Fatalf("insert chain: %v", err)
	}
}

func seedProbeTargetFederationBinding(t *testing.T, h *Handler, tunnelID int64) {
	t.Helper()
	now := time.Now().UnixMilli()
	if err := h.repo.DB().Exec(`
		INSERT INTO federation_tunnel_binding(tunnel_id, node_id, chain_type, hop_inx, remote_url, resource_key, remote_binding_id, allocated_port, status, created_time, updated_time)
		VALUES(?, 10, 1, 0, 'http://peer.example', ?, 'remote-binding', 30001, 1, ?, ?)
	`, tunnelID, "probe-target-test-binding", now, now).Error; err != nil {
		t.Fatalf("insert federation binding: %v", err)
	}
}

func assertProbeTargetSuccess(t *testing.T, res *httptest.ResponseRecorder) {
	t.Helper()
	var payload struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	decodeProbeTargetResponse(t, res, &payload)
	if payload.Code != 0 {
		t.Fatalf("expected success, got %+v", payload)
	}
}

func decodeProbeTargetResponse(t *testing.T, res *httptest.ResponseRecorder, v any) {
	t.Helper()
	if res.Code != http.StatusOK {
		t.Fatalf("expected HTTP %d, got %d", http.StatusOK, res.Code)
	}
	if err := json.NewDecoder(res.Body).Decode(v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func findProbeTargetTunnelItem(t *testing.T, items []map[string]interface{}, id int64) map[string]interface{} {
	t.Helper()
	for _, item := range items {
		if asInt64(item["id"], 0) == id {
			return item
		}
	}
	t.Fatalf("tunnel %d not found: %+v", id, items)
	return nil
}
