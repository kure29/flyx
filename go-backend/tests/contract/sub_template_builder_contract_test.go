package contract_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"go-backend/internal/auth"
	"go-backend/internal/http/response"
)

func TestSubscriptionTemplateBuilderContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, _ := setupContractRouter(t, secret)
	admin, _ := auth.GenerateToken(1, "admin_user", 0, secret)
	post := func(path, body string) response.R { return kernelContractPost(t, router, admin, path, body) }
	allGroup := jsonInt64(saveNodeGroup(t, router, admin, `{"name":"全部","sourceAll":true}`))
	catalog := post("/api/v1/subscription/template/catalog", `{}`)
	if catalog.Code != 0 || len(catalog.Data.([]interface{})) != 19 {
		t.Fatalf("catalog: %+v", catalog)
	}
	link := "ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:secret")) + "@198.51.100.20:8388#US-test"
	if res := post("/api/v1/inbound/import", `{"links":"`+link+`"}`); res.Code != 0 {
		t.Fatal(res.Msg)
	}
	sub := post("/api/v1/subscription/save", `{"name":"builder-test","groupIds":[`+allGroup+`]}`)
	subID := jsonInt64(int64(sub.Data.(map[string]interface{})["id"].(float64)))
	path := sub.Data.(map[string]interface{})["path"].(string)
	builder := `{"version":1,"preset":"custom","groups":[{"name":"美国","type":"url-test","filter":"US"}],"rules":[{"type":"domain_suffix","value":"example.com","target":"美国"},{"type":"category","value":"ads","target":"REJECT"}],"final":"节点选择"}`
	for _, kind := range []string{"clash", "singbox", "surge"} {
		t.Run(kind, func(t *testing.T) {
			payload := `"kind":"` + kind + `","content":"untrusted content must be ignored","builder":` + builder
			check := post("/api/v1/subscription/template/check", `{`+payload+`,"subscriptionId":`+subID+`}`)
			if check.Code != 0 || !strings.Contains(check.Data.(map[string]interface{})["content"].(string), "example.com") {
				t.Fatalf("check: %+v", check)
			}
			saved := post("/api/v1/subscription/template/save", `{"name":"visual-`+kind+`",`+payload+`}`)
			if saved.Code != 0 {
				t.Fatal(saved.Msg)
			}
			view := saved.Data.(map[string]interface{})
			id := jsonInt64(int64(view["id"].(float64)))
			if view["builder"] == nil || strings.Contains(view["content"].(string), "untrusted") {
				t.Fatalf("recipe not persisted: %v", view)
			}
			if res := post("/api/v1/subscription/save", `{"id":`+subID+`,"name":"builder-test","groupIds":[`+allGroup+`],"options":{"templates":{"`+kind+`":`+id+`}}}`); res.Code != 0 {
				t.Fatal(res.Msg)
			}
			body := getSub(t, router, path+"?format="+kind, "").Body.String()
			if !strings.Contains(body, "US-test") || !strings.Contains(body, "example.com") || strings.Contains(body, "{all}") {
				t.Fatal(body)
			}
			if kind == "clash" {
				// Visual Mihomo templates also serve the Stash / Shadowrocket slots.
				for _, slot := range []string{"stash", "shadowrocket"} {
					res := post("/api/v1/subscription/save", `{"id":`+subID+`,"name":"builder-test","groupIds":[`+allGroup+`],"options":{"templates":{"`+slot+`":`+id+`}}}`)
					if res.Code != 0 {
						t.Fatal(res.Msg)
					}
					body := getSub(t, router, path+"?format="+slot, "").Body.String()
					if !strings.Contains(body, "example.com") || !strings.Contains(body, "漏网之鱼") || strings.Contains(body, "include-all") || strings.Contains(body, ".mrs") {
						t.Fatalf("visual template not expanded for %s: %s", slot, body)
					}
				}
			}
			list := post("/api/v1/subscription/template/list", `{}`).Data.([]interface{})
			found := false
			for _, item := range list {
				v := item.(map[string]interface{})
				if v["id"] == view["id"] {
					found = v["builder"] != nil
				}
			}
			if !found {
				t.Fatal("list lost the editable recipe")
			}
			// Switching to raw editing clears metadata instead of reopening stale rules.
			raw := post("/api/v1/subscription/template/save", `{"id":`+id+`,"name":"raw","kind":"`+kind+`","content":`+jsonString(view["content"].(string))+`}`)
			if raw.Code != 0 || raw.Data.(map[string]interface{})["builder"] != nil {
				t.Fatalf("raw edit: %+v", raw)
			}
		})
	}
	if res := post("/api/v1/subscription/template/save", `{"name":"invalid","kind":"clash","sourceUrl":"https://example.com","builder":`+builder+`}`); res.Code == 0 {
		t.Fatal("accepted both URL and builder")
	}
	if res := post("/api/v1/subscription/template/check", `{"kind":"clash","builder":`+strings.ReplaceAll(builder, `"ads"`, `"unknown"`)+`}`); res.Code == 0 {
		t.Fatal("accepted unknown category")
	}
}
