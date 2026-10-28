package kernel

import (
	"encoding/json"
	"strings"
	"testing"
)

func renderJSON(t *testing.T, kernelType string, inbounds []Inbound) map[string]interface{} {
	t.Helper()
	r, err := Render(kernelType, inbounds)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(r.Files[0].Content), &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func outboundByTag(cfg map[string]interface{}, tag string) map[string]interface{} {
	for _, o := range cfg["outbounds"].([]interface{}) {
		if m := o.(map[string]interface{}); m["tag"] == tag {
			return m
		}
	}
	return nil
}

func ruleOutbounds(rules []interface{}, key string) []string {
	var out []string
	for _, r := range rules {
		m := r.(map[string]interface{})
		if v, ok := m[key].(string); ok {
			out = append(out, v)
		} else if a, ok := m["action"].(string); ok {
			out = append(out, a)
		}
	}
	return out
}

var chainUser = []User{{ID: 7, UUID: "11111111-1111-4111-8111-111111111111", Password: "userpw"}}

func TestChainSingboxImportedProxy(t *testing.T) {
	via := Endpoint{Host: "1.2.3.4", Port: 443, Direct: true, Proxy: Proxy{
		"type": "trojan", "server": "1.2.3.4", "port": 443, "password": "tpw", "sni": "t.example.com"}}
	in := Inbound{ID: 1, Protocol: ProtocolVLESS, Port: 443, Settings: Settings{UUID: "00000000-0000-4000-8000-000000000000"},
		Security: Security{Type: SecurityNone}, Users: chainUser, Via: &via}
	plain := Inbound{ID: 2, Protocol: ProtocolTrojan, Port: 444, Settings: Settings{Password: "p"},
		Security: Security{Type: SecurityTLS, SNI: "a.example.com", CertPEM: "c", KeyPEM: "k"}, Users: chainUser}
	cfg := renderJSON(t, TypeSingbox, []Inbound{in, plain})

	ob := outboundByTag(cfg, "via-1")
	if ob == nil || ob["type"] != "trojan" || ob["server"] != "1.2.3.4" || ob["password"] != "tpw" || ob["routing_mark"] != nil {
		t.Fatalf("chain outbound: %v", ob)
	}
	if ob := outboundByTag(cfg, "via-1-u7"); ob == nil || ob["routing_mark"] != float64(UserMark(7)) {
		t.Fatalf("user chain outbound must carry the user's mark: %v", ob)
	}
	route := cfg["route"].(map[string]interface{})
	got := strings.Join(ruleOutbounds(route["rules"].([]interface{}), "outbound"), ",")
	if got != "via-1-u7,via-1,flyx-u7" || route["final"] != "direct" {
		t.Fatalf("rule order: %s", got)
	}
}

func TestChainXraySelfBuiltTargets(t *testing.T) {
	cert, key, err := SelfSignedCert("b.example.com")
	if err != nil {
		t.Fatal(err)
	}
	trojan := Inbound{ID: 9, Protocol: ProtocolTrojan, Port: 8443, Settings: Settings{Password: "tpw"},
		Transport: Transport{Type: TransportWS, Path: "/ws"},
		Security:  Security{Type: SecurityTLS, SNI: "b.example.com", CertMode: CertModeSelf, CertPEM: cert, KeyPEM: key, Fingerprint: "chrome"}}
	reality := Inbound{ID: 10, Protocol: ProtocolVLESS, Port: 443,
		Settings:  Settings{UUID: "22222222-2222-4222-8222-222222222222", Flow: FlowVision},
		Transport: Transport{Type: TransportTCP},
		Security: Security{Type: SecurityReality, SNI: "www.example.com", RealityPublicKey: "pub",
			RealityShortIDs: []string{"ab"}, Fingerprint: "chrome"}}

	a := Inbound{ID: 1, Protocol: ProtocolVLESS, Port: 1000, Settings: Settings{UUID: "00000000-0000-4000-8000-000000000000"},
		Security: Security{Type: SecurityNone}, Users: chainUser,
		Via: &Endpoint{Host: "5.6.7.8", Port: 8443, Direct: true, Inbound: trojan}}
	b := Inbound{ID: 2, Protocol: ProtocolShadowsocks, Port: 1001,
		Settings: Settings{Method: "aes-128-gcm", Password: "x"},
		Via:      &Endpoint{Host: "9.9.9.9", Port: 443, Direct: true, Inbound: reality}}
	cfg := renderJSON(t, TypeXray, []Inbound{a, b})

	ob := outboundByTag(cfg, "via-1")
	stream := ob["streamSettings"].(map[string]interface{})
	tls := stream["tlsSettings"].(map[string]interface{})
	pin, _ := certSHA256(cert)
	if ob["protocol"] != "trojan" || stream["network"] != "ws" || tls["pinnedPeerCertSha256"] != pin || tls["allowInsecure"] != nil {
		t.Fatalf("self-signed target must be pinned: %v", ob)
	}
	userOb := outboundByTag(cfg, "via-1-u7")
	if mark := userOb["streamSettings"].(map[string]interface{})["sockopt"].(map[string]interface{})["mark"]; mark != float64(UserMark(7)) {
		t.Fatalf("user chain outbound mark: %v", userOb)
	}
	r := outboundByTag(cfg, "via-2")
	user := r["settings"].(map[string]interface{})["vnext"].([]interface{})[0].(map[string]interface{})["users"].([]interface{})[0].(map[string]interface{})
	rs := r["streamSettings"].(map[string]interface{})["realitySettings"].(map[string]interface{})
	if user["flow"] != FlowVision || user["encryption"] != "none" || rs["publicKey"] != "pub" || rs["shortId"] != "ab" {
		t.Fatalf("reality target: %v", r)
	}
	rules := cfg["routing"].(map[string]interface{})["rules"].([]interface{})
	if got := strings.Join(ruleOutbounds(rules, "outboundTag"), ","); got != "via-1-u7,via-1,via-2,flyx-u7" {
		t.Fatalf("rule order: %s", got)
	}
}

func TestChainBlockedWhenTargetGone(t *testing.T) {
	in := Inbound{ID: 3, Protocol: ProtocolShadowsocks, Port: 1000, Settings: Settings{Method: "aes-128-gcm", Password: "x"},
		Security: Security{Type: SecurityShadowTLS, SNI: "www.example.com", ShadowTLSPassword: "stlspassword"},
		Via:      &Endpoint{}}
	cfg := renderJSON(t, TypeSingbox, []Inbound{in})
	rule := cfg["route"].(map[string]interface{})["rules"].([]interface{})[0].(map[string]interface{})
	if rule["action"] != "reject" || len(rule["inbound"].([]interface{})) != 2 {
		t.Fatalf("sing-box must reject both the ShadowTLS front and its backend: %v", rule)
	}

	in.Security = Security{Type: SecurityNone}
	cfg = renderJSON(t, TypeXray, []Inbound{in})
	rule = cfg["routing"].(map[string]interface{})["rules"].([]interface{})[0].(map[string]interface{})
	if rule["outboundTag"] != "block" {
		t.Fatalf("xray must block: %v", rule)
	}
}

func TestCheckChainRejections(t *testing.T) {
	hy2 := Endpoint{Host: "1.1.1.1", Port: 443, Inbound: Inbound{Protocol: ProtocolHysteria2, Settings: Settings{Password: "p"},
		Security: Security{Type: SecurityTLS, SNI: "h.example.com"}}}
	if err := CheckChain(TypeXray, hy2); err == nil {
		t.Fatal("xray cannot dial hysteria2")
	}
	if err := CheckChain(TypeSingbox, hy2); err != nil {
		t.Fatalf("sing-box dials hysteria2: %v", err)
	}
	snell := Endpoint{Host: "1.1.1.1", Port: 443, Inbound: Inbound{Protocol: ProtocolSnell, Settings: Settings{Password: "p"}}}
	if err := CheckChain(TypeSingbox, snell); err == nil {
		t.Fatal("sing-box cannot dial snell")
	}
	if err := CheckChain(TypeSnell, hy2); err == nil {
		t.Fatal("snell kernel has no outbounds")
	}
	insecure := Endpoint{Host: "1.1.1.1", Port: 443, Proxy: Proxy{"type": "trojan", "server": "1.1.1.1", "port": 443,
		"password": "p", "skip-cert-verify": true}}
	if err := CheckChain(TypeXray, insecure); err == nil {
		t.Fatal("xray cannot skip certificate verification")
	}
	insecure.Proxy["fingerprint"] = strings.Repeat("ab", 32)
	if err := CheckChain(TypeXray, insecure); err != nil {
		t.Fatalf("a pinned certificate works: %v", err)
	}
	if _, err := Render(TypeSnell, []Inbound{{ID: 1, Protocol: ProtocolSnell, Port: 1, Via: &hy2}}); err == nil {
		t.Fatal("snell render must refuse a chained inbound")
	}
}

func renderJSONString(t *testing.T, content string) map[string]interface{} {
	t.Helper()
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(content), &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}
