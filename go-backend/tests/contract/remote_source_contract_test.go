package contract_test

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"go-backend/internal/auth"
)

func ssLink(host, port, name string) string {
	cred := base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:pw"))
	return "ss://" + cred + "@" + host + ":" + port + "#" + name
}

func TestRemoteSourceContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, r := setupContractRouter(t, secret)
	admin, _ := auth.GenerateToken(1, "admin_user", 0, secret)

	var mu sync.Mutex
	body := strings.Join([]string{ssLink("203.0.113.1", "8388", "香港 01"), ssLink("203.0.113.2", "8388", "日本 01")}, "\n")
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		gotUA = req.UserAgent()
		w.Header().Set("Subscription-Userinfo", "upload=1; download=2; total=3; expire=4")
		_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(body))))
	}))
	defer srv.Close()
	serve := func(lines ...string) {
		mu.Lock()
		body = strings.Join(lines, "\n")
		mu.Unlock()
	}

	if out := kernelContractPost(t, router, admin, "/api/v1/subscription/source/save", `{"name":"机场","url":"ftp://x"}`); out.Code == 0 {
		t.Fatal("non-http url must be rejected")
	}
	// The test server is on loopback: refused until the panel allows it.
	if out := kernelContractPost(t, router, admin, "/api/v1/subscription/source/save",
		`{"name":"机场","url":"`+srv.URL+`"}`); out.Code == 0 || !strings.Contains(out.Msg, "拉取订阅可访问内网地址") {
		t.Fatalf("loopback source must be refused: %+v", out)
	}
	if err := r.UpsertConfig("fetch_allow_local", "true", time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	out := kernelContractPost(t, router, admin, "/api/v1/subscription/source/save",
		`{"name":"机场","url":"`+srv.URL+`","intervalHours":12}`)
	data := out.Data.(map[string]interface{})
	src := data["source"].(map[string]interface{})
	srcID := int64(src["id"].(float64))
	if out.Code != 0 || valueAsInt(src["nodeCount"]) != 2 || src["userinfo"] != "upload=1; download=2; total=3; expire=4" {
		t.Fatalf("create: %+v", out)
	}
	hk := mustQueryInt64(t, r, `SELECT id FROM node_inbound WHERE name = '香港 01' AND source_id = ?`, srcID)
	if gotUA != "clash.meta/1.19.0" || src["userAgent"] != "" {
		t.Fatalf("default user agent: %q %v", gotUA, src["userAgent"])
	}

	// A source can send its own User-Agent; changing it fetches again.
	if out := kernelContractPost(t, router, admin, "/api/v1/subscription/source/save",
		`{"id":`+jsonInt64(srcID)+`,"name":"机场","url":"`+srv.URL+`","intervalHours":12,"userAgent":"a\nb"}`); out.Code == 0 {
		t.Fatal("a multi-line user agent must be rejected")
	}
	out = kernelContractPost(t, router, admin, "/api/v1/subscription/source/save",
		`{"id":`+jsonInt64(srcID)+`,"name":"机场","url":"`+srv.URL+`","intervalHours":12,"userAgent":" Shadowrocket/2070 "}`)
	if out.Code != 0 || gotUA != "Shadowrocket/2070" || out.Data.(map[string]interface{})["source"].(map[string]interface{})["userAgent"] != "Shadowrocket/2070" {
		t.Fatalf("custom user agent: %q %+v", gotUA, out)
	}
	gotUA = ""
	if out := kernelContractPost(t, router, admin, "/api/v1/subscription/source/refresh", `{"id":`+jsonInt64(srcID)+`}`); out.Code != 0 || gotUA != "Shadowrocket/2070" {
		t.Fatalf("refresh keeps the user agent: %q %+v", gotUA, out)
	}

	// Inbound list names the source; a subscription can take the whole source.
	list := kernelContractPost(t, router, admin, "/api/v1/inbound/list", `{}`).Data.([]interface{})
	if len(list) != 2 || list[0].(map[string]interface{})["sourceName"] != "机场" {
		t.Fatalf("inbound list: %v", list)
	}
	group := saveNodeGroup(t, router, admin, `{"name":"机场","sourceAll":false,"inboundIds":[],"sourceIds":[`+jsonInt64(srcID)+`]}`)
	sub := kernelContractPost(t, router, admin, "/api/v1/subscription/save", `{"name":"合并","groupIds":[`+jsonInt64(group)+`]}`)
	subView := sub.Data.(map[string]interface{})
	if sub.Code != 0 || valueAsInt(subView["nodeCount"]) != 2 {
		t.Fatalf("subscription from source: %+v", sub)
	}

	// The provider moves 香港 01, drops 日本 01 and adds 美国 01.
	serve(ssLink("203.0.113.9", "8388", "香港 01"), ssLink("203.0.113.3", "443", "美国 01"))
	out = kernelContractPost(t, router, admin, "/api/v1/subscription/source/refresh", `{"id":`+jsonInt64(srcID)+`}`)
	res := out.Data.(map[string]interface{})["result"].(map[string]interface{})
	if out.Code != 0 || valueAsInt(res["added"]) != 1 || valueAsInt(res["updated"]) != 1 || valueAsInt(res["removed"]) != 1 {
		t.Fatalf("refresh: %+v", out)
	}
	if got := mustQueryString(t, r, `SELECT listen FROM node_inbound WHERE id = ?`, hk); got != "203.0.113.9" {
		t.Fatalf("moved node must keep its id and get the new address, got %q", got)
	}
	// A renamed node keeps the user's name across refreshes, even when the
	// provider renames it; untouched nodes follow the provider's name.
	us := mustQueryInt64(t, r, `SELECT id FROM node_inbound WHERE name = '美国 01' AND source_id = ?`, srcID)
	if out := kernelContractPost(t, router, admin, "/api/v1/inbound/external/update",
		`{"id":`+jsonInt64(hk)+`,"name":"我的香港","host":"203.0.113.9","port":8388}`); out.Code != 0 {
		t.Fatalf("rename: %+v", out)
	}
	for _, hkName := range []string{"香港 01", "香港 IPLC"} {
		serve(ssLink("203.0.113.9", "8388", hkName), ssLink("203.0.113.3", "443", "美国 02"))
		if out := kernelContractPost(t, router, admin, "/api/v1/subscription/source/refresh", `{"id":`+jsonInt64(srcID)+`}`); out.Code != 0 {
			t.Fatalf("refresh after rename: %+v", out)
		}
		if got := mustQueryString(t, r, `SELECT name FROM node_inbound WHERE id = ?`, hk); got != "我的香港" {
			t.Fatalf("refresh reverted the rename to %q", got)
		}
		if got := mustQueryString(t, r, `SELECT name FROM node_inbound WHERE id = ?`, us); got != "美国 02" {
			t.Fatalf("untouched node must follow the provider, got %q", got)
		}
	}
	subs := kernelContractPost(t, router, admin, "/api/v1/subscription/list", `{}`).Data.(map[string]interface{})["items"].([]interface{})
	for _, it := range subs {
		if v := it.(map[string]interface{}); v["name"] == "合并" && valueAsInt(v["nodeCount"]) != 2 {
			t.Fatalf("new source nodes follow into the subscription: %v", v)
		}
	}

	// An empty answer keeps the nodes and records the error.
	serve("")
	if out := kernelContractPost(t, router, admin, "/api/v1/subscription/source/refresh", `{"id":`+jsonInt64(srcID)+`}`); out.Code == 0 {
		t.Fatal("empty subscription must fail")
	}
	if n := mustQueryInt(t, r, `SELECT COUNT(*) FROM node_inbound WHERE source_id = ?`, srcID); n != 2 {
		t.Fatalf("nodes kept on a failed refresh, got %d", n)
	}
	src = kernelContractPost(t, router, admin, "/api/v1/subscription/source/list", `{}`).Data.([]interface{})[0].(map[string]interface{})
	if src["lastError"] == "" {
		t.Fatal("refresh error must be recorded")
	}

	if out := kernelContractPost(t, router, admin, "/api/v1/subscription/source/delete", `{"id":`+jsonInt64(srcID)+`,"keepNodes":true}`); out.Code != 0 {
		t.Fatalf("delete: %+v", out)
	}
	if n := mustQueryInt(t, r, `SELECT COUNT(*) FROM node_inbound WHERE source_id = 0`); n != 2 {
		t.Fatalf("keepNodes leaves plain imports, got %d", n)
	}
}

// An airport uploaded as a file is the relay of a chain by source: its
// nodes stay relays across renames and stay out of the node lists.
func TestFileSourceChainContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, _ := setupContractRouter(t, secret)
	admin, _ := auth.GenerateToken(1, "admin_user", 0, secret)
	save := func(body string) map[string]interface{} {
		out := kernelContractPost(t, router, admin, "/api/v1/subscription/source/save", body)
		if out.Code != 0 {
			t.Fatalf("save %s: %+v", body, out)
		}
		return out.Data.(map[string]interface{})["source"].(map[string]interface{})
	}
	quote := func(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), "\n", `\n`) }

	if out := kernelContractPost(t, router, admin, "/api/v1/subscription/source/save", `{"name":"机场","kind":"file"}`); out.Code == 0 {
		t.Fatal("a file source needs a file")
	}
	airport := save(`{"name":"机场","kind":"file","role":"relay","url":"https://ignored.test","intervalHours":6,"content":"` +
		quote(ssLink("203.0.113.1", "8388", "香港 01")+"\n"+ssLink("203.0.113.2", "8388", "日本 01")) + `"}`)
	if airport["kind"] != "file" || airport["role"] != "relay" || airport["url"] != "" || valueAsInt(airport["intervalHours"]) != 0 || valueAsInt(airport["nodeCount"]) != 2 {
		t.Fatalf("file source: %v", airport)
	}
	airportID := jsonInt64(int64(airport["id"].(float64)))
	landing := save(`{"name":"落地","kind":"file","role":"landing","content":"` + quote(ssLink("198.51.100.1", "8388", "美国 落地")) + `"}`)
	if out := kernelContractPost(t, router, admin, "/api/v1/subscription/source/refresh", `{"id":`+airportID+`}`); out.Code == 0 {
		t.Fatal("a file source is not refreshed from a URL")
	}

	// The chain picks relays and landings among its node groups' nodes.
	landingID := jsonInt64(int64(landing["id"].(float64)))
	chainGroup := jsonInt64(saveNodeGroup(t, router, admin, `{"name":"机场+落地","sourceIds":[`+airportID+`,`+landingID+`]}`))
	sub := kernelContractPost(t, router, admin, "/api/v1/subscription/save", `{"name":"链式","groupIds":[`+chainGroup+`],"options":{`+
		`"chainRelaySources":[`+airportID+`],"chainLandingSources":[`+jsonInt64(int64(landing["id"].(float64)))+`],"chainNoneNames":["日本 01"],"chainHideRelays":true}}`)
	if sub.Code != 0 || valueAsInt(sub.Data.(map[string]interface{})["nodeCount"]) != 3 {
		t.Fatalf("chain over the group's nodes: %+v", sub)
	}
	path := sub.Data.(map[string]interface{})["path"].(string)
	check := func(relay string) {
		t.Helper()
		var cfg struct {
			Proxies []map[string]interface{} `yaml:"proxies"`
			Groups  []struct {
				Name    string   `yaml:"name"`
				Proxies []string `yaml:"proxies"`
			} `yaml:"proxy-groups"`
		}
		if err := yaml.Unmarshal(getSub(t, router, path+"?format=clash", "").Body.Bytes(), &cfg); err != nil {
			t.Fatal(err)
		}
		groups := map[string][]string{}
		for _, g := range cfg.Groups {
			groups[g.Name] = g.Proxies
		}
		if strings.Join(groups["中转节点"], ",") != relay || strings.Join(groups["落地节点"], ",") != "美国 落地" {
			t.Fatalf("chain groups: %v", groups)
		}
		if strings.Contains(strings.Join(groups["节点选择"], ","), relay) || !strings.Contains(strings.Join(groups["节点选择"], ","), "日本 01") {
			t.Fatalf("relays stay out of the node list, 不用 nodes stay in: %v", groups["节点选择"])
		}
		for _, p := range cfg.Proxies {
			if p["name"] == "美国 落地" && p["dialer-proxy"] != "中转节点" {
				t.Fatalf("landing dials through the relays: %v", p)
			}
		}
	}
	check("香港 01")
	// The airport renames its node; it is still a relay.
	save(`{"id":` + airportID + `,"name":"机场","kind":"file","role":"relay","content":"` +
		quote(ssLink("203.0.113.1", "8388", "香港 01 2x")+"\n"+ssLink("203.0.113.2", "8388", "日本 01")) + `"}`)
	check("香港 01 2x")

	// Only the airport's 香港 nodes relay; the rest of it is left out.
	sub = kernelContractPost(t, router, admin, "/api/v1/subscription/save", `{"name":"香港前置","groupIds":[`+chainGroup+`],"options":{`+
		`"chainRelaySources":[`+airportID+`],"chainLandingSources":[`+jsonInt64(int64(landing["id"].(float64)))+`],`+
		`"chainSourceFilters":{"`+airportID+`":"香港","999":"ignored"},"chainDropUnused":true}}`)
	if sub.Code != 0 {
		t.Fatalf("filtered chain: %+v", sub)
	}
	view := sub.Data.(map[string]interface{})
	if f := view["options"].(map[string]interface{})["chainSourceFilters"].(map[string]interface{}); len(f) != 1 || f[airportID] != "香港" {
		t.Fatalf("filters are kept for chain sources only: %v", f)
	}
	body := getSub(t, router, view["path"].(string)+"?format=clash", "").Body.String()
	if !strings.Contains(body, "香港 01 2x") || strings.Contains(body, "日本 01") || !strings.Contains(body, `dialer-proxy: "中转节点"`) {
		t.Fatalf("filtered chain profile:\n%s", body)
	}
}
