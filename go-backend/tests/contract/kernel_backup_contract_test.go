package contract_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-backend/internal/auth"
)

func TestInboundBackupRoundTripContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, r := setupContractRouter(t, secret)
	admin, _ := auth.GenerateToken(1, "admin_user", 0, secret)
	landing := seedKernelNode(t, r, "landing")
	relayNode := seedKernelNode(t, r, "relay")

	inboundID := seedInboundOnNode(t, router, r, admin, landing,
		`{"nodeId":`+jsonInt64(landing)+`,"kernelType":"xray","name":"东京","protocol":"vless","port":443,"settings":{"flow":"xtls-rprx-vision"},"security":{"type":"reality"}}`)
	link := "ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:secret")) + "@198.51.100.20:8388#ext"
	if out := kernelContractPost(t, router, admin, "/api/v1/inbound/import", `{"links":"`+link+`"}`); out.Code != 0 {
		t.Fatalf("import: %+v", out)
	}
	_, forwardID := seedRelayForward(t, r, 1, relayNode, 0, 30005, inboundID)
	settingsBefore := mustQueryString(t, r, `SELECT settings FROM node_inbound WHERE id = ?`, inboundID)
	group := saveNodeGroup(t, router, admin, `{"name":"东京组","inboundIds":[`+jsonInt64(inboundID)+`],"options":{"prefix":"[JP] "}}`)
	if out := kernelContractPost(t, router, admin, "/api/v1/subscription/save", `{"name":"备份订阅","groupIds":[`+jsonInt64(group)+`]}`); out.Code != 0 {
		t.Fatalf("subscription: %+v", out)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/backup/export", bytes.NewBufferString(`{"types":["inbounds"]}`))
	req.Header.Set("Authorization", admin)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	var backup map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&backup); err != nil {
		t.Fatal(err)
	}
	section, _ := backup["inbounds"].(map[string]interface{})
	if section == nil || len(section["kernels"].([]interface{})) != 1 || len(section["inbounds"].([]interface{})) != 2 ||
		len(section["relays"].([]interface{})) != 1 {
		t.Fatalf("export must carry kernels, inbounds (imported too) and relays: %v", backup["inbounds"])
	}

	if groups, _ := section["nodeGroups"].([]interface{}); len(groups) < 2 { // 我的订阅's group and 东京组
		t.Fatalf("export must carry node groups: %v", section["nodeGroups"])
	}
	for _, table := range []string{"node_inbound_relay", "node_inbound", "node_kernel", "node_group"} {
		if err := r.DB().Exec(`DELETE FROM ` + table).Error; err != nil {
			t.Fatal(err)
		}
	}

	backup["types"] = []string{"inbounds"}
	raw, _ := json.Marshal(backup)
	out := kernelContractPost(t, router, admin, "/api/v1/backup/import", string(raw))
	if out.Code != 0 || valueAsInt(out.Data.(map[string]interface{})["inboundsImported"]) != 2 {
		t.Fatalf("import backup: %+v", out)
	}
	if mustQueryInt(t, r, `SELECT COUNT(*) FROM node_inbound`) != 2 ||
		mustQueryString(t, r, `SELECT settings FROM node_inbound WHERE id = ?`, inboundID) != settingsBefore {
		t.Fatal("inbounds must come back unchanged")
	}
	if mustQueryInt(t, r, `SELECT COUNT(*) FROM node_inbound_relay WHERE inbound_id = ? AND forward_id = ?`, inboundID, forwardID) != 1 {
		t.Fatal("the relay link must come back")
	}
	if mustQueryString(t, r, `SELECT options FROM node_group WHERE id = ?`, group) != `{"lines":"","regions":[],"protocols":[],"include":"","exclude":"","rename":"","addFlag":false,"prefix":"[JP] ","dedupe":false,"sort":"","script":""}` ||
		mustQueryString(t, r, `SELECT group_ids FROM subscription WHERE name = '备份订阅'`) != jsonInt64(group) {
		t.Fatal("node groups come back with their ids, still used by the subscription")
	}
	if mustQueryString(t, r, `SELECT applied_hash FROM node_kernel WHERE node_id = ?`, landing) != "" {
		t.Fatal("a restored kernel must be pushed to its node again")
	}

	// Importing the same backup twice upserts instead of duplicating.
	if out := kernelContractPost(t, router, admin, "/api/v1/backup/import", string(raw)); out.Code != 0 {
		t.Fatalf("re-import: %+v", out)
	}
	if mustQueryInt(t, r, `SELECT COUNT(*) FROM node_inbound`) != 2 || mustQueryInt(t, r, `SELECT COUNT(*) FROM node_kernel`) != 1 ||
		mustQueryInt(t, r, `SELECT COUNT(*) FROM node_group`) != 2 {
		t.Fatal("re-import must not duplicate rows")
	}
}
