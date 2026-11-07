package kernel

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func mustImportOne(t *testing.T, text string) ImportedNode {
	t.Helper()
	nodes, errs := ParseImport(text)
	if len(errs) > 0 || len(nodes) != 1 {
		t.Fatalf("%s: nodes=%v errs=%v", text, nodes, errs)
	}
	return nodes[0]
}

// normalized compares proxies after a JSON round trip (numbers as float64).
func normalized(p Proxy) map[string]interface{} {
	b, _ := json.Marshal(p)
	var out map[string]interface{}
	_ = json.Unmarshal(b, &out)
	return out
}

func TestImportShareLinksRoundTrip(t *testing.T) {
	vmess := map[string]string{"v": "2", "ps": "VM 1", "add": "vm.example.com", "port": "443", "id": "11111111-2222-3333-4444-555555555555",
		"aid": "0", "scy": "auto", "net": "ws", "host": "cdn.example.com", "path": "/ws", "tls": "tls", "sni": "cdn.example.com"}
	vb, _ := json.Marshal(vmess)
	links := []string{
		"ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:pa:ss")) + "@203.0.113.5:8388#%E9%A6%99%E6%B8%AF",
		"ss://2022-blake3-aes-128-gcm:MTIzNDU2Nzg5MDEyMzQ1Ng%3D%3D@[2001:db8::1]:9000#v6",
		"ss://" + base64.RawURLEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:pw")) + "@1.2.3.4:80/?plugin=obfs-local%3Bobfs%3Dhttp%3Bobfs-host%3Dbing.com#obfs",
		"vmess://" + base64.StdEncoding.EncodeToString(vb),
		"vless://11111111-2222-3333-4444-555555555555@1.2.3.4:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=www.microsoft.com&fp=chrome&pbk=PUBKEY&sid=ab12&type=tcp#Reality",
		"vless://11111111-2222-3333-4444-555555555555@1.2.3.4:443?encryption=none&security=tls&sni=a.example.com&type=grpc&serviceName=svc#gRPC",
		"trojan://pass%40word@t.example.com:443?security=tls&sni=t.example.com&type=ws&path=%2Ftr&host=t.example.com&allowInsecure=1#Trojan",
		"hysteria2://secret@5.6.7.8:443/?sni=h.example.com&obfs=salamander&obfs-password=ob&mport=20000-30000&insecure=1#HY2",
		"tuic://11111111-2222-3333-4444-555555555555:pw@5.6.7.8:8443?sni=t.example.com&alpn=h3&congestion_control=bbr&udp_relay_mode=native#TUIC",
		"anytls://pw@5.6.7.8:8443/?sni=a.example.com&insecure=1#AnyTLS",
		"socks5://user:pass@5.6.7.8:1080#Socks",
	}
	for _, link := range links {
		n := mustImportOne(t, link)
		uri, ok := proxyURI(Endpoint{Name: n.Name, Host: n.Host, Port: n.Port, Proxy: n.Proxy, Direct: true})
		if !ok {
			t.Fatalf("no link for %s", link)
		}
		back := mustImportOne(t, uri)
		if back.Name != n.Name || back.Host != n.Host || back.Port != n.Port || !reflect.DeepEqual(normalized(back.Proxy), normalized(n.Proxy)) {
			t.Fatalf("round trip changed the node:\n%s\n%s\n%v\n%v", link, uri, n.Proxy, back.Proxy)
		}
	}

	n := mustImportOne(t, links[0])
	if n.Name != "香港" || n.Proxy.str("password") != "pa:ss" || n.Proxy.str("cipher") != "aes-256-gcm" {
		t.Fatalf("legacy-style ss: %+v", n)
	}
	if got := DescribeProxy(mustImportOne(t, links[4]).Proxy); got != "VLESS + Reality" {
		t.Fatalf("describe: %q", got)
	}
}

func TestImportProfiles(t *testing.T) {
	clash := `
port: 7890
proxies:
  - {name: "HK 01", type: ss, server: 1.2.3.4, port: 8388, cipher: aes-128-gcm, password: "p1", udp: true}
  - name: JP
    type: vmess
    server: jp.example.com
    port: 443
    uuid: 11111111-2222-3333-4444-555555555555
    alterId: 0
    cipher: auto
    tls: true
    network: ws
    ws-opts: {path: /v, headers: {Host: jp.example.com}}
  - {name: WG, type: wireguard, server: 1.2.3.4, port: 51820}
proxy-groups: []
`
	nodes, errs := ParseImport(clash)
	if len(nodes) != 2 || len(errs) != 1 || !strings.Contains(errs[0], "wireguard") {
		t.Fatalf("clash: %v %v", nodes, errs)
	}
	if nodes[1].Proxy.sub("ws-opts").str("path") != "/v" {
		t.Fatalf("nested options must survive: %v", nodes[1].Proxy)
	}

	singbox := `{"outbounds":[
	  {"type":"selector","tag":"select","outbounds":["a"]},
	  {"type":"hysteria2","tag":"a","server":"5.6.7.8","server_port":443,"password":"pw","tls":{"enabled":true,"server_name":"h.example.com"}},
	  {"type":"shadowsocks","tag":"b","method":"2022-blake3-aes-128-gcm","password":"k","detour":"b-stls"},
	  {"type":"shadowtls","tag":"b-stls","server":"9.9.9.9","server_port":443,"version":3,"password":"sp","tls":{"enabled":true,"server_name":"www.bing.com"}}
	]}`
	nodes, errs = ParseImport(singbox)
	if len(nodes) != 2 || len(errs) != 0 {
		t.Fatalf("sing-box: %v %v", nodes, errs)
	}
	if nodes[1].Host != "9.9.9.9" || nodes[1].Proxy.str("plugin") != "shadow-tls" {
		t.Fatalf("ss over shadowtls: %+v", nodes[1])
	}

	surge := `[Proxy]
Snell = snell, 1.2.3.4, 6160, psk=abc, version=4, reuse=true, shadow-tls-password=sp, shadow-tls-sni=www.bing.com, shadow-tls-version=3
HY = hysteria2, 5.6.7.8, 443, password=pw, sni=h.example.com, port-hopping="20000-30000"
DIRECT-ish = direct
`
	nodes, errs = ParseImport(surge)
	if len(nodes) != 2 || len(errs) != 1 {
		t.Fatalf("surge: %v %v", nodes, errs)
	}
	line, ok := proxySurge(Endpoint{Name: "Snell", Host: "1.2.3.4", Port: 6160, Proxy: nodes[0].Proxy, Direct: true})
	if !ok || !strings.Contains(line, "psk=abc, version=4") || !strings.Contains(line, "shadow-tls-password=sp") {
		t.Fatalf("snell surge line: %s", line)
	}
	if _, ok := proxyClash(Endpoint{Name: "Snell", Host: "1.2.3.4", Port: 6160, Proxy: nodes[0].Proxy}); ok {
		t.Fatal("Mihomo has no Snell v4")
	}

	b64 := base64.StdEncoding.EncodeToString([]byte("ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:x")) + "@1.2.3.4:80#a\ntrojan://p@5.6.7.8:443#b\n"))
	if nodes, errs := ParseImport(b64); len(nodes) != 2 || len(errs) != 0 {
		t.Fatalf("base64 subscription: %v %v", nodes, errs)
	}
}

func TestImportedRelayEndpoint(t *testing.T) {
	n := mustImportOne(t, "trojan://pw@t.example.com:443?type=ws&path=%2Fws#T")
	relay := Endpoint{Name: "T relay", Host: "198.51.100.7", Port: 30001, Proxy: n.Proxy}
	p := n.Proxy.forEndpoint(relay)
	if p.str("server") != "198.51.100.7" || p.num("port") != 30001 || p.str("sni") != "t.example.com" ||
		p.sub("ws-opts").sub("headers").str("Host") != "t.example.com" {
		t.Fatalf("relays must keep the domain as SNI and Host: %v", p)
	}
	if n.Proxy.str("server") != "t.example.com" {
		t.Fatal("rendering must not modify the stored proxy")
	}

	hy := mustImportOne(t, "hysteria2://pw@5.6.7.8:443/?sni=h.example.com&mport=20000-30000#H")
	if uri, _ := proxyURI(Endpoint{Name: "H", Host: "198.51.100.7", Port: 30002, Proxy: hy.Proxy}); strings.Contains(uri, "mport") {
		t.Fatalf("relays forward a single port: %s", uri)
	}
	if obs, ok := proxySingbox(Endpoint{Name: "H", Host: "5.6.7.8", Port: 443, Proxy: hy.Proxy, Direct: true}); !ok || obs[0]["server_ports"] == nil {
		t.Fatalf("direct hy2 keeps hopping: %v", obs)
	}

	for _, e := range []Endpoint{{Name: "x", Host: "1.2.3.4", Port: 1, Proxy: n.Proxy}} {
		for _, f := range []string{FormatBase64, FormatClash, FormatSingbox, FormatSurge} {
			body, _, err := Subscription(f, []Endpoint{e}, SubOptions{})
			if err != nil || body == "" {
				t.Fatalf("%s: %v", f, err)
			}
		}
	}
}

func TestImportRejections(t *testing.T) {
	bad := map[string]string{
		"vmess://abc": "vmess 链接格式不正确",
		"ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:x")) + "@127.0.0.1:80":              "本机",
		"ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:x")) + "@1.2.3.4:70000":             "端口",
		"ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:x")) + "@1.2.3.4:80/?plugin=kcptun": "插件",
		"vless://u@1.2.3.4:443?security=reality&sni=a.com":                                                     "pbk",
		"wireguard://x@1.2.3.4:51820": "暂不支持",
		"hello world":                 "无法识别",
	}
	for input, want := range bad {
		_, errs := ParseImport(input)
		if len(errs) == 0 || !strings.Contains(errs[0], want) {
			t.Fatalf("%s: want %q, got %v", input, want, errs)
		}
	}
}
