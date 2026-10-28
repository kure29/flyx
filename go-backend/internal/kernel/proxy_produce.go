package kernel

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// ---- share link ----

func proxyURI(e Endpoint) (string, bool) {
	p := e.Proxy.forEndpoint(e)
	hp := hostPort(e.Host, e.Port)
	frag := "#" + url.PathEscape(e.Name)
	switch p.Type() {
	case "ss":
		cipher, pw := p.str("cipher"), p.str("password")
		userinfo := base64.RawURLEncoding.EncodeToString([]byte(cipher + ":" + pw))
		if SS2022KeyLen(cipher) > 0 {
			userinfo = url.QueryEscape(cipher) + ":" + url.QueryEscape(pw)
		}
		query := ""
		if plugin := ssPluginString(p); plugin != "" {
			query = "/?plugin=" + url.QueryEscape(plugin)
		}
		return "ss://" + userinfo + "@" + hp + query + frag, true
	case "vmess":
		v := map[string]interface{}{"v": "2", "ps": e.Name, "add": strings.Trim(e.Host, "[]"), "port": strconv.Itoa(e.Port),
			"id": p.str("uuid"), "aid": strconv.Itoa(p.num("alterId")), "scy": firstNonEmptyString(p.str("cipher"), "auto"),
			"net": "tcp", "type": "none", "host": "", "path": "", "tls": ""}
		host, path, service, _ := transportFields(p)
		switch n := p.network(); n {
		case "ws", "grpc", "h2":
			v["net"] = n
			v["host"], v["path"] = host, path
			if n == "grpc" {
				v["path"], v["type"] = service, "gun"
			}
		case "http":
			v["type"], v["host"], v["path"] = "http", host, path
		}
		if p.usesTLS() {
			v["tls"] = "tls"
			v["sni"] = p.serverName()
			v["fp"] = p.str("client-fingerprint")
			if alpn := p.strs("alpn"); len(alpn) > 0 {
				v["alpn"] = strings.Join(alpn, ",")
			}
			if p.flag("skip-cert-verify") {
				v["allowInsecure"] = "1"
			}
		}
		b, _ := json.Marshal(v)
		return "vmess://" + base64.StdEncoding.EncodeToString(b), true
	case "vless", "trojan":
		if p.network() == "http" {
			return "", false
		}
		q := url.Values{}
		user := url.PathEscape(p.str("password"))
		if p.Type() == "vless" {
			user = p.str("uuid")
			q.Set("encryption", firstNonEmptyString(p.str("encryption"), "none"))
			if f := p.str("flow"); f != "" {
				q.Set("flow", f)
			}
		}
		netType := p.network()
		if p.sub("ws-opts").flag("v2ray-http-upgrade") {
			netType = "httpupgrade"
		}
		q.Set("type", netType)
		host, path, service, mode := transportFields(p)
		switch p.network() {
		case "ws", "h2", "xhttp":
			setIf(q, "path", path)
			setIf(q, "host", host)
			setIf(q, "mode", mode)
		case "grpc":
			q.Set("serviceName", service)
			q.Set("mode", "gun")
		}
		switch {
		case len(p.sub("reality-opts")) > 0:
			q.Set("security", "reality")
			ro := p.sub("reality-opts")
			q.Set("pbk", ro.str("public-key"))
			setIf(q, "sid", ro.str("short-id"))
		case p.usesTLS():
			q.Set("security", "tls")
		default:
			q.Set("security", "none")
		}
		if q.Get("security") != "none" {
			setIf(q, "sni", p.serverName())
			setIf(q, "fp", p.str("client-fingerprint"))
			if alpn := p.strs("alpn"); len(alpn) > 0 {
				q.Set("alpn", strings.Join(alpn, ","))
			}
			if p.flag("skip-cert-verify") {
				q.Set("allowInsecure", "1")
			}
		}
		return p.Type() + "://" + user + "@" + hp + "?" + q.Encode() + frag, true
	case "hysteria2":
		q := url.Values{}
		setIf(q, "sni", p.serverName())
		if p.flag("skip-cert-verify") {
			q.Set("insecure", "1")
		}
		setIf(q, "obfs", p.str("obfs"))
		setIf(q, "obfs-password", p.str("obfs-password"))
		setIf(q, "mport", p.str("ports"))
		setIf(q, "pinSHA256", p.str("fingerprint"))
		if alpn := p.strs("alpn"); len(alpn) > 0 {
			q.Set("alpn", strings.Join(alpn, ","))
		}
		return "hysteria2://" + url.PathEscape(p.str("password")) + "@" + hp + "/?" + q.Encode() + frag, true
	case "tuic":
		q := url.Values{}
		setIf(q, "sni", p.serverName())
		setIf(q, "congestion_control", p.str("congestion-controller"))
		setIf(q, "udp_relay_mode", p.str("udp-relay-mode"))
		if alpn := p.strs("alpn"); len(alpn) > 0 {
			q.Set("alpn", strings.Join(alpn, ","))
		}
		if p.flag("skip-cert-verify") {
			q.Set("allow_insecure", "1")
		}
		if p.flag("disable-sni") {
			q.Set("disable_sni", "1")
		}
		return "tuic://" + p.str("uuid") + ":" + url.PathEscape(p.str("password")) + "@" + hp + "?" + q.Encode() + frag, true
	case "anytls":
		q := url.Values{}
		setIf(q, "sni", p.serverName())
		setIf(q, "fp", p.str("client-fingerprint"))
		if p.flag("skip-cert-verify") {
			q.Set("insecure", "1")
		}
		return "anytls://" + url.PathEscape(p.str("password")) + "@" + hp + "/?" + q.Encode() + frag, true
	case "socks5":
		auth := ""
		if u := p.str("username"); u != "" {
			auth = url.PathEscape(u) + ":" + url.PathEscape(p.str("password")) + "@"
		}
		return "socks5://" + auth + hp + frag, true
	case "wireguard":
		// v2rayN / NekoBox form.
		q := url.Values{}
		q.Set("publickey", p.str("public-key"))
		q.Set("address", p.str("ip")+"/32")
		setIf(q, "mtu", p.str("mtu"))
		return "wireguard://" + url.QueryEscape(p.str("private-key")) + "@" + hp + "?" + q.Encode() + frag, true
	}
	// Snell and HTTP proxies have no common share link.
	return "", false
}

// hasVLESSEncryption reports a VLESS proxy using VLESS Encryption, which
// only Xray-based clients and Mihomo speak.
func hasVLESSEncryption(p Proxy) bool {
	enc := p.str("encryption")
	return enc != "" && enc != "none"
}

func setIfMap(m map[string]interface{}, k, v string) {
	if v != "" {
		m[k] = v
	}
}

func firstNonEmptyStrings(a, b []string) []string {
	if len(a) > 0 {
		return a
	}
	return b
}

// wireGuardPrefix turns a tunnel address into a host prefix.
func wireGuardPrefix(ip string) string {
	if strings.Contains(ip, "/") {
		return ip
	}
	if strings.Contains(ip, ":") {
		return ip + "/128"
	}
	return ip + "/32"
}

func setIf(q url.Values, k, v string) {
	if v != "" {
		q.Set(k, v)
	}
}

// transportFields extracts host / path / gRPC service / XHTTP mode.
func transportFields(p Proxy) (host, path, service, mode string) {
	switch p.network() {
	case "ws":
		wo := p.sub("ws-opts")
		path = wo.str("path")
		host = firstNonEmptyString(wo.sub("headers").str("Host"), wo.sub("headers").str("host"))
	case "grpc":
		service = p.sub("grpc-opts").str("grpc-service-name")
	case "h2":
		ho := p.sub("h2-opts")
		path = ho.str("path")
		if hosts := ho.strs("host"); len(hosts) > 0 {
			host = hosts[0]
		}
	case "http":
		ho := p.sub("http-opts")
		if paths := ho.strs("path"); len(paths) > 0 {
			path = paths[0]
		}
		if hosts := ho.sub("headers").strs("Host"); len(hosts) > 0 {
			host = hosts[0]
		}
	case "xhttp":
		xo := p.sub("xhttp-opts")
		path, host, mode = xo.str("path"), xo.str("host"), xo.str("mode")
	}
	return
}

func ssPluginString(p Proxy) string {
	po := p.sub("plugin-opts")
	switch p.str("plugin") {
	case "obfs":
		s := "obfs-local;obfs=" + po.str("mode")
		if h := po.str("host"); h != "" {
			s += ";obfs-host=" + h
		}
		return s
	case "v2ray-plugin":
		s := "v2ray-plugin;mode=websocket"
		if h := po.str("host"); h != "" {
			s += ";host=" + h
		}
		if path := po.str("path"); path != "" {
			s += ";path=" + path
		}
		if po.flag("tls") {
			s += ";tls"
		}
		return s
	case "shadow-tls":
		version := po.num("version")
		if version == 0 {
			version = 3
		}
		return fmt.Sprintf("shadow-tls;host=%s;password=%s;version=%d", po.str("host"), po.str("password"), version)
	}
	return ""
}

// ---- Clash / Mihomo ----

func proxyClash(e Endpoint) (yamlNode, bool) {
	p := e.Proxy.forEndpoint(e)
	if p.network() == "xhttp" || (p.Type() == "snell" && p.num("version") > 3) {
		// Mihomo XHTTP support is unstable; it only speaks Snell v1-v3.
		return nil, false
	}
	if p.Type() == "vless" {
		if realityOpts := p.sub("reality-opts"); len(realityOpts) > 0 {
			realityOpts["support-x25519mlkem768"] = true
		}
	}
	delete(p, "shadow-tls") // panel-only key for Snell over ShadowTLS
	node := yamlNode{{"name", e.Name}, {"type", p.Type()}, {"server", p.str("server")}, {"port", e.Port}}
	keys := make([]string, 0, len(p))
	for k := range p {
		if k != "name" && k != "type" && k != "server" && k != "port" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		node = append(node, yamlField{k, p[k]})
	}
	return node, true
}

// ---- sing-box ----

func proxySingbox(e Endpoint) ([]map[string]interface{}, bool) {
	p := e.Proxy.forEndpoint(e)
	o := map[string]interface{}{"tag": e.Name, "server": strings.Trim(e.Host, "[]"), "server_port": e.Port}
	tls := func(enabled bool) {
		if !enabled {
			return
		}
		t := map[string]interface{}{"enabled": true}
		if sni := p.serverName(); sni != "" {
			t["server_name"] = sni
		}
		if p.flag("skip-cert-verify") {
			t["insecure"] = true
		}
		if alpn := p.strs("alpn"); len(alpn) > 0 {
			t["alpn"] = alpn
		}
		if fp := p.str("client-fingerprint"); fp != "" {
			t["utls"] = map[string]interface{}{"enabled": true, "fingerprint": fp}
		}
		if ro := p.sub("reality-opts"); len(ro) > 0 {
			t["reality"] = map[string]interface{}{"enabled": true, "public_key": ro.str("public-key"), "short_id": ro.str("short-id")}
			if _, ok := t["utls"]; !ok {
				t["utls"] = map[string]interface{}{"enabled": true, "fingerprint": "chrome"}
			}
		}
		o["tls"] = t
	}
	transport := func() bool {
		host, path, service, _ := transportFields(p)
		switch p.network() {
		case "tcp":
		case "ws":
			t := map[string]interface{}{"type": "ws", "path": path}
			if p.sub("ws-opts").flag("v2ray-http-upgrade") {
				t["type"] = "httpupgrade"
				t["host"] = host
			} else if host != "" {
				t["headers"] = map[string]interface{}{"Host": host}
			}
			o["transport"] = t
		case "grpc":
			o["transport"] = map[string]interface{}{"type": "grpc", "service_name": service}
		case "h2":
			t := map[string]interface{}{"type": "http", "path": path}
			if host != "" {
				t["host"] = []string{host}
			}
			o["transport"] = t
		default:
			return false
		}
		return true
	}
	switch p.Type() {
	case "ss":
		o["type"] = "shadowsocks"
		o["method"] = p.str("cipher")
		o["password"] = p.str("password")
		po := p.sub("plugin-opts")
		switch p.str("plugin") {
		case "":
		case "obfs":
			o["plugin"] = "obfs-local"
			o["plugin_opts"] = strings.TrimPrefix(ssPluginString(p), "obfs-local;")
		case "v2ray-plugin":
			o["plugin"] = "v2ray-plugin"
			o["plugin_opts"] = strings.TrimPrefix(ssPluginString(p), "v2ray-plugin;")
		case "shadow-tls":
			tag := e.Name + " (shadowtls)"
			o["detour"] = tag
			version := po.num("version")
			if version == 0 {
				version = 3
			}
			return []map[string]interface{}{o, {
				"type": "shadowtls", "tag": tag, "server": strings.Trim(e.Host, "[]"), "server_port": e.Port,
				"version": version, "password": po.str("password"),
				"tls": map[string]interface{}{"enabled": true, "server_name": po.str("host"),
					"utls": map[string]interface{}{"enabled": true, "fingerprint": firstNonEmptyString(p.str("client-fingerprint"), "chrome")}},
			}}, true
		default:
			return nil, false
		}
	case "vmess":
		o["type"] = "vmess"
		o["uuid"] = p.str("uuid")
		o["alter_id"] = p.num("alterId")
		o["security"] = firstNonEmptyString(p.str("cipher"), "auto")
		if !transport() {
			return nil, false
		}
		tls(p.usesTLS())
	case "vless":
		if hasVLESSEncryption(p) {
			return nil, false // sing-box has no VLESS Encryption
		}
		o["type"] = "vless"
		o["uuid"] = p.str("uuid")
		if f := p.str("flow"); f != "" {
			o["flow"] = f
		}
		if !transport() {
			return nil, false
		}
		tls(p.usesTLS())
	case "trojan":
		o["type"] = "trojan"
		o["password"] = p.str("password")
		if !transport() {
			return nil, false
		}
		tls(true)
	case "hysteria2":
		o["type"] = "hysteria2"
		o["password"] = p.str("password")
		if ob := p.str("obfs"); ob != "" {
			o["obfs"] = map[string]interface{}{"type": ob, "password": p.str("obfs-password")}
		}
		if ports := p.str("ports"); ports != "" {
			delete(o, "server_port")
			o["server_ports"] = []string{strings.ReplaceAll(ports, "-", ":")}
		}
		tls(true)
	case "tuic":
		o["type"] = "tuic"
		o["uuid"] = p.str("uuid")
		o["password"] = p.str("password")
		if cc := p.str("congestion-controller"); cc != "" {
			o["congestion_control"] = cc
		}
		if m := p.str("udp-relay-mode"); m != "" {
			o["udp_relay_mode"] = m
		}
		tls(true)
	case "anytls":
		o["type"] = "anytls"
		o["password"] = p.str("password")
		tls(true)
	case "socks5":
		o["type"] = "socks"
		o["version"] = "5"
		if u := p.str("username"); u != "" {
			o["username"], o["password"] = u, p.str("password")
		}
	case "http":
		o["type"] = "http"
		if u := p.str("username"); u != "" {
			o["username"], o["password"] = u, p.str("password")
		}
		tls(p.flag("tls"))
	case "wireguard":
		// An endpoint in sing-box 1.11+; singboxProfile moves it out of the
		// outbounds.
		peer := map[string]interface{}{
			"address":     strings.Trim(e.Host, "[]"),
			"port":        e.Port,
			"public_key":  p.str("public-key"),
			"allowed_ips": firstNonEmptyStrings(p.strs("allowed-ips"), []string{"0.0.0.0/0", "::/0"}),
		}
		setIfMap(peer, "pre_shared_key", p.str("pre-shared-key"))
		o = map[string]interface{}{
			"type":        "wireguard",
			"tag":         e.Name,
			"address":     []string{wireGuardPrefix(p.str("ip"))},
			"private_key": p.str("private-key"),
			"peers":       []interface{}{peer},
		}
		if mtu := p.num("mtu"); mtu > 0 {
			o["mtu"] = mtu
		}
	default:
		return nil, false
	}
	if t, ok := o["tls"].(map[string]interface{}); ok && (p.Type() == "hysteria2" || p.Type() == "tuic") {
		delete(t, "utls") // sing-box rejects uTLS on QUIC outbounds
	}
	return []map[string]interface{}{o}, true
}

// ---- Surge ----

func proxySurge(e Endpoint) (string, bool) {
	p := e.Proxy.forEndpoint(e)
	name := strings.NewReplacer(",", " ", "=", " ", "\n", " ").Replace(e.Name)
	host := strings.Trim(e.Host, "[]")
	port := strconv.Itoa(e.Port)
	var parts []string
	tlsTail := func() {
		if sni := p.serverName(); sni != "" {
			parts = append(parts, "sni="+sni)
		}
		if p.flag("skip-cert-verify") {
			parts = append(parts, "skip-cert-verify=true")
		}
	}
	ws := func() bool {
		switch p.network() {
		case "tcp":
			return true
		case "ws":
			if p.sub("ws-opts").flag("v2ray-http-upgrade") {
				return false
			}
			h, path, _, _ := transportFields(p)
			parts = append(parts, "ws=true", "ws-path="+firstNonEmptyString(path, "/"))
			if h != "" {
				parts = append(parts, "ws-headers=Host:"+h)
			}
			return true
		}
		return false
	}
	shadowTLS := func(po Proxy) {
		if po.str("password") == "" {
			return
		}
		version := po.num("version")
		if version == 0 {
			version = 3
		}
		parts = append(parts, "shadow-tls-password="+po.str("password"), "shadow-tls-sni="+po.str("host"),
			fmt.Sprintf("shadow-tls-version=%d", version))
	}
	switch p.Type() {
	case "ss":
		parts = append(parts, "ss", host, port, "encrypt-method="+p.str("cipher"), "password="+p.str("password"))
		po := p.sub("plugin-opts")
		switch p.str("plugin") {
		case "":
			parts = append(parts, "udp-relay=true")
		case "obfs":
			parts = append(parts, "obfs="+po.str("mode"))
			if h := po.str("host"); h != "" {
				parts = append(parts, "obfs-host="+h)
			}
		case "shadow-tls":
			shadowTLS(po)
		default:
			return "", false
		}
	case "vmess":
		parts = append(parts, "vmess", host, port, "username="+p.str("uuid"))
		if p.num("alterId") == 0 {
			parts = append(parts, "vmess-aead=true")
		}
		if !ws() {
			return "", false
		}
		if p.usesTLS() {
			parts = append(parts, "tls=true")
			tlsTail()
		}
	case "trojan":
		parts = append(parts, "trojan", host, port, "password="+p.str("password"))
		if !ws() {
			return "", false
		}
		tlsTail()
	case "snell":
		parts = append(parts, "snell", host, port, "psk="+p.str("psk"), fmt.Sprintf("version=%d", maxInt(p.num("version"), 1)))
		if oo := p.sub("obfs-opts"); oo.str("mode") != "" {
			parts = append(parts, "obfs="+oo.str("mode"))
			if h := oo.str("host"); h != "" {
				parts = append(parts, "obfs-host="+h)
			}
		}
		shadowTLS(p.sub("shadow-tls"))
	case "hysteria2":
		if p.str("obfs") != "" && p.str("obfs") != "salamander" {
			return "", false
		}
		parts = append(parts, "hysteria2", host, port, "password="+p.str("password"))
		if pw := p.str("obfs-password"); pw != "" {
			parts = append(parts, "salamander-password="+pw)
		}
		if ports := p.str("ports"); ports != "" {
			parts = append(parts, fmt.Sprintf("port-hopping=%q", ports), "port-hopping-interval=30")
		}
		tlsTail()
	case "tuic":
		parts = append(parts, "tuic-v5", host, port, "password="+p.str("password"), "uuid="+p.str("uuid"),
			"alpn="+firstNonEmptyString(strings.Join(p.strs("alpn"), ","), "h3"))
		tlsTail()
	case "anytls":
		parts = append(parts, "anytls", host, port, "password="+p.str("password"))
		tlsTail()
	case "socks5", "http":
		kind := p.Type()
		if p.flag("tls") {
			kind = map[string]string{"socks5": "socks5-tls", "http": "https"}[kind]
		}
		parts = append(parts, kind, host, port)
		if u := p.str("username"); u != "" {
			parts = append(parts, "username="+u, "password="+p.str("password"))
		}
		if p.flag("tls") {
			tlsTail()
		}
	default:
		return "", false
	}
	return name + " = " + strings.Join(parts, ", "), true
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
