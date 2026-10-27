package contract_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-backend/internal/auth"
	"go-backend/internal/http/response"
	"go-backend/internal/store/repo"
)

func kernelContractPost(t *testing.T, router http.Handler, token, path, body string) response.R {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Authorization", token)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	var out response.R
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return out
}

func seedKernelNode(t *testing.T, r *repo.Repository, name string) int64 {
	t.Helper()
	now := time.Now().UnixMilli()
	if err := r.DB().Exec(`
		INSERT INTO node(name, secret, server_ip, server_ip_v4, server_ip_v6, port, interface_name, version, http, tls, socks, created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx)
		VALUES(?, ?, '203.0.113.10', '203.0.113.10', '', '30000-30010', '', 'v1', 0, 0, 0, ?, ?, 0, '[::]', '[::]', 0)
	`, name, name+"-secret", now, now).Error; err != nil {
		t.Fatalf("seed node: %v", err)
	}
	return mustLastInsertID(t, r, "node")
}

func TestKernelInboundLifecycleContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, r := setupContractRouter(t, secret)
	admin, err := auth.GenerateToken(1, "admin_user", 0, secret)
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	nodeID := seedKernelNode(t, r, "landing")

	caps := kernelContractPost(t, router, admin, "/api/v1/kernel/capabilities", `{}`)
	if caps.Code != 0 {
		t.Fatalf("capabilities: %+v", caps)
	}
	if !strings.Contains(mustJSON(t, caps.Data), `"implemented":true,"name":"Xray"`) {
		t.Fatalf("xray not advertised: %s", mustJSON(t, caps.Data))
	}

	t.Run("inbound requires installed kernel", func(t *testing.T) {
		out := kernelContractPost(t, router, admin, "/api/v1/inbound/create",
			`{"nodeId":`+jsonInt64(nodeID)+`,"kernelType":"xray","name":"a","protocol":"vless","port":443}`)
		if out.Code == 0 || !strings.Contains(out.Msg, "安装内核") {
			t.Fatalf("expected install hint, got %+v", out)
		}
	})

	t.Run("unpinned snell version rejected", func(t *testing.T) {
		out := kernelContractPost(t, router, admin, "/api/v1/kernel/install",
			`{"nodeId":`+jsonInt64(nodeID)+`,"kernelType":"snell","version":"6.0.0rc"}`)
		if out.Code == 0 || !strings.Contains(out.Msg, "5.0.1") {
			t.Fatalf("snell v6 must be rejected with the supported list: %+v", out)
		}
		if mustQueryInt(t, r, `SELECT COUNT(*) FROM node_kernel WHERE kernel_type = 'snell'`) != 0 {
			t.Fatal("rejected install must not register a kernel")
		}
	})

	t.Run("bad version rejected", func(t *testing.T) {
		out := kernelContractPost(t, router, admin, "/api/v1/kernel/install",
			`{"nodeId":`+jsonInt64(nodeID)+`,"kernelType":"xray","version":"1.0/../../x"}`)
		if out.Code == 0 {
			t.Fatalf("path-like version must be rejected: %+v", out)
		}
	})

	// Node is offline: the kernel is registered and stays pending.
	install := kernelContractPost(t, router, admin, "/api/v1/kernel/install",
		`{"nodeId":`+jsonInt64(nodeID)+`,"kernelType":"xray","version":"latest"}`)
	if install.Code == 0 || !strings.Contains(install.Msg, "不在线") {
		t.Fatalf("offline install should report dispatch failure, got %+v", install)
	}
	if got := mustQueryString(t, r, `SELECT status FROM node_kernel WHERE node_id = ?`, nodeID); got != "pending" {
		t.Fatalf("kernel status = %q, want pending", got)
	}
	kernelID := mustQueryInt64(t, r, `SELECT id FROM node_kernel WHERE node_id = ?`, nodeID)

	create := kernelContractPost(t, router, admin, "/api/v1/inbound/create",
		`{"nodeId":`+jsonInt64(nodeID)+`,"kernelType":"xray","name":"东京-Reality","protocol":"vless","port":443,
		  "settings":{"flow":"xtls-rprx-vision"},"security":{"type":"reality","sni":"www.example.com"}}`)
	if create.Code != 0 || !strings.Contains(create.Msg, "下发到服务器失败") {
		t.Fatalf("create should succeed with dispatch warning, got %+v", create)
	}
	inboundID := mustQueryInt64(t, r, `SELECT id FROM node_inbound WHERE kernel_id = ?`, kernelID)

	list := kernelContractPost(t, router, admin, "/api/v1/inbound/list", `{}`)
	items, _ := list.Data.([]interface{})
	if list.Code != 0 || len(items) != 1 {
		t.Fatalf("list: %+v", list)
	}
	item := items[0].(map[string]interface{})
	settings := item["settings"].(map[string]interface{})
	security := item["security"].(map[string]interface{})
	firstUUID := valueAsString(settings["uuid"])
	if len(firstUUID) != 36 || settings["flow"] != "xtls-rprx-vision" {
		t.Fatalf("settings not generated: %v", settings)
	}
	if security["realityPublicKey"] == "" || security["realityDest"] != "www.example.com:443" {
		t.Fatalf("reality not generated: %v", security)
	}
	if item["nodeName"] != "landing" || item["kernelType"] != "xray" {
		t.Fatalf("joined fields: %v", item)
	}

	t.Run("port conflicts", func(t *testing.T) {
		dup := kernelContractPost(t, router, admin, "/api/v1/inbound/create",
			`{"nodeId":`+jsonInt64(nodeID)+`,"kernelType":"xray","name":"dup","protocol":"vmess","port":443,"security":{"type":"none"}}`)
		if dup.Code == 0 || !strings.Contains(dup.Msg, "已被") {
			t.Fatalf("duplicate inbound port must be rejected: %+v", dup)
		}
		if err := r.DB().Exec(`INSERT INTO forward_port(forward_id, node_id, port) VALUES(999, ?, 8443)`, nodeID).Error; err != nil {
			t.Fatal(err)
		}
		fwd := kernelContractPost(t, router, admin, "/api/v1/inbound/create",
			`{"nodeId":`+jsonInt64(nodeID)+`,"kernelType":"xray","name":"fwd","protocol":"vmess","port":8443,"security":{"type":"none"}}`)
		if fwd.Code == 0 || !strings.Contains(fwd.Msg, "转发") {
			t.Fatalf("forward port must be rejected: %+v", fwd)
		}
		occupied, err := r.HasOtherForwardOnNodePort(nodeID, 443, 0)
		if err != nil || !occupied {
			t.Fatalf("forward port check must see inbound port: %v %v", occupied, err)
		}
		used, err := r.ListUsedPortsOnNode(nodeID)
		if err != nil || !containsInt(used, 443) {
			t.Fatalf("used ports must include inbound: %v %v", used, err)
		}
	})

	t.Run("auto port and validation", func(t *testing.T) {
		auto := kernelContractPost(t, router, admin, "/api/v1/inbound/create",
			`{"nodeId":`+jsonInt64(nodeID)+`,"kernelType":"xray","name":"ss","protocol":"shadowsocks","port":0}`)
		if auto.Code != 0 {
			t.Fatalf("auto port create: %+v", auto)
		}
		port := mustQueryInt(t, r, `SELECT port FROM node_inbound WHERE name = 'ss'`)
		if port < 20000 || port >= 60000 {
			t.Fatalf("auto port out of range: %d", port)
		}
		bad := kernelContractPost(t, router, admin, "/api/v1/inbound/create",
			`{"nodeId":`+jsonInt64(nodeID)+`,"kernelType":"xray","name":"bad","protocol":"vless","port":9000,"transport":{"type":"ws"},"security":{"type":"reality"}}`)
		if bad.Code == 0 {
			t.Fatalf("ws+reality must be rejected: %+v", bad)
		}
	})

	t.Run("update keeps hidden secrets", func(t *testing.T) {
		tlsCreate := kernelContractPost(t, router, admin, "/api/v1/inbound/create",
			`{"nodeId":`+jsonInt64(nodeID)+`,"kernelType":"xray","name":"trojan","protocol":"trojan","port":2053,"security":{"type":"tls","sni":"t.example.com"}}`)
		if tlsCreate.Code != 0 {
			t.Fatalf("tls create: %+v", tlsCreate)
		}
		id := mustQueryInt64(t, r, `SELECT id FROM node_inbound WHERE name = 'trojan'`)
		before := mustQueryString(t, r, `SELECT security FROM node_inbound WHERE id = ?`, id)
		if !strings.Contains(before, "BEGIN CERTIFICATE") {
			t.Fatalf("self-signed cert missing: %s", before)
		}
		listed := kernelContractPost(t, router, admin, "/api/v1/inbound/list", `{}`)
		if strings.Contains(mustJSON(t, listed.Data), "PRIVATE KEY") {
			t.Fatal("list must not expose the certificate private key")
		}
		upd := kernelContractPost(t, router, admin, "/api/v1/inbound/update",
			`{"id":`+jsonInt64(id)+`,"name":"trojan2","protocol":"trojan","port":2053,"security":{"type":"tls","sni":"t.example.com","certMode":"self"}}`)
		if upd.Code != 0 {
			t.Fatalf("update: %+v", upd)
		}
		after := mustQueryString(t, r, `SELECT security FROM node_inbound WHERE id = ?`, id)
		if after != before {
			t.Fatal("update without PEM must keep the existing certificate")
		}
	})

	t.Run("regenerate and toggle", func(t *testing.T) {
		out := kernelContractPost(t, router, admin, "/api/v1/inbound/regenerate", `{"id":`+jsonInt64(inboundID)+`}`)
		if out.Code != 0 {
			t.Fatalf("regenerate: %+v", out)
		}
		settingsRaw := mustQueryString(t, r, `SELECT settings FROM node_inbound WHERE id = ?`, inboundID)
		if strings.Contains(settingsRaw, firstUUID) {
			t.Fatal("uuid not rotated")
		}
		out = kernelContractPost(t, router, admin, "/api/v1/inbound/toggle", `{"id":`+jsonInt64(inboundID)+`,"enabled":false}`)
		if out.Code != 0 || mustQueryInt(t, r, `SELECT enabled FROM node_inbound WHERE id = ?`, inboundID) != 0 {
			t.Fatalf("toggle: %+v", out)
		}
	})

	t.Run("traffic counters", func(t *testing.T) {
		now := time.Now().UnixMilli()
		apply := func(epoch string, in, out int64) {
			t.Helper()
			if err := r.ApplyInboundCounterSamples([]repo.InboundCounterSample{
				{InboundID: inboundID, Protocol: "tcp", Direction: "in", Epoch: epoch, Bytes: in},
				{InboundID: inboundID, Protocol: "tcp", Direction: "out", Epoch: epoch, Bytes: out},
				{InboundID: 424242, Protocol: "tcp", Direction: "in", Epoch: epoch, Bytes: 5},
			}, now); err != nil {
				t.Fatal(err)
			}
		}
		apply("e1", 100, 1000)
		apply("e1", 150, 1600) // +50 / +600
		apply("e2", 30, 40)    // new epoch counts from zero
		apply("e2", 10, 50)    // same-epoch regression: ignore the old upload sample
		up := mustQueryInt64(t, r, `SELECT upload_bytes FROM node_inbound WHERE id = ?`, inboundID)
		down := mustQueryInt64(t, r, `SELECT download_bytes FROM node_inbound WHERE id = ?`, inboundID)
		if up != 100+50+30 || down != 1000+600+40+10 {
			t.Fatalf("traffic up=%d down=%d", up, down)
		}
		if out := kernelContractPost(t, router, admin, "/api/v1/inbound/reset-traffic", `{"id":`+jsonInt64(inboundID)+`}`); out.Code != 0 {
			t.Fatalf("reset: %+v", out)
		}
		if mustQueryInt64(t, r, `SELECT upload_bytes + download_bytes FROM node_inbound WHERE id = ?`, inboundID) != 0 {
			t.Fatal("traffic not reset")
		}
	})

	t.Run("non admin forbidden", func(t *testing.T) {
		userToken, err := auth.GenerateToken(2, "normal_user", 1, secret)
		if err != nil {
			t.Fatal(err)
		}
		seedContractUser(t, r, 2, "normal_user", 1, 1)
		out := kernelContractPost(t, router, userToken, "/api/v1/inbound/list", `{}`)
		if out.Code != 403 && out.Code != 401 {
			t.Fatalf("non-admin must be rejected, got %+v", out)
		}
	})

	t.Run("delete inbound and kernel", func(t *testing.T) {
		if out := kernelContractPost(t, router, admin, "/api/v1/inbound/delete", `{"id":`+jsonInt64(inboundID)+`}`); out.Code != 0 {
			t.Fatalf("delete inbound: %+v", out)
		}
		if mustQueryInt(t, r, `SELECT COUNT(*) FROM node_inbound_counter WHERE inbound_id = ?`, inboundID) != 0 {
			t.Fatal("counters must be removed with the inbound")
		}
		offline := kernelContractPost(t, router, admin, "/api/v1/kernel/control", `{"id":`+jsonInt64(kernelID)+`,"action":"remove"}`)
		if offline.Code == 0 {
			t.Fatal("remove on offline node must require force")
		}
		forced := kernelContractPost(t, router, admin, "/api/v1/kernel/control", `{"id":`+jsonInt64(kernelID)+`,"action":"remove","force":true}`)
		if forced.Code != 0 {
			t.Fatalf("forced remove: %+v", forced)
		}
		if mustQueryInt(t, r, `SELECT COUNT(*) FROM node_inbound WHERE kernel_id = ?`, kernelID) != 0 {
			t.Fatal("inbounds must be removed with the kernel")
		}
	})
}

func TestKernelRejectsIneligibleNodesContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, r := setupContractRouter(t, secret)
	admin, _ := auth.GenerateToken(1, "admin_user", 0, secret)
	nodeID := seedKernelNode(t, r, "nft")
	if err := r.DB().Exec(`UPDATE node SET forward_mode = 'nftables' WHERE id = ?`, nodeID).Error; err != nil {
		t.Fatal(err)
	}
	out := kernelContractPost(t, router, admin, "/api/v1/kernel/install", `{"nodeId":`+jsonInt64(nodeID)+`,"kernelType":"xray"}`)
	if out.Code == 0 || !strings.Contains(out.Msg, "nftables") {
		t.Fatalf("nftables node must be rejected: %+v", out)
	}
}

func TestNodeDeleteRemovesKernelsContract(t *testing.T) {
	_, r := setupContractRouter(t, "contract-jwt-secret")
	nodeID := seedKernelNode(t, r, "gone")
	now := time.Now().UnixMilli()
	if err := r.DB().Exec(`INSERT INTO node_kernel(node_id, kernel_type, version, status, created_time, updated_time) VALUES(?, 'xray', 'latest', 'pending', ?, ?)`, nodeID, now, now).Error; err != nil {
		t.Fatal(err)
	}
	kernelID := mustLastInsertID(t, r, "node_kernel")
	if err := r.DB().Exec(`INSERT INTO node_inbound(kernel_id, node_id, name, protocol, port, created_time, updated_time) VALUES(?, ?, 'x', 'vless', 443, ?, ?)`, kernelID, nodeID, now, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteNodeCascade(nodeID); err != nil {
		t.Fatal(err)
	}
	if mustQueryInt(t, r, `SELECT COUNT(*) FROM node_kernel`)+mustQueryInt(t, r, `SELECT COUNT(*) FROM node_inbound`) != 0 {
		t.Fatal("kernel rows must be deleted with the node")
	}
}

func mustJSON(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func containsInt(list []int, v int) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

// saveNodeGroup creates a subscription node group and returns its id.
func saveNodeGroup(t *testing.T, router http.Handler, token, body string) int64 {
	t.Helper()
	out := kernelContractPost(t, router, token, "/api/v1/subscription/group/save", body)
	if out.Code != 0 {
		t.Fatalf("node group: %+v", out)
	}
	return int64(out.Data.(map[string]interface{})["id"].(float64))
}
