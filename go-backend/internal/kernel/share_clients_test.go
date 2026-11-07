package kernel

import (
	"strings"
	"testing"
)

func clientEndpoints(t *testing.T) []Endpoint {
	t.Helper()
	norm := func(kt string, in Inbound) Inbound {
		if err := Normalize(kt, &in); err != nil {
			t.Fatal(err)
		}
		return in
	}
	reality := norm(TypeXray, Inbound{ID: 1, Protocol: ProtocolVLESS, Port: 443, Settings: Settings{Flow: FlowVision},
		Security: Security{Type: SecurityReality}})
	trojanWS := norm(TypeXray, Inbound{ID: 2, Protocol: ProtocolTrojan, Port: 8443,
		Transport: Transport{Type: TransportWS, Path: "/ws"}, Security: Security{Type: SecurityTLS, SNI: "b.example.com"}})
	hy2 := norm(TypeSingbox, Inbound{ID: 3, Protocol: ProtocolHysteria2, Port: 4443, Transport: Transport{Type: TransportQUIC},
		Security: Security{Type: SecurityTLS}})
	tuic := norm(TypeSingbox, Inbound{ID: 4, Protocol: ProtocolTUIC, Port: 6443, Transport: Transport{Type: TransportQUIC},
		Security: Security{Type: SecurityTLS}})
	return []Endpoint{
		{Name: "美国 落地", Host: "9.9.9.9", Port: 443, Inbound: reality, Direct: true},
		{Name: "香港 中转 1", Host: "8.8.8.8", Port: 8443, Inbound: trojanWS, Direct: true},
		{Name: "香港 中转 2", Host: "8.8.4.4", Port: 4443, Inbound: hy2, Direct: true},
		{Name: "日本, tuic", Host: "7.7.7.7", Port: 6443, Inbound: tuic, Direct: true},
		{Name: "机场 ss", Host: "5.6.7.8", Port: 8388, Direct: true, Proxy: Proxy{"type": "ss", "server": "5.6.7.8",
			"port": 8388, "cipher": "aes-128-gcm", "password": "pw", "udp": true}},
	}
}

func TestLoonList(t *testing.T) {
	eps := clientEndpoints(t)
	body, _, err := Subscription(FormatLoon, eps, chainOpts2)
	if err != nil {
		t.Fatal(err)
	}
	pub := eps[0].Inbound.Security.RealityPublicKey
	sid := eps[0].Inbound.Security.RealityShortIDs[0]
	for _, want := range []string{
		`美国 落地=vless,9.9.9.9,443,"` + eps[0].Inbound.Settings.UUID + `",transport=tcp,over-tls=true,flow=xtls-rprx-vision,sni=www.microsoft.com,public-key="` + pub + `",short-id=` + sid + `,udp=true`,
		`香港 中转 1=trojan,8.8.8.8,8443,"` + eps[1].Inbound.Settings.Password + `",transport=ws,path=/ws,skip-cert-verify=true,tls-name=b.example.com,udp=true`,
		`香港 中转 2=Hysteria2,8.8.4.4,4443,"` + eps[2].Inbound.Settings.Password + `",skip-cert-verify=true,tls-name=www.bing.com,udp=true`,
		`机场 ss=shadowsocks,5.6.7.8,8388,aes-128-gcm,"pw",udp=true`,
	} {
		if !strings.Contains(body, want+"\n") {
			t.Fatalf("missing %q in:\n%s", want, body)
		}
	}
	if strings.Contains(body, "tuic") || strings.Contains(body, "dialer") || strings.Count(body, "\n") != 4 {
		t.Fatalf("tuic is not a Loon protocol and Loon has no chain:\n%s", body)
	}
}

func TestQXList(t *testing.T) {
	eps := clientEndpoints(t)
	body, _, _ := Subscription(FormatQX, eps, SubOptions{})
	pub := eps[0].Inbound.Security.RealityPublicKey
	for _, want := range []string{
		"vless=9.9.9.9:443, method=none, password=" + eps[0].Inbound.Settings.UUID + ", obfs=over-tls, obfs-host=www.microsoft.com, tls-verification=true, reality-base64-pubkey=" + pub,
		"vless-flow=xtls-rprx-vision, udp-relay=true, tag=美国 落地\n",
		"trojan=8.8.8.8:8443, password=" + eps[1].Inbound.Settings.Password + ", obfs=wss, obfs-uri=/ws, obfs-host=b.example.com, tls-verification=false, tag=香港 中转 1\n",
		"shadowsocks=5.6.7.8:8388, method=aes-128-gcm, password=pw, udp-relay=true, tag=机场 ss\n",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in:\n%s", want, body)
		}
	}
	if strings.Count(body, "\n") != 3 {
		t.Fatalf("QX has no Hysteria2 / TUIC:\n%s", body)
	}
}

var chainOpts2 = SubOptions{ChainLanding: "落地", ChainRelay: "中转"}

func TestShadowrocketChain(t *testing.T) {
	eps := clientEndpoints(t)
	body, _, _ := Subscription(FormatShadowrocket, eps, chainOpts2)
	for _, want := range []string{
		"- name: \"美国 落地（经 香港 中转 1）\"",
		"dialer-proxy: \"香港 中转 1\"",
		"- name: \"美国 落地（经 香港 中转 2）\"",
		"dialer-proxy: \"香港 中转 2\"",
		"- name: \"日本, tuic\"",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in:\n%s", want, body)
		}
	}
	if strings.Contains(body, "proxy-groups") || strings.Contains(body, ChainRelayGroup) {
		t.Fatalf("a node list has no groups to dial through:\n%s", body)
	}
	// One relay keeps the landing node's name.
	body, _, _ = Subscription(FormatShadowrocket, eps[:2], chainOpts2)
	if !strings.Contains(body, "- name: \"美国 落地\"") || !strings.Contains(body, "dialer-proxy: \"香港 中转 1\"") {
		t.Fatalf("single relay:\n%s", body)
	}
}

func TestFormatForUserAgentIOSClients(t *testing.T) {
	cases := map[string]string{
		"Shadowrocket/2070 CFNetwork/1568 Darwin/24.1.0": FormatShadowrocket,
		"Loon/876 CFNetwork/1568":                        FormatLoon,
		"Quantumult%20X/1.5.2 (iPhone15,2; iOS 18.1)":    FormatQX,
		"Stash/2.4.7 Clash/1.9.0":                        FormatStash,
	}
	for ua, want := range cases {
		if got := FormatForUserAgent(ua); got != want {
			t.Errorf("%q => %q, want %q", ua, got, want)
		}
	}
}
