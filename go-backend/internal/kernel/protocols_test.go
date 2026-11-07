package kernel

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newProtocolInbounds returns one normalized inbound per new protocol for a
// kernel; VLESS Encryption only exists on Xray.
func newProtocolInbounds(t *testing.T, kernelType string) []Inbound {
	t.Helper()
	ins := []Inbound{
		{ID: 1, Protocol: ProtocolSOCKS, Port: 1080, Users: chainUser},
		{ID: 2, Protocol: ProtocolHTTP, Port: 8080, Users: chainUser},
		{ID: 3, Protocol: ProtocolWireGuard, Port: 51820, Users: chainUser},
	}
	if kernelType == TypeXray {
		ins = append(ins, Inbound{ID: 4, Protocol: ProtocolVLESS, Port: 8443, Users: chainUser,
			Settings: Settings{Decryption: "auto", Flow: FlowVision}, Security: Security{Type: SecurityNone}})
	}
	for i := range ins {
		if err := Normalize(kernelType, &ins[i]); err != nil {
			t.Fatalf("normalize %s: %v", ins[i].Protocol, err)
		}
	}
	return ins
}

func TestNormalizeNewProtocols(t *testing.T) {
	ins := newProtocolInbounds(t, TypeXray)
	socks, wg, vless := ins[0], ins[2], ins[3]
	if socks.Settings.Username == "" || socks.Settings.Password == "" || socks.Network() != "tcp,udp" {
		t.Fatalf("socks credentials: %+v", socks.Settings)
	}
	s := wg.Settings
	if s.PrivateKey == "" || s.PeerPrivateKey == "" || s.PeerAddress != DefaultWireGuardPeer || s.MTU != DefaultWireGuardMTU ||
		wg.Transport.Type != TransportUDP || wg.Network() != "udp" || s.WireGuardServerAddress() != "10.66.66.1" {
		t.Fatalf("wireguard: %+v %+v", s, wg.Transport)
	}
	if pub, _ := WireGuardPublicKey(s.PeerPrivateKey); pub != s.PeerPublicKey {
		t.Fatal("peer public key must match its private key")
	}
	if !strings.HasPrefix(vless.Settings.Decryption, "mlkem768x25519plus.native.600s.") ||
		!strings.HasPrefix(vless.Settings.Encryption, "mlkem768x25519plus.native.0rtt.") ||
		vless.Settings.Flow != FlowVision || vless.Security.Type != SecurityNone {
		t.Fatalf("vless encryption: %+v", vless.Settings)
	}
	// Saving again keeps the keys; the client side follows the server key.
	again := vless
	if err := Normalize(TypeXray, &again); err != nil || again.Settings.Decryption != vless.Settings.Decryption ||
		again.Settings.Encryption != vless.Settings.Encryption {
		t.Fatalf("keys must survive a re-save: %+v %v", again.Settings, err)
	}

	bad := Inbound{ID: 9, Protocol: ProtocolVLESS, Port: 1, Settings: Settings{Decryption: "auto"}}
	if err := Normalize(TypeSingbox, &bad); err == nil {
		t.Fatal("sing-box has no VLESS Encryption")
	}
	bad = Inbound{ID: 9, Protocol: ProtocolWireGuard, Port: 1, Settings: Settings{PeerAddress: "10.0.0.1"}}
	if err := Normalize(TypeXray, &bad); err == nil {
		t.Fatal("the peer cannot take the server's address")
	}
	bad = Inbound{ID: 9, Protocol: ProtocolSOCKS, Port: 1, Settings: Settings{Username: "a b"}}
	if err := Normalize(TypeXray, &bad); err == nil {
		t.Fatal("usernames are checked")
	}
}

func TestRenderNewProtocolsXray(t *testing.T) {
	ins := newProtocolInbounds(t, TypeXray)
	cfg := renderJSON(t, TypeXray, ins)
	list := cfg["inbounds"].([]interface{})
	socks := list[0].(map[string]interface{})["settings"].(map[string]interface{})
	if accts := socks["accounts"].([]interface{}); len(accts) != 2 || accts[1].(map[string]interface{})["user"] != "u7" {
		t.Fatalf("socks accounts: %v", socks)
	}
	wg := list[2].(map[string]interface{})
	if wg["protocol"] != "wireguard" || wg["streamSettings"] != nil {
		t.Fatalf("wireguard inbound: %v", wg)
	}
	vless := list[3].(map[string]interface{})["settings"].(map[string]interface{})
	if !strings.HasPrefix(vless["decryption"].(string), "mlkem768x25519plus.") {
		t.Fatalf("vless decryption: %v", vless)
	}
	// The SOCKS / HTTP user is matched by its username.
	rules := cfg["routing"].(map[string]interface{})["rules"].([]interface{})
	users := rules[len(rules)-1].(map[string]interface{})["user"].([]interface{})
	if len(users) != 2 || users[1] != "u7" {
		t.Fatalf("user rule: %v", rules)
	}
}

func TestRenderNewProtocolsSingbox(t *testing.T) {
	ins := newProtocolInbounds(t, TypeSingbox)
	cfg := renderJSON(t, TypeSingbox, ins)
	if n := len(cfg["inbounds"].([]interface{})); n != 2 {
		t.Fatalf("socks and http are inbounds, got %d", n)
	}
	eps := cfg["endpoints"].([]interface{})
	ep := eps[0].(map[string]interface{})
	if ep["type"] != "wireguard" || ep["tag"] != "in-3" || ep["listen_port"].(float64) != 51820 {
		t.Fatalf("wireguard endpoint: %v", ep)
	}
}

func TestShareNewProtocols(t *testing.T) {
	ins := newProtocolInbounds(t, TypeXray)
	eps := make([]Endpoint, len(ins))
	for i, in := range ins {
		eps[i] = Endpoint{Name: in.Protocol, Host: "203.0.113.1", Port: in.Port, Inbound: in, Direct: true}
	}
	user := eps[0]
	user.Inbound = user.Inbound.ForUser(chainUser[0])
	if uri, _ := ShareURI(user); uri != "socks5://u7:userpw@203.0.113.1:1080#socks" {
		t.Fatalf("socks user link: %s", uri)
	}
	if uri, _ := ShareURI(eps[2]); !strings.HasPrefix(uri, "wireguard://") || !strings.Contains(uri, "address=10.66.66.2%2F32") {
		t.Fatalf("wireguard link: %s", uri)
	}
	if conf, ok := WireGuardConf(eps[2]); !ok || !strings.Contains(conf, "Endpoint = 203.0.113.1:51820") ||
		!strings.Contains(conf, "PublicKey = "+eps[2].Inbound.Settings.PublicKey) {
		t.Fatalf("wireguard conf: %s", conf)
	}
	if uri, _ := ShareURI(eps[3]); !strings.Contains(uri, "encryption=mlkem768x25519plus.native.0rtt.") {
		t.Fatalf("vless encryption link: %s", uri)
	}

	clash, _, err := Subscription(FormatClash, eps, SubOptions{})
	if err != nil || !strings.Contains(clash, `type: "socks5"`) || !strings.Contains(clash, `type: "http"`) ||
		!strings.Contains(clash, `type: "wireguard"`) || !strings.Contains(clash, `encryption: "mlkem768x25519plus`) {
		t.Fatalf("clash: %v\n%s", err, clash)
	}
	sb, _, err := Subscription(FormatSingbox, eps, SubOptions{})
	cfg := renderJSONString(t, sb)
	if err != nil || cfg["endpoints"] == nil || strings.Contains(sb, "mlkem") {
		t.Fatalf("sing-box: WireGuard as endpoint, no VLESS Encryption: %v\n%s", err, sb)
	}
	for _, f := range []string{FormatLoon, FormatQX, FormatShadowrocket} {
		out, _, _ := Subscription(f, eps, SubOptions{})
		if strings.Contains(out, "wireguard") || strings.Contains(out, "mlkem") {
			t.Fatalf("%s must skip WireGuard and VLESS Encryption:\n%s", f, out)
		}
		if !strings.Contains(strings.ToLower(out), "socks5") {
			t.Fatalf("%s keeps SOCKS5:\n%s", f, out)
		}
	}
	surge, _, _ := Subscription(FormatSurge, eps, SubOptions{})
	if !strings.Contains(surge, "socks5, 203.0.113.1, 1080") || !strings.Contains(surge, "http, 203.0.113.1, 8080") {
		t.Fatalf("surge:\n%s", surge)
	}

	// FLVX_DUMP=dir writes matching server and client configs per kernel for
	// checking with the real binaries.
	if dir := os.Getenv("FLVX_DUMP"); dir != "" {
		for _, k := range []string{TypeXray, TypeSingbox} {
			kin := newProtocolInbounds(t, k)
			keps := make([]Endpoint, len(kin))
			for i, in := range kin {
				keps[i] = Endpoint{Name: in.Protocol, Host: "127.0.0.1", Port: in.Port, Inbound: in, Direct: true}
			}
			r, err := Render(k, kin)
			if err != nil {
				t.Fatal(err)
			}
			kclash, _, _ := Subscription(FormatClash, keps, SubOptions{})
			ksb, _, _ := Subscription(FormatSingbox, keps, SubOptions{})
			b64, _, _ := Subscription(FormatBase64, keps, SubOptions{})
			links, _ := base64.StdEncoding.DecodeString(b64)
			for name, body := range map[string]string{
				"server.json": r.Files[0].Content, "clash.yaml": kclash, "singbox-client.json": ksb, "links.txt": string(links),
			} {
				if err := os.WriteFile(filepath.Join(dir, k+"-"+name), []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

func TestChainToNewProtocols(t *testing.T) {
	ins := newProtocolInbounds(t, TypeXray)
	ep := func(in Inbound) Endpoint { return Endpoint{Host: "203.0.113.1", Port: in.Port, Inbound: in} }
	for _, in := range []Inbound{ins[0], ins[1]} {
		if err := CheckChain(TypeXray, ep(in)); err != nil {
			t.Fatalf("xray via %s: %v", in.Protocol, err)
		}
		if err := CheckChain(TypeSingbox, ep(in)); err != nil {
			t.Fatalf("sing-box via %s: %v", in.Protocol, err)
		}
	}
	if err := CheckChain(TypeXray, ep(ins[2])); err == nil || !strings.Contains(err.Error(), "WireGuard") {
		t.Fatalf("wireguard is not an exit: %v", err)
	}
	if err := CheckChain(TypeSingbox, ep(ins[2])); err == nil {
		t.Fatal("wireguard is not an exit for sing-box either")
	}
	ob, err := xrayChainOutbound(ep(ins[3]), "via")
	if err != nil {
		t.Fatal(err)
	}
	user := ob["settings"].(obj)["vnext"].([]interface{})[0].(obj)["users"].([]interface{})[0].(obj)
	if !strings.HasPrefix(user["encryption"].(string), "mlkem768x25519plus.native.0rtt.") {
		t.Fatalf("xray dials VLESS Encryption: %v", user)
	}
	if err := CheckChain(TypeSingbox, ep(ins[3])); err == nil {
		t.Fatal("sing-box cannot dial VLESS Encryption")
	}
}
