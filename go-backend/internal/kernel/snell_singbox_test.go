package kernel

import (
	"strings"
	"testing"
)

func singboxSnellInbounds(t *testing.T) []Inbound {
	t.Helper()
	ins := []Inbound{
		{ID: 1, Protocol: ProtocolSnell, Port: 6160, Settings: Settings{Password: "psk-aaaaaaaa", SnellVersion: 4}},
		{ID: 2, Protocol: ProtocolSnell, Port: 6161, Settings: Settings{Password: "psk-bbbbbbbbbbbb", SnellVersion: 6}},
		{ID: 3, Protocol: ProtocolSnell, Port: 6162, Settings: Settings{Password: "psk-cccccccc"},
			Security: Security{Type: SecurityShadowTLS, SNI: "www.microsoft.com", ShadowTLSPassword: "stls-pass-1"}},
	}
	for i := range ins {
		if err := Normalize(TypeSingbox, &ins[i]); err != nil {
			t.Fatalf("normalize %d: %v", ins[i].ID, err)
		}
	}
	return ins
}

func TestRenderSingboxSnell(t *testing.T) {
	ins := singboxSnellInbounds(t)
	out, err := Render(TypeSingbox, ins)
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "singbox-snell.golden.json", out.Files[0].Content)

	if ins[2].Settings.SnellVersion != DefaultSnellVersion || ins[2].Settings.BackendPort != 0 {
		t.Fatalf("sing-box snell behind shadowtls needs no backend port: %+v", ins[2].Settings)
	}
	if ins[0].Network() != "tcp" || ins[1].Network() != "tcp" || ins[2].Network() != "tcp" {
		t.Fatal("v4, v6 and shadowtls-fronted snell are tcp only")
	}
	list := renderJSONString(t, out.Files[0].Content)["inbounds"].([]interface{})
	if len(list) != 4 {
		t.Fatalf("shadowtls must add a loopback snell backend: %v", list)
	}
	v4, v6 := list[0].(map[string]interface{}), list[1].(map[string]interface{})
	if v4["type"] != "snell" || v4["version"] != 5.0 || v6["version"] != 6.0 {
		t.Fatalf("v4 clients use the v5 server, v6 its own: %v %v", v4, v6)
	}
	front, backend := list[2].(map[string]interface{}), list[3].(map[string]interface{})
	if front["type"] != "shadowtls" || front["detour"] != "in-3-snell" || backend["tag"] != "in-3-snell" ||
		backend["type"] != "snell" || backend["listen"] != "127.0.0.1" {
		t.Fatalf("shadowtls front: %v -> %v", front, backend)
	}
}

func TestSingboxSnellRules(t *testing.T) {
	bad := []struct {
		kernel string
		in     Inbound
	}{
		{TypeSingbox, Inbound{Protocol: ProtocolSnell, Port: 1, Settings: Settings{SnellVersion: 7}}},
		{TypeSingbox, Inbound{Protocol: ProtocolSnell, Port: 1, Settings: Settings{SnellVersion: 6, Password: "short-psk"}}},
		{TypeSnell, Inbound{Protocol: ProtocolSnell, Port: 1, Settings: Settings{SnellVersion: 6, Password: "short-psk"}}},
	}
	for i, tc := range bad {
		in := tc.in
		if err := Normalize(tc.kernel, &in); err == nil {
			t.Fatalf("case %d must be rejected: %+v", i, in)
		}
	}
	v6 := Inbound{Protocol: ProtocolSnell, Port: 1, Settings: Settings{SnellVersion: 6}}
	if err := Normalize(TypeSingbox, &v6); err != nil || len(v6.Settings.Password) < snellV6MinPSK {
		t.Fatalf("generated v6 psk: %q %v", v6.Settings.Password, err)
	}
}

func TestSingboxSnellChainCoversBackend(t *testing.T) {
	in := singboxSnellInbounds(t)[2]
	in.Via = &Endpoint{}
	cfg := renderJSON(t, TypeSingbox, []Inbound{in})
	rule := cfg["route"].(map[string]interface{})["rules"].([]interface{})[0].(map[string]interface{})
	tags := rule["inbound"].([]interface{})
	if rule["action"] != "reject" || len(tags) != 2 || tags[1] != "in-3-snell" {
		t.Fatalf("the ShadowTLS front and its Snell backend route together: %v", rule)
	}
}

func TestSurgeSnellV6(t *testing.T) {
	in := singboxSnellInbounds(t)[1]
	line, ok := SurgeLine(Endpoint{Name: "v6", Host: "1.2.3.4", Port: in.Port, Inbound: in})
	if !ok || !strings.Contains(line, "snell, 1.2.3.4, 6161, psk=psk-bbbbbbbbbbbb, version=6") {
		t.Fatalf("surge line: %q", line)
	}
}

func snellEndpoints(t *testing.T) []Endpoint {
	t.Helper()
	ins := singboxSnellInbounds(t)
	ins[1].Settings.SnellMode = SnellModeUnshaped
	if err := Normalize(TypeSingbox, &ins[1]); err != nil {
		t.Fatal(err)
	}
	names := []string{"Snell v4", "Snell v6", "Snell STLS"}
	eps := make([]Endpoint, len(ins))
	for i, in := range ins {
		eps[i] = Endpoint{Name: names[i], Host: "127.0.0.1", Port: in.Port, Direct: true, Inbound: in}
	}
	return eps
}

func TestSingboxClientHasSnell(t *testing.T) {
	for ua, want := range map[string]bool{
		"SFA (sing-box 1.14.3; language zh_CN)": true,
		"SFI/1.15.0 (sing-box 1.15.0)":          true,
		"sing-box/2.0.1":                        true,
		"SFA (sing-box 1.12.8; language en_US)": false,
		"SFM (sing-box 1.13.9)":                 false,
		"clash-verge/v2.0":                      false,
		"":                                      false,
	} {
		if got := SingboxClientHasSnell(ua); got != want {
			t.Errorf("%q: got %v, want %v", ua, got, want)
		}
	}
}

func TestSingboxProfileSnellOnlyForNewClients(t *testing.T) {
	eps := snellEndpoints(t)
	old, _, err := Subscription(FormatSingbox, eps, SubOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(old, `"snell"`) {
		t.Fatal("clients before sing-box 1.14 must not get Snell")
	}
	sb, _, err := Subscription(FormatSingbox, eps, SubOptions{SingboxSnell: true})
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "singbox-snell-client.golden.json", sb)
	obs := renderJSONString(t, sb)["outbounds"].([]interface{})
	byTag := map[string]map[string]interface{}{}
	for _, o := range obs {
		m := o.(map[string]interface{})
		byTag[m["tag"].(string)] = m
	}
	if v4 := byTag["Snell v4"]; v4["type"] != "snell" || v4["version"] != 4.0 || v4["psk"] != "psk-aaaaaaaa" {
		t.Fatalf("v4 client: %v", v4)
	}
	if v6 := byTag["Snell v6"]; v6["version"] != 6.0 || v6["mode"] != "unshaped" {
		t.Fatalf("v6 client: %v", v6)
	}
	stls, front := byTag["Snell STLS"], byTag["Snell STLS (shadowtls)"]
	if stls["detour"] != "Snell STLS (shadowtls)" || stls["server"] != nil || front["type"] != "shadowtls" || front["password"] != "stls-pass-1" {
		t.Fatalf("shadowtls pair: %v %v", stls, front)
	}
}

func TestSingboxChainThroughSnell(t *testing.T) {
	via := snellEndpoints(t)[2]
	in := Inbound{ID: 5, Protocol: ProtocolVLESS, Port: 8443, Settings: Settings{UUID: testUUID}, Security: Security{Type: SecurityNone}}
	if err := Normalize(TypeSingbox, &in); err != nil {
		t.Fatal(err)
	}
	in.Via = &via
	if err := CheckChainWith(TypeSingbox, via, RenderOptions{}); err == nil || !strings.Contains(err.Error(), "1.14") {
		t.Fatalf("sing-box before 1.14 cannot dial snell: %v", err)
	}
	if err := CheckChainWith(TypeSingbox, via, RenderOptions{SingboxSnell: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := Render(TypeSingbox, []Inbound{in}); err == nil {
		t.Fatal("rendering a snell chain for an old sing-box must fail")
	}
	out, err := RenderWith(TypeSingbox, []Inbound{in}, RenderOptions{SingboxSnell: true})
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "singbox-snell-chain.golden.json", out.Files[0].Content)
	if ob := outboundByTag(renderJSONString(t, out.Files[0].Content), "via-5"); ob == nil || ob["type"] != "snell" || ob["detour"] == nil {
		t.Fatalf("chain outbound: %v", ob)
	}
}

func TestSnellModeRules(t *testing.T) {
	in := Inbound{Protocol: ProtocolSnell, Port: 1, Settings: Settings{SnellVersion: 5, SnellMode: SnellModeUnshaped}}
	if err := Normalize(TypeSingbox, &in); err != nil || in.Settings.SnellMode != "" {
		t.Fatalf("mode is v6 only: %+v %v", in.Settings, err)
	}
	bad := Inbound{Protocol: ProtocolSnell, Port: 1, Settings: Settings{SnellVersion: 6, SnellMode: "unsafe-raw"}}
	if err := Normalize(TypeSingbox, &bad); err == nil {
		t.Fatal("unsafe-raw must be refused")
	}
	v5 := singboxSnellInbounds(t)[0]
	if v5.NetworkOn(TypeSingbox) != "tcp" || v5.NetworkOn(TypeSnell) != "tcp" {
		t.Fatal("v4 is tcp everywhere")
	}
	v5.Settings.SnellVersion = 5
	if v5.NetworkOn(TypeSingbox) != "tcp" || v5.NetworkOn(TypeSnell) != "tcp,udp" {
		t.Fatal("v5 adds udp only on snell-server")
	}
	ep := snellEndpoints(t)[1]
	if line, _ := SurgeLine(ep); !strings.Contains(line, "version=6") || !strings.Contains(line, "mode=unshaped") {
		t.Fatalf("surge v6 line: %s", line)
	}
}

// The official snell-server 6.0.0 (beta) takes mode instead of obfs and
// only ships amd64 / aarch64 builds.
func TestSnellKernelV6(t *testing.T) {
	ins := []Inbound{
		{ID: 1, Protocol: ProtocolSnell, Port: 7160, Settings: Settings{Password: "psk-default-123", SnellVersion: 6}},
		{ID: 2, Protocol: ProtocolSnell, Port: 7161, Settings: Settings{Password: "psk-unshaped-12", SnellVersion: 6, SnellMode: SnellModeUnshaped}},
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
	def, uns := out.Files[0].Content, out.Files[1].Content
	if strings.Contains(def, "obfs") || strings.Contains(def, "mode") || !strings.Contains(uns, "mode = unshaped\n") {
		t.Fatalf("v6 configs:\n%s\n%s", def, uns)
	}
	if ins[0].NetworkOn(TypeSnell) != "tcp" {
		t.Fatal("v6 has no QUIC proxy mode")
	}

	info, _ := Lookup(TypeSnell)
	if v, err := ResolveVersion(info, "latest"); err != nil || v != "5.0.1" {
		t.Fatalf("the 6.0.0 beta must not be the default: %s %v", v, err)
	}
	spec, err := ResolveDownload(info, "6.0.0", "")
	if err != nil {
		t.Fatal(err)
	}
	arch := spec["archMap"].(map[string]string)
	if arch["amd64"] != "amd64" || arch["arm64"] != "aarch64" || arch["arm"] != "" || len(spec["sha256"].(map[string]string)) != 2 {
		t.Fatalf("6.0.0 download: %v", spec)
	}
	if spec, _ := ResolveDownload(info, "4.1.1", ""); spec["archMap"].(map[string]string)["arm"] != "armv7l" {
		t.Fatalf("4.1.1 keeps armv7: %v", spec)
	}
}
