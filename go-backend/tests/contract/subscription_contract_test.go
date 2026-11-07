package contract_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"go-backend/internal/auth"
	"go-backend/internal/store/repo"
)

func TestSubscriptionContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, r := setupContractRouter(t, secret)
	admin, _ := auth.GenerateToken(1, "admin_user", 0, secret)
	landing := seedKernelNode(t, r, "landing")
	if err := r.DB().Exec(`UPDATE node SET server_ip = '198.51.100.9' WHERE id = ?`, landing).Error; err != nil {
		t.Fatal(err)
	}
	vless := seedInboundOnNode(t, router, r, admin, landing,
		`{"nodeId":`+jsonInt64(landing)+`,"kernelType":"xray","name":"东京 VLESS","protocol":"vless","port":443,"settings":{"flow":"xtls-rprx-vision"},"security":{"type":"reality"}}`)
	ss := seedInboundOnNode(t, router, r, admin, landing,
		`{"nodeId":`+jsonInt64(landing)+`,"kernelType":"xray","name":"香港 SS","protocol":"shadowsocks","port":8388}`)
	var ownerSettings struct {
		UUID string `json:"uuid"`
	}
	_ = json.Unmarshal([]byte(mustQueryString(t, r, `SELECT settings FROM node_inbound WHERE id = ?`, vless)), &ownerSettings)

	sub := func(path string) (int, string, string) {
		resp := getSub(t, router, path+"?format=base64", "")
		body, _ := base64.StdEncoding.DecodeString(resp.Body.String())
		return resp.Code, string(body), resp.Header().Get("Subscription-Userinfo")
	}

	// A default subscription (with its own credentials) always exists and is
	// what the old API returns.
	list := kernelContractPost(t, router, admin, "/api/v1/subscription/list", `{}`).Data.(map[string]interface{})
	items := list["items"].([]interface{})
	if len(items) != 1 || items[0].(map[string]interface{})["kind"] != "share" || len(list["regions"].([]interface{})) < 10 {
		t.Fatalf("default subscription: %v", list)
	}
	def := items[0].(map[string]interface{})
	info := kernelContractPost(t, router, admin, "/api/v1/inbound/sub/info", `{}`).Data.(map[string]interface{})
	if info["path"] != def["path"] || valueAsInt(def["nodeCount"]) != 2 {
		t.Fatalf("legacy info must point at the default subscription: %v vs %v", info, def)
	}

	t.Run("node group processing", func(t *testing.T) {
		hk := saveNodeGroup(t, router, admin, `{"name":"香港","sourceAll":true,"options":{"regions":["🇭🇰 香港"],"addFlag":true,"prefix":"[x] "}}`)
		out := kernelContractPost(t, router, admin, "/api/v1/subscription/save", `{"name":"只要香港","groupIds":[`+jsonInt64(hk)+`]}`)
		if out.Code != 0 {
			t.Fatalf("save: %+v", out)
		}
		code, body, _ := sub(out.Data.(map[string]interface{})["path"].(string))
		if code != 200 || strings.Contains(body, "vless://") || !strings.Contains(body, "%5Bx%5D%20%F0%9F%87%AD%F0%9F%87%B0%20") {
			t.Fatalf("filtered, flagged, prefixed: %d %s", code, body)
		}
		if out := kernelContractPost(t, router, admin, "/api/v1/subscription/group/save",
			`{"name":"bad","sourceAll":true,"options":{"include":"/[/"}}`); out.Code == 0 {
			t.Fatal("bad regex must be rejected")
		}
		if out := kernelContractPost(t, router, admin, "/api/v1/subscription/save", `{"name":"空","groupIds":[]}`); out.Code == 0 {
			t.Fatal("a subscription needs a node group")
		}
		if out := kernelContractPost(t, router, admin, "/api/v1/subscription/group/delete", `{"id":`+jsonInt64(hk)+`}`); out.Code == 0 {
			t.Fatal("a group in use must not be deleted")
		}

		// Groups are listed in order: Tokyo first, then the Hong Kong group.
		tokyo := saveNodeGroup(t, router, admin, `{"name":"东京","sourceAll":true,"options":{"protocols":["vless"]}}`)
		out = kernelContractPost(t, router, admin, "/api/v1/subscription/save", `{"name":"合并","groupIds":[`+jsonInt64(tokyo)+`,`+jsonInt64(hk)+`]}`)
		if v := out.Data.(map[string]interface{}); out.Code != 0 || valueAsInt(v["nodeCount"]) != 2 || len(v["groupIds"].([]interface{})) != 2 {
			t.Fatalf("two groups: %+v", out)
		}
		_, body, _ = sub(out.Data.(map[string]interface{})["path"].(string))
		if lines := strings.Split(body, "\n"); len(lines) != 2 || !strings.HasPrefix(lines[0], "vless://") {
			t.Fatalf("groups in order: %s", body)
		}
		groups := kernelContractPost(t, router, admin, "/api/v1/subscription/group/list", `{}`).Data.(map[string]interface{})["items"].([]interface{})
		for _, g := range groups {
			m := g.(map[string]interface{})
			if int64(m["id"].(float64)) == hk && len(m["subscriptions"].([]interface{})) != 2 {
				t.Fatalf("group users: %v", m)
			}
		}

		prev := kernelContractPost(t, router, admin, "/api/v1/subscription/group/preview",
			`{"sourceAll":true,"options":{"protocols":["vless"],"rename":"东京 => Tokyo"}}`).Data.(map[string]interface{})
		pi := prev["items"].([]interface{})
		if len(pi) != 1 || pi[0].(map[string]interface{})["name"] != "Tokyo VLESS" ||
			pi[0].(map[string]interface{})["original"] != "东京 VLESS" || valueAsInt(prev["dropped"]) != 1 {
			t.Fatalf("preview: %v", prev)
		}
	})

	t.Run("share subscription lifecycle", func(t *testing.T) {
		if out := kernelContractPost(t, router, admin, "/api/v1/subscription/group/save", `{"name":"空","sourceAll":false,"inboundIds":[]}`); out.Code == 0 {
			t.Fatal("picking nothing must be rejected")
		}
		group := saveNodeGroup(t, router, admin, `{"name":"小明的节点","inboundIds":[`+jsonInt64(vless)+`,`+jsonInt64(ss)+`,999]}`)
		groupIDs := `"groupIds":[` + jsonInt64(group) + `]`
		out := kernelContractPost(t, router, admin, "/api/v1/subscription/save",
			`{"name":"小明","kind":"share",`+groupIDs+`}`)
		if out.Code != 0 {
			t.Fatalf("create: %+v", out)
		}
		view := out.Data.(map[string]interface{})
		id := int64(view["id"].(float64))
		path := view["path"].(string)
		if len(view["groupIds"].([]interface{})) != 1 || len(view["sharedIds"].([]interface{})) != 1 || view["active"] != true {
			t.Fatalf("view: %v", view)
		}
		uuid := mustQueryString(t, r, `SELECT uuid FROM subscription WHERE id = ?`, id)
		// A link that stopped working answers with one placeholder node
		// named after the reason, so the client shows why.
		stopped := func(what, reason string) {
			t.Helper()
			code, body, _ := sub(path)
			name, _ := url.PathUnescape(body)
			if code != 200 || strings.Count(body, "\n") != 0 || !strings.Contains(body, "@127.0.0.1:1") ||
				!strings.Contains(name, reason) || strings.Contains(body, uuid) {
				t.Fatalf("%s: %d %s", what, code, name)
			}
		}
		code, body, hdr := sub(path)
		if code != 200 || !strings.Contains(body, "vless://"+uuid+"@") || strings.Contains(body, ownerSettings.UUID) ||
			!strings.Contains(hdr, "upload=0") {
			t.Fatalf("share uses its own uuid and hides traffic: %d %s %s", code, body, hdr)
		}
		if _, ownBody, _ := sub(def["path"].(string)); strings.Contains(ownBody, uuid) {
			t.Fatal("own subscription must not carry share credentials")
		}

		past := time.Now().Add(-time.Hour).UnixMilli()
		kernelContractPost(t, router, admin, "/api/v1/subscription/save",
			`{"id":`+jsonInt64(id)+`,"name":"小明","kind":"share",`+groupIDs+`,"expireTime":`+jsonInt64(past)+`}`)
		stopped("expired", "订阅已过期")
		future := time.Now().Add(48 * time.Hour).UnixMilli()
		kernelContractPost(t, router, admin, "/api/v1/subscription/save",
			`{"id":`+jsonInt64(id)+`,"name":"小明","kind":"share",`+groupIDs+`,"expireTime":`+jsonInt64(future)+`,"enabled":false}`)
		stopped("disabled", "订阅已停用")
		kernelContractPost(t, router, admin, "/api/v1/subscription/save",
			`{"id":`+jsonInt64(id)+`,"name":"小明","kind":"share",`+groupIDs+`,"expireTime":`+jsonInt64(future)+`,"enabled":true}`)
		if code, _, hdr := sub(path); code != 200 || !strings.Contains(hdr, "expire="+jsonInt64(future/1000)) {
			t.Fatalf("re-enabled: %d %s", code, hdr)
		}

		// Traffic limit: reported in the header, cut off once reached.
		out = kernelContractPost(t, router, admin, "/api/v1/subscription/save",
			`{"id":`+jsonInt64(id)+`,"name":"小明","kind":"share",`+groupIDs+`,"expireTime":`+jsonInt64(future)+`,"trafficLimit":1000,"resetDay":5}`)
		if v := out.Data.(map[string]interface{}); out.Code != 0 || valueAsInt(v["trafficLimit"]) != 1000 || valueAsInt(v["resetDay"]) != 5 {
			t.Fatalf("limit save: %+v", out)
		}
		if out := kernelContractPost(t, router, admin, "/api/v1/subscription/save",
			`{"id":`+jsonInt64(id)+`,"name":"小明","kind":"share",`+groupIDs+`,"resetDay":31}`); out.Code == 0 {
			t.Fatal("reset day 31 must be rejected")
		}
		if err := r.ApplySubscriptionCounterSamples(landing, "e", []repo.SubscriptionCounterSample{
			{SubscriptionID: id, Direction: "up", Bytes: 200}, {SubscriptionID: id, Direction: "down", Bytes: 500}}, time.Now().UnixMilli()); err != nil {
			t.Fatal(err)
		}
		if code, _, hdr := sub(path); code != 200 || !strings.HasPrefix(hdr, "upload=200; download=500; total=1000;") {
			t.Fatalf("share traffic header: %d %s", code, hdr)
		}
		_ = r.ApplySubscriptionCounterSamples(landing, "e", []repo.SubscriptionCounterSample{{SubscriptionID: id, Direction: "down", Bytes: 900}}, time.Now().UnixMilli())
		stopped("over the limit", "流量已用完")

		// Info nodes: expiry and traffic first, as placeholder nodes.
		out = kernelContractPost(t, router, admin, "/api/v1/subscription/save",
			`{"id":`+jsonInt64(id)+`,"name":"小明","kind":"share",`+groupIDs+`,"expireTime":`+jsonInt64(future)+`,"trafficLimit":4000,"resetDay":5,"options":{"infoNodes":true}}`)
		if out.Code != 0 || out.Data.(map[string]interface{})["options"].(map[string]interface{})["infoNodes"] != true {
			t.Fatalf("info nodes save: %+v", out)
		}
		_, body, _ = sub(path)
		lines := strings.Split(body, "\n")
		first, _ := url.PathUnescape(lines[0])
		second, _ := url.PathUnescape(lines[1])
		if len(lines) != 4 || !strings.Contains(first, "⏳ 到期 "+time.UnixMilli(future).Format("2006-01-02")) ||
			!strings.HasSuffix(second, "KB / 3.91KB") || !strings.Contains(second, "📊 剩余 ") || !strings.Contains(lines[2], uuid) {
			t.Fatalf("info nodes: %q %q %v", first, second, lines)
		}
		out = kernelContractPost(t, router, admin, "/api/v1/subscription/reset-traffic", `{"id":`+jsonInt64(id)+`}`)
		if v := out.Data.(map[string]interface{}); out.Code != 0 || valueAsInt(v["downloadBytes"]) != 0 || v["active"] != true {
			t.Fatalf("reset traffic: %+v", out)
		}
		if code, _, _ := sub(path); code != 200 {
			t.Fatalf("after reset: %d", code)
		}
		// The hourly history keeps the traffic a reset zeroes.
		for _, rg := range []string{"hour", "day", "month"} {
			out = kernelContractPost(t, router, admin, "/api/v1/subscription/traffic", `{"id":`+jsonInt64(id)+`,"range":"`+rg+`","tzOffset":480}`)
			v := out.Data.(map[string]interface{})
			points := v["points"].([]interface{})
			last := points[len(points)-1].(map[string]interface{})
			if out.Code != 0 || valueAsInt(v["up"]) != 200 || valueAsInt(v["down"]) != 900 || valueAsInt(last["down"]) != 900 {
				t.Fatalf("%s traffic: %+v", rg, out)
			}
		}

		out = kernelContractPost(t, router, admin, "/api/v1/subscription/reset", `{"id":`+jsonInt64(id)+`}`)
		newPath := out.Data.(map[string]interface{})["path"].(string)
		if code, _, _ := sub(path); code != http.StatusNotFound || newPath == path ||
			mustQueryString(t, r, `SELECT uuid FROM subscription WHERE id = ?`, id) == uuid {
			t.Fatal("reset must rotate link and credentials")
		}
		kernelContractPost(t, router, admin, "/api/v1/subscription/delete", `{"id":`+jsonInt64(id)+`}`)
		if n := mustQueryInt(t, r, `SELECT COUNT(*) FROM subscription_traffic WHERE subscription_id = ?`, id); n != 0 {
			t.Fatalf("history of a deleted subscription must go, got %d rows", n)
		}
		if code, _, _ := sub(newPath); code != http.StatusNotFound {
			t.Fatal("deleted subscription link must stop working")
		}
	})
}
