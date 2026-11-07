package kernel

import (
	"fmt"
	"strings"
)

// Node-list formats for iOS clients. They are produced from the Mihomo
// schema (the panel's own inbounds go through ClashProxy first), like
// Sub-Store does; nodes a client cannot express are skipped.

const (
	FormatShadowrocket = "shadowrocket"
	FormatLoon         = "loon"
	FormatQX           = "qx"
)

// endpointProxy returns the endpoint as a Mihomo proxy, named after it.
func endpointProxy(e Endpoint) (Proxy, bool) {
	if e.Proxy != nil {
		return e.Proxy.forEndpoint(e), true
	}
	node, ok := ClashProxy(e)
	if !ok {
		return nil, false
	}
	return Proxy(yamlToMap(node)), true
}

func yamlToMap(n yamlNode) map[string]interface{} {
	out := make(map[string]interface{}, len(n))
	for _, f := range n {
		if sub, ok := f.Value.(yamlNode); ok {
			out[f.Key] = yamlToMap(sub)
		} else {
			out[f.Key] = f.Value
		}
	}
	return out
}

// ---- Shadowrocket ----

// shadowrocketProfile is a Clash proxy list, which Shadowrocket imports as
// a node subscription. Groups are not imported, so a chained landing node
// names its relay directly: one landing node per relay node.
func shadowrocketProfile(endpoints []Endpoint) string {
	proxies, _ := shadowrocketProxies(endpoints)
	return marshalYAML(yamlNode{{"proxies", proxies}})
}

// shadowrocketProxies returns the Clash proxies Shadowrocket imports and
// their names.
func shadowrocketProxies(endpoints []Endpoint) ([]interface{}, []string) {
	var rendered []Endpoint
	var nodes []yamlNode
	for _, e := range endpoints {
		if p, ok := ClashProxy(e); ok && !shadowrocketSkips(Proxy(yamlToMap(p))) {
			rendered = append(rendered, e)
			nodes = append(nodes, p)
		}
	}
	_, relays, chain := chainGroups(rendered)
	proxies := []interface{}{}
	names := []string{}
	for i, e := range rendered {
		if !chain || e.ChainRole != ChainRoleLanding {
			proxies = append(proxies, nodes[i])
			names = append(names, e.Name)
			continue
		}
		for _, relay := range relays {
			p := append(yamlNode{}, nodes[i]...)
			name := e.Name
			if len(relays) > 1 {
				name = chainedName(e.Name, relay)
				p[0] = yamlField{"name", name}
			}
			proxies = append(proxies, append(p, yamlField{"dialer-proxy", relay}))
			names = append(names, name)
		}
	}
	return proxies, names
}

// chainedName names the copy of a landing node dialed through one relay.
func chainedName(landing, relay string) string {
	return landing + "（经 " + relay + "）"
}

// ---- Loon ----

// loonList renders Loon node lines. Loon nodes cannot name a front proxy,
// so landing nodes connect directly.
func loonList(endpoints []Endpoint) string {
	var lines []string
	for _, e := range endpoints {
		if p, ok := endpointProxy(e); ok {
			if line, ok := loonLine(p); ok {
				lines = append(lines, line)
			}
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func loonName(p Proxy) string {
	return strings.NewReplacer(",", " ", "=", " ", "\n", " ").Replace(p.str("name"))
}

var loonSSCiphers = toSet([]string{"aes-128-gcm", "aes-192-gcm", "aes-256-gcm", "chacha20-ietf-poly1305",
	"xchacha20-ietf-poly1305", "2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "aes-128-cfb", "aes-256-cfb",
	"chacha20", "chacha20-ietf", "rc4-md5"})

// shadowrocketSkips lists what the Shadowrocket profile leaves out because
// its Clash-list import is not known to handle it.
func shadowrocketSkips(p Proxy) bool {
	return hasVLESSEncryption(p) || p.Type() == "wireguard"
}

func loonLine(p Proxy) (string, bool) {
	if hasVLESSEncryption(p) || p.Type() == "wireguard" {
		return "", false
	}
	server, port := p.str("server"), p.num("port")
	parts := []string{}
	add := func(s ...string) { parts = append(parts, s...) }
	addIf := func(key, v string) {
		if v != "" {
			add(key + "=" + v)
		}
	}
	tlsTail := func() {
		if p.flag("skip-cert-verify") {
			add("skip-cert-verify=true")
		}
		if ro := p.sub("reality-opts"); len(ro) > 0 {
			addIf("sni", p.serverName())
			add(fmt.Sprintf("public-key=%q", ro.str("public-key")))
			addIf("short-id", ro.str("short-id"))
		} else {
			addIf("tls-name", p.serverName())
		}
	}
	transport := func() bool {
		host, path, _, _ := transportFields(p)
		switch p.network() {
		case "tcp":
			add("transport=tcp")
		case "ws":
			if p.sub("ws-opts").flag("v2ray-http-upgrade") {
				return false
			}
			add("transport=ws")
			addIf("path", path)
			addIf("host", host)
		default:
			return false
		}
		return true
	}
	udp := func() {
		if p.flag("udp") {
			add("udp=true")
		}
	}

	switch p.Type() {
	case "ss":
		if !loonSSCiphers[p.str("cipher")] {
			return "", false
		}
		add("shadowsocks", server, fmt.Sprint(port), p.str("cipher"), fmt.Sprintf("%q", p.str("password")))
		po := p.sub("plugin-opts")
		switch p.str("plugin") {
		case "":
		case "obfs":
			add("obfs-name=" + po.str("mode"))
			addIf("obfs-host", po.str("host"))
			addIf("obfs-uri", po.str("path"))
		case "shadow-tls":
			add("shadow-tls-password=" + po.str("password"))
			addIf("shadow-tls-sni", po.str("host"))
			if v := po.num("version"); v > 0 {
				add(fmt.Sprintf("shadow-tls-version=%d", v))
			}
		default:
			return "", false
		}
		udp()
	case "vmess":
		add("vmess", server, fmt.Sprint(port), firstNonEmptyString(p.str("cipher"), "auto"), fmt.Sprintf("%q", p.str("uuid")))
		if !transport() {
			return "", false
		}
		if p.usesTLS() {
			add("over-tls=true")
			tlsTail()
		}
		add(fmt.Sprintf("alterId=%d", p.num("alterId")))
		udp()
	case "vless":
		if f := p.str("flow"); f != "" && f != FlowVision {
			return "", false
		}
		add("vless", server, fmt.Sprint(port), fmt.Sprintf("%q", p.str("uuid")))
		if !transport() {
			return "", false
		}
		if p.usesTLS() {
			add("over-tls=true")
			addIf("flow", p.str("flow"))
			tlsTail()
		}
		udp()
	case "trojan":
		add("trojan", server, fmt.Sprint(port), fmt.Sprintf("%q", p.str("password")))
		if p.network() != "tcp" && !transport() {
			return "", false
		}
		tlsTail()
		udp()
	case "hysteria2":
		if ob := p.str("obfs"); ob != "" && ob != "salamander" {
			return "", false
		}
		add("Hysteria2", server, fmt.Sprint(port), fmt.Sprintf("%q", p.str("password")))
		if ports := p.str("ports"); ports != "" {
			add(fmt.Sprintf("server-ports=%q", ports))
		}
		tlsTail()
		addIf("salamander-password", p.str("obfs-password"))
		add("udp=true")
	case "anytls":
		add("anytls", server, fmt.Sprint(port), fmt.Sprintf("%q", p.str("password")))
		tlsTail()
		add("udp=true")
	case "http":
		kind := "http"
		if p.flag("tls") {
			kind = "https"
		}
		add(kind, server, fmt.Sprint(port))
		if u := p.str("username"); u != "" {
			add(u, fmt.Sprintf("%q", p.str("password")))
		}
		if kind == "https" {
			tlsTail()
		}
	case "socks5":
		add("socks5", server, fmt.Sprint(port))
		if u := p.str("username"); u != "" {
			add(u, fmt.Sprintf("%q", p.str("password")))
		}
		if p.flag("tls") {
			add("over-tls=true")
			tlsTail()
		}
		udp()
	default:
		return "", false
	}
	return loonName(p) + "=" + strings.Join(parts, ","), true
}

// ---- Quantumult X ----

// qxList renders Quantumult X server lines ([server_remote] resource). QX
// has no front-proxy option, so landing nodes connect directly.
func qxList(endpoints []Endpoint) string {
	var lines []string
	for _, e := range endpoints {
		if p, ok := endpointProxy(e); ok {
			if line, ok := qxLine(p); ok {
				lines = append(lines, line)
			}
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

var qxSSCiphers = toSet([]string{"aes-128-gcm", "aes-192-gcm", "aes-256-gcm", "chacha20-ietf-poly1305",
	"xchacha20-ietf-poly1305", "2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "aes-128-cfb", "aes-256-cfb",
	"chacha20", "chacha20-ietf", "rc4-md5", "none"})

func qxLine(p Proxy) (string, bool) {
	if hasVLESSEncryption(p) || p.Type() == "wireguard" {
		return "", false
	}
	parts := []string{fmt.Sprintf("%s:%d", p.str("server"), p.num("port"))}
	add := func(s ...string) { parts = append(parts, s...) }
	addIf := func(key, v string) {
		if v != "" {
			add(key + "=" + v)
		}
	}
	reality := func() {
		if ro := p.sub("reality-opts"); len(ro) > 0 {
			add("reality-base64-pubkey=" + ro.str("public-key"))
			addIf("reality-hex-shortid", ro.str("short-id"))
		}
	}
	verify := func() {
		if p.flag("skip-cert-verify") {
			add("tls-verification=false")
		} else {
			add("tls-verification=true")
		}
	}
	// obfs carries TLS and WebSocket for VMess / VLESS / Trojan(ws); its
	// host is the WebSocket Host, else the TLS server name.
	obfs := func() bool {
		host, path, _, _ := transportFields(p)
		tls := p.usesTLS()
		switch p.network() {
		case "tcp":
			if !tls {
				return true
			}
			add("obfs=over-tls")
		case "ws":
			if p.sub("ws-opts").flag("v2ray-http-upgrade") {
				return false
			}
			if tls {
				add("obfs=wss")
			} else {
				add("obfs=ws")
			}
			addIf("obfs-uri", path)
		default:
			return false
		}
		addIf("obfs-host", firstNonEmptyString(host, p.serverName()))
		if tls {
			verify()
			reality()
		}
		return true
	}

	var kind string
	switch p.Type() {
	case "ss":
		if !qxSSCiphers[p.str("cipher")] {
			return "", false
		}
		kind = "shadowsocks"
		add("method="+p.str("cipher"), "password="+p.str("password"))
		po := p.sub("plugin-opts")
		switch p.str("plugin") {
		case "":
		case "obfs":
			add("obfs=" + po.str("mode"))
			addIf("obfs-host", po.str("host"))
			addIf("obfs-uri", po.str("path"))
		default:
			return "", false
		}
		if p.flag("udp") {
			add("udp-relay=true")
		}
	case "vmess":
		kind = "vmess"
		method := p.str("cipher")
		if method == "" || method == "auto" {
			method = "chacha20-poly1305"
		}
		add("method="+method, "password="+p.str("uuid"))
		if !obfs() {
			return "", false
		}
		add(fmt.Sprintf("aead=%t", p.num("alterId") == 0))
	case "vless":
		if f := p.str("flow"); f != "" && f != FlowVision {
			return "", false
		}
		kind = "vless"
		add("method=none", "password="+p.str("uuid"))
		if !obfs() {
			return "", false
		}
		addIf("vless-flow", p.str("flow"))
		if p.flag("udp") {
			add("udp-relay=true")
		}
	case "trojan":
		kind = "trojan"
		add("password=" + p.str("password"))
		switch p.network() {
		case "ws":
			if !obfs() {
				return "", false
			}
		case "tcp":
			add("over-tls=true")
			addIf("tls-host", p.serverName())
			verify()
			reality()
		default:
			return "", false
		}
	case "anytls":
		kind = "anytls"
		add("password="+p.str("password"), "over-tls=true")
		addIf("tls-host", p.serverName())
		verify()
		reality()
		add("udp-relay=true")
	case "http", "socks5":
		kind = p.Type()
		if u := p.str("username"); u != "" {
			add("username="+u, "password="+p.str("password"))
		}
		if p.flag("tls") {
			add("over-tls=true")
			addIf("tls-host", p.serverName())
			verify()
		}
	default:
		return "", false
	}
	return kind + "=" + strings.Join(append(parts, "tag="+qxName(p)), ", "), true
}

func qxName(p Proxy) string {
	return strings.NewReplacer(",", " ", "\n", " ").Replace(p.str("name"))
}
