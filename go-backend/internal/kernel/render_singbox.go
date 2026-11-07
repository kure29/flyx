package kernel

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

func renderSingbox(inbounds []Inbound, opts RenderOptions) (string, error) {
	list := make([]interface{}, 0, len(inbounds))
	acme := &acmeProviders{}
	for _, in := range inbounds {
		items, err := singboxInbounds(in, acme)
		if err != nil {
			return "", fmt.Errorf("入站 %d: %v", in.ID, err)
		}
		list = append(list, items...)
	}
	// WireGuard servers are endpoints in sing-box's schema.
	var endpoints, inboundList []interface{}
	for _, item := range list {
		if o, ok := item.(obj); ok && o["type"] == "wireguard" {
			endpoints = append(endpoints, o)
		} else {
			inboundList = append(inboundList, item)
		}
	}
	if inboundList == nil {
		inboundList = []interface{}{}
	}
	cfg := obj{
		"log":       obj{"level": "warn"},
		"inbounds":  inboundList,
		"outbounds": []interface{}{obj{"type": "direct", "tag": "direct"}},
	}
	if opts.SingboxPreferGoDNS {
		// The implicit resolver uses systemd-resolved over DBus. Servers
		// without that service cannot resolve REALITY/ShadowTLS handshakes
		// or outbound destinations; use the system DNS through Go instead.
		const tag = "flyx-local-resolver"
		cfg["dns"] = obj{"servers": []interface{}{obj{"type": "local", "tag": tag, "prefer_go": true}}}
		cfg["route"] = obj{"default_domain_resolver": tag}
	}
	if len(endpoints) > 0 {
		cfg["endpoints"] = endpoints
	}
	if len(acme.list) > 0 {
		cfg["certificate_providers"] = acme.list
	}
	// Chained inbounds leave through their target; their rules come first
	// so their users do not match the direct per-user rules below.
	chained, rules, sets, err := singboxChainRoutes(inbounds, opts.SingboxSnell)
	if err != nil {
		return "", err
	}
	outbounds := append(cfg["outbounds"].([]interface{}), chained...)
	// Each user leaves through a direct outbound whose sockets carry the
	// user's mark (see UserMark); the first outbound stays the default.
	for _, id := range markedUsers(inbounds) {
		tag := userOutboundTag(id)
		outbounds = append(outbounds, obj{"type": "direct", "tag": tag, "routing_mark": UserMark(id)})
		rules = append(rules, obj{"auth_user": []string{User{ID: id}.Name()}, "outbound": tag})
	}
	cfg["outbounds"] = outbounds
	if len(rules) > 0 {
		route, _ := cfg["route"].(obj)
		if route == nil {
			route = obj{}
		}
		route["rules"], route["final"] = rules, "direct"
		if defs := singboxRuleSets(sets, opts); len(defs) > 0 {
			// sing-box downloads the 分流 rule sets itself and keeps them in
			// the cache file across restarts.
			route["rule_set"] = defs
			cfg["experimental"] = obj{"cache_file": obj{"enabled": true, "path": "cache.db"}}
			if opts.SingboxHTTPClient {
				cfg["http_clients"] = []interface{}{obj{"tag": ruleSetHTTPClient}}
			}
		}
		cfg["route"] = route
	}
	if err := applyOutboundIPPolicies(TypeSingbox, cfg, inbounds, opts.SingboxPreferGoDNS); err != nil {
		return "", err
	}
	return marshalConfig(cfg)
}

// acmeProviders shares one ACME certificate provider per domain + challenge
// setup, so inbounds on the same domain do not race for port 80 or the
// account. Requires sing-box 1.14+.
type acmeProviders struct {
	list []interface{}
	tags map[string]string
}

func (a *acmeProviders) tag(sec Security) string {
	key := strings.Join([]string{sec.SNI, sec.ACMEEmail, sec.ACMEDNS, sec.ACMEDNSKey, sec.ACMEDNSSecret}, "\x00")
	if t, ok := a.tags[key]; ok {
		return t
	}
	if a.tags == nil {
		a.tags = map[string]string{}
	}
	t := fmt.Sprintf("acme-%d", len(a.list)+1)
	p := obj{
		"type":           "acme",
		"tag":            t,
		"domain":         []string{sec.SNI},
		"data_directory": "acme", // relative to the run dir, kept across applies
	}
	if sec.ACMEEmail != "" {
		p["email"] = sec.ACMEEmail
	}
	switch sec.ACMEDNS {
	case ACMEDNSCloudflare:
		p["dns01_challenge"] = obj{"provider": "cloudflare", "api_token": sec.ACMEDNSKey}
	case ACMEDNSAliDNS:
		p["dns01_challenge"] = obj{"provider": "alidns", "access_key_id": sec.ACMEDNSKey, "access_key_secret": sec.ACMEDNSSecret}
	}
	a.tags[key] = t
	a.list = append(a.list, p)
	return t
}

// singboxInbounds returns the inbound objects for one panel inbound. A
// ShadowTLS-fronted Shadowsocks or Snell becomes two: the public shadowtls
// inbound and a loopback backend it detours to.
func singboxInbounds(in Inbound, acme *acmeProviders) ([]interface{}, error) {
	item := obj{
		"tag":         in.Tag(),
		"listen":      in.Listen,
		"listen_port": in.Port,
	}
	s := in.Settings
	switch in.Protocol {
	case ProtocolVLESS:
		user := obj{"uuid": s.UUID}
		if s.Flow != "" {
			user["flow"] = s.Flow
		}
		item["type"] = "vless"
		item["users"] = []interface{}{user}
	case ProtocolVMess:
		item["type"] = "vmess"
		item["users"] = []interface{}{obj{"uuid": s.UUID, "alterId": 0}}
	case ProtocolTrojan:
		item["type"] = "trojan"
		item["users"] = []interface{}{obj{"password": s.Password}}
	case ProtocolShadowsocks:
		if in.Security.Type == SecurityShadowTLS {
			return singboxShadowTLSFront(in, obj{
				"type":     "shadowsocks",
				"method":   s.Method,
				"password": s.Password,
			})
		}
		item["type"] = "shadowsocks"
		item["method"] = s.Method
		item["password"] = s.Password
	case ProtocolSnell:
		// Server version 5 also serves v4 clients (the same wire protocol
		// without QUIC proxy mode, which sing-box does not offer).
		snell := obj{"type": "snell", "version": 5, "psk": s.Password}
		if s.SnellVersion == 6 {
			snell["version"] = 6
			if s.SnellMode != "" {
				snell["mode"] = s.SnellMode
			}
		}
		if in.Security.Type == SecurityShadowTLS {
			return singboxShadowTLSFront(in, snell)
		}
		for k, v := range snell {
			item[k] = v
		}
	case ProtocolHysteria2:
		item["type"] = "hysteria2"
		item["users"] = []interface{}{obj{"password": s.Password}}
		item["ignore_client_bandwidth"] = true
		if s.Masquerade != "" {
			item["masquerade"] = s.Masquerade
		}
	case ProtocolTUIC:
		item["type"] = "tuic"
		item["users"] = []interface{}{obj{"uuid": s.UUID, "password": s.Password}}
		item["congestion_control"] = "bbr"
	case ProtocolAnyTLS:
		item["type"] = "anytls"
		item["users"] = []interface{}{obj{"password": s.Password}}
	case ProtocolNaive:
		item["type"] = "naive"
		item["users"] = []interface{}{obj{"username": s.Username, "password": s.Password}}
	case ProtocolSOCKS:
		item["type"] = "socks"
		item["users"] = []interface{}{obj{"username": s.Username, "password": s.Password}}
	case ProtocolHTTP:
		item["type"] = "http"
		item["users"] = []interface{}{obj{"username": s.Username, "password": s.Password}}
	case ProtocolWireGuard:
		// An endpoint, not an inbound (renderSingbox moves it); it listens
		// on every address.
		ep := obj{
			"type":        "wireguard",
			"tag":         in.Tag(),
			"system":      false,
			"mtu":         s.MTU,
			"address":     []string{s.WireGuardServerAddress() + "/24"},
			"private_key": s.PrivateKey,
			"listen_port": in.Port,
			"peers":       []interface{}{obj{"public_key": s.PeerPublicKey, "allowed_ips": []string{s.PeerAddress + "/32"}}},
		}
		if err := mergeExtra(ep, in.Extra); err != nil {
			return nil, err
		}
		return []interface{}{ep}, nil
	default:
		return nil, fmt.Errorf("sing-box 不支持协议 %s", in.Protocol)
	}
	if extra := singboxUsers(in); len(extra) > 0 {
		item["users"] = append(item["users"].([]interface{}), extra...)
	}

	if tls, err := singboxTLS(in, acme); err != nil {
		return nil, err
	} else if tls != nil {
		item["tls"] = tls
	}
	switch in.Transport.Type {
	case TransportWS:
		t := obj{"type": "ws", "path": in.Transport.Path}
		if in.Transport.Host != "" {
			t["headers"] = obj{"Host": in.Transport.Host}
		}
		item["transport"] = t
	case TransportGRPC:
		item["transport"] = obj{"type": "grpc", "service_name": in.Transport.ServiceName}
	}
	if err := mergeExtra(item, in.Extra); err != nil {
		return nil, err
	}
	return []interface{}{item}, nil
}

// singboxShadowTLSFront returns the public shadowtls v3 inbound and the
// loopback backend (a Shadowsocks or Snell inbound) it detours to.
func singboxShadowTLSFront(in Inbound, backend obj) ([]interface{}, error) {
	backendTag := singboxShadowTLSBackendTag(in)
	front := obj{
		"type":        "shadowtls",
		"tag":         in.Tag(),
		"listen":      in.Listen,
		"listen_port": in.Port,
		"version":     3,
		"users":       []interface{}{obj{"password": in.Security.ShadowTLSPassword}},
		"handshake":   obj{"server": in.Security.SNI, "server_port": 443},
		"detour":      backendTag,
	}
	backend["tag"] = backendTag
	backend["listen"] = "127.0.0.1"
	if err := mergeExtra(front, in.Extra); err != nil {
		return nil, err
	}
	return []interface{}{front, backend}, nil
}

func singboxTLS(in Inbound, acme *acmeProviders) (obj, error) {
	sec := in.Security
	switch sec.Type {
	case SecurityTLS:
		tls := obj{"enabled": true, "server_name": sec.SNI}
		if len(sec.ALPN) > 0 {
			tls["alpn"] = sec.ALPN
		}
		switch sec.CertMode {
		case CertModeACME:
			tls["certificate_provider"] = acme.tag(sec)
			// Without DNS-01, sing-box answers the TLS-ALPN challenge on this
			// listener, so acme-tls/1 becomes its only ALPN and clients that
			// offer h2/http/1.1 (every uTLS fingerprint) get "no application
			// protocol". Advertise the usual web ALPN next to it.
			if len(sec.ALPN) == 0 && sec.ACMEDNS == "" {
				tls["alpn"] = []string{"h2", "http/1.1"}
				if in.Transport.Type == TransportWS {
					tls["alpn"] = []string{"http/1.1"}
				}
			}
		case CertModeFile:
			tls["certificate_path"] = sec.CertFile
			tls["key_path"] = sec.KeyFile
		default:
			tls["certificate"] = pemLines(sec.CertPEM)
			tls["key"] = pemLines(sec.KeyPEM)
		}
		return tls, nil
	case SecurityReality:
		host, portStr, err := net.SplitHostPort(sec.RealityDest)
		if err != nil {
			return nil, fmt.Errorf("Reality 目标地址错误")
		}
		port, _ := strconv.Atoi(portStr)
		return obj{
			"enabled":     true,
			"server_name": sec.SNI,
			"reality": obj{
				"enabled":     true,
				"handshake":   obj{"server": host, "server_port": port},
				"private_key": sec.RealityPrivateKey,
				"short_id":    sec.RealityShortIDs,
			},
		}, nil
	}
	return nil, nil
}

// ruleSetHTTPClient is the HTTP client rule sets are downloaded with: a
// direct connection.
const ruleSetHTTPClient = "rule-set"

// singboxRuleSets defines each rule set once, in first-use order.
func singboxRuleSets(tags []string, opts RenderOptions) []interface{} {
	var out []interface{}
	seen := map[string]bool{}
	for _, t := range tags {
		if seen[t] {
			continue
		}
		seen[t] = true
		// Downloaded directly (download_detour is deprecated in sing-box
		// 1.14; before it, the default outbound is the direct one).
		set := obj{"type": "remote", "tag": t, "format": "binary", "url": RuleSetURL(t, opts.RuleSetMirror)}
		if opts.SingboxHTTPClient {
			set["http_client"] = ruleSetHTTPClient
		}
		out = append(out, set)
	}
	return out
}
