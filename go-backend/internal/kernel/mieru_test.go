package kernel

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMieruCatalogAndDownload(t *testing.T) {
	info, ok := Lookup(TypeMieru)
	if !ok || !info.Implemented || info.Name != "Mieru" || info.ReleaseRepo != "enfein/mieru" {
		t.Fatalf("unexpected mieru catalog entry: %+v ok=%v", info, ok)
	}
	spec, err := ResolveDownload(info, "3.40.0", "")
	if err != nil {
		t.Fatalf("resolve pinned version: %v", err)
	}
	if spec["url"] != "https://github.com/enfein/mieru/releases/download/v3.40.0/mita_3.40.0_linux_{ARCH}.tar.gz" ||
		spec["binName"] != "mita" || spec["archive"] != "tar.gz" {
		t.Fatalf("unexpected download spec: %+v", spec)
	}
	gh := spec["github"].(map[string]interface{})
	if gh["repo"] != "enfein/mieru" || gh["tag"] != "v3.40.0" || gh["asset"] != "mita_{VERSION}_linux_{ARCH}.tar.gz" {
		t.Fatalf("unexpected github spec: %+v", gh)
	}

	latest, err := ResolveDownload(info, "latest", "https://gh.example.com")
	if err != nil {
		t.Fatalf("resolve latest: %v", err)
	}
	ghLatest := latest["github"].(map[string]interface{})
	if ghLatest["proxy"] != "https://gh.example.com" || ghLatest["tag"] != nil {
		t.Fatalf("unexpected latest github spec: %+v", ghLatest)
	}
}

func TestMieruNormalizeAndRegenerate(t *testing.T) {
	in := Inbound{
		ID:       1,
		Protocol: ProtocolMieru,
		Port:     23456,
	}
	if err := Normalize(TypeMieru, &in); err != nil {
		t.Fatalf("normalize defaults: %v", err)
	}
	if in.Transport.Type != TransportTCP || in.Security.Type != SecurityNone || in.Listen != "::" {
		t.Fatalf("defaults not applied: transport=%+v security=%+v listen=%q", in.Transport, in.Security, in.Listen)
	}
	if in.Settings.Username == "" || in.Settings.Password == "" {
		t.Fatalf("credentials not generated: %+v", in.Settings)
	}
	if in.Settings.MTU != DefaultMieruMTU || in.Settings.MieruMultiplexing != MieruMuxLow || in.Settings.MieruHandshakeMode != MieruHandshakeStandard {
		t.Fatalf("mieru defaults wrong: %+v", in.Settings)
	}
	if in.Network() != "tcp" || in.NetworkOn(TypeMieru) != "tcp" {
		t.Fatalf("TCP network mismatch: %s / %s", in.Network(), in.NetworkOn(TypeMieru))
	}
	if in.SupportsUsers() {
		t.Fatal("mieru must not claim per-user socket mark support")
	}

	// Regenerate rotates password while keeping username, MTU and client prefs.
	oldUser, oldPass := in.Settings.Username, in.Settings.Password
	in.Settings.MTU = 1380
	in.Settings.MieruMultiplexing = MieruMuxHigh
	in.Settings.MieruHandshakeMode = MieruHandshakeNoWait
	if err := Regenerate(TypeMieru, &in); err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if in.Settings.Username != oldUser || in.Settings.Password == "" || in.Settings.Password == oldPass {
		t.Fatalf("regenerate credentials unexpected: user=%q pass=%q (old=%q)", in.Settings.Username, in.Settings.Password, oldPass)
	}
	if in.Settings.MTU != 1380 || in.Settings.MieruMultiplexing != MieruMuxHigh || in.Settings.MieruHandshakeMode != MieruHandshakeNoWait {
		t.Fatalf("regenerate lost mieru settings: %+v", in.Settings)
	}

	// UDP transport
	udpIn := Inbound{ID: 2, Protocol: ProtocolMieru, Port: 23456, Transport: Transport{Type: TransportUDP}}
	if err := Normalize(TypeMieru, &udpIn); err != nil {
		t.Fatalf("normalize udp: %v", err)
	}
	if udpIn.Network() != "udp" || udpIn.NetworkOn(TypeMieru) != "udp" {
		t.Fatalf("UDP network mismatch: %s / %s", udpIn.Network(), udpIn.NetworkOn(TypeMieru))
	}

	// Validation errors
	cases := []struct {
		name string
		in   Inbound
	}{
		{"bad username", Inbound{Protocol: ProtocolMieru, Port: 23456, Settings: Settings{Username: "bad user!"}}},
		{"whitespace password", Inbound{Protocol: ProtocolMieru, Port: 23456, Settings: Settings{Password: "has space"}}},
		{"too long password", Inbound{Protocol: ProtocolMieru, Port: 23456, Settings: Settings{Password: strings.Repeat("a", 65)}}},
		{"mtu too small", Inbound{Protocol: ProtocolMieru, Port: 23456, Settings: Settings{MTU: 1200}}},
		{"mtu too large", Inbound{Protocol: ProtocolMieru, Port: 23456, Settings: Settings{MTU: 1600}}},
		{"bad multiplexing", Inbound{Protocol: ProtocolMieru, Port: 23456, Settings: Settings{MieruMultiplexing: "ULTRA"}}},
		{"bad handshake", Inbound{Protocol: ProtocolMieru, Port: 23456, Settings: Settings{MieruHandshakeMode: "FAST"}}},
		{"unsupported security", Inbound{Protocol: ProtocolMieru, Port: 23456, Security: Security{Type: SecurityTLS}}},
		{"proxy protocol", Inbound{Protocol: ProtocolMieru, Port: 23456, AcceptProxyProtocol: true}},
		{"extra overrides dns.dualStack", Inbound{Protocol: ProtocolMieru, Port: 23456, OutboundIPStrategy: OutboundIPv4Only, Extra: json.RawMessage(`{"dns":{"dualStack":"ONLY_IPv6"}}`)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := tc.in
			if err := Normalize(TypeMieru, &in); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}

func TestMieruRender(t *testing.T) {
	idle, err := Render(TypeMieru, nil)
	if err != nil {
		t.Fatalf("render empty: %v", err)
	}
	if !idle.Idle || len(idle.Files) != 0 || len(idle.Processes) != 0 {
		t.Fatalf("empty render must be idle: %+v", idle)
	}

	in1 := Inbound{
		ID:                 10,
		Protocol:           ProtocolMieru,
		Listen:             "::",
		Port:               20120,
		OutboundIPStrategy: OutboundIPv4Only,
		Settings: Settings{
			Username: "alice@example.com",
			Password: "secret-password-1",
			MTU:      1400,
		},
		Transport: Transport{Type: TransportTCP},
	}
	in2 := Inbound{
		ID:                 11,
		Protocol:           ProtocolMieru,
		Listen:             "0.0.0.0",
		Port:               20121,
		OutboundIPStrategy: OutboundPreferIPv6,
		Settings: Settings{
			Username: "bob",
			Password: "secret-password-2",
			MTU:      1360,
		},
		Transport: Transport{Type: TransportUDP},
		Extra:     json.RawMessage(`{"advancedSettings":{"tcpIdleTimeout":"10m"}}`),
	}
	for _, in := range []*Inbound{&in1, &in2} {
		if err := Normalize(TypeMieru, in); err != nil {
			t.Fatal(err)
		}
	}

	rendered, err := Render(TypeMieru, []Inbound{in1, in2})
	if err != nil {
		t.Fatalf("render mieru: %v", err)
	}
	if rendered.Idle || len(rendered.Files) != 2 || len(rendered.Processes) != 2 {
		t.Fatalf("unexpected render counts: idle=%v files=%d procs=%d", rendered.Idle, len(rendered.Files), len(rendered.Processes))
	}

	var cfg1, cfg2 map[string]interface{}
	if err := json.Unmarshal([]byte(rendered.Files[0].Content), &cfg1); err != nil {
		t.Fatalf("unmarshal cfg1: %v", err)
	}
	if err := json.Unmarshal([]byte(rendered.Files[1].Content), &cfg2); err != nil {
		t.Fatalf("unmarshal cfg2: %v", err)
	}

	// Listen "::" omits listenIPAddress so mita listens dual-stack on all interfaces.
	if _, exists := cfg1["listenIPAddress"]; exists {
		t.Fatalf("listen :: must omit listenIPAddress: %v", cfg1)
	}
	if cfg1["loggingLevel"] != "WARN" || cfg1["mtu"] != float64(1400) {
		t.Fatalf("cfg1 fields wrong: %v", cfg1)
	}
	pb1 := cfg1["portBindings"].([]interface{})[0].(map[string]interface{})
	if pb1["port"] != float64(20120) || pb1["protocol"] != "TCP" {
		t.Fatalf("cfg1 portBinding wrong: %v", pb1)
	}
	u1 := cfg1["users"].([]interface{})[0].(map[string]interface{})
	if u1["name"] != "alice@example.com" || u1["password"] != "secret-password-1" {
		t.Fatalf("cfg1 user wrong: %v", u1)
	}
	if cfg1["dns"].(map[string]interface{})["dualStack"] != "ONLY_IPv4" {
		t.Fatalf("cfg1 dns wrong: %v", cfg1["dns"])
	}

	// Explicit IP sets listenIPAddress, UDP protocol, PREFER_IPv6, and merged Extra.
	if cfg2["listenIPAddress"] != "0.0.0.0" || cfg2["mtu"] != float64(1360) {
		t.Fatalf("cfg2 listen/mtu wrong: %v", cfg2)
	}
	pb2 := cfg2["portBindings"].([]interface{})[0].(map[string]interface{})
	if pb2["port"] != float64(20121) || pb2["protocol"] != "UDP" {
		t.Fatalf("cfg2 portBinding wrong: %v", pb2)
	}
	if cfg2["dns"].(map[string]interface{})["dualStack"] != "PREFER_IPv6" {
		t.Fatalf("cfg2 dns wrong: %v", cfg2["dns"])
	}
	if cfg2["advancedSettings"].(map[string]interface{})["tcpIdleTimeout"] != "10m" {
		t.Fatalf("cfg2 extra not merged: %v", cfg2)
	}

	// Process specs carry isolated per-process env vars.
	p0 := rendered.Processes[0]
	if p0.Name != "mita-10" || len(p0.Args) != 1 || p0.Args[0] != "run" ||
		p0.Env["MITA_CONFIG_JSON_FILE"] != "{RUN_DIR}/mita-10.json" ||
		p0.Env["MITA_UDS_PATH"] != "{RUN_DIR}/mita-10.sock" ||
		p0.Env["MITA_INSECURE_UDS"] != "1" ||
		p0.Env["MITA_LOG_NO_TIMESTAMP"] != "1" {
		t.Fatalf("process 0 spec wrong: %+v", p0)
	}

	// Chain and split must be rejected on render.
	badChain := in1
	badChain.Via = &Endpoint{Host: "1.2.3.4", Port: 443}
	if _, err := Render(TypeMieru, []Inbound{badChain}); err == nil {
		t.Fatal("render must reject Via on Mieru")
	}
	badSplit := in1
	badSplit.Split = []SplitRule{{Action: SplitBlock, Domains: []string{"example.com"}}}
	if _, err := Render(TypeMieru, []Inbound{badSplit}); err == nil {
		t.Fatal("render must reject Split on Mieru")
	}
}

func TestMieruShareAndSubscriptions(t *testing.T) {
	tcpIn := Inbound{
		ID:       1,
		Protocol: ProtocolMieru,
		Port:     20120,
		Settings: Settings{
			Username:           "user1",
			Password:           "p@ss/word+1",
			MTU:                1400,
			MieruMultiplexing:  MieruMuxMiddle,
			MieruHandshakeMode: MieruHandshakeNoWait,
		},
		Transport: Transport{Type: TransportTCP},
	}
	udpIn := Inbound{
		ID:       2,
		Protocol: ProtocolMieru,
		Port:     20121,
		Settings: Settings{
			Username:           "user2",
			Password:           "simplepass",
			MTU:                1380,
			MieruMultiplexing:  MieruMuxLow,
			MieruHandshakeMode: MieruHandshakeStandard,
		},
		Transport: Transport{Type: TransportUDP},
	}
	if err := Normalize(TypeMieru, &tcpIn); err != nil {
		t.Fatal(err)
	}
	if err := Normalize(TypeMieru, &udpIn); err != nil {
		t.Fatal(err)
	}

	epTCP := Endpoint{Name: "Tokyo Mieru", Host: "203.0.113.10", Port: 20120, Inbound: tcpIn, Direct: true}
	epUDP := Endpoint{Name: "IPv6 Mieru", Host: "2001:db8::1", Port: 20121, Inbound: udpIn, Direct: true}

	uriTCP, ok := ShareURI(epTCP)
	if !ok {
		t.Fatal("TCP ShareURI failed")
	}
	if !strings.HasPrefix(uriTCP, "mierus://user1:p%40ss%2Fword+1@203.0.113.10?") ||
		!strings.Contains(uriTCP, "port=20120") ||
		!strings.Contains(uriTCP, "protocol=TCP") ||
		!strings.Contains(uriTCP, "multiplexing=MULTIPLEXING_MIDDLE") ||
		!strings.Contains(uriTCP, "handshake-mode=HANDSHAKE_NO_WAIT") ||
		!strings.Contains(uriTCP, "mtu=1400") ||
		!strings.Contains(uriTCP, "profile=Tokyo+Mieru") {
		t.Fatalf("unexpected TCP mierus URI: %s", uriTCP)
	}

	uriUDP, ok := ShareURI(epUDP)
	if !ok {
		t.Fatal("UDP ShareURI failed")
	}
	if !strings.HasPrefix(uriUDP, "mierus://user2:simplepass@[2001:db8::1]?") ||
		!strings.Contains(uriUDP, "port=20121") ||
		!strings.Contains(uriUDP, "protocol=UDP") ||
		!strings.Contains(uriUDP, "mtu=1380") ||
		strings.Contains(uriUDP, "handshake-mode=") {
		t.Fatalf("unexpected UDP mierus URI: %s", uriUDP)
	}

	// Clash & Stash include Mieru without unsupported mtu key.
	for _, format := range []string{FormatClash, FormatStash} {
		body, _, err := Subscription(format, []Endpoint{epTCP, epUDP}, SubOptions{})
		if err != nil {
			t.Fatalf("%s subscription error: %v", format, err)
		}
		for _, want := range []string{
			`type: "mieru"`,
			`transport: "TCP"`,
			`transport: "UDP"`,
			`username: "user1"`,
			`multiplexing: "MULTIPLEXING_MIDDLE"`,
			`handshake-mode: "HANDSHAKE_NO_WAIT"`,
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s missing %q:\n%s", format, want, body)
			}
		}
		if strings.Contains(body, "mtu:") {
			t.Fatalf("%s must not emit unsupported mtu field for mieru:\n%s", format, body)
		}
	}

	// Clients without Mieru support skip cleanly.
	for _, format := range []string{FormatSingbox, FormatSurge, FormatShadowrocket, FormatLoon, FormatQX} {
		body, _, err := Subscription(format, []Endpoint{epTCP, epUDP}, SubOptions{})
		if err != nil {
			t.Fatalf("%s subscription error: %v", format, err)
		}
		if strings.Contains(body, "Tokyo Mieru") || strings.Contains(body, "IPv6 Mieru") {
			t.Fatalf("%s must skip mieru endpoints:\n%s", format, body)
		}
	}

	// Round-trip ParseImport on generated mierus:// URIs.
	imported, errs := ParseImport(uriTCP + "\n" + uriUDP)
	if len(errs) > 0 || len(imported) != 2 {
		t.Fatalf("ParseImport mierus URIs failed: nodes=%+v errs=%v", imported, errs)
	}
	if imported[0].Name != "Tokyo Mieru" || imported[0].Host != "203.0.113.10" || imported[0].Port != 20120 ||
		imported[0].Proxy.str("password") != "p@ss/word+1" || imported[0].Proxy.str("transport") != "TCP" ||
		imported[0].Proxy.str("handshake-mode") != MieruHandshakeNoWait {
		t.Fatalf("imported TCP mismatch: %+v", imported[0])
	}
	if imported[1].Name != "IPv6 Mieru" || imported[1].Host != "2001:db8::1" || imported[1].Port != 20121 ||
		imported[1].Proxy.str("transport") != "UDP" || DescribeProxy(imported[1].Proxy) != "Mieru + UDP" {
		t.Fatalf("imported UDP mismatch: %+v summary=%q", imported[1], DescribeProxy(imported[1].Proxy))
	}

	// Chain checks reject Mieru as source and as target.
	if err := CheckChain(TypeMieru, Endpoint{Host: "1.2.3.4", Port: 443}); err == nil {
		t.Fatal("Mieru kernel must reject chaining out")
	}
	for _, kt := range []string{TypeXray, TypeSingbox} {
		if err := CheckChain(kt, epTCP); err == nil {
			t.Fatalf("%s must reject Mieru as chain target", kt)
		}
	}
}

func TestMieruImportPortRangeAndRelay(t *testing.T) {
	clashYAML := `proxies:
  - name: Mieru Range
    type: mieru
    server: 198.51.100.20
    port-range: 20000-20010
    transport: udp
    username: alice
    password: secret
    multiplexing: MULTIPLEXING_HIGH
`
	nodes, errs := ParseImport(clashYAML)
	if len(errs) > 0 || len(nodes) != 1 {
		t.Fatalf("ParseImport Clash mieru port-range failed: nodes=%+v errs=%v", nodes, errs)
	}
	n := nodes[0]
	if n.Port != 20000 || n.Proxy.str("port-range") != "20000-20010" || n.Proxy.str("transport") != "UDP" {
		t.Fatalf("imported port-range node wrong: %+v", n)
	}

	// Direct endpoint keeps port-range and omits port in Clash YAML (Mihomo forbids both).
	directEp := Endpoint{Name: n.Name, Host: n.Host, Port: n.Port, Direct: true, Proxy: n.Proxy}
	directClash, _, err := Subscription(FormatClash, []Endpoint{directEp}, SubOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(directClash, `port-range: "20000-20010"`) || strings.Contains(directClash, "\n    port:") {
		t.Fatalf("direct port-range Clash YAML invalid:\n%s", directClash)
	}
	directURI, ok := ShareURI(directEp)
	if !ok || !strings.Contains(directURI, "port=20000-20010") {
		t.Fatalf("direct port-range URI invalid: %q", directURI)
	}

	// Relay endpoint drops port-range and uses the relay's single port.
	relayEp := Endpoint{Name: "Relay -> Mieru", Host: "203.0.113.5", Port: 31000, Direct: false, Proxy: n.Proxy}
	relayClash, _, err := Subscription(FormatClash, []Endpoint{relayEp}, SubOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(relayClash, "port-range:") || !strings.Contains(relayClash, "port: 31000") {
		t.Fatalf("relay Clash YAML must drop port-range and use single port:\n%s", relayClash)
	}
	relayURI, ok := ShareURI(relayEp)
	if !ok || !strings.Contains(relayURI, "port=31000") || strings.Contains(relayURI, "20000-20010") {
		t.Fatalf("relay URI must use relay port: %q", relayURI)
	}

	// Bad mierus:// links are rejected.
	for _, bad := range []string{
		"mierus://onlyuser@1.2.3.4?port=20000&protocol=TCP",
		"mierus://u:p@1.2.3.4?port=20000&protocol=QUIC",
		"mierus://u:p@1.2.3.4?protocol=TCP",
	} {
		if got, errs := ParseImport(bad); len(got) != 0 || len(errs) == 0 {
			t.Fatalf("bad URI %q should fail: got=%+v errs=%v", bad, got, errs)
		}
	}
}
