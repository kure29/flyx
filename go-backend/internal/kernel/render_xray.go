package kernel

import (
	"fmt"
	"strings"
)

type obj = map[string]interface{}

func renderXray(inbounds []Inbound) (string, error) {
	list := make([]interface{}, 0, len(inbounds))
	for _, in := range inbounds {
		item, err := xrayInbound(in)
		if err != nil {
			return "", fmt.Errorf("入站 %d: %v", in.ID, err)
		}
		list = append(list, item)
	}
	cfg := obj{
		"log":      obj{"loglevel": "warning"},
		"inbounds": list,
		"outbounds": []interface{}{
			obj{"protocol": "freedom", "tag": "direct"},
			obj{"protocol": "blackhole", "tag": "block"},
		},
	}
	// Chained inbounds leave through their target; their rules come first
	// so their users do not match the direct per-user rules below.
	chained, rules, err := xrayChainRoutes(inbounds)
	if err != nil {
		return "", err
	}
	outbounds := append(cfg["outbounds"].([]interface{}), chained...)
	// Each user leaves through a direct outbound whose sockets carry the
	// user's mark (see UserMark).
	for _, id := range markedUsers(inbounds) {
		tag := userOutboundTag(id)
		outbounds = append(outbounds, obj{"protocol": "freedom", "tag": tag,
			"streamSettings": obj{"sockopt": obj{"mark": UserMark(id)}}})
		rules = append(rules, obj{"type": "field", "user": xrayUserMatch(id), "outboundTag": tag})
	}
	cfg["outbounds"] = outbounds
	if len(rules) > 0 {
		cfg["routing"] = obj{"rules": rules}
	}
	if err := applyOutboundIPPolicies(TypeXray, cfg, inbounds, false); err != nil {
		return "", err
	}
	return marshalConfig(cfg)
}

func xrayInbound(in Inbound) (obj, error) {
	settings, err := xraySettings(in)
	if err != nil {
		return nil, err
	}
	item := obj{
		"tag":      in.Tag(),
		"listen":   in.Listen,
		"port":     in.Port,
		"protocol": in.Protocol,
		"settings": settings,
		"sniffing": obj{
			"enabled":      true,
			"destOverride": []string{"http", "tls", "quic"},
		},
	}
	if in.Protocol == ProtocolWireGuard {
		// WireGuard brings its own UDP transport.
	} else if in.Protocol != ProtocolShadowsocks {
		item["streamSettings"] = xrayStream(in)
	} else if in.AcceptProxyProtocol {
		item["streamSettings"] = obj{"network": TransportTCP}
	}
	if in.AcceptProxyProtocol {
		stream := item["streamSettings"].(obj)
		stream["sockopt"] = obj{"acceptProxyProtocol": true}
	}
	if err := mergeExtra(item, in.Extra); err != nil {
		return nil, err
	}
	return item, nil
}

func xraySettings(in Inbound) (obj, error) {
	s := in.Settings
	switch in.Protocol {
	case ProtocolVLESS:
		client := obj{"id": s.UUID}
		if s.Flow != "" {
			client["flow"] = s.Flow
		}
		return obj{"clients": append([]interface{}{client}, xrayUserClients(in)...),
			"decryption": firstNonEmptyString(s.Decryption, "none")}, nil
	case ProtocolVMess:
		return obj{"clients": append([]interface{}{obj{"id": s.UUID}}, xrayUserClients(in)...)}, nil
	case ProtocolTrojan:
		return obj{"clients": append([]interface{}{obj{"password": s.Password}}, xrayUserClients(in)...)}, nil
	case ProtocolShadowsocks:
		return obj{"method": s.Method, "password": s.Password, "network": "tcp,udp"}, nil
	case ProtocolSOCKS:
		accounts := append([]interface{}{obj{"user": s.Username, "pass": s.Password}}, xrayUserClients(in)...)
		return obj{"auth": "password", "accounts": accounts, "udp": true}, nil
	case ProtocolHTTP:
		return obj{"accounts": append([]interface{}{obj{"user": s.Username, "pass": s.Password}}, xrayUserClients(in)...)}, nil
	case ProtocolWireGuard:
		return obj{
			"secretKey": s.PrivateKey,
			"peers":     []interface{}{obj{"publicKey": s.PeerPublicKey, "allowedIPs": []string{s.PeerAddress + "/32"}}},
			"mtu":       s.MTU,
		}, nil
	}
	return nil, fmt.Errorf("Xray 不支持协议 %s", in.Protocol)
}

func xrayStream(in Inbound) obj {
	t := in.Transport
	stream := obj{"network": t.Type, "security": in.Security.Type}
	switch t.Type {
	case TransportWS:
		ws := obj{"path": t.Path}
		if t.Host != "" {
			ws["host"] = t.Host
		}
		stream["wsSettings"] = ws
	case TransportGRPC:
		stream["grpcSettings"] = obj{"serviceName": t.ServiceName}
	case TransportXHTTP:
		x := obj{"path": t.Path, "mode": t.Mode}
		if t.Host != "" {
			x["host"] = t.Host
		}
		stream["xhttpSettings"] = x
	}

	sec := in.Security
	switch sec.Type {
	case SecurityTLS:
		tls := obj{"serverName": sec.SNI}
		if len(sec.ALPN) > 0 {
			tls["alpn"] = sec.ALPN
		}
		if sec.CertMode == "file" {
			tls["certificates"] = []interface{}{obj{"certificateFile": sec.CertFile, "keyFile": sec.KeyFile}}
		} else {
			tls["certificates"] = []interface{}{obj{
				"certificate": pemLines(sec.CertPEM),
				"key":         pemLines(sec.KeyPEM),
			}}
		}
		stream["tlsSettings"] = tls
	case SecurityReality:
		stream["realitySettings"] = obj{
			"show":        false,
			"dest":        sec.RealityDest,
			"xver":        0,
			"serverNames": []string{sec.SNI},
			"privateKey":  sec.RealityPrivateKey,
			"shortIds":    sec.RealityShortIDs,
		}
		// Inbounds saved before the field existed get the default too.
		minVer := sec.RealityMinClientVer
		if minVer == "" {
			minVer = DefaultRealityMinClientVer
		}
		stream["realitySettings"].(obj)["minClientVer"] = minVer
	}
	return stream
}

// pemLines splits a PEM block into the line array form Xray accepts inline.
func pemLines(pem string) []string {
	lines := strings.Split(strings.TrimSpace(pem), "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}
