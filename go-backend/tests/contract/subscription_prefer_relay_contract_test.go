package contract_test

import (
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"go-backend/internal/auth"
)

func TestSubscriptionPreferRelayContract(t *testing.T) {
	const secret = "prefer-relay-contract"
	router, r := setupContractRouter(t, secret)
	admin, _ := auth.GenerateToken(1, "admin_user", 0, secret)
	landing := seedKernelNode(t, r, "落地")
	entry := seedKernelNode(t, r, "入口")
	if err := r.DB().Exec(`UPDATE node SET server_ip = '198.51.100.7' WHERE id = ?`, entry).Error; err != nil {
		t.Fatal(err)
	}
	a := seedInboundOnNode(t, router, r, admin, landing, `{"nodeId":`+jsonInt64(landing)+`,"kernelType":"xray","name":"同名落地","protocol":"trojan","security":{"type":"tls","sni":"test.example.com"},"port":443}`)
	seedInboundOnNode(t, router, r, admin, landing, `{"nodeId":`+jsonInt64(landing)+`,"kernelType":"xray","name":"同名落地","protocol":"trojan","security":{"type":"tls","sni":"test.example.com"},"port":444}`)
	_, f1 := seedRelayForward(t, r, 1, entry, 0, 30005, a)
	_, f2 := seedRelayForward(t, r, 1, entry, 0, 30006, a)
	// Shared subscriptions replace credentials but retain the inbound identity.
	group := saveNodeGroup(t, router, admin, `{"name":"中转优先","sourceAll":true,"options":{"lines":"prefer_relay"}}`)
	save := kernelContractPost(t, router, admin, "/api/v1/subscription/save", `{"name":"中转优先","groupIds":[`+jsonInt64(group)+`]}`)
	if save.Code != 0 {
		t.Fatalf("save: %+v", save)
	}
	view := save.Data.(map[string]interface{})
	path := view["path"].(string)
	if valueAsInt(view["nodeCount"]) != 3 {
		t.Fatalf("count: %v", view)
	}
	list := kernelContractPost(t, router, admin, "/api/v1/subscription/group/list", `{}`).Data.(map[string]interface{})["items"].([]interface{})
	for _, item := range list {
		v := item.(map[string]interface{})
		if int64(v["id"].(float64)) == group && v["options"].(map[string]interface{})["lines"] != "prefer_relay" {
			t.Fatal("saved option lost on read")
		}
	}
	checkPreview := func(want int) {
		t.Helper()
		p := kernelContractPost(t, router, admin, "/api/v1/subscription/group/preview", `{"sourceAll":true,"options":{"lines":"prefer_relay"}}`)
		if p.Code != 0 || len(p.Data.(map[string]interface{})["items"].([]interface{})) != want {
			t.Fatalf("preview: %+v", p)
		}
	}
	checkBody := func(wantDirect bool, wantPorts ...string) {
		t.Helper()
		res := getSub(t, router, path+"?format=base64", "")
		if res.Code != http.StatusOK {
			t.Fatalf("status %d: %s", res.Code, res.Body.String())
		}
		raw, err := base64.StdEncoding.DecodeString(res.Body.String())
		if err != nil {
			t.Fatal(err)
		}
		body := string(raw)
		if strings.Contains(body, "@203.0.113.10:443?") != wantDirect || !strings.Contains(body, "@203.0.113.10:444?") {
			t.Fatalf("landing filtering: %s", body)
		}
		wantCount := 1 + len(wantPorts)
		if wantDirect {
			wantCount++
		}
		if strings.Count(body, "trojan://") != wantCount {
			t.Fatalf("count: %s", body)
		}
		for _, port := range wantPorts {
			if !strings.Contains(body, "@198.51.100.7:"+port+"?") {
				t.Fatalf("missing relay %s: %s", port, body)
			}
		}
	}
	checkPreview(3)
	checkBody(false, "30005", "30006")
	// All native renderers use the same reduced endpoints.
	for _, format := range []string{"clash", "singbox", "surge", "stash"} {
		res := getSub(t, router, path+"?format="+format, "")
		if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "30005") || !strings.Contains(res.Body.String(), "30006") {
			t.Fatalf("%s: %d %s", format, res.Code, res.Body.String())
		}
	}
	// An unrelated subscription keeps both direct and relay endpoints.
	ordinary := kernelContractPost(t, router, admin, "/api/v1/subscription/group/preview", `{"sourceAll":true,"options":{}}`)
	if len(ordinary.Data.(map[string]interface{})["items"].([]interface{})) != 4 {
		t.Fatal("other subscriptions changed")
	}
	// Pausing one line leaves the other; pausing both restores the original.
	if err := r.DB().Exec(`UPDATE forward SET status = 0 WHERE id = ?`, f1).Error; err != nil {
		t.Fatal(err)
	}
	checkPreview(2)
	checkBody(false, "30006")
	if err := r.DB().Exec(`UPDATE forward SET status = 0 WHERE id = ?`, f2).Error; err != nil {
		t.Fatal(err)
	}
	checkPreview(2)
	checkBody(true)
	// A configured enabled line still counts when the agent is offline.
	if err := r.DB().Exec(`UPDATE forward SET status = 1 WHERE id = ?`, f1).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.DB().Exec(`UPDATE node SET status = 0 WHERE id = ?`, entry).Error; err != nil {
		t.Fatal(err)
	}
	checkBody(false, "30005")
	if err := r.DB().Exec(`DELETE FROM node_inbound_relay WHERE forward_id = ?`, f1).Error; err != nil {
		t.Fatal(err)
	}
	checkBody(true)
}
