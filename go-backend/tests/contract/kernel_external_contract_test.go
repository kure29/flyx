package contract_test

import (
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-backend/internal/auth"
)

func TestExternalInboundImportContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, r := setupContractRouter(t, secret)
	admin, _ := auth.GenerateToken(1, "admin_user", 0, secret)
	relayNode := seedKernelNode(t, r, "relay")

	good := "ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:secret")) + "@198.51.100.20:8388#%E8%90%BD%E5%9C%B0"
	links := good + `\nvmess://abc\n\n`
	out := kernelContractPost(t, router, admin, "/api/v1/inbound/import", `{"links":"`+links+`"}`)
	if out.Code != 0 {
		t.Fatalf("import: %+v", out)
	}
	data := out.Data.(map[string]interface{})
	if len(data["created"].([]interface{})) != 1 || len(data["errors"].([]interface{})) != 1 ||
		!strings.Contains(data["errors"].([]interface{})[0].(string), "第 2 行") {
		t.Fatalf("one link imported, one rejected with its line: %v", data)
	}
	id := int64(data["created"].([]interface{})[0].(float64))

	dup := kernelContractPost(t, router, admin, "/api/v1/inbound/import", `{"links":"`+good+`"}`)
	if dup.Code == 0 || !strings.Contains(dup.Msg, "已经导入过") {
		t.Fatalf("re-importing the same address must be skipped: %+v", dup)
	}

	list := kernelContractPost(t, router, admin, "/api/v1/inbound/list", `{}`)
	item := list.Data.([]interface{})[0].(map[string]interface{})
	if item["external"] != true || item["name"] != "落地" || item["listen"] != "198.51.100.20" || valueAsInt(item["port"]) != 8388 {
		t.Fatalf("list item: %v", item)
	}

	share := kernelContractPost(t, router, admin, "/api/v1/inbound/share", `{"id":`+jsonInt64(id)+`}`)
	entry := share.Data.([]interface{})[0].(map[string]interface{})
	if !strings.HasPrefix(entry["uri"].(string), "ss://") || entry["host"] != "198.51.100.20" {
		t.Fatalf("direct share: %v", entry)
	}

	// A port-forward relay points the forward at the imported address.
	_, forwardID := seedRelayForward(t, r, 1, relayNode, 0, 30005, id)
	sub := kernelContractPost(t, router, admin, "/api/v1/inbound/sub/info", `{}`)
	token := sub.Data.(map[string]interface{})["token"].(string)
	res := getSub(t, router, "/api/v1/open_api/sub/"+token+"?format=clash", "")
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "198.51.100.20") || !strings.Contains(res.Body.String(), "30005") {
		t.Fatalf("subscription must carry the direct and relay lines:\n%s", res.Body.String())
	}

	update := func(body string) string {
		return kernelContractPost(t, router, admin, "/api/v1/inbound/external/update", body).Msg
	}
	if msg := update(`{"id":` + jsonInt64(id) + `,"name":"x","host":"127.0.0.1","port":1,"method":"aes-256-gcm","password":"p"}`); !strings.Contains(msg, "本机") {
		t.Fatalf("loopback address must be rejected: %s", msg)
	}
	if msg := update(`{"id":` + jsonInt64(id) + `,"name":"x","host":"198.51.100.20","port":8388,"link":"vmess://bad"}`); !strings.Contains(msg, "vmess") {
		t.Fatalf("a bad replacement link must be rejected: %s", msg)
	}
	// Replacing the parameters with a new link moves the address too.
	newLink := "trojan://p2@198.51.100.21:9000?sni=t.example.com#x"
	if msg := update(`{"id":` + jsonInt64(id) + `,"name":"新落地","host":"198.51.100.20","port":8388,"link":"` + newLink + `","relayOnly":true}`); msg != "操作成功" {
		t.Fatalf("replace: %s", msg)
	}
	item = kernelContractPost(t, router, admin, "/api/v1/inbound/list", `{}`).Data.([]interface{})[0].(map[string]interface{})
	if item["protocol"] != "trojan" || item["importedSummary"] != "Trojan" || item["name"] != "新落地" {
		t.Fatalf("replaced node: %v", item)
	}
	if got := mustQueryString(t, r, `SELECT remote_addr FROM forward WHERE id = ?`, forwardID); got != "198.51.100.21:9000" {
		t.Fatalf("relay forward must follow the new address, got %q", got)
	}
	res = getSub(t, router, "/api/v1/open_api/sub/"+token+"?format=clash", "")
	if strings.Contains(res.Body.String(), "server: 198.51.100.21") {
		t.Fatalf("relay-only node must not have a direct line:\n%s", res.Body.String())
	}

	if out := kernelContractPost(t, router, admin, "/api/v1/inbound/update", `{"id":`+jsonInt64(id)+`,"name":"x","protocol":"shadowsocks","port":1}`); out.Code == 0 {
		t.Fatal("kernel update path must refuse imported nodes")
	}
	if out := kernelContractPost(t, router, admin, "/api/v1/inbound/regenerate", `{"id":`+jsonInt64(id)+`}`); out.Code == 0 {
		t.Fatal("imported nodes have no keys to regenerate")
	}
	if out := kernelContractPost(t, router, admin, "/api/v1/inbound/toggle", `{"id":`+jsonInt64(id)+`,"enabled":false}`); out.Code != 0 || out.Msg != "操作成功" {
		t.Fatalf("toggle: %+v", out)
	}
	if out := kernelContractPost(t, router, admin, "/api/v1/inbound/delete", `{"id":`+jsonInt64(id)+`}`); out.Code != 0 || out.Msg != "操作成功" {
		t.Fatalf("delete: %+v", out)
	}
	if mustQueryInt(t, r, `SELECT COUNT(*) FROM node_inbound_relay`) != 0 {
		t.Fatal("deleting the node must drop its relays")
	}
}

func TestExternalImportFromSubscriptionURLContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, r := setupContractRouter(t, secret)
	admin, _ := auth.GenerateToken(1, "admin_user", 0, secret)
	relayNode := seedKernelNode(t, r, "relay")

	var gotUA string
	sub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotUA = req.UserAgent()
		_, _ = w.Write([]byte("proxies:\n  - {name: A, type: trojan, server: a.example.com, port: 443, password: pw}\n  - {name: B, type: hysteria2, server: 198.51.100.30, port: 443, password: pw, sni: b.example.com}\n"))
	}))
	defer sub.Close()
	if err := r.UpsertConfig("fetch_allow_local", "true", time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}

	out := kernelContractPost(t, router, admin, "/api/v1/inbound/import", `{"links":"`+sub.URL+`/sub","relayOnly":true}`)
	if out.Code != 0 || len(out.Data.(map[string]interface{})["created"].([]interface{})) != 2 {
		t.Fatalf("import from URL: %+v", out)
	}
	if !strings.Contains(gotUA, "clash") {
		t.Fatalf("subscriptions are fetched as Mihomo to get full parameters: %q", gotUA)
	}

	list := kernelContractPost(t, router, admin, "/api/v1/inbound/list", `{}`).Data.([]interface{})
	var trojanID int64
	for _, it := range list {
		m := it.(map[string]interface{})
		if m["relayOnly"] != true {
			t.Fatalf("relay-only must be stored: %v", m)
		}
		if m["name"] == "A" {
			trojanID = int64(m["id"].(float64))
		}
	}
	seedRelayForward(t, r, 1, relayNode, 0, 30009, trojanID)

	token := kernelContractPost(t, router, admin, "/api/v1/inbound/sub/info", `{}`).Data.(map[string]interface{})["token"].(string)
	body := getSub(t, router, "/api/v1/open_api/sub/"+token+"?format=clash", "").Body.String()
	if strings.Contains(body, "server: \"a.example.com\"") || strings.Contains(body, "198.51.100.30") {
		t.Fatalf("relay-only nodes must not have direct lines:\n%s", body)
	}
	if !strings.Contains(body, "30009") || !strings.Contains(body, `sni: "a.example.com"`) {
		t.Fatalf("the relay line must keep the original domain as SNI:\n%s", body)
	}

	ids := make([]string, 0, len(list))
	for _, it := range list {
		ids = append(ids, jsonInt64(int64(it.(map[string]interface{})["id"].(float64))))
	}
	out = kernelContractPost(t, router, admin, "/api/v1/inbound/batch-toggle", `{"ids":[`+strings.Join(ids, ",")+`],"enabled":false}`)
	if out.Code != 0 || valueAsInt(out.Data.(map[string]interface{})["successCount"]) != 2 ||
		mustQueryInt(t, r, `SELECT COUNT(*) FROM node_inbound WHERE enabled = 1`) != 0 {
		t.Fatalf("batch disable: %+v", out)
	}
	out = kernelContractPost(t, router, admin, "/api/v1/inbound/batch-delete", `{"ids":[`+strings.Join(ids, ",")+`,999999]}`)
	res := out.Data.(map[string]interface{})
	if out.Code != 0 || valueAsInt(res["successCount"]) != 2 || valueAsInt(res["failCount"]) != 1 {
		t.Fatalf("batch delete: %+v", out)
	}
	if mustQueryInt(t, r, `SELECT COUNT(*) FROM node_inbound`) != 0 || mustQueryInt(t, r, `SELECT COUNT(*) FROM node_inbound_relay`) != 0 {
		t.Fatal("batch delete must drop the nodes and their relays")
	}
}

func TestExternalProbeContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, r := setupContractRouter(t, secret)
	admin, _ := auth.GenerateToken(1, "admin_user", 0, secret)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	closed, _ := net.Listen("tcp", "127.0.0.1:0")
	deadPort := closed.Addr().(*net.TCPAddr).Port
	_ = closed.Close()

	userinfo := base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:secret"))
	links := "ss://" + userinfo + "@198.51.100.40:1001#up\\n" + "ss://" + userinfo + "@198.51.100.41:1002#down"
	if out := kernelContractPost(t, router, admin, "/api/v1/inbound/import", `{"links":"`+links+`"}`); out.Code != 0 {
		t.Fatalf("import: %+v", out)
	}
	// Import refuses loopback addresses, so point the rows at local ports.
	upPort := ln.Addr().(*net.TCPAddr).Port
	for name, port := range map[string]int{"up": upPort, "down": deadPort} {
		if err := r.DB().Exec(`UPDATE node_inbound SET listen = '127.0.0.1', port = ? WHERE name = ?`, port, name).Error; err != nil {
			t.Fatal(err)
		}
	}

	out := kernelContractPost(t, router, admin, "/api/v1/inbound/probe", `{}`)
	if out.Code != 0 || len(out.Data.(map[string]interface{})) != 2 {
		t.Fatalf("probe: %+v", out)
	}
	byName := map[string]map[string]interface{}{}
	for _, it := range kernelContractPost(t, router, admin, "/api/v1/inbound/list", `{}`).Data.([]interface{}) {
		m := it.(map[string]interface{})
		probe, _ := m["probe"].(map[string]interface{})
		if probe == nil {
			t.Fatalf("imported nodes must carry the probe result: %v", m)
		}
		byName[m["name"].(string)] = probe
	}
	if byName["up"]["ok"] != true || valueAsInt(byName["up"]["latencyMs"]) < 1 {
		t.Fatalf("reachable node: %v", byName["up"])
	}
	if byName["down"]["ok"] == true || byName["down"]["error"] == "" {
		t.Fatalf("closed port: %v", byName["down"])
	}
}

func TestBuiltInboundProbeContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, r := setupContractRouter(t, secret)
	admin, _ := auth.GenerateToken(1, "admin_user", 0, secret)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	upPort := ln.Addr().(*net.TCPAddr).Port
	closed, _ := net.Listen("tcp", "127.0.0.1:0")
	deadPort := closed.Addr().(*net.TCPAddr).Port
	_ = closed.Close()

	nodeID := seedKernelNode(t, r, "probe-node")
	offlineNodeID := seedKernelNode(t, r, "probe-node-offline")
	if err := r.DB().Exec(`UPDATE node SET server_ip = '127.0.0.1', status = 1 WHERE id = ?`, nodeID).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.DB().Exec(`UPDATE node SET server_ip = '127.0.0.1' WHERE id = ?`, offlineNodeID).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	kernel := func(nodeID int64, kernelType, status string) int64 {
		if err := r.DB().Exec(`INSERT INTO node_kernel(node_id, kernel_type, version, status, created_time, updated_time) VALUES(?, ?, 'latest', ?, ?, ?)`, nodeID, kernelType, status, now, now).Error; err != nil {
			t.Fatal(err)
		}
		return mustLastInsertID(t, r, "node_kernel")
	}
	running := kernel(nodeID, "xray", "running")
	stopped := kernel(nodeID, "sing-box", "stopped")
	offline := kernel(offlineNodeID, "xray", "running")

	type inboundRow struct {
		name, protocol, listen, shareHost string
		port, whitelist                   int
		kernelID, nodeID                  int64
	}
	ids := map[string]int64{}
	for _, row := range []inboundRow{
		// Dialed at the node's server IP.
		{"node-ip", "vless", "::", "", upPort, 0, running, nodeID},
		// Dialed at the share host.
		{"share-host", "vless", "::", "127.0.0.1", deadPort, 0, running, nodeID},
		// Skipped.
		{"whitelisted", "vless", "::", "", upPort, 1, running, nodeID},
		{"loopback", "vless", "127.0.0.1", "", upPort, 0, running, nodeID},
		{"udp-only", "hysteria2", "::", "", upPort, 0, running, nodeID},
		{"kernel-stopped", "vless", "::", "", upPort, 0, stopped, nodeID},
		{"node-offline", "vless", "::", "", upPort, 0, offline, offlineNodeID},
	} {
		if err := r.DB().Exec(`INSERT INTO node_inbound(kernel_id, node_id, name, protocol, listen, port, share_host, relay_whitelist, created_time, updated_time) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			row.kernelID, row.nodeID, row.name, row.protocol, row.listen, row.port, row.shareHost, row.whitelist, now, now).Error; err != nil {
			t.Fatal(err)
		}
		ids[row.name] = mustLastInsertID(t, r, row.name)
	}

	out := kernelContractPost(t, router, admin, "/api/v1/inbound/probe", `{}`)
	if out.Code != 0 {
		t.Fatalf("probe: %+v", out)
	}
	results := out.Data.(map[string]interface{})
	get := func(name string) map[string]interface{} {
		p, _ := results[jsonInt64(ids[name])].(map[string]interface{})
		if p == nil {
			t.Fatalf("built inbound %q has no probe result: %v", name, results)
		}
		return p
	}
	if p := get("node-ip"); p["ok"] != true || valueAsInt(p["latencyMs"]) < 1 {
		t.Fatalf("inbound reachable at the node's server IP: %v", p)
	}
	if p := get("share-host"); p["ok"] == true || p["error"] == "" {
		t.Fatalf("inbound probed at its share host must fail on a closed port: %v", p)
	}
	for _, name := range []string{"whitelisted", "loopback", "udp-only", "kernel-stopped", "node-offline"} {
		if p := get(name); p["skipped"] != true {
			t.Fatalf("%s inbound must be skipped: %v", name, p)
		}
	}

	for _, it := range kernelContractPost(t, router, admin, "/api/v1/inbound/list", `{}`).Data.([]interface{}) {
		m := it.(map[string]interface{})
		if m["name"] == "node-ip" {
			if probe, _ := m["probe"].(map[string]interface{}); probe == nil || probe["ok"] != true {
				t.Fatalf("list must carry the built inbound's probe: %v", m)
			}
			return
		}
	}
	t.Fatal("node-ip inbound missing from the list")
}
