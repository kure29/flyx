package kernel

import (
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
)

// Proxy is an imported node in the Mihomo (Clash Meta) proxy schema. Like
// Sub-Store, every import format is parsed into this one shape and every
// subscription format is produced from it, so no parameter is lost when a
// node only passes through the panel.
type Proxy map[string]interface{}

// importTypes maps the supported Mihomo proxy types to the panel's protocol
// names (used for display and the inbound's protocol column).
var importTypes = map[string]string{
	"ss":        ProtocolShadowsocks,
	"vmess":     ProtocolVMess,
	"vless":     ProtocolVLESS,
	"trojan":    ProtocolTrojan,
	"hysteria2": ProtocolHysteria2,
	"tuic":      ProtocolTUIC,
	"anytls":    ProtocolAnyTLS,
	"snell":     ProtocolSnell,
	"socks5":    "socks5",
	"http":      "http",
}

var proxyLabels = map[string]string{
	"ss": "Shadowsocks", "vmess": "VMess", "vless": "VLESS", "trojan": "Trojan",
	"hysteria2": "Hysteria2", "tuic": "TUIC", "anytls": "AnyTLS", "snell": "Snell",
	"socks5": "SOCKS5", "http": "HTTP", "wireguard": "WireGuard",
}

// ImportedNode is a node the panel does not deploy; it only relays and
// shares it.
type ImportedNode struct {
	Name  string
	Host  string
	Port  int
	Proxy Proxy
}

func (p Proxy) str(k string) string {
	switch v := p[k].(type) {
	case string:
		return strings.TrimSpace(v)
	case nil:
		return ""
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func (p Proxy) num(k string) int {
	switch v := p[k].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	}
	return 0
}

func (p Proxy) flag(k string) bool {
	switch v := p[k].(type) {
	case bool:
		return v
	case string:
		v = strings.ToLower(strings.TrimSpace(v))
		return v == "true" || v == "1"
	case int:
		return v != 0
	case float64:
		return v != 0
	}
	return false
}

func (p Proxy) sub(k string) Proxy {
	switch v := p[k].(type) {
	case map[string]interface{}:
		return Proxy(v)
	case Proxy:
		return v
	}
	return Proxy{}
}

func (p Proxy) strs(k string) []string {
	var out []string
	switch v := p[k].(type) {
	case []interface{}:
		for _, x := range v {
			if s := strings.TrimSpace(fmt.Sprint(x)); s != "" {
				out = append(out, s)
			}
		}
	case []string:
		for _, s := range v {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	case string:
		for _, s := range strings.Split(v, ",") {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// set stores v unless it is empty, keeping generated maps tidy.
func (p Proxy) set(k string, v interface{}) {
	switch t := v.(type) {
	case string:
		if t == "" {
			return
		}
	case int:
		if t == 0 {
			return
		}
	case bool:
		if !t {
			return
		}
	case []string:
		if len(t) == 0 {
			return
		}
	case Proxy:
		if len(t) == 0 {
			return
		}
		v = map[string]interface{}(t)
	case map[string]interface{}:
		if len(t) == 0 {
			return
		}
	}
	p[k] = v
}

func (p Proxy) Type() string { return strings.ToLower(p.str("type")) }

func (p Proxy) network() string {
	n := strings.ToLower(p.str("network"))
	if n == "" {
		return "tcp"
	}
	return n
}

// serverName is the TLS server name, whichever key the type uses.
func (p Proxy) serverName() string {
	if s := p.str("servername"); s != "" {
		return s
	}
	return p.str("sni")
}

func (p Proxy) usesTLS() bool {
	switch p.Type() {
	case "trojan", "hysteria2", "tuic", "anytls":
		return true
	}
	return p.flag("tls") || len(p.sub("reality-opts")) > 0
}

func (p Proxy) clone() Proxy {
	b, _ := json.Marshal(p)
	out := Proxy{}
	_ = json.Unmarshal(b, &out)
	return out
}

// ProxyProtocol is the panel protocol name for an imported proxy.
func ProxyProtocol(p Proxy) string { return importTypes[p.Type()] }

// DescribeProxy summarises an imported proxy, e.g. "VLESS + WS + TLS".
func DescribeProxy(p Proxy) string {
	parts := []string{proxyLabels[p.Type()]}
	if parts[0] == "" {
		parts[0] = p.Type()
	}
	if n := p.network(); n != "tcp" && n != "" {
		parts = append(parts, strings.ToUpper(n))
	}
	switch {
	case len(p.sub("reality-opts")) > 0:
		parts = append(parts, "Reality")
	case p.flag("tls"):
		parts = append(parts, "TLS")
	}
	if plugin := p.str("plugin"); plugin != "" {
		parts = append(parts, plugin)
	}
	if p.str("obfs") != "" || len(p.sub("obfs-opts")) > 0 {
		parts = append(parts, "obfs")
	}
	return strings.Join(parts, " + ")
}

// ProxyParams flattens an imported proxy for display, sorted by key.
func ProxyParams(p Proxy) [][2]string {
	var out [][2]string
	var walk func(prefix string, m Proxy)
	walk = func(prefix string, m Proxy) {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if prefix == "" && (k == "name" || k == "server" || k == "port") {
				continue
			}
			switch v := m[k].(type) {
			case map[string]interface{}:
				walk(prefix+k+".", Proxy(v))
			case []interface{}:
				out = append(out, [2]string{prefix + k, strings.Join(Proxy{"x": v}.strs("x"), ", ")})
			default:
				out = append(out, [2]string{prefix + k, Proxy{"x": v}.str("x")})
			}
		}
	}
	walk("", p)
	return out
}

// forEndpoint renders the proxy for one endpoint. Relays dial a different
// address, so a domain the node was reached by is kept as the TLS server
// name and WebSocket Host, and port hopping (one forwarded port) is dropped.
func (p Proxy) forEndpoint(e Endpoint) Proxy {
	out := p.clone()
	orig := p.str("server")
	out["name"] = e.Name
	out["server"] = strings.Trim(e.Host, "[]")
	out["port"] = e.Port
	if e.Direct {
		return out
	}
	delete(out, "ports")
	delete(out, "hop-interval")
	if orig == "" || net.ParseIP(orig) != nil {
		return out
	}
	if out.usesTLS() && out.serverName() == "" {
		if t := out.Type(); t == "vmess" || t == "vless" {
			out["servername"] = orig
		} else {
			out["sni"] = orig
		}
	}
	if out.network() == "ws" {
		ws := out.sub("ws-opts").clone()
		headers := ws.sub("headers").clone()
		if headers.str("Host") == "" && headers.str("host") == "" {
			headers["Host"] = orig
			ws["headers"] = map[string]interface{}(headers)
			out["ws-opts"] = map[string]interface{}(ws)
		}
	}
	return out
}
