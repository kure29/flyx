package kernel

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func shareFixtures(t *testing.T) []Endpoint {
	t.Helper()
	list := goldenInbounds(t) // ids 1..5: vless-reality, ss2022, vmess-ws-tls(file), trojan-grpc-tls(self), vless-xhttp
	byID := map[int64]Inbound{}
	for _, in := range list {
		byID[in.ID] = in
	}
	return []Endpoint{
		{Name: "东京 Reality", Host: "203.0.113.10", Port: 443, Inbound: byID[1], PostQuantumReality: true},
		{Name: "SS", Host: "2001:db8::1", Port: 8388, Inbound: byID[2]},
		{Name: "CDN", Host: "cdn.example.com", Port: 8443, Inbound: byID[3]},
		{Name: "Trojan", Host: "203.0.113.10", Port: 2053, Inbound: byID[4]},
		{Name: "XHTTP", Host: "203.0.113.10", Port: 9443, Inbound: byID[5]},
		{Name: "东京 Reality", Host: "198.51.100.7", Port: 30001, Inbound: byID[1]}, // relay, duplicate name
	}
}

func TestShareURIs(t *testing.T) {
	eps := shareFixtures(t)
	var lines []string
	for _, e := range eps {
		uri, ok := ShareURI(e)
		if !ok {
			t.Fatalf("no uri for %s", e.Name)
		}
		lines = append(lines, uri)
	}
	assertGolden(t, "share.golden.txt", strings.Join(lines, "\n")+"\n")

	if !strings.Contains(lines[0], "pbk="+eps[0].Inbound.Security.RealityPublicKey) || !strings.Contains(lines[0], "flow=xtls-rprx-vision") {
		t.Fatalf("reality uri: %s", lines[0])
	}
	if !strings.Contains(lines[1], "@[2001:db8::1]:8388") {
		t.Fatalf("ipv6 must be bracketed: %s", lines[1])
	}
	if !strings.Contains(lines[3], "allowInsecure=1") {
		t.Fatalf("self-signed trojan must allow insecure: %s", lines[3])
	}
	vm, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(lines[2], "vmess://"))
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]string
	if err := json.Unmarshal(vm, &v); err != nil || v["net"] != "ws" || v["tls"] != "tls" || v["allowInsecure"] != "" {
		t.Fatalf("vmess json: %s", vm)
	}
}

func TestSubscriptionFormats(t *testing.T) {
	eps := shareFixtures(t)

	b64, ctype, err := Subscription(FormatBase64, eps, SubOptions{})
	if err != nil || !strings.HasPrefix(ctype, "text/plain") {
		t.Fatal(err, ctype)
	}
	decoded, _ := base64.StdEncoding.DecodeString(b64)
	if strings.Count(string(decoded), "\n") != len(eps)-1 {
		t.Fatalf("base64 should carry every endpoint: %s", decoded)
	}
	if !strings.Contains(string(decoded), "%E4%B8%9C%E4%BA%AC%20Reality%20%232") {
		t.Fatalf("duplicate names must be suffixed: %s", decoded)
	}

	clash, _, err := Subscription(FormatClash, eps, SubOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "clash.golden.yaml", clash)
	if strings.Contains(clash, "XHTTP") {
		t.Fatal("xhttp must be skipped for clash")
	}

	sb, _, err := Subscription(FormatSingbox, eps, SubOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(sb), &parsed); err != nil {
		t.Fatalf("sing-box json: %v", err)
	}
	assertGolden(t, "singbox.golden.json", sb)

	surge, _, err := Subscription(FormatSurge, eps, SubOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "surge.golden.conf", surge)
	if strings.Contains(surge, "Reality") {
		t.Fatal("surge has no reality support")
	}

	if _, _, err := Subscription("nope", eps, SubOptions{}); err == nil {
		t.Fatal("unknown format must fail")
	}
}

func TestSubscriptionEmpty(t *testing.T) {
	for _, f := range []string{FormatBase64, FormatClash, FormatSingbox, FormatSurge} {
		if _, _, err := Subscription(f, nil, SubOptions{}); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
	}
}

func TestFormatForUserAgent(t *testing.T) {
	cases := map[string]string{
		"clash-verge/v2.0":            FormatClash,
		"mihomo/1.19":                 FormatClash,
		"Stash/2.4":                   FormatStash,
		"sing-box 1.12.0":             FormatSingbox,
		"SFA/1.11":                    FormatSingbox,
		"Surge iOS/2920":              FormatSurge,
		"v2rayN/7.0":                  FormatBase64,
		"Shadowrocket/2070 CFNetwork": FormatShadowrocket,
	}
	for ua, want := range cases {
		if got := FormatForUserAgent(ua); got != want {
			t.Fatalf("%q -> %s, want %s", ua, got, want)
		}
	}
}

func TestYAMLQuotesHostileNames(t *testing.T) {
	out := marshalYAML(yamlNode{{"proxies", []interface{}{yamlNode{{"name", "a\n- evil: 1"}, {"port", 1}}}}})
	if strings.Count(out, "\n") != 3 || !strings.Contains(out, `"a\n- evil: 1"`) {
		t.Fatalf("names must stay a single quoted scalar:\n%s", out)
	}
}

func TestSharePhase3(t *testing.T) {
	var eps []Endpoint
	for _, in := range phase3Inbounds(t)[:4] {
		eps = append(eps, Endpoint{Name: in.Protocol, Host: "203.0.113.10", Port: in.Port, Inbound: in})
	}
	snell := Inbound{ID: 9, Protocol: ProtocolSnell, Port: 6160, Settings: Settings{Password: "psk-cccccccc", SnellVersion: 5, BackendPort: 36160},
		Security: Security{Type: SecurityShadowTLS, SNI: "www.microsoft.com", ShadowTLSPassword: "stls-pass-2"}}
	if err := Normalize(TypeSnell, &snell); err != nil {
		t.Fatal(err)
	}
	eps = append(eps, Endpoint{Name: "snell", Host: "203.0.113.10", Port: 6160, Inbound: snell})

	var uris []string
	for _, e := range eps {
		if uri, ok := ShareURI(e); ok {
			uris = append(uris, uri)
		}
	}
	if len(uris) != 4 {
		t.Fatalf("snell has no share link, others do: %v", uris)
	}
	var all strings.Builder
	all.WriteString(strings.Join(uris, "\n") + "\n")
	for _, f := range []string{FormatClash, FormatSingbox, FormatSurge} {
		body, _, err := Subscription(f, eps, SubOptions{})
		if err != nil {
			t.Fatal(err)
		}
		all.WriteString("### " + f + "\n" + body)
	}
	assertGolden(t, "share-phase3.golden.txt", all.String())
	out := all.String()
	for _, want := range []string{
		"hysteria2://hy2-secret@203.0.113.10:443/?insecure=1&sni=www.bing.com#hysteria2",
		"plugin=shadow-tls%3Bhost%3Dwww.microsoft.com%3Bpassword%3Dstls-secret-123%3Bversion%3D3",
		"snell = snell, 203.0.113.10, 6160, psk=psk-cccccccc, version=5, reuse=true, tfo=true, shadow-tls-password=stls-pass-2, shadow-tls-sni=www.microsoft.com, shadow-tls-version=3",
		"tuic-v5, 203.0.113.10, 8443",
		`"detour": "shadowsocks (shadowtls)"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q", want)
		}
	}
	for _, e := range eps[:2] { // hysteria2, tuic
		o, _ := singboxOutbound(e)
		if _, bad := o["tls"].(map[string]interface{})["utls"]; bad {
			t.Fatalf("sing-box rejects uTLS on QUIC outbound %s", e.Name)
		}
	}
	if strings.Contains(out[strings.Index(out, "### clash"):strings.Index(out, "### singbox")], "snell") {
		t.Fatal("snell v5 must not be offered to Mihomo")
	}
}

func TestHysteria2PortHopping(t *testing.T) {
	in := Inbound{ID: 7, Protocol: ProtocolHysteria2, Port: 443, Settings: Settings{Password: "hop-secret", PortHopping: " 20000:30000 "},
		Transport: Transport{Type: TransportQUIC}, Security: Security{Type: SecurityTLS}}
	if err := Normalize(TypeSingbox, &in); err != nil {
		t.Fatal(err)
	}
	if in.Settings.PortHopping != "20000-30000" {
		t.Fatalf("range not normalized: %q", in.Settings.PortHopping)
	}
	direct := Endpoint{Name: "hy2", Host: "203.0.113.10", Port: 443, Inbound: in, Direct: true}
	relay := Endpoint{Name: "relay", Host: "198.51.100.7", Port: 31000, Inbound: in}

	if uri, _ := ShareURI(direct); !strings.Contains(uri, "mport=20000-30000") {
		t.Fatalf("direct uri lacks mport: %s", uri)
	}
	if uri, _ := ShareURI(relay); strings.Contains(uri, "mport") {
		t.Fatalf("relays forward a single port: %s", uri)
	}
	if line, _ := SurgeLine(direct); !strings.Contains(line, `port-hopping="20000-30000", port-hopping-interval=30`) {
		t.Fatalf("surge: %s", line)
	}
	o, _ := singboxOutbound(direct)
	if _, has := o["server_port"]; has || o["server_ports"].([]string)[0] != "20000:30000" {
		t.Fatalf("sing-box: %v", o)
	}
	clash := marshalYAML(clashQUICFamily(direct))
	if !strings.Contains(clash, `ports: "20000-30000"`) || !strings.Contains(clash, "hop-interval: 30") {
		t.Fatalf("clash:\n%s", clash)
	}
	if strings.Contains(marshalYAML(clashQUICFamily(relay)), "ports") {
		t.Fatal("relay must not hop")
	}

	bad := in
	bad.Settings.PortHopping = "400-500"
	if err := Normalize(TypeSingbox, &bad); err == nil {
		t.Fatal("ports below 1024 must be rejected")
	}
	bad.Settings.PortHopping = "400-30000"
	bad.Port = 25000
	if err := Normalize(TypeSingbox, &bad); err == nil {
		t.Fatal("range containing the listen port must be rejected")
	}
	tuic := Inbound{ID: 8, Protocol: ProtocolTUIC, Port: 8443, Settings: Settings{PortHopping: "20000-30000"},
		Transport: Transport{Type: TransportQUIC}, Security: Security{Type: SecurityTLS}}
	if err := Normalize(TypeSingbox, &tuic); err != nil || tuic.Settings.PortHopping != "" {
		t.Fatalf("hopping is Hysteria2 only: %v %q", err, tuic.Settings.PortHopping)
	}
}
