package kernel

import (
	"strings"
	"testing"
)

func splitInbound() Inbound {
	unlock := Endpoint{Host: "5.6.7.8", Port: 8388, Proxy: Proxy{"type": "ss", "server": "5.6.7.8", "port": 8388,
		"cipher": "aes-128-gcm", "password": "pw"}}
	return Inbound{ID: 1, Protocol: ProtocolTrojan, Port: 443, Settings: Settings{Password: "p"},
		Security: Security{Type: SecurityTLS, SNI: "a.example.com", CertPEM: "c", KeyPEM: "k"}, Users: chainUser,
		Split: []SplitRule{
			{Categories: []string{"netflix", "cn"}, Action: SplitProxy, OutboundID: 9, Via: &unlock},
			{Categories: []string{"ads", "bt"}, Action: SplitBlock},
			{Domains: []string{"example.com"}, IPs: []string{"1.2.3.0/24"}, Action: SplitDirect},
			{Categories: []string{"openai"}, Action: SplitProxy, OutboundID: 10, Via: &Endpoint{}},
		}}
}

func ruleTargets(rules []interface{}, key string) []string {
	var out []string
	for _, r := range rules {
		m := r.(map[string]interface{})
		t, _ := m[key].(string)
		if a, ok := m["action"].(string); ok {
			t = a
		}
		if u, ok := m["user"]; ok && u != nil {
			t += "(u)"
		}
		if u, ok := m["auth_user"]; ok && u != nil {
			t += "(u)"
		}
		out = append(out, t)
	}
	return out
}

func TestSplitXray(t *testing.T) {
	cfg := renderJSON(t, TypeXray, []Inbound{splitInbound()})
	rules := cfg["routing"].(map[string]interface{})["rules"].([]interface{})
	got := strings.Join(ruleTargets(rules, "outboundTag"), ",")
	want := strings.Join([]string{
		// netflix + cn → unlock node: domains, then China IPs; users first.
		"via-1-s1-u7(u)", "via-1-s1", "via-1-s1-u7(u)", "via-1-s1",
		// ads (domains) and BT (protocol) → block.
		"block(u)", "block", "block(u)", "block",
		// custom domain / IP → direct, users through their counted outbound.
		"flyx-u7(u)", "direct", "flyx-u7(u)", "direct",
		// target deleted → dropped.
		"block(u)", "block",
		// everything else: the users' direct rule.
		"flyx-u7(u)",
	}, ",")
	if got != want {
		t.Fatalf("rules:\n got %s\nwant %s", got, want)
	}
	first := rules[0].(map[string]interface{})
	if d := first["domain"].([]interface{}); len(d) != 2 || d[0] != "geosite:netflix" || d[1] != "geosite:cn" {
		t.Fatalf("netflix + cn domains: %v", first)
	}
	if ip := rules[2].(map[string]interface{})["ip"].([]interface{}); len(ip) != 1 || ip[0] != "geoip:cn" {
		t.Fatalf("cn IPs: %v", rules[2])
	}
	if p := rules[6].(map[string]interface{})["protocol"].([]interface{}); p[0] != "bittorrent" {
		t.Fatalf("bt: %v", rules[6])
	}
	if d := rules[9].(map[string]interface{})["domain"].([]interface{}); d[0] != "domain:example.com" {
		t.Fatalf("custom domain: %v", rules[9])
	}
	if ob := outboundByTag(cfg, "via-1-s1-u7"); ob == nil || ob["protocol"] != "shadowsocks" {
		t.Fatalf("split outbound: %v", ob)
	}
}

func TestSplitSingboxWithChain(t *testing.T) {
	in := splitInbound()
	in.Via = &Endpoint{Host: "9.9.9.9", Port: 443, Proxy: Proxy{"type": "trojan", "server": "9.9.9.9", "port": 443, "password": "x"}}
	r, err := RenderWith(TypeSingbox, []Inbound{in}, RenderOptions{RuleSetMirror: "https://gh.example.com", SingboxHTTPClient: true})
	if err != nil {
		t.Fatal(err)
	}
	cfg := renderJSONString(t, r.Files[0].Content)
	route := cfg["route"].(map[string]interface{})
	rules := route["rules"].([]interface{})
	got := strings.Join(ruleTargets(rules, "outbound"), ",")
	want := strings.Join([]string{
		"sniff",
		"via-1-s1-u7(u)", "via-1-s1", // rule sets (netflix + cn)
		"reject(u)", "reject", // ads rule set
		"reject(u)", "reject", // bt
		"flyx-u7(u)", "direct", "flyx-u7(u)", "direct", // custom domain, IP
		"reject(u)", "reject", // deleted target
		"via-1-u7(u)", "via-1", // the chained default exit
		"flyx-u7(u)",
	}, ",")
	if got != want {
		t.Fatalf("rules:\n got %s\nwant %s", got, want)
	}
	sets := route["rule_set"].([]interface{})
	if len(sets) != 5 { // netflix, cn (geosite + geoip), ads, openai
		t.Fatalf("rule sets defined once each: %v", sets)
	}
	first := sets[0].(map[string]interface{})
	if first["tag"] != "geosite-netflix" || first["url"] != "https://gh.example.com/https://raw.githubusercontent.com/SagerNet/sing-geosite/rule-set/geosite-netflix.srs" {
		t.Fatalf("rule set: %v", first)
	}
	if u := sets[2].(map[string]interface{})["url"].(string); !strings.Contains(u, "sing-geoip/rule-set/geoip-cn.srs") {
		t.Fatalf("geoip rule set: %s", u)
	}
	if cfg["experimental"] == nil || first["http_client"] != "rule-set" || cfg["http_clients"] == nil {
		t.Fatal("rule sets need the cache file and an explicit HTTP client")
	}
}

func TestNormalizeSplitRules(t *testing.T) {
	got, err := NormalizeSplitRules([]SplitRule{{Domains: []string{" *.Example.COM. ", ""}, IPs: []string{"1.2.3.4"},
		Categories: []string{"bt", "bt"}, Action: SplitBlock}})
	if err != nil || got[0].Domains[0] != "example.com" || got[0].IPs[0] != "1.2.3.4/32" || len(got[0].Categories) != 1 {
		t.Fatalf("normalize: %+v %v", got, err)
	}
	for _, bad := range [][]SplitRule{
		{{Action: SplitDirect}},
		{{Categories: []string{"nope"}, Action: SplitDirect}},
		{{Domains: []string{"bad domain"}, Action: SplitDirect}},
		{{IPs: []string{"1.2.3"}, Action: SplitDirect}},
		{{Categories: []string{"ads"}, Action: SplitProxy}},
		{{Categories: []string{"ads"}, Action: "nowhere"}},
	} {
		if _, err := NormalizeSplitRules(bad); err == nil {
			t.Fatalf("must reject %+v", bad)
		}
	}
	if _, err := Render(TypeSnell, []Inbound{{ID: 1, Protocol: ProtocolSnell, Port: 1, Split: got}}); err == nil {
		t.Fatal("snell has no 分流")
	}
}

func TestXrayGeoData(t *testing.T) {
	files := XrayGeoData("https://gh.example.com/")
	if len(files) != 2 || files[0].Name != "geosite.dat" ||
		files[0].URL != "https://gh.example.com/https://github.com/v2fly/domain-list-community/releases/latest/download/dlc.dat" ||
		files[1].ChecksumURL != "https://gh.example.com/https://github.com/v2fly/geoip/releases/latest/download/geoip.dat.sha256sum" {
		t.Fatalf("geo data: %+v", files)
	}
}
