package kernel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
)

// Chained outbounds: an inbound whose Via is set sends its traffic on
// through another proxy (a node of this panel or an imported one) instead
// of leaving from the node itself. Each kernel only dials the next hop, so
// longer chains are built by chaining the target inbound in turn.

func chainTag(in Inbound) string { return fmt.Sprintf("via-%d", in.ID) }

func chainUserTag(in Inbound, userID int64) string {
	return fmt.Sprintf("via-%d-u%d", in.ID, userID)
}

// chainBlocked reports a chained inbound whose target is gone; its traffic
// is dropped rather than leaked out of the node's own address.
func (in Inbound) chainBlocked() bool { return in.Via != nil && in.Via.Host == "" }

// countedUsers returns the inbound's users whose traffic carries a mark.
func (in Inbound) countedUsers() []int64 {
	if !in.SupportsUsers() || in.noUserMarks {
		return nil
	}
	var ids []int64
	for _, u := range in.Users {
		if UserMark(u.ID) != 0 {
			ids = append(ids, u.ID)
		}
	}
	return ids
}

// CheckChain reports whether a kernel can dial the given endpoint as a
// chained outbound.
func CheckChain(kernelType string, via Endpoint) error {
	return CheckChainWith(kernelType, via, RenderOptions{})
}

// CheckChainWith is CheckChain for a kernel with the given options (what
// its version supports).
func CheckChainWith(kernelType string, via Endpoint, opts RenderOptions) error {
	via.Name = "check"
	switch kernelType {
	case TypeSingbox:
		if via.Proxy == nil && via.Inbound.Protocol == ProtocolSnell && !opts.SingboxSnell {
			return errors.New("sing-box 1.14 及以上才能连接 Snell 节点，请先更新内核")
		}
		_, err := singboxChainOutbounds(via, "check", 0, opts.SingboxSnell)
		return err
	case TypeXray:
		_, err := xrayChainOutbound(via, "check")
		return err
	}
	info, _ := Lookup(kernelType)
	name := info.Name
	if name == "" {
		name = kernelType
	}
	return fmt.Errorf("%s 内核不支持链式出站", name)
}

// ---- sing-box ----

// singboxChainOutbounds renders the outbounds that dial the chain target;
// a non-zero mark goes on the one that opens the socket.
func singboxChainOutbounds(via Endpoint, tag string, mark int64, snell bool) ([]interface{}, error) {
	via.Name = tag
	obs, ok := singboxOutbounds(via, snell)
	if !ok {
		return nil, fmt.Errorf("sing-box 无法连接 %s 类型的出站节点", chainTargetLabel(via))
	}
	for _, o := range obs {
		if o["type"] == "wireguard" {
			return nil, errors.New("WireGuard 节点不能用作出站，请选其他节点")
		}
	}
	out := make([]interface{}, 0, len(obs))
	for _, o := range obs {
		if _, detour := o["detour"]; !detour && mark != 0 {
			o["routing_mark"] = mark
		}
		out = append(out, obj(o))
	}
	return out, nil
}

// singboxInboundTags lists the tags a panel inbound's connections carry; a
// ShadowTLS front hands them to its loopback Shadowsocks / Snell backend.
func singboxInboundTags(in Inbound) []string {
	if backend := singboxShadowTLSBackendTag(in); backend != "" {
		return []string{in.Tag(), backend}
	}
	return []string{in.Tag()}
}

// singboxShadowTLSBackendTag is the tag of the inbound a ShadowTLS front
// detours to, or "" when the inbound has no front.
func singboxShadowTLSBackendTag(in Inbound) string {
	if in.Security.Type != SecurityShadowTLS {
		return ""
	}
	switch in.Protocol {
	case ProtocolShadowsocks:
		return in.Tag() + "-ss"
	case ProtocolSnell:
		return in.Tag() + "-snell"
	}
	return ""
}

// singboxChainRoutes returns the outbounds, route rules and rule-set tags of
// the chained and split-routed inbounds. The rules must precede the per-user
// direct rules.
func singboxChainRoutes(inbounds []Inbound, snell bool) ([]interface{}, []interface{}, []string, error) {
	var outbounds, rules []interface{}
	var sets []string
	for _, in := range inbounds {
		if !in.routed() {
			continue
		}
		tags := singboxInboundTags(in)
		if in.splitRequiresSniff() {
			rules = append(rules, obj{"inbound": tags, "action": "sniff"})
		}
		obs, rs, ss, err := singboxSplitRoutes(in, snell)
		if err != nil {
			return nil, nil, nil, err
		}
		outbounds, rules, sets = append(outbounds, obs...), append(rules, rs...), append(sets, ss...)
		if in.Via == nil {
			continue
		}
		if in.chainBlocked() {
			rules = append(rules, obj{"inbound": tags, "action": "reject"})
			continue
		}
		for _, id := range in.countedUsers() {
			tag := chainUserTag(in, id)
			obs, err := singboxChainOutbounds(*in.Via, tag, UserMark(id), snell)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("入站 %d: %v", in.ID, err)
			}
			outbounds = append(outbounds, obs...)
			rules = append(rules, obj{"inbound": tags, "auth_user": []string{User{ID: id}.Name()}, "outbound": tag})
		}
		tag := chainTag(in)
		obs, err = singboxChainOutbounds(*in.Via, tag, 0, snell)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("入站 %d: %v", in.ID, err)
		}
		outbounds = append(outbounds, obs...)
		rules = append(rules, obj{"inbound": tags, "outbound": tag})
	}
	return outbounds, rules, sets, nil
}

// ---- Xray ----

// xrayChainRoutes is singboxChainRoutes for Xray.
func xrayChainRoutes(inbounds []Inbound) ([]interface{}, []interface{}, error) {
	var outbounds, rules []interface{}
	for _, in := range inbounds {
		if !in.routed() {
			continue
		}
		obs, rs, err := xraySplitRoutes(in)
		if err != nil {
			return nil, nil, err
		}
		outbounds, rules = append(outbounds, obs...), append(rules, rs...)
		if in.Via == nil {
			continue
		}
		tags := []string{in.Tag()}
		if in.chainBlocked() {
			rules = append(rules, obj{"type": "field", "inboundTag": tags, "outboundTag": "block"})
			continue
		}
		for _, id := range in.countedUsers() {
			tag := chainUserTag(in, id)
			ob, err := xrayChainOutbound(*in.Via, tag)
			if err != nil {
				return nil, nil, fmt.Errorf("入站 %d: %v", in.ID, err)
			}
			ob["streamSettings"].(obj)["sockopt"] = obj{"mark": UserMark(id)}
			outbounds = append(outbounds, ob)
			rules = append(rules, obj{"type": "field", "inboundTag": tags, "user": xrayUserMatch(id), "outboundTag": tag})
		}
		tag := chainTag(in)
		ob, err := xrayChainOutbound(*in.Via, tag)
		if err != nil {
			return nil, nil, fmt.Errorf("入站 %d: %v", in.ID, err)
		}
		outbounds = append(outbounds, ob)
		rules = append(rules, obj{"type": "field", "inboundTag": tags, "outboundTag": tag})
	}
	return outbounds, rules, nil
}

// chainTarget is the client side of a chain target, flattened from either
// a panel inbound or an imported proxy.
type chainTarget struct {
	Protocol string // vless, vmess, trojan, shadowsocks, socks, http
	Host     string
	Port     int

	UUID     string
	Flow     string
	Cipher   string // VMess security / Shadowsocks method
	Username string
	Password string

	Network     string // tcp, ws, httpupgrade, grpc, xhttp
	Path        string
	HostHeader  string
	ServiceName string
	Mode        string

	Security    string // none, tls, reality
	SNI         string
	ALPN        []string
	Fingerprint string
	// PinSHA256 replaces CA verification (self-signed certificates); Xray
	// no longer accepts allowInsecure.
	PinSHA256      string
	RealityPublic  string
	RealityShortID string
	// Encryption is the VLESS Encryption client setting ("" for none).
	Encryption string
}

func chainTargetLabel(e Endpoint) string {
	if e.Proxy != nil {
		return DescribeProxy(e.Proxy)
	}
	parts := []string{e.Inbound.Protocol}
	if t := e.Inbound.Transport.Type; t != "" && t != TransportTCP {
		parts = append(parts, t)
	}
	if s := e.Inbound.Security.Type; s != "" && s != SecurityNone {
		parts = append(parts, s)
	}
	return strings.Join(parts, " + ")
}

func chainTargetFromInbound(e Endpoint) (chainTarget, error) {
	in := e.Inbound
	t := chainTarget{Host: strings.Trim(e.Host, "[]"), Port: e.Port}
	switch in.Protocol {
	case ProtocolVLESS:
		t.Protocol, t.UUID, t.Flow = "vless", in.Settings.UUID, in.Settings.Flow
		t.Encryption = in.Settings.Encryption
	case ProtocolVMess:
		t.Protocol, t.UUID, t.Cipher = "vmess", in.Settings.UUID, "auto"
	case ProtocolTrojan:
		t.Protocol, t.Password = "trojan", in.Settings.Password
	case ProtocolShadowsocks:
		if in.Security.Type == SecurityShadowTLS {
			return t, errors.New("Xray 无法连接 ShadowTLS 节点，请改用 sing-box 内核")
		}
		t.Protocol, t.Cipher, t.Password = "shadowsocks", in.Settings.Method, in.Settings.Password
		return t, nil
	default:
		return t, fmt.Errorf("Xray 无法连接 %s 节点，请改用 sing-box 内核", in.Protocol)
	}
	tr := in.Transport
	t.Network = firstNonEmptyString(tr.Type, TransportTCP)
	t.Path, t.HostHeader, t.ServiceName, t.Mode = tr.Path, tr.Host, tr.ServiceName, tr.Mode

	sec := in.Security
	t.Security = firstNonEmptyString(sec.Type, SecurityNone)
	if t.Security != SecurityNone {
		t.SNI, t.ALPN, t.Fingerprint = sec.SNI, sec.ALPN, sec.Fingerprint
	}
	switch {
	case t.Security == SecurityReality:
		t.RealityPublic = sec.RealityPublicKey
		if len(sec.RealityShortIDs) > 0 {
			t.RealityShortID = sec.RealityShortIDs[0]
		}
	case e.selfSigned():
		pin, err := certSHA256(sec.CertPEM)
		if err != nil {
			return t, err
		}
		t.PinSHA256 = pin
	}
	return t, nil
}

func chainTargetFromProxy(e Endpoint) (chainTarget, error) {
	p := e.Proxy.forEndpoint(e)
	t := chainTarget{Host: p.str("server"), Port: e.Port}
	unsupported := fmt.Errorf("Xray 无法连接 %s 节点，请改用 sing-box 内核", DescribeProxy(e.Proxy))
	switch p.Type() {
	case "vless":
		t.Protocol, t.UUID, t.Flow = "vless", p.str("uuid"), p.str("flow")
		if hasVLESSEncryption(p) {
			t.Encryption = p.str("encryption")
		}
	case "wireguard":
		return t, errors.New("WireGuard 节点不能用作出站，请选其他节点")
	case "vmess":
		t.Protocol, t.UUID, t.Cipher = "vmess", p.str("uuid"), firstNonEmptyString(p.str("cipher"), "auto")
	case "trojan":
		t.Protocol, t.Password = "trojan", p.str("password")
	case "ss":
		if p.str("plugin") != "" {
			return t, unsupported
		}
		t.Protocol, t.Cipher, t.Password = "shadowsocks", p.str("cipher"), p.str("password")
		return t, nil
	case "socks5":
		t.Protocol, t.Username, t.Password = "socks", p.str("username"), p.str("password")
	case "http":
		t.Protocol, t.Username, t.Password = "http", p.str("username"), p.str("password")
	default:
		return t, unsupported
	}

	host, path, service, mode := transportFields(p)
	t.HostHeader, t.Path, t.ServiceName, t.Mode = host, path, service, mode
	switch n := p.network(); n {
	case "tcp", "grpc", "xhttp":
		t.Network = n
	case "ws":
		t.Network = TransportWS
		if p.sub("ws-opts").flag("v2ray-http-upgrade") {
			t.Network = "httpupgrade"
		}
	default:
		return t, unsupported
	}
	if t.Protocol == "socks" || t.Protocol == "http" {
		if t.Network != TransportTCP {
			return t, unsupported
		}
	}

	t.Security = SecurityNone
	if p.usesTLS() {
		t.Security = SecurityTLS
		t.SNI = p.serverName()
		t.ALPN = p.strs("alpn")
		t.Fingerprint = p.str("client-fingerprint")
		if ro := p.sub("reality-opts"); len(ro) > 0 {
			t.Security = SecurityReality
			t.RealityPublic, t.RealityShortID = ro.str("public-key"), ro.str("short-id")
		} else if p.flag("skip-cert-verify") {
			// Mihomo's fingerprint is the certificate's SHA-256.
			t.PinSHA256 = p.str("fingerprint")
			if t.PinSHA256 == "" {
				return t, errors.New("该节点跳过了证书验证，Xray 已不支持，请改用 sing-box 内核")
			}
		}
	}
	return t, nil
}

// xrayChainOutbound renders the Xray outbound that dials a chain target.
func xrayChainOutbound(e Endpoint, tag string) (obj, error) {
	var t chainTarget
	var err error
	if p, ok := clientProxy(e); ok && e.Proxy == nil {
		e.Proxy = p
	}
	if e.Proxy != nil {
		t, err = chainTargetFromProxy(e)
	} else {
		t, err = chainTargetFromInbound(e)
	}
	if err != nil {
		return nil, err
	}
	if t.Host == "" || t.Port <= 0 {
		return nil, errors.New("出站节点缺少地址")
	}

	var settings obj
	switch t.Protocol {
	case "vless":
		user := obj{"id": t.UUID, "encryption": firstNonEmptyString(t.Encryption, "none")}
		if t.Flow != "" {
			user["flow"] = t.Flow
		}
		settings = obj{"vnext": []interface{}{obj{"address": t.Host, "port": t.Port, "users": []interface{}{user}}}}
	case "vmess":
		settings = obj{"vnext": []interface{}{obj{"address": t.Host, "port": t.Port,
			"users": []interface{}{obj{"id": t.UUID, "security": t.Cipher}}}}}
	case "trojan":
		settings = obj{"servers": []interface{}{obj{"address": t.Host, "port": t.Port, "password": t.Password}}}
	case "shadowsocks":
		settings = obj{"servers": []interface{}{obj{"address": t.Host, "port": t.Port, "method": t.Cipher, "password": t.Password}}}
	case "socks", "http":
		server := obj{"address": t.Host, "port": t.Port}
		if t.Username != "" {
			server["users"] = []interface{}{obj{"user": t.Username, "pass": t.Password}}
		}
		settings = obj{"servers": []interface{}{server}}
	}

	stream := obj{"network": firstNonEmptyString(t.Network, TransportTCP)}
	switch t.Network {
	case TransportWS, "httpupgrade":
		s := obj{"path": firstNonEmptyString(t.Path, "/")}
		if t.HostHeader != "" {
			s["host"] = t.HostHeader
		}
		stream[t.Network+"Settings"] = s
	case TransportGRPC:
		stream["grpcSettings"] = obj{"serviceName": t.ServiceName}
	case TransportXHTTP:
		s := obj{"path": firstNonEmptyString(t.Path, "/"), "mode": firstNonEmptyString(t.Mode, defaultXHTTPMode)}
		if t.HostHeader != "" {
			s["host"] = t.HostHeader
		}
		stream["xhttpSettings"] = s
	}
	switch t.Security {
	case SecurityTLS:
		tls := obj{}
		if t.SNI != "" {
			tls["serverName"] = t.SNI
		}
		if len(t.ALPN) > 0 {
			tls["alpn"] = t.ALPN
		}
		if t.Fingerprint != "" {
			tls["fingerprint"] = t.Fingerprint
		}
		if t.PinSHA256 != "" {
			tls["pinnedPeerCertSha256"] = t.PinSHA256
		}
		stream["security"] = SecurityTLS
		stream["tlsSettings"] = tls
	case SecurityReality:
		r := obj{
			"serverName":  t.SNI,
			"fingerprint": firstNonEmptyString(t.Fingerprint, DefaultFingerprint),
			"publicKey":   t.RealityPublic,
		}
		if t.RealityShortID != "" {
			r["shortId"] = t.RealityShortID
		}
		stream["security"] = SecurityReality
		stream["realitySettings"] = r
	}
	return obj{"tag": tag, "protocol": t.Protocol, "settings": settings, "streamSettings": stream}, nil
}

// certSHA256 is the hex SHA-256 of the first certificate in a PEM bundle.
func certSHA256(certPEM string) (string, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil || block.Type != "CERTIFICATE" {
		return "", errors.New("出站节点的自签证书损坏")
	}
	sum := sha256.Sum256(block.Bytes)
	return hex.EncodeToString(sum[:]), nil
}
