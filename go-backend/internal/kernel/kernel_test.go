package kernel

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

const (
	testUUID       = "0b6e4c38-8f0e-4a3e-9d2d-7a1c2b3c4d5e"
	testRealityKey = "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA" // bytes 1..32, raw-url
)

func TestNormalizeFillsCredentials(t *testing.T) {
	in := Inbound{ID: 1, Protocol: "VLESS", Port: 443, Settings: Settings{Flow: FlowVision}, Security: Security{Type: SecurityReality}}
	if err := Normalize(TypeXray, &in); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if in.Protocol != ProtocolVLESS || in.Transport.Type != TransportTCP || in.Listen != "::" {
		t.Fatalf("defaults not applied: %+v", in)
	}
	if !uuidPattern.MatchString(in.Settings.UUID) {
		t.Fatalf("uuid not generated: %q", in.Settings.UUID)
	}
	if in.Settings.Flow != FlowVision {
		t.Fatalf("vision flow dropped on tcp+reality")
	}
	sec := in.Security
	if sec.SNI != DefaultRealitySNI || sec.RealityDest != DefaultRealitySNI+":443" {
		t.Fatalf("reality defaults: %+v", sec)
	}
	pub, err := RealityPublicKey(sec.RealityPrivateKey)
	if err != nil || pub != sec.RealityPublicKey {
		t.Fatalf("public key mismatch: %v", err)
	}
	if len(sec.RealityShortIDs) != 1 || len(sec.RealityShortIDs[0]) != 16 {
		t.Fatalf("short id: %v", sec.RealityShortIDs)
	}
}

func TestNormalizeRejectsUnsupportedCombos(t *testing.T) {
	cases := []struct {
		name string
		in   Inbound
	}{
		{"unknown protocol", Inbound{Protocol: "snell", Port: 1}},
		{"ws reality", Inbound{Protocol: ProtocolVLESS, Port: 1, Transport: Transport{Type: TransportWS}, Security: Security{Type: SecurityReality}}},
		{"trojan without tls", Inbound{Protocol: ProtocolTrojan, Port: 1, Security: Security{Type: SecurityNone}}},
		{"bad port", Inbound{Protocol: ProtocolVMess, Port: 70000}},
		{"tls without sni", Inbound{Protocol: ProtocolVMess, Port: 1, Security: Security{Type: SecurityTLS}}},
		{"bad ss2022 key", Inbound{Protocol: ProtocolShadowsocks, Port: 1, Settings: Settings{Method: DefaultSSMethod, Password: "short"}}},
		{"bad uuid", Inbound{Protocol: ProtocolVMess, Port: 1, Settings: Settings{UUID: "nope"}}},
		{"extra not object", Inbound{Protocol: ProtocolVMess, Port: 1, Extra: json.RawMessage(`[1]`)}},
		{"bad short id", Inbound{Protocol: ProtocolVLESS, Port: 1, Security: Security{Type: SecurityReality, RealityShortIDs: []string{"xyz"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := tc.in
			if err := Normalize(TypeXray, &in); err == nil {
				t.Fatalf("expected error for %+v", tc.in)
			}
		})
	}
	in := Inbound{Protocol: ProtocolVLESS, Port: 1}
	if err := Normalize(TypeSnell, &in); err == nil {
		t.Fatal("unimplemented kernel must be rejected")
	}
}

func TestNormalizeDropsVisionWhereUnsupported(t *testing.T) {
	in := Inbound{Protocol: ProtocolVLESS, Port: 1, Transport: Transport{Type: TransportWS}, Security: Security{Type: SecurityNone}, Settings: Settings{Flow: FlowVision}}
	if err := Normalize(TypeXray, &in); err != nil {
		t.Fatal(err)
	}
	if in.Settings.Flow != "" {
		t.Fatalf("vision must be cleared on ws: %q", in.Settings.Flow)
	}
	if !strings.HasPrefix(in.Transport.Path, "/") {
		t.Fatalf("ws path default: %q", in.Transport.Path)
	}
}

func TestNormalizeSS2022Key(t *testing.T) {
	in := Inbound{Protocol: ProtocolShadowsocks, Port: 8388}
	if err := Normalize(TypeXray, &in); err != nil {
		t.Fatal(err)
	}
	key, err := base64.StdEncoding.DecodeString(in.Settings.Password)
	if err != nil || len(key) != 16 {
		t.Fatalf("ss2022 key: %q", in.Settings.Password)
	}
	if in.Network() != "tcp,udp" {
		t.Fatalf("ss network: %s", in.Network())
	}
}

func TestNormalizeSelfSignedCertKeptWhenSNIUnchanged(t *testing.T) {
	in := Inbound{Protocol: ProtocolTrojan, Port: 443, Security: Security{Type: SecurityTLS, SNI: "a.example.com"}}
	if err := Normalize(TypeXray, &in); err != nil {
		t.Fatal(err)
	}
	cert := in.Security.CertPEM
	if !certMatchesName(cert, "a.example.com") {
		t.Fatal("cert does not match sni")
	}
	if err := Normalize(TypeXray, &in); err != nil {
		t.Fatal(err)
	}
	if in.Security.CertPEM != cert {
		t.Fatal("cert regenerated although sni unchanged")
	}
	in.Security.SNI = "b.example.com"
	if err := Normalize(TypeXray, &in); err != nil {
		t.Fatal(err)
	}
	if in.Security.CertPEM == cert || !certMatchesName(in.Security.CertPEM, "b.example.com") {
		t.Fatal("cert must follow sni change")
	}
}

func TestRegenerateRotatesSecrets(t *testing.T) {
	in := Inbound{Protocol: ProtocolVLESS, Port: 1, Security: Security{Type: SecurityReality}}
	if err := Normalize(TypeXray, &in); err != nil {
		t.Fatal(err)
	}
	before := in
	if err := Regenerate(TypeXray, &in); err != nil {
		t.Fatal(err)
	}
	if in.Settings.UUID == before.Settings.UUID || in.Security.RealityPrivateKey == before.Security.RealityPrivateKey {
		t.Fatal("secrets not rotated")
	}
	if in.Security.SNI != before.Security.SNI {
		t.Fatal("shape must be kept")
	}
}

func goldenInbounds(t *testing.T) []Inbound {
	t.Helper()
	certPEM := "-----BEGIN CERTIFICATE-----\nMIIB\nAAAA\n-----END CERTIFICATE-----\n"
	keyPEM := "-----BEGIN EC PRIVATE KEY-----\nMHcC\n-----END EC PRIVATE KEY-----\n"
	list := []Inbound{
		{ID: 3, Protocol: ProtocolVMess, Port: 8443,
			Settings:  Settings{UUID: testUUID},
			Transport: Transport{Type: TransportWS, Path: "/ws", Host: "cdn.example.com"},
			Security:  Security{Type: SecurityTLS, SNI: "cdn.example.com", CertMode: "file", CertFile: "/etc/ssl/a.crt", KeyFile: "/etc/ssl/a.key"}},
		{ID: 1, Protocol: ProtocolVLESS, Port: 443,
			Settings: Settings{UUID: testUUID, Flow: FlowVision},
			Security: Security{Type: SecurityReality, SNI: "www.example.com", RealityPrivateKey: testRealityKey, RealityShortIDs: []string{"0123456789abcdef"}}},
		{ID: 2, Protocol: ProtocolShadowsocks, Port: 8388,
			Settings: Settings{Method: DefaultSSMethod, Password: "AAAAAAAAAAAAAAAAAAAAAA=="}},
		{ID: 4, Protocol: ProtocolTrojan, Port: 2053,
			Settings:  Settings{Password: "trojan-secret"},
			Transport: Transport{Type: TransportGRPC, ServiceName: "tun"},
			Security:  Security{Type: SecurityTLS, SNI: "t.example.com", ALPN: []string{"h2"}, CertPEM: certPEM, KeyPEM: keyPEM}},
		{ID: 5, Protocol: ProtocolVLESS, Port: 9443, Listen: "127.0.0.1",
			Settings:  Settings{UUID: testUUID},
			Transport: Transport{Type: TransportXHTTP, Path: "/x"},
			Security:  Security{Type: SecurityNone},
			Extra:     json.RawMessage(`{"sniffing":{"enabled":false},"streamSettings":{"sockopt":{"tcpFastOpen":true}}}`)},
	}
	for i := range list {
		if list[i].Security.Type == SecurityTLS && list[i].Security.CertPEM != "" {
			// Keep the fixed fake PEM: Normalize would replace a cert that
			// does not parse, so only validate the non-TLS parts here.
			saved := list[i].Security
			if err := Normalize(TypeXray, &list[i]); err != nil {
				t.Fatalf("normalize %d: %v", list[i].ID, err)
			}
			list[i].Security.CertPEM, list[i].Security.KeyPEM = saved.CertPEM, saved.KeyPEM
			continue
		}
		if err := Normalize(TypeXray, &list[i]); err != nil {
			t.Fatalf("normalize %d: %v", list[i].ID, err)
		}
	}
	return list
}

func TestRenderXrayGolden(t *testing.T) {
	out, err := Render(TypeXray, goldenInbounds(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Files) != 1 || out.Files[0].Name != "config.json" {
		t.Fatalf("files: %+v", out.Files)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(out.Files[0].Content), &parsed); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	assertGolden(t, "xray.golden.json", out.Files[0].Content)

	again, _ := Render(TypeXray, goldenInbounds(t))
	if again.Hash != out.Hash {
		t.Fatal("render must be deterministic")
	}
}

func TestRenderEmptyXray(t *testing.T) {
	out, err := Render(TypeXray, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Files[0].Content, `"inbounds": []`) {
		t.Fatalf("empty inbounds must render as []: %s", out.Files[0].Content)
	}
}

func TestResolveDownload(t *testing.T) {
	xray, _ := Lookup(TypeXray)
	d, err := ResolveDownload(xray, "v25.1.1", "https://mirror.example/")
	if err != nil {
		t.Fatal(err)
	}
	if d["url"] != "https://mirror.example/https://github.com/XTLS/Xray-core/releases/download/v25.1.1/Xray-linux-{ARCH}.zip" {
		t.Fatalf("url: %v", d["url"])
	}
	latest, _ := ResolveDownload(xray, "latest", "")
	if !strings.Contains(latest["url"].(string), "/releases/latest/download/") || latest["checksumUrl"] == "" {
		t.Fatalf("latest: %v", latest)
	}

	snell, _ := Lookup(TypeSnell)
	sd, err := ResolveDownload(snell, "latest", "https://mirror.example")
	if err != nil {
		t.Fatal(err)
	}
	if sd["url"] != "https://dl.nssurge.com/snell/snell-server-v5.0.1-linux-{ARCH}.zip" {
		t.Fatalf("snell must default to 5.0.1 and never use the github mirror: %v", sd["url"])
	}
	if sd["sha256"].(map[string]string)["amd64"] != "9bea1c2b9e35b73b31634856c04d18c393072b9e5dcde6a32781d8b8f908c539" {
		t.Fatalf("snell pin: %v", sd["sha256"])
	}
	if _, err := ResolveDownload(snell, "6.0.0rc", ""); err == nil {
		t.Fatal("unpinned snell version must be rejected")
	}

	sb, _ := Lookup(TypeSingbox)
	sl, err := ResolveDownload(sb, "latest", "")
	if err != nil || !strings.Contains(sl["url"].(string), "{VERSION}") || sl["github"].(map[string]interface{})["repo"] != "SagerNet/sing-box" {
		t.Fatalf("sing-box latest must defer to the agent: %v %v", sl, err)
	}
	sv, _ := ResolveDownload(sb, "1.12.8", "")
	if sv["url"] != "https://github.com/SagerNet/sing-box/releases/download/v1.12.8/sing-box-1.12.8-linux-{ARCH}.tar.gz" || sv["github"].(map[string]interface{})["tag"] != "v1.12.8" {
		t.Fatalf("sing-box pinned version: %v", sv)
	}

	st, err := ExtraDownload(ShadowTLSDownload(), ShadowTLSVersion, "")
	if err != nil || st["sha256"].(map[string]string)["x86_64-unknown-linux-musl"] == "" {
		t.Fatalf("shadow-tls: %v %v", st, err)
	}
}

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update): %v", err)
	}
	if string(want) != got {
		t.Fatalf("%s mismatch:\n--- got ---\n%s", name, got)
	}
}

func TestRenderAcceptProxyProtocol(t *testing.T) {
	ins := []Inbound{
		{ID: 1, Protocol: ProtocolVLESS, Port: 443, Security: Security{Type: SecurityReality}, AcceptProxyProtocol: true},
		{ID: 2, Protocol: ProtocolShadowsocks, Port: 8388, AcceptProxyProtocol: true},
		{ID: 3, Protocol: ProtocolShadowsocks, Port: 8389},
	}
	for i := range ins {
		if err := Normalize(TypeXray, &ins[i]); err != nil {
			t.Fatal(err)
		}
	}
	out, err := Render(TypeXray, ins)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Inbounds []struct {
			Tag            string                 `json:"tag"`
			StreamSettings map[string]interface{} `json:"streamSettings"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal([]byte(out.Files[0].Content), &cfg); err != nil {
		t.Fatal(err)
	}
	for _, in := range cfg.Inbounds[:2] {
		sockopt, _ := in.StreamSettings["sockopt"].(map[string]interface{})
		if sockopt["acceptProxyProtocol"] != true {
			t.Fatalf("%s: acceptProxyProtocol missing: %v", in.Tag, in.StreamSettings)
		}
	}
	if cfg.Inbounds[2].StreamSettings != nil {
		t.Fatalf("plain ss must not get streamSettings: %v", cfg.Inbounds[2].StreamSettings)
	}
}

func TestParseAllowList(t *testing.T) {
	got, err := ParseAllowList("1.2.3.4, 10.0.0.0/8\n2001:db8::1，1.2.3.4 10.1.2.3/8")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"1.2.3.4/32", "10.0.0.0/8", "2001:db8::1/128"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v want %v", got, want)
	}
	for _, bad := range []string{"example.com", "1.2.3.4/33", "1.2.3.4; drop"} {
		if _, err := ParseAllowList(bad); err == nil {
			t.Fatalf("%q must be rejected", bad)
		}
	}
	if got, _ := ParseAllowList("  "); len(got) != 0 {
		t.Fatal("empty list")
	}
}

func phase3Inbounds(t *testing.T) []Inbound {
	t.Helper()
	certPEM := "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"
	keyPEM := "-----BEGIN EC PRIVATE KEY-----\nMHcC\n-----END EC PRIVATE KEY-----\n"
	tls := Security{Type: SecurityTLS, SNI: "www.bing.com", CertMode: "self", CertPEM: certPEM, KeyPEM: keyPEM, Fingerprint: DefaultFingerprint}
	list := []Inbound{
		{ID: 1, Protocol: ProtocolHysteria2, Listen: "::", Port: 443, Transport: Transport{Type: TransportQUIC}, Security: tls,
			Settings: Settings{Password: "hy2-secret", Masquerade: "https://www.bing.com"}},
		{ID: 2, Protocol: ProtocolTUIC, Listen: "::", Port: 8443, Transport: Transport{Type: TransportQUIC}, Security: tls,
			Settings: Settings{UUID: testUUID, Password: "tuic-secret"}},
		{ID: 3, Protocol: ProtocolAnyTLS, Listen: "::", Port: 9443, Security: tls, Settings: Settings{Password: "anytls-secret"}},
		{ID: 4, Protocol: ProtocolShadowsocks, Listen: "::", Port: 10443,
			Settings: Settings{Method: DefaultSSMethod, Password: "AAAAAAAAAAAAAAAAAAAAAA=="},
			Security: Security{Type: SecurityShadowTLS, SNI: "www.microsoft.com", ShadowTLSPassword: "stls-secret-123"}},
		{ID: 5, Protocol: ProtocolVLESS, Listen: "::", Port: 11443, Settings: Settings{UUID: testUUID, Flow: FlowVision},
			Security: Security{Type: SecurityReality, SNI: "www.example.com", RealityPrivateKey: testRealityKey, RealityShortIDs: []string{"0123456789abcdef"}}},
	}
	for i := range list {
		saved := list[i].Security
		if err := Normalize(TypeSingbox, &list[i]); err != nil {
			t.Fatalf("normalize %d: %v", list[i].ID, err)
		}
		if saved.CertPEM != "" {
			list[i].Security.CertPEM, list[i].Security.KeyPEM = saved.CertPEM, saved.KeyPEM
		}
	}
	return list
}

func TestRenderSingboxGolden(t *testing.T) {
	out, err := Render(TypeSingbox, phase3Inbounds(t))
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(out.Files[0].Content), &parsed); err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "singbox-server.golden.json", out.Files[0].Content)
	if len(parsed["inbounds"].([]interface{})) != 6 {
		t.Fatal("shadowtls must add a loopback shadowsocks backend")
	}
}

func TestRenderSnell(t *testing.T) {
	ins := []Inbound{
		{ID: 7, Protocol: ProtocolSnell, Listen: "::", Port: 6160, Settings: Settings{Password: "psk-aaaaaaaa", SnellVersion: 4}},
		{ID: 8, Protocol: ProtocolSnell, Listen: "0.0.0.0", Port: 6161, Settings: Settings{Password: "psk-bbbbbbbb", BackendPort: 36161},
			Security: Security{Type: SecurityShadowTLS, SNI: "www.microsoft.com", ShadowTLSPassword: "stls-pass-1"}},
	}
	for i := range ins {
		if err := Normalize(TypeSnell, &ins[i]); err != nil {
			t.Fatal(err)
		}
	}
	out, err := Render(TypeSnell, ins)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, f := range out.Files {
		b.WriteString("# " + f.Name + "\n" + f.Content)
	}
	for _, p := range out.Processes {
		b.WriteString(p.Name + " [" + p.Bin + "] " + strings.Join(p.Args, " ") + "\n")
	}
	b.WriteString("extras: " + strings.Join(out.Extras, ",") + "\n")
	assertGolden(t, "snell.golden.txt", b.String())
	if ins[0].Network() != "tcp" || ins[1].Network() != "tcp" {
		t.Fatal("snell v4 and shadowtls-fronted snell are tcp only")
	}
	idle, _ := Render(TypeSnell, nil)
	if !idle.Idle || len(idle.Processes) != 0 {
		t.Fatal("snell without inbounds must be idle")
	}
}

func TestNormalizePhase3Rules(t *testing.T) {
	hy := Inbound{Protocol: ProtocolHysteria2, Port: 443, Transport: Transport{Type: TransportQUIC}, Security: Security{Type: SecurityTLS}}
	if err := Normalize(TypeSingbox, &hy); err != nil {
		t.Fatal(err)
	}
	if hy.Security.SNI != DefaultQUICSNI || strings.Join(hy.Security.ALPN, ",") != "h3" || hy.Settings.Password == "" || hy.Network() != "udp" {
		t.Fatalf("hy2 defaults: %+v", hy)
	}
	sn := Inbound{Protocol: ProtocolSnell, Port: 6160, Security: Security{Type: SecurityShadowTLS}}
	if err := Normalize(TypeSnell, &sn); err != nil {
		t.Fatal(err)
	}
	if sn.Settings.SnellVersion != 5 || sn.Settings.BackendPort < 20000 || sn.Security.ShadowTLSPassword == "" || sn.Security.SNI != DefaultRealitySNI {
		t.Fatalf("snell defaults: %+v", sn)
	}
	bad := []struct {
		kernel string
		in     Inbound
	}{
		{TypeSnell, Inbound{Protocol: ProtocolSnell, Port: 1, Settings: Settings{SnellVersion: 7}}},
		{TypeSnell, Inbound{Protocol: ProtocolSnell, Port: 1, Settings: Settings{Password: "has space"}}},
		{TypeSingbox, Inbound{Protocol: ProtocolHysteria2, Port: 1, Transport: Transport{Type: TransportQUIC}, Settings: Settings{Masquerade: "ftp://x"}}},
		{TypeSingbox, Inbound{Protocol: ProtocolVLESS, Port: 1, AcceptProxyProtocol: true}},
		{TypeXray, Inbound{Protocol: ProtocolHysteria2, Port: 1}},
		{TypeSingbox, Inbound{Protocol: ProtocolTrojan, Port: 1, Security: Security{Type: SecurityShadowTLS}}},
	}
	for i, tc := range bad {
		in := tc.in
		if err := Normalize(tc.kernel, &in); err == nil {
			t.Fatalf("case %d must fail: %+v", i, tc.in)
		}
	}
}

func TestSingboxACMEAndNaive(t *testing.T) {
	acme := Security{Type: SecurityTLS, CertMode: CertModeACME, SNI: "hk.example.com", ACMEEmail: "me@example.com"}
	list := []Inbound{
		{ID: 1, Protocol: ProtocolNaive, Port: 443, Settings: Settings{Username: "flyx", Password: "naive-secret"}, Security: acme},
		{ID: 2, Protocol: ProtocolHysteria2, Port: 443, Transport: Transport{Type: TransportQUIC}, Settings: Settings{Password: "hy2-secret"}, Security: acme},
		{ID: 3, Protocol: ProtocolTrojan, Port: 8443, Settings: Settings{Password: "trojan-secret"},
			Security: Security{Type: SecurityTLS, CertMode: CertModeACME, SNI: "cf.example.com", ACMEDNS: ACMEDNSCloudflare, ACMEDNSKey: "cf-token"}},
		{ID: 4, Protocol: ProtocolAnyTLS, Port: 9443, Settings: Settings{Password: "anytls-secret"}, Security: acme},
		{ID: 5, Protocol: ProtocolVLESS, Port: 10443, Transport: Transport{Type: TransportWS, Path: "/ws"}, Settings: Settings{UUID: testUUID}, Security: acme},
	}
	for i := range list {
		if err := Normalize(TypeSingbox, &list[i]); err != nil {
			t.Fatalf("inbound %d: %v", list[i].ID, err)
		}
	}
	out, err := Render(TypeSingbox, list)
	if err != nil {
		t.Fatal(err)
	}
	cfg := out.Files[0].Content
	assertGolden(t, "singbox-acme.golden.json", cfg)
	var parsed struct {
		Providers []map[string]interface{} `json:"certificate_providers"`
		Inbounds  []map[string]interface{} `json:"inbounds"`
	}
	if err := json.Unmarshal([]byte(cfg), &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Providers) != 2 {
		t.Fatalf("one provider per domain, got %d", len(parsed.Providers))
	}
	for i, want := range []string{"acme-1", "acme-1", "acme-2", "acme-1", "acme-1"} {
		if got := parsed.Inbounds[i]["tls"].(map[string]interface{})["certificate_provider"]; got != want {
			t.Fatalf("inbound %d provider %v, want %s", i, got, want)
		}
	}
	// HTTP-01/TLS-ALPN puts acme-tls/1 in the listener's ALPN; uTLS clients
	// offering h2/http/1.1 are refused unless the usual protocols sit next
	// to it. DNS-01 adds nothing, and QUIC keeps h3.
	for i, want := range []string{"[h2 http/1.1]", "[h3]", "<nil>", "[h2 http/1.1]", "[http/1.1]"} {
		if got := fmt.Sprint(parsed.Inbounds[i]["tls"].(map[string]interface{})["alpn"]); got != want {
			t.Fatalf("inbound %d alpn %s, want %s", i, got, want)
		}
	}

	for name, c := range map[string]struct {
		kernel string
		in     Inbound
		want   string
	}{
		"acme on xray":       {TypeXray, Inbound{Protocol: ProtocolTrojan, Port: 1443, Security: acme}, "sing-box"},
		"self-signed naive":  {TypeSingbox, Inbound{Protocol: ProtocolNaive, Port: 1443, Security: Security{Type: SecurityTLS, SNI: "a.example.com"}}, "真实证书"},
		"acme without host":  {TypeSingbox, Inbound{Protocol: ProtocolHysteria2, Port: 1443, Transport: Transport{Type: TransportQUIC}, Security: Security{Type: SecurityTLS, CertMode: CertModeACME}}, "域名"},
		"alidns w/o secret":  {TypeSingbox, Inbound{Protocol: ProtocolAnyTLS, Port: 1443, Security: Security{Type: SecurityTLS, CertMode: CertModeACME, SNI: "a.example.com", ACMEDNS: ACMEDNSAliDNS, ACMEDNSKey: "id"}}, "AccessKey"},
		"naive bad username": {TypeSingbox, Inbound{Protocol: ProtocolNaive, Port: 1443, Settings: Settings{Username: "a b"}, Security: acme}, "用户名"},
	} {
		in := c.in
		if err := Normalize(c.kernel, &in); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: got %v, want error containing %q", name, err, c.want)
		}
	}

	naive := list[0]
	direct := Endpoint{Name: "naive", Host: "hk.example.com", Port: 443, Inbound: naive, Direct: true}
	relay := Endpoint{Name: "relay", Host: "198.51.100.7", Port: 31000, Inbound: naive}
	if uri, _ := ShareURI(direct); uri != "naive+https://flyx:naive-secret@hk.example.com:443#naive" {
		t.Fatalf("naive uri: %s", uri)
	}
	if uri, _ := ShareURI(relay); !strings.Contains(uri, "@198.51.100.7:31000?sni=hk.example.com#") {
		t.Fatalf("relay naive uri needs sni: %s", uri)
	}
	if _, ok := ClashProxy(direct); ok {
		t.Fatal("Mihomo has no naive client")
	}
	if _, ok := SurgeLine(direct); ok {
		t.Fatal("Surge has no naive client")
	}
	o, _ := singboxOutbound(direct)
	tls := o["tls"].(map[string]interface{})
	if o["type"] != "naive" || tls["utls"] != nil || tls["insecure"] != nil {
		t.Fatalf("sing-box naive outbound: %v", o)
	}
	if uri, _ := ShareURI(Endpoint{Name: "hy2", Host: "hk.example.com", Port: 443, Inbound: list[1], Direct: true}); strings.Contains(uri, "insecure") {
		t.Fatalf("ACME certificates are trusted: %s", uri)
	}
}

func TestRealityMinClientVer(t *testing.T) {
	in := Inbound{ID: 1, Protocol: ProtocolVLESS, Port: 443, Security: Security{Type: SecurityReality}}
	if err := Normalize(TypeXray, &in); err != nil || in.Security.RealityMinClientVer != "1.0.0" {
		t.Fatalf("default: %v %q", err, in.Security.RealityMinClientVer)
	}
	// Stored before the field existed: still rendered with the default.
	in.Security.RealityMinClientVer = ""
	out, err := Render(TypeXray, []Inbound{in})
	if err != nil || !strings.Contains(out.Files[0].Content, `"minClientVer": "1.0.0"`) {
		t.Fatalf("legacy inbound must get minClientVer: %v", err)
	}
	bad := Inbound{ID: 2, Protocol: ProtocolVLESS, Port: 443, Security: Security{Type: SecurityReality, RealityMinClientVer: "v26"}}
	if err := Normalize(TypeXray, &bad); err == nil {
		t.Fatal("malformed version must be rejected")
	}
}
