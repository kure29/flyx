package contract_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-backend/internal/auth"
	"go-backend/internal/store/repo"
)

// seedInboundOnNode creates a kernel + inbound directly, bypassing dispatch.
func seedInboundOnNode(t *testing.T, router http.Handler, r *repo.Repository, admin string, nodeID int64, body string) int64 {
	t.Helper()
	now := time.Now().UnixMilli()
	if mustQueryInt(t, r, `SELECT COUNT(*) FROM node_kernel WHERE node_id = ?`, nodeID) == 0 {
		if err := r.DB().Exec(`INSERT INTO node_kernel(node_id, kernel_type, version, status, created_time, updated_time) VALUES(?, 'xray', 'latest', 'pending', ?, ?)`, nodeID, now, now).Error; err != nil {
			t.Fatal(err)
		}
	}
	out := kernelContractPost(t, router, admin, "/api/v1/inbound/create", body)
	if out.Code != 0 {
		t.Fatalf("create inbound: %+v", out)
	}
	return int64(out.Data.(map[string]interface{})["id"].(float64))
}

func seedRelayForward(t *testing.T, r *repo.Repository, tunnelType int, entryNodeID, exitNodeID int64, entryPort int, inboundID int64) (tunnelID, forwardID int64) {
	t.Helper()
	now := time.Now().UnixMilli()
	if err := r.DB().Exec(`
		INSERT INTO tunnel(name, traffic_ratio, type, protocol, flow, created_time, updated_time, status, in_ip, inx)
		VALUES('香港中转', 1.0, ?, 'tls', 99999, ?, ?, 1, NULL, 0)`, tunnelType, now, now).Error; err != nil {
		t.Fatal(err)
	}
	tunnelID = mustLastInsertID(t, r, "tunnel")
	if err := r.DB().Exec(`INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol) VALUES(?, 1, ?, NULL, 'round', 1, 'tls')`, tunnelID, entryNodeID).Error; err != nil {
		t.Fatal(err)
	}
	if exitNodeID > 0 {
		if err := r.DB().Exec(`INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol) VALUES(?, 3, ?, 31000, 'round', 1, 'tls')`, tunnelID, exitNodeID).Error; err != nil {
			t.Fatal(err)
		}
	}
	if inboundID == 0 {
		return tunnelID, 0
	}
	if err := r.DB().Exec(`
		INSERT INTO forward(user_id, user_name, name, tunnel_id, remote_addr, strategy, in_flow, out_flow, created_time, updated_time, status, inx)
		VALUES(1, 'admin_user', 'relay-fwd', ?, '203.0.113.10:443', 'fifo', 0, 0, ?, ?, 1, 0)`, tunnelID, now, now).Error; err != nil {
		t.Fatal(err)
	}
	forwardID = mustLastInsertID(t, r, "forward")
	if err := r.DB().Exec(`INSERT INTO forward_port(forward_id, node_id, port) VALUES(?, ?, ?)`, forwardID, entryNodeID, entryPort).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.DB().Exec(`INSERT INTO node_inbound_relay(inbound_id, forward_id, name, share_host, managed, created_time) VALUES(?, ?, '香港中转 → 东京', '', 1, ?)`, inboundID, forwardID, now).Error; err != nil {
		t.Fatal(err)
	}
	return tunnelID, forwardID
}

func getSub(t *testing.T, router http.Handler, path, ua string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	return res
}

func TestInboundShareAndSubscriptionContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, r := setupContractRouter(t, secret)
	admin, _ := auth.GenerateToken(1, "admin_user", 0, secret)
	landing := seedKernelNode(t, r, "东京落地")
	relayNode := seedKernelNode(t, r, "香港中转")
	if err := r.DB().Exec(`UPDATE node SET server_ip = '198.51.100.7' WHERE id = ?`, relayNode).Error; err != nil {
		t.Fatal(err)
	}

	inboundID := seedInboundOnNode(t, router, r, admin, landing,
		`{"nodeId":`+jsonInt64(landing)+`,"kernelType":"xray","name":"东京","protocol":"vless","port":443,"settings":{"flow":"xtls-rprx-vision"},"security":{"type":"reality"}}`)
	seedInboundOnNode(t, router, r, admin, landing,
		`{"nodeId":`+jsonInt64(landing)+`,"kernelType":"xray","name":"本机","protocol":"shadowsocks","listen":"127.0.0.1","port":8388}`)
	_, forwardID := seedRelayForward(t, r, 1, relayNode, 0, 30005, inboundID)

	share := kernelContractPost(t, router, admin, "/api/v1/inbound/share", `{"id":`+jsonInt64(inboundID)+`}`)
	items, _ := share.Data.([]interface{})
	if share.Code != 0 || len(items) != 2 {
		t.Fatalf("share should list direct + relay: %+v", share)
	}
	direct, relay := items[0].(map[string]interface{}), items[1].(map[string]interface{})
	if direct["kind"] != "direct" || !strings.Contains(direct["uri"].(string), "@203.0.113.10:443?") {
		t.Fatalf("direct entry: %v", direct)
	}
	if relay["kind"] != "relay" || !strings.Contains(relay["uri"].(string), "@198.51.100.7:30005?") {
		t.Fatalf("relay entry must use the relay entry address: %v", relay)
	}

	info := kernelContractPost(t, router, admin, "/api/v1/inbound/sub/info", `{}`)
	token := info.Data.(map[string]interface{})["token"].(string)
	if len(token) != 32 {
		t.Fatalf("token: %q", token)
	}
	again := kernelContractPost(t, router, admin, "/api/v1/inbound/sub/info", `{}`)
	if again.Data.(map[string]interface{})["token"] != token {
		t.Fatal("token must be stable until reset")
	}

	t.Run("token is not readable through config endpoints by users", func(t *testing.T) {
		seedContractUser(t, r, 2, "normal_user", 1, 1)
		userToken, _ := auth.GenerateToken(2, "normal_user", 1, secret)
		out := kernelContractPost(t, router, userToken, "/api/v1/config/get", `{"name":"node_sub_token"}`)
		if out.Code == 0 {
			t.Fatalf("subscription token leaked: %+v", out)
		}
		// Tokens live in the subscription table; a config value named like
		// the old key must not open a subscription.
		kernelContractPost(t, router, admin, "/api/v1/config/update-single", `{"name":"node_sub_token","value":"`+strings.Repeat("a", 32)+`"}`)
		if res := getSub(t, router, "/api/v1/open_api/sub/"+strings.Repeat("a", 32), ""); res.Code != http.StatusNotFound {
			t.Fatalf("config value must not work as a token: %d", res.Code)
		}
		if mustQueryString(t, r, `SELECT token FROM subscription ORDER BY id LIMIT 1`) != token {
			t.Fatal("default subscription token changed")
		}
	})

	t.Run("bad token is 404", func(t *testing.T) {
		if res := getSub(t, router, "/api/v1/open_api/sub/"+strings.Repeat("0", 32), ""); res.Code != http.StatusNotFound {
			t.Fatalf("status %d", res.Code)
		}
		if res := getSub(t, router, "/api/v1/open_api/sub/", ""); res.Code != http.StatusNotFound {
			t.Fatalf("empty token status %d", res.Code)
		}
	})

	t.Run("base64 default", func(t *testing.T) {
		res := getSub(t, router, "/api/v1/open_api/sub/"+token, "v2rayN/7.0")
		if res.Code != http.StatusOK {
			t.Fatalf("status %d", res.Code)
		}
		raw, err := base64.StdEncoding.DecodeString(res.Body.String())
		if err != nil {
			t.Fatal(err)
		}
		body := string(raw)
		hasSS := false
		for _, line := range strings.Split(body, "\n") {
			hasSS = hasSS || strings.HasPrefix(line, "ss://")
		}
		if strings.Count(body, "vless://") != 2 || hasSS {
			t.Fatalf("loopback-only inbound must not be shared directly: %s", body)
		}
		if !strings.HasPrefix(res.Header().Get("Subscription-Userinfo"), "upload=0; download=0") {
			t.Fatalf("userinfo header: %q", res.Header().Get("Subscription-Userinfo"))
		}
	})

	t.Run("clash by user agent", func(t *testing.T) {
		res := getSub(t, router, "/api/v1/open_api/sub/"+token, "clash-verge/v2")
		if !strings.Contains(res.Body.String(), "proxy-groups:") || !strings.Contains(res.Header().Get("Content-Type"), "yaml") {
			t.Fatalf("clash body: %s", res.Body.String())
		}
	})

	t.Run("explicit format wins", func(t *testing.T) {
		res := getSub(t, router, "/api/v1/open_api/sub/"+token+"?format=singbox", "clash-verge/v2")
		if !strings.Contains(res.Body.String(), `"outbounds"`) {
			t.Fatalf("singbox body: %s", res.Body.String())
		}
	})

	t.Run("relay only hides the landing address", func(t *testing.T) {
		upd := kernelContractPost(t, router, admin, "/api/v1/inbound/update",
			`{"id":`+jsonInt64(inboundID)+`,"name":"东京","protocol":"vless","port":443,"relayOnly":true,"settings":{"flow":"xtls-rprx-vision"},"security":`+
				mustQueryString(t, r, `SELECT security FROM node_inbound WHERE id = ?`, inboundID)+`}`)
		if upd.Code != 0 {
			t.Fatalf("update: %+v", upd)
		}
		raw, _ := base64.StdEncoding.DecodeString(getSub(t, router, "/api/v1/open_api/sub/"+token, "").Body.String())
		if strings.Contains(string(raw), "203.0.113.10") || !strings.Contains(string(raw), "198.51.100.7:30005") {
			t.Fatalf("relay-only sub: %s", raw)
		}
	})

	t.Run("disabled relay forward is skipped", func(t *testing.T) {
		if err := r.DB().Exec(`UPDATE forward SET status = 0 WHERE id = ?`, forwardID).Error; err != nil {
			t.Fatal(err)
		}
		raw, _ := base64.StdEncoding.DecodeString(getSub(t, router, "/api/v1/open_api/sub/"+token, "").Body.String())
		if strings.Contains(string(raw), "198.51.100.7") {
			t.Fatalf("paused relay must not be offered: %s", raw)
		}
		_ = r.DB().Exec(`UPDATE forward SET status = 1 WHERE id = ?`, forwardID).Error
	})

	t.Run("reset rotates token", func(t *testing.T) {
		out := kernelContractPost(t, router, admin, "/api/v1/inbound/sub/info", `{"reset":true}`)
		if out.Data.(map[string]interface{})["token"] == token {
			t.Fatal("token not rotated")
		}
		if res := getSub(t, router, "/api/v1/open_api/sub/"+token, ""); res.Code != http.StatusNotFound {
			t.Fatal("old token must stop working")
		}
	})

	t.Run("relay rename changes the shared name and host", func(t *testing.T) {
		relayID := int64(mustQueryInt(t, r, `SELECT id FROM node_inbound_relay WHERE forward_id = ?`, forwardID))
		if out := kernelContractPost(t, router, admin, "/api/v1/inbound/relay/update", `{"id":`+jsonInt64(relayID)+`,"name":"  "}`); out.Code == 0 {
			t.Fatal("blank name must be rejected")
		}
		out := kernelContractPost(t, router, admin, "/api/v1/inbound/relay/update",
			`{"id":`+jsonInt64(relayID)+`,"name":" 香港 IPLC ","shareHost":"hk.example.com"}`)
		if out.Code != 0 {
			t.Fatalf("update: %+v", out)
		}
		share := kernelContractPost(t, router, admin, "/api/v1/inbound/share", `{"id":`+jsonInt64(inboundID)+`}`)
		var relay map[string]interface{}
		for _, it := range share.Data.([]interface{}) {
			if m := it.(map[string]interface{}); m["kind"] == "relay" {
				relay = m
			}
		}
		if relay == nil || relay["name"] != "香港 IPLC" || !strings.Contains(relay["uri"].(string), "@hk.example.com:30005?") {
			t.Fatalf("renamed relay entry: %v", relay)
		}
		kernelContractPost(t, router, admin, "/api/v1/inbound/relay/update",
			`{"id":`+jsonInt64(relayID)+`,"name":"香港中转 → 东京","shareHost":""}`)
	})

	t.Run("relay list and forward deletion cascade", func(t *testing.T) {
		list := kernelContractPost(t, router, admin, "/api/v1/inbound/relay/list", `{"inboundId":`+jsonInt64(inboundID)+`}`)
		rels, _ := list.Data.([]interface{})
		if len(rels) != 1 {
			t.Fatalf("relays: %+v", list)
		}
		rel := rels[0].(map[string]interface{})
		if rel["mode"] != "port" || rel["tunnelName"] != "香港中转" || len(rel["entries"].([]interface{})) != 1 {
			t.Fatalf("relay view: %v", rel)
		}
		inList := kernelContractPost(t, router, admin, "/api/v1/inbound/list", `{}`)
		for _, it := range inList.Data.([]interface{}) {
			m := it.(map[string]interface{})
			if int64(m["id"].(float64)) == inboundID && valueAsInt(m["relayCount"]) != 1 {
				t.Fatalf("relayCount: %v", m["relayCount"])
			}
		}
		if err := r.DeleteForwardCascade(forwardID); err != nil {
			t.Fatal(err)
		}
		if mustQueryInt(t, r, `SELECT COUNT(*) FROM node_inbound_relay`) != 0 {
			t.Fatal("deleting the forward must drop the relay link")
		}
	})
}

func TestInboundRelayValidationContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, r := setupContractRouter(t, secret)
	admin, _ := auth.GenerateToken(1, "admin_user", 0, secret)
	landing := seedKernelNode(t, r, "landing")
	relayNode := seedKernelNode(t, r, "relay")
	other := seedKernelNode(t, r, "other")

	public := seedInboundOnNode(t, router, r, admin, landing,
		`{"nodeId":`+jsonInt64(landing)+`,"kernelType":"xray","name":"pub","protocol":"vless","port":443,"security":{"type":"reality"}}`)
	local := seedInboundOnNode(t, router, r, admin, landing,
		`{"nodeId":`+jsonInt64(landing)+`,"kernelType":"xray","name":"local","protocol":"vless","listen":"127.0.0.1","port":444,"security":{"type":"reality"}}`)

	portTunnel, _ := seedRelayForward(t, r, 1, relayNode, 0, 0, 0)
	wrongExit, _ := seedRelayForward(t, r, 2, relayNode, other, 0, 0)

	cases := []struct {
		name, body, want string
	}{
		{"loopback inbound over port forward", `{"inboundId":` + jsonInt64(local) + `,"tunnelId":` + jsonInt64(portTunnel) + `}`, "只监听本机"},
		{"loopback inbound over a tunnel exiting elsewhere", `{"inboundId":` + jsonInt64(local) + `,"tunnelId":` + jsonInt64(wrongExit) + `}`, "只监听本机"},
		{"missing inbound", `{"inboundId":999,"tunnelId":` + jsonInt64(portTunnel) + `}`, "入站不存在"},
		{"missing tunnel", `{"inboundId":` + jsonInt64(public) + `,"tunnelId":999}`, "隧道不存在"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := kernelContractPost(t, router, admin, "/api/v1/inbound/relay/create", tc.body)
			if out.Code == 0 || !strings.Contains(out.Msg, tc.want) {
				t.Fatalf("want error containing %q, got %+v", tc.want, out)
			}
		})
	}
	if mustQueryInt(t, r, `SELECT COUNT(*) FROM forward`) != 0 {
		t.Fatal("rejected relays must not leave forwards behind")
	}
}

func TestInboundProxyProtocolAndWhitelistContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, r := setupContractRouter(t, secret)
	admin, _ := auth.GenerateToken(1, "admin_user", 0, secret)
	landing := seedKernelNode(t, r, "landing")
	relayNode := seedKernelNode(t, r, "relay")
	inboundID := seedInboundOnNode(t, router, r, admin, landing,
		`{"nodeId":`+jsonInt64(landing)+`,"kernelType":"xray","name":"pp","protocol":"vless","port":443,"security":{"type":"reality"}}`)
	_, forwardID := seedRelayForward(t, r, 1, relayNode, 0, 30007, inboundID)
	security := mustQueryString(t, r, `SELECT security FROM node_inbound WHERE id = ?`, inboundID)
	update := func(extra string) string {
		return `{"id":` + jsonInt64(inboundID) + `,"name":"pp","protocol":"vless","port":443,"security":` + security + extra + `}`
	}

	bad := kernelContractPost(t, router, admin, "/api/v1/inbound/update", update(`,"relayWhitelist":true,"whitelistExtra":"example.com"`))
	if bad.Code == 0 || !strings.Contains(bad.Msg, "白名单") {
		t.Fatalf("hostname in whitelist must be rejected: %+v", bad)
	}

	out := kernelContractPost(t, router, admin, "/api/v1/inbound/update",
		update(`,"acceptProxyProtocol":true,"relayWhitelist":true,"whitelistExtra":"203.0.113.9, 10.0.0.0/8"`))
	if out.Code != 0 {
		t.Fatalf("update: %+v", out)
	}
	if mustQueryInt(t, r, `SELECT relay_only FROM node_inbound WHERE id = ?`, inboundID) != 1 {
		t.Fatal("accepting PROXY protocol must force relay-only")
	}
	if got := mustQueryString(t, r, `SELECT whitelist_extra FROM node_inbound WHERE id = ?`, inboundID); got != "203.0.113.9/32, 10.0.0.0/8" {
		t.Fatalf("whitelist not normalized: %q", got)
	}
	if mustQueryInt(t, r, `SELECT proxy_protocol_send FROM forward WHERE id = ?`, forwardID) != 1 {
		t.Fatal("relay forward must start sending PROXY protocol")
	}

	list := kernelContractPost(t, router, admin, "/api/v1/inbound/list", `{}`)
	item := list.Data.([]interface{})[0].(map[string]interface{})
	if item["acceptProxyProtocol"] != true || item["relayWhitelist"] != true || item["relayOnly"] != true {
		t.Fatalf("list flags: %v", item)
	}

	out = kernelContractPost(t, router, admin, "/api/v1/inbound/update", update(`,"acceptProxyProtocol":false`))
	if out.Code != 0 {
		t.Fatalf("disable: %+v", out)
	}
	if mustQueryInt(t, r, `SELECT proxy_protocol_send FROM forward WHERE id = ?`, forwardID) != 0 {
		t.Fatal("relay forward must stop sending PROXY protocol")
	}
}

func TestPhase3KernelsContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, r := setupContractRouter(t, secret)
	admin, _ := auth.GenerateToken(1, "admin_user", 0, secret)
	nodeID := seedKernelNode(t, r, "landing")

	// Offline node: kernels are registered, versions resolved.
	kernelContractPost(t, router, admin, "/api/v1/kernel/install", `{"nodeId":`+jsonInt64(nodeID)+`,"kernelType":"snell","version":"4.1.1"}`)
	kernelContractPost(t, router, admin, "/api/v1/kernel/install", `{"nodeId":`+jsonInt64(nodeID)+`,"kernelType":"singbox"}`)
	if got := mustQueryString(t, r, `SELECT version FROM node_kernel WHERE kernel_type = 'snell'`); got != "4.1.1" {
		t.Fatalf("snell version: %q", got)
	}
	if got := mustQueryString(t, r, `SELECT version FROM node_kernel WHERE kernel_type = 'singbox'`); got != "latest" {
		t.Fatalf("sing-box version: %q", got)
	}

	create := func(body string) (int64, string) {
		out := kernelContractPost(t, router, admin, "/api/v1/inbound/create", body)
		if out.Code != 0 {
			return 0, out.Msg
		}
		return int64(out.Data.(map[string]interface{})["id"].(float64)), out.Msg
	}
	n := jsonInt64(nodeID)

	if _, msg := create(`{"nodeId":` + n + `,"kernelType":"snell","name":"s5","protocol":"snell","port":6160,"settings":{"snellVersion":5}}`); !strings.Contains(msg, "4.1.1") {
		t.Fatalf("v5 inbound on a 4.1.1 binary must be rejected: %s", msg)
	}
	snellID, msg := create(`{"nodeId":` + n + `,"kernelType":"snell","name":"s4","protocol":"snell","port":6160,"settings":{"snellVersion":4},"security":{"type":"shadowtls","sni":"www.microsoft.com"}}`)
	if snellID == 0 {
		t.Fatalf("snell v4 + shadowtls: %s", msg)
	}
	hyID, msg := create(`{"nodeId":` + n + `,"kernelType":"singbox","name":"hy2","protocol":"hysteria2","port":443,"transport":{"type":"quic"},"security":{"type":"tls"}}`)
	if hyID == 0 {
		t.Fatalf("hysteria2: %s", msg)
	}
	if _, msg := create(`{"nodeId":` + n + `,"kernelType":"singbox","name":"pp","protocol":"vless","port":9443,"acceptProxyProtocol":true,"security":{"type":"reality"}}`); !strings.Contains(msg, "Xray") {
		t.Fatalf("PROXY protocol must be Xray-only: %s", msg)
	}
	if _, msg := create(`{"nodeId":` + n + `,"kernelType":"singbox","name":"dup","protocol":"anytls","port":6160,"security":{"type":"tls"}}`); !strings.Contains(msg, "已被") {
		t.Fatalf("port shared with the snell inbound must be rejected: %s", msg)
	}
	if got := mustQueryString(t, r, `SELECT settings FROM node_inbound WHERE id = ?`, snellID); !strings.Contains(got, `"backendPort"`) {
		t.Fatalf("shadowtls snell needs a backend port: %s", got)
	}

	// Upgrading the binary is fine; downgrading below a v5 inbound is not.
	if out := kernelContractPost(t, router, admin, "/api/v1/inbound/update",
		`{"id":`+jsonInt64(snellID)+`,"name":"s4","protocol":"snell","port":6160,"settings":{"snellVersion":4},"security":{"type":"shadowtls","sni":"www.microsoft.com"}}`); out.Code != 0 {
		t.Fatalf("update: %+v", out)
	}

	share := kernelContractPost(t, router, admin, "/api/v1/inbound/share", `{"id":`+jsonInt64(snellID)+`}`)
	entry := share.Data.([]interface{})[0].(map[string]interface{})
	if entry["uri"] != "" || !strings.Contains(entry["surge"].(string), "shadow-tls-version=3") || !strings.Contains(entry["surge"].(string), "version=4") {
		t.Fatalf("snell share must be a Surge line: %v", entry)
	}
	// Port hopping: the range is reserved on the node like a port.
	hop := func(rng string) string {
		return kernelContractPost(t, router, admin, "/api/v1/inbound/update",
			`{"id":`+jsonInt64(hyID)+`,"name":"hy2","protocol":"hysteria2","port":443,"transport":{"type":"quic"},"security":{"type":"tls"},"settings":{"portHopping":"`+rng+`"}}`).Msg
	}
	if msg := hop("30005-30020"); !strings.Contains(msg, "转发端口段") {
		t.Fatalf("hop range inside the node forwarding range must be rejected: %s", msg)
	}
	if msg := hop("6100-6200"); !strings.Contains(msg, "已被") {
		t.Fatalf("hop range covering the snell port must be rejected: %s", msg)
	}
	if msg := hop("1100-1200"); strings.Contains(msg, "端口") {
		t.Fatalf("free hop range: %s", msg)
	}
	if _, msg := create(`{"nodeId":` + n + `,"kernelType":"singbox","name":"in-hop","protocol":"anytls","port":1150,"security":{"type":"tls"}}`); !strings.Contains(msg, "端口跳跃范围") {
		t.Fatalf("port inside another inbound's hop range must be rejected: %s", msg)
	}
	hyShare := kernelContractPost(t, router, admin, "/api/v1/inbound/share", `{"id":`+jsonInt64(hyID)+`}`)
	if uri := hyShare.Data.([]interface{})[0].(map[string]interface{})["uri"].(string); !strings.HasPrefix(uri, "hysteria2://") || !strings.Contains(uri, "mport=1100-1200") {
		t.Fatalf("hy2 share: %+v", hyShare)
	}

	info := kernelContractPost(t, router, admin, "/api/v1/inbound/sub/info", `{}`)
	token := info.Data.(map[string]interface{})["token"].(string)
	surge := getSub(t, router, "/api/v1/open_api/sub/"+token+"?format=surge", "").Body.String()
	if !strings.Contains(surge, "= snell,") || !strings.Contains(surge, "= hysteria2,") {
		t.Fatalf("surge sub: %s", surge)
	}
	clash := getSub(t, router, "/api/v1/open_api/sub/"+token, "clash-verge").Body.String()
	if strings.Contains(clash, "snell") || !strings.Contains(clash, `type: "hysteria2"`) {
		t.Fatalf("clash sub: %s", clash)
	}
}

func TestACMEAndNaiveContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, r := setupContractRouter(t, secret)
	admin, _ := auth.GenerateToken(1, "admin_user", 0, secret)
	nodeID := seedKernelNode(t, r, "landing")
	oldNode := seedKernelNode(t, r, "old")
	n, o := jsonInt64(nodeID), jsonInt64(oldNode)
	kernelContractPost(t, router, admin, "/api/v1/kernel/install", `{"nodeId":`+n+`,"kernelType":"singbox"}`)
	kernelContractPost(t, router, admin, "/api/v1/kernel/install", `{"nodeId":`+n+`,"kernelType":"xray"}`)
	kernelContractPost(t, router, admin, "/api/v1/kernel/install", `{"nodeId":`+o+`,"kernelType":"singbox","version":"1.12.0"}`)

	create := func(body string) (int64, string) {
		out := kernelContractPost(t, router, admin, "/api/v1/inbound/create", body)
		if out.Code != 0 {
			return 0, out.Msg
		}
		return int64(out.Data.(map[string]interface{})["id"].(float64)), out.Msg
	}
	acme := `"security":{"type":"tls","certMode":"acme","sni":"hk.example.com","acmeDns":"cloudflare","acmeDnsKey":"cf-secret-token"}`

	if _, msg := create(`{"nodeId":` + n + `,"kernelType":"singbox","name":"self","protocol":"naive","port":2443,"security":{"type":"tls","sni":"hk.example.com"}}`); !strings.Contains(msg, "真实证书") {
		t.Fatalf("self-signed naive must be rejected: %s", msg)
	}
	if _, msg := create(`{"nodeId":` + n + `,"kernelType":"xray","name":"x","protocol":"trojan","port":2444,` + acme + `}`); !strings.Contains(msg, "sing-box") {
		t.Fatalf("ACME on Xray must be rejected: %s", msg)
	}
	if _, msg := create(`{"nodeId":` + o + `,"kernelType":"singbox","name":"old","protocol":"naive","port":2443,` + acme + `}`); !strings.Contains(msg, "1.14") {
		t.Fatalf("ACME needs sing-box 1.14+: %s", msg)
	}
	naiveID, msg := create(`{"nodeId":` + n + `,"kernelType":"singbox","name":"naive","protocol":"naive","port":2443,` + acme + `}`)
	if naiveID == 0 {
		t.Fatalf("naive + acme: %s", msg)
	}

	list := kernelContractPost(t, router, admin, "/api/v1/inbound/list", `{}`)
	raw, _ := json.Marshal(list.Data)
	if strings.Contains(string(raw), "cf-secret-token") || !strings.Contains(string(raw), `"acmeDnsSet":true`) {
		t.Fatalf("DNS token must be hidden from the list: %s", raw)
	}
	// Saving without resending the token keeps it.
	if out := kernelContractPost(t, router, admin, "/api/v1/inbound/update",
		`{"id":`+jsonInt64(naiveID)+`,"name":"naive2","protocol":"naive","port":2443,"security":{"type":"tls","certMode":"acme","sni":"hk.example.com","acmeDns":"cloudflare"}}`); out.Code != 0 {
		t.Fatalf("update: %+v", out)
	}
	if got := mustQueryString(t, r, `SELECT security FROM node_inbound WHERE id = ?`, naiveID); !strings.Contains(got, "cf-secret-token") {
		t.Fatalf("token lost on update: %s", got)
	}
	share := kernelContractPost(t, router, admin, "/api/v1/inbound/share", `{"id":`+jsonInt64(naiveID)+`}`)
	uri := share.Data.([]interface{})[0].(map[string]interface{})["uri"].(string)
	if !strings.HasPrefix(uri, "naive+https://") || !strings.Contains(uri, "@203.0.113.10:2443?sni=hk.example.com#") {
		t.Fatalf("naive share: %s", uri)
	}
}
