package contract_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go-backend/internal/auth"
	"go-backend/internal/http/response"
)

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

const contractClashTpl = `dns: {enable: true}
proxies: []
proxy-groups:
  - {name: 🚀 节点选择, type: select, proxies: [🌍 落地, DIRECT]}
  - {name: 🌍 落地, type: url-test, include-all: true, filter: "落地"}
rules:
  - MATCH,🚀 节点选择
`

// Subscription templates: pasted or from a URL, picked per client kind in a
// subscription, rendered into the subscription link, kept while in use.
func TestSubscriptionTemplateContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, r := setupContractRouter(t, secret)
	admin, _ := auth.GenerateToken(1, "admin_user", 0, secret)
	post := func(path, body string) response.R { return kernelContractPost(t, router, admin, path, body) }
	allGroup := jsonInt64(saveNodeGroup(t, router, admin, `{"name":"全部","sourceAll":true}`))

	link := "ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:secret")) + "@198.51.100.20:8388#%E6%97%A5%E6%9C%AC%E8%90%BD%E5%9C%B0"
	if res := post("/api/v1/inbound/import", `{"links":"`+link+`"}`); res.Code != 0 {
		t.Fatalf("import: %+v", res)
	}
	sub := post("/api/v1/subscription/save", `{"name":"我的","groupIds":[`+allGroup+`]}`)
	subID := jsonInt64(int64(sub.Data.(map[string]interface{})["id"].(float64)))
	path := sub.Data.(map[string]interface{})["path"].(string)

	if res := post("/api/v1/subscription/template/save", `{"name":"坏的","kind":"clash","content":"rules: []"}`); res.Code == 0 || !strings.Contains(res.Msg, "proxy-groups") {
		t.Fatalf("an invalid template is rejected: %+v", res)
	}
	saved := post("/api/v1/subscription/template/save", `{"name":"我的分流","kind":"clash","content":`+jsonString(contractClashTpl)+`}`)
	if saved.Code != 0 {
		t.Fatalf("save: %+v", saved)
	}
	tpl := saved.Data.(map[string]interface{})
	clashID := jsonInt64(int64(tpl["id"].(float64)))
	if valueAsInt(tpl["groups"]) != 2 || valueAsInt(tpl["autoGroups"]) != 1 {
		t.Fatalf("template view: %v", tpl)
	}

	check := post("/api/v1/subscription/template/check", `{"kind":"clash","content":`+jsonString(contractClashTpl)+`,"subscriptionId":`+subID+`}`)
	groups := check.Data.(map[string]interface{})["summary"].(map[string]interface{})["groups"].([]interface{})
	if check.Code != 0 || len(groups[1].(map[string]interface{})["nodes"].([]interface{})) != 1 {
		t.Fatalf("check previews the subscription's nodes: %+v", check)
	}

	// A URL template is downloaded on save and again on refresh.
	var mu sync.Mutex
	remote, status := `{"outbounds":[{"type":"selector","tag":"选择","outbounds":["{all}"]},{"type":"direct","tag":"direct"}]}`, http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(remote))
	}))
	defer srv.Close()
	if err := r.UpsertConfig("fetch_allow_local", "true", time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	urlTpl := post("/api/v1/subscription/template/save", `{"name":"网址","kind":"singbox","sourceUrl":"`+srv.URL+`"}`)
	if urlTpl.Code != 0 || !strings.Contains(urlTpl.Data.(map[string]interface{})["content"].(string), "{all}") {
		t.Fatalf("url template: %+v", urlTpl)
	}
	sbID := jsonInt64(int64(urlTpl.Data.(map[string]interface{})["id"].(float64)))

	if res := post("/api/v1/subscription/save", `{"id":`+subID+`,"name":"我的","groupIds":[`+allGroup+`],"options":{"templates":{"singbox":`+clashID+`}}}`); res.Code == 0 {
		t.Fatal("a clash template cannot serve sing-box")
	}
	if res := post("/api/v1/subscription/save", `{"id":`+subID+`,"name":"我的","groupIds":[`+allGroup+`],"options":{"templates":{"clash":`+clashID+`,"singbox":`+sbID+`}}}`); res.Code != 0 {
		t.Fatalf("pick templates: %+v", res)
	}

	clash := getSub(t, router, path+"?format=clash", "").Body.String()
	if !strings.Contains(clash, "🌍 落地") || !strings.Contains(clash, "日本落地") || !strings.Contains(clash, "dns:") {
		t.Fatalf("clash link renders into the template:\n%s", clash)
	}
	singbox := getSub(t, router, path+"?format=singbox", "").Body.String()
	if !strings.Contains(singbox, `"选择"`) || strings.Contains(singbox, "{all}") || !strings.Contains(singbox, "日本落地") {
		t.Fatalf("sing-box link expands {all}:\n%s", singbox)
	}
	if surge := getSub(t, router, path+"?format=surge", "").Body.String(); !strings.Contains(surge, "节点选择 = select") {
		t.Fatalf("surge has no template and keeps the built-in profile:\n%s", surge)
	}

	preview := post("/api/v1/subscription/preview", `{"name":"我的","groupIds":[`+allGroup+`],"options":{"templates":{"clash":`+clashID+`}}}`)
	view := preview.Data.(map[string]interface{})["templates"].(map[string]interface{})["clash"].(map[string]interface{})
	if view["name"] != "我的分流" || len(view["groups"].([]interface{})) != 2 {
		t.Fatalf("preview groups: %v", view)
	}

	if res := post("/api/v1/subscription/template/delete", `{"id":`+clashID+`}`); res.Code == 0 || !strings.Contains(res.Msg, "1 个订阅") {
		t.Fatalf("a template in use is kept: %+v", res)
	}
	if res := post("/api/v1/subscription/template/save", `{"id":`+clashID+`,"name":"x","kind":"surge","content":"[Proxy Group]\na = select, DIRECT\n"}`); res.Code == 0 {
		t.Fatal("a template in use keeps its kind")
	}
	list := post("/api/v1/subscription/template/list", `{}`).Data.([]interface{})
	if len(list) != 2 || valueAsInt(list[0].(map[string]interface{})["usedBy"]) != 1 {
		t.Fatalf("list: %v", list)
	}

	// A failed refresh keeps the last good content.
	mu.Lock()
	status = http.StatusBadGateway
	mu.Unlock()
	if res := post("/api/v1/subscription/template/refresh", `{"id":`+sbID+`}`); res.Code == 0 {
		t.Fatal("refresh must report the failure")
	}
	if singbox := getSub(t, router, path+"?format=singbox", "").Body.String(); !strings.Contains(singbox, `"选择"`) {
		t.Fatalf("the last good template still serves:\n%s", singbox)
	}
	mu.Lock()
	status, remote = http.StatusOK, `{"outbounds":[{"type":"selector","tag":"新组","outbounds":["{all}"]}]}`
	mu.Unlock()
	if res := post("/api/v1/subscription/template/refresh", `{"id":`+sbID+`}`); res.Code != 0 {
		t.Fatalf("refresh: %+v", res)
	}
	if singbox := getSub(t, router, path+"?format=singbox", "").Body.String(); !strings.Contains(singbox, `"新组"`) {
		t.Fatalf("refreshed template:\n%s", singbox)
	}

	// Without the subscription using it, the template can go.
	if res := post("/api/v1/subscription/save", `{"id":`+subID+`,"name":"我的","groupIds":[`+allGroup+`]}`); res.Code != 0 {
		t.Fatal(res.Msg)
	}
	if res := post("/api/v1/subscription/template/delete", `{"id":`+clashID+`}`); res.Code != 0 {
		t.Fatalf("delete: %+v", res)
	}
}

// Stash and Shadowrocket take Mihomo templates, with the auto groups
// written out; a Shadowrocket template makes users import a full config.
func TestSubscriptionTemplateStashShadowrocketContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, _ := setupContractRouter(t, secret)
	admin, _ := auth.GenerateToken(1, "admin_user", 0, secret)
	post := func(path, body string) response.R { return kernelContractPost(t, router, admin, path, body) }
	allGroup := jsonInt64(saveNodeGroup(t, router, admin, `{"name":"全部","sourceAll":true}`))

	link := "ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:secret")) + "@198.51.100.20:8388#%E6%97%A5%E6%9C%AC%E8%90%BD%E5%9C%B0"
	if res := post("/api/v1/inbound/import", `{"links":"`+link+`"}`); res.Code != 0 {
		t.Fatalf("import: %+v", res)
	}
	clashID := jsonInt64(int64(post("/api/v1/subscription/template/save", `{"name":"共用","kind":"clash","content":`+jsonString(contractClashTpl)+`}`).Data.(map[string]interface{})["id"].(float64)))
	sbID := jsonInt64(int64(post("/api/v1/subscription/template/save", `{"name":"sb","kind":"singbox","content":`+jsonString(`{"outbounds":[{"type":"selector","tag":"a","outbounds":["{all}"]}]}`)+`}`).Data.(map[string]interface{})["id"].(float64)))

	if res := post("/api/v1/subscription/save", `{"name":"x","groupIds":[`+allGroup+`],"options":{"templates":{"shadowrocket":`+sbID+`}}}`); res.Code == 0 {
		t.Fatal("shadowrocket takes Mihomo templates only")
	}
	sub := post("/api/v1/subscription/save", `{"name":"我的","groupIds":[`+allGroup+`],"options":{"templates":{"stash":`+clashID+`,"shadowrocket":`+clashID+`}}}`)
	if sub.Code != 0 {
		t.Fatalf("save: %+v", sub)
	}
	path := sub.Data.(map[string]interface{})["path"].(string)
	for _, format := range []string{"stash", "shadowrocket"} {
		body := getSub(t, router, path+"?format="+format, "").Body.String()
		if !strings.Contains(body, "🌍 落地") || strings.Contains(body, "include-all") || !strings.Contains(body, "- \"日本落地\"") {
			t.Fatalf("%s link writes the group out:\n%s", format, body)
		}
	}
	if body := getSub(t, router, path+"?format=clash", "").Body.String(); strings.Contains(body, "🌍 落地") {
		t.Fatalf("mihomo has no template here:\n%s", body)
	}
}
