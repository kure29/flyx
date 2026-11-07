package kernel

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type Settings struct {
	UUID     string `json:"uuid,omitempty"`
	Username string `json:"username,omitempty"` // NaïveProxy
	Flow     string `json:"flow,omitempty"`
	Password string `json:"password,omitempty"`
	Method   string `json:"method,omitempty"`

	// SnellVersion is the protocol version clients use (4, 5 or 6).
	SnellVersion int `json:"snellVersion,omitempty"`
	// SnellMode is Snell v6's traffic shaping: "" (default) or "unshaped";
	// clients must use the same.
	SnellMode string `json:"snellMode,omitempty"`
	// Masquerade is the site Hysteria2 serves to non-clients (optional URL).
	Masquerade string `json:"masquerade,omitempty"`
	// BackendPort is the loopback port a ShadowTLS-fronted Snell listens on.
	BackendPort int `json:"backendPort,omitempty"`
	// PortHopping is a UDP range ("20000-30000") the agent redirects to the
	// Hysteria2 port so clients can hop between ports.
	PortHopping string `json:"portHopping,omitempty"`

	// VLESS Encryption (Xray only): Decryption is the server setting
	// ("auto" asks for new keys), Encryption what clients send.
	Decryption string `json:"decryption,omitempty"`
	Encryption string `json:"encryption,omitempty"`

	// WireGuard: the server key and one client peer (shared by everyone
	// connecting to the inbound, like a Shadowsocks password).
	PrivateKey     string `json:"privateKey,omitempty"`
	PublicKey      string `json:"publicKey,omitempty"`
	PeerPrivateKey string `json:"peerPrivateKey,omitempty"`
	PeerPublicKey  string `json:"peerPublicKey,omitempty"`
	// PeerAddress is the client's tunnel address; the server takes the
	// first host of its /24.
	PeerAddress string `json:"peerAddress,omitempty"`
	MTU         int    `json:"mtu,omitempty"`
}

// HopRange returns the port-hopping range, or 0, 0 when disabled.
func (in Inbound) HopRange() (int, int) {
	lo, hi, err := parseHopRange(in.Settings.PortHopping)
	if err != nil {
		return 0, 0
	}
	return lo, hi
}

func parseHopRange(spec string) (int, int, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return 0, 0, nil
	}
	parts := strings.Split(strings.ReplaceAll(spec, ":", "-"), "-")
	if len(parts) != 2 {
		return 0, 0, errors.New("端口跳跃格式应为 起始-结束，例如 20000-30000")
	}
	lo, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	hi, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || lo < 1024 || hi > 65535 || lo >= hi {
		return 0, 0, errors.New("端口跳跃范围需在 1024-65535 之间，且起始小于结束")
	}
	return lo, hi, nil
}

type Transport struct {
	Type        string `json:"type"`
	Path        string `json:"path,omitempty"`
	Host        string `json:"host,omitempty"`
	ServiceName string `json:"serviceName,omitempty"`
	Mode        string `json:"mode,omitempty"`
}

type Security struct {
	Type        string   `json:"type"`
	SNI         string   `json:"sni,omitempty"`
	ALPN        []string `json:"alpn,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"`

	// TLS: "self" stores a generated certificate inline, "file" points at
	// paths that already exist on the node, "acme" lets sing-box obtain a
	// certificate for SNI (HTTP-01 / TLS-ALPN-01, or DNS-01 when configured).
	CertMode string `json:"certMode,omitempty"`
	CertPEM  string `json:"certPem,omitempty"`
	KeyPEM   string `json:"keyPem,omitempty"`
	CertFile string `json:"certFile,omitempty"`
	KeyFile  string `json:"keyFile,omitempty"`

	ACMEEmail string `json:"acmeEmail,omitempty"`
	// ACMEDNS selects DNS-01 ("cloudflare" / "alidns"); empty uses HTTP-01,
	// which needs TCP port 80 on the node reachable from the internet.
	ACMEDNS string `json:"acmeDns,omitempty"`
	// Cloudflare: API token. AliDNS: AccessKey ID.
	ACMEDNSKey string `json:"acmeDnsKey,omitempty"`
	// AliDNS: AccessKey secret.
	ACMEDNSSecret string `json:"acmeDnsSecret,omitempty"`

	RealityDest       string   `json:"realityDest,omitempty"`
	RealityPrivateKey string   `json:"realityPrivateKey,omitempty"`
	RealityPublicKey  string   `json:"realityPublicKey,omitempty"`
	RealityShortIDs   []string `json:"realityShortIds,omitempty"`
	// RealityMinClientVer is the oldest client Xray accepts. Xray 26.7.11+
	// defaults to 26.3.27 when unset, which locks out Mihomo/Clash (they
	// report 1.8.2) and older mobile clients, so the panel always sets it.
	RealityMinClientVer string `json:"realityMinClientVer,omitempty"`

	// ShadowTLS v3: SNI is the handshake site, this is the shared secret.
	ShadowTLSPassword string `json:"shadowTlsPassword,omitempty"`
}

// Inbound is the kernel-independent description of one listening service.
type Inbound struct {
	ID                 int64           `json:"id"`
	Protocol           string          `json:"protocol"`
	Listen             string          `json:"listen"`
	OutboundIPStrategy string          `json:"outboundIpStrategy,omitempty"`
	Port               int             `json:"port"`
	Settings           Settings        `json:"settings"`
	Transport          Transport       `json:"transport"`
	Security           Security        `json:"security"`
	Extra              json.RawMessage `json:"extra,omitempty"`
	// AcceptProxyProtocol makes the inbound require a PROXY protocol header
	// (sent by relay forwards) so the kernel sees the real client address.
	AcceptProxyProtocol bool `json:"acceptProxyProtocol,omitempty"`
	// Users are extra clients (subscription users) rendered next to the
	// inbound's own credentials. Filled in when building a kernel config.
	Users []User `json:"-"`
	// Via is the proxy the inbound's traffic leaves through (a chained
	// outbound); nil leaves from the node itself. A Via without a host (its
	// target was deleted) drops the traffic. Filled in when building a
	// kernel config.
	Via *Endpoint `json:"-"`
	// Split are the inbound's 分流 rules, tried before its default exit;
	// targets resolved. Filled in when building a kernel config.
	Split []SplitRule `json:"-"`
	// noUserMarks is set by RenderWith (RenderOptions.NoUserMarks): no
	// per-user marked outbounds for this inbound.
	noUserMarks bool
}

func (in Inbound) Tag() string {
	return fmt.Sprintf("in-%d", in.ID)
}

// NetworkOn is Network for the kernel serving the inbound: sing-box's Snell
// has no QUIC proxy mode, so it only listens on TCP.
func (in Inbound) NetworkOn(kernelType string) string {
	if in.Protocol == ProtocolSnell && kernelType == TypeSingbox {
		return "tcp"
	}
	return in.Network()
}

// Network reports which L4 protocols the inbound listens on, used for nft
// traffic counters.
func (in Inbound) Network() string {
	switch in.Protocol {
	case ProtocolHysteria2, ProtocolTUIC, ProtocolWireGuard:
		return "udp"
	case ProtocolSOCKS:
		return "tcp,udp"
	case ProtocolShadowsocks:
		if in.Security.Type == SecurityShadowTLS {
			return "tcp"
		}
		return "tcp,udp"
	case ProtocolNaive:
		// HTTP/2 over TCP, plus HTTP/3 for clients that enable QUIC.
		return "tcp,udp"
	case ProtocolSnell:
		// v5 adds UDP (QUIC proxy mode); behind ShadowTLS only TCP is exposed.
		// v6 (sing-box) carries UDP over TCP.
		if in.Security.Type == SecurityShadowTLS || in.Settings.SnellVersion == 4 || in.Settings.SnellVersion == 6 {
			return "tcp"
		}
		return "tcp,udp"
	}
	return "tcp"
}

const (
	FlowVision                 = "xtls-rprx-vision"
	DefaultRealitySNI          = "www.microsoft.com"
	DefaultSSMethod            = "2022-blake3-aes-128-gcm"
	DefaultFingerprint         = "chrome"
	DefaultTLSCertMode         = CertModeSelf
	CertModeSelf               = "self"
	CertModeFile               = "file"
	CertModeACME               = "acme"
	ACMEDNSCloudflare          = "cloudflare"
	ACMEDNSAliDNS              = "alidns"
	DefaultQUICSNI             = "www.bing.com"
	DefaultRealityMinClientVer = "1.0.0"
	DefaultSnellVersion        = 5
	defaultGRPCService         = "grpc"
	defaultXHTTPMode           = "auto"
	DefaultWireGuardPeer       = "10.66.66.2"
	DefaultWireGuardMTU        = 1420
	maxExtraConfigBytes        = 64 * 1024
)

var ssMethods = []string{
	"2022-blake3-aes-128-gcm",
	"2022-blake3-aes-256-gcm",
	"2022-blake3-chacha20-poly1305",
	"aes-128-gcm",
	"aes-256-gcm",
	"chacha20-ietf-poly1305",
}

var (
	shortIDPattern = regexp.MustCompile(`^[0-9a-f]{0,16}$`)
	hostPattern    = regexp.MustCompile(`^[A-Za-z0-9.-]+$`)
)

// SSMethods lists the Shadowsocks ciphers the panel offers.
func SSMethods() []string {
	out := make([]string, len(ssMethods))
	copy(out, ssMethods)
	return out
}

// Normalize validates an inbound against a kernel's capabilities, fills
// defaults and generates any missing credentials. It mutates in place so the
// caller persists exactly what will be rendered.
func Normalize(kernelType string, in *Inbound) error {
	if in == nil {
		return errors.New("入站不能为空")
	}
	info, ok := Lookup(kernelType)
	if !ok || !info.Implemented {
		return fmt.Errorf("暂不支持内核 %s", kernelType)
	}
	strategy, err := NormalizeOutboundIPStrategy(kernelType, in.OutboundIPStrategy)
	if err != nil {
		return err
	}
	in.OutboundIPStrategy = strategy

	in.Protocol = strings.ToLower(strings.TrimSpace(in.Protocol))
	proto, ok := info.protocol(in.Protocol)
	if !ok {
		return fmt.Errorf("%s 不支持协议 %s", info.Name, in.Protocol)
	}

	in.Transport.Type = strings.ToLower(strings.TrimSpace(in.Transport.Type))
	if in.Transport.Type == "" {
		in.Transport.Type = proto.Transports[0].Type
	}
	tcap, ok := proto.transport(in.Transport.Type)
	if !ok {
		return fmt.Errorf("%s 协议不支持传输方式 %s", in.Protocol, in.Transport.Type)
	}

	in.Security.Type = strings.ToLower(strings.TrimSpace(in.Security.Type))
	if in.Security.Type == "" {
		in.Security.Type = tcap.Securities[0]
	}
	if !containsString(tcap.Securities, in.Security.Type) {
		return fmt.Errorf("%s + %s 不支持安全层 %s", in.Protocol, in.Transport.Type, in.Security.Type)
	}

	if in.Port < 1 || in.Port > 65535 {
		return errors.New("端口必须在 1-65535 之间")
	}
	in.Listen = strings.TrimSpace(in.Listen)
	if in.Listen == "" {
		in.Listen = "::"
	}
	if net.ParseIP(in.Listen) == nil {
		return errors.New("监听地址必须是 IP")
	}
	if in.AcceptProxyProtocol && kernelType != TypeXray {
		return errors.New("获取真实 IP（PROXY protocol）目前只支持 Xray 内核")
	}
	if in.AcceptProxyProtocol && in.Protocol == ProtocolWireGuard {
		return errors.New("WireGuard 走 UDP，不支持获取真实 IP（PROXY protocol）")
	}

	if err := normalizeExtra(kernelType, in); err != nil {
		return err
	}
	if err := normalizeTransport(&in.Transport); err != nil {
		return err
	}
	if err := normalizeSettings(in); err != nil {
		return err
	}
	if err := normalizeSecurity(in); err != nil {
		return err
	}
	if in.Protocol == ProtocolSnell && kernelType == TypeSingbox {
		// sing-box chains ShadowTLS to Snell internally; no loopback port.
		in.Settings.BackendPort = 0
	}
	if in.Settings.Decryption != "" && kernelType != TypeXray {
		return errors.New("VLESS Encryption 目前只支持 Xray 内核")
	}
	if in.Security.CertMode == CertModeACME && kernelType != TypeSingbox {
		return errors.New("自动申请证书（ACME）目前只支持 sing-box 内核")
	}
	if in.Protocol == ProtocolNaive && in.Security.CertMode == CertModeSelf {
		// Naive clients verify certificates and self-signed ones are
		// trivially fingerprinted, which defeats NaïveProxy.
		return errors.New("NaïveProxy 需要真实证书：请选择自动申请证书或节点上的证书文件")
	}
	return nil
}

func normalizeExtra(kernelType string, in *Inbound) error {
	raw := strings.TrimSpace(string(in.Extra))
	if raw == "" || raw == "null" || raw == "{}" {
		in.Extra = nil
		return nil
	}
	if len(raw) > maxExtraConfigBytes {
		return errors.New("高级配置过大")
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return errors.New("高级配置必须是 JSON 对象")
	}
	in.Extra = json.RawMessage(raw)
	return ValidateOutboundIPExtra(kernelType, in.OutboundIPStrategy, in.Extra)
}

func normalizeTransport(t *Transport) error {
	t.Path = strings.TrimSpace(t.Path)
	t.Host = strings.TrimSpace(t.Host)
	t.ServiceName = strings.TrimSpace(t.ServiceName)
	t.Mode = strings.TrimSpace(t.Mode)
	if t.Host != "" && !hostPattern.MatchString(t.Host) {
		return errors.New("Host 格式错误")
	}
	switch t.Type {
	case TransportTCP, TransportQUIC, TransportUDP:
		t.Path, t.Host, t.ServiceName, t.Mode = "", "", "", ""
	case TransportWS, TransportXHTTP:
		if t.Path == "" {
			t.Path = "/" + RealityShortID()[:8]
		}
		if !strings.HasPrefix(t.Path, "/") {
			t.Path = "/" + t.Path
		}
		t.ServiceName = ""
		if t.Type == TransportWS {
			t.Mode = ""
		} else if t.Mode == "" {
			t.Mode = defaultXHTTPMode
		}
	case TransportGRPC:
		if t.ServiceName == "" {
			t.ServiceName = defaultGRPCService
		}
		t.Path, t.Mode = "", ""
	}
	return nil
}

func normalizeSettings(in *Inbound) error {
	s := &in.Settings
	s.UUID = strings.TrimSpace(s.UUID)
	s.Password = strings.TrimSpace(s.Password)
	s.Method = strings.TrimSpace(s.Method)
	s.Flow = strings.TrimSpace(s.Flow)
	if in.Protocol != ProtocolNaive && in.Protocol != ProtocolSOCKS && in.Protocol != ProtocolHTTP {
		s.Username = ""
	}
	if in.Protocol != ProtocolVLESS {
		s.Decryption = ""
	}
	if in.Protocol != ProtocolWireGuard {
		s.PrivateKey, s.PublicKey, s.PeerPrivateKey, s.PeerPublicKey, s.PeerAddress, s.MTU = "", "", "", "", "", 0
	}
	s.Encryption = ""

	switch in.Protocol {
	case ProtocolVLESS, ProtocolVMess:
		if s.UUID == "" {
			s.UUID = NewUUID()
		}
		if !uuidPattern.MatchString(strings.ToLower(s.UUID)) {
			return errors.New("UUID 格式错误")
		}
		s.Password, s.Method = "", ""
		if err := normalizeVLESSEncryption(s); err != nil {
			return err
		}
		// Vision needs a TLS-like layer: TLS, Reality or VLESS Encryption.
		visionOK := in.Protocol == ProtocolVLESS && in.Transport.Type == TransportTCP &&
			(in.Security.Type != SecurityNone || s.Decryption != "")
		if s.Flow != "" && (s.Flow != FlowVision || !visionOK) {
			s.Flow = ""
		}
	case ProtocolTrojan:
		if s.Password == "" {
			s.Password = RandomPassword()
		}
		s.UUID, s.Method, s.Flow = "", "", ""
	case ProtocolShadowsocks:
		if s.Method == "" {
			s.Method = DefaultSSMethod
		}
		if !containsString(ssMethods, s.Method) {
			return fmt.Errorf("不支持的加密方式 %s", s.Method)
		}
		if s.Password == "" {
			s.Password = ShadowsocksPassword(s.Method)
		}
		if n := SS2022KeyLen(s.Method); n > 0 {
			key, err := base64.StdEncoding.DecodeString(s.Password)
			if err != nil || len(key) != n {
				return fmt.Errorf("%s 的密钥必须是 %d 字节的 base64", s.Method, n)
			}
		}
		s.UUID, s.Flow = "", ""
	case ProtocolHysteria2, ProtocolAnyTLS:
		if s.Password == "" {
			s.Password = RandomPassword()
		}
		s.UUID, s.Method, s.Flow = "", "", ""
		s.Masquerade = strings.TrimSpace(s.Masquerade)
		if in.Protocol != ProtocolHysteria2 {
			s.Masquerade = ""
		}
		if s.Masquerade != "" {
			u, err := url.Parse(s.Masquerade)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return errors.New("伪装地址必须是 http(s):// 开头的网址")
			}
		}
	case ProtocolNaive:
		s.Username = strings.TrimSpace(s.Username)
		if s.Username == "" {
			s.Username = "u" + RealityShortID()[:8]
		}
		if !naiveUserPattern.MatchString(s.Username) {
			return errors.New("用户名只能包含字母、数字和 -_，长度 1-32")
		}
		if s.Password == "" {
			s.Password = RandomPassword()
		}
		if !snellPSKPattern.MatchString(s.Password) {
			return errors.New("密码只能包含字母、数字和 -_，长度 8-64")
		}
		s.UUID, s.Method, s.Flow = "", "", ""
	case ProtocolTUIC:
		if s.UUID == "" {
			s.UUID = NewUUID()
		}
		if !uuidPattern.MatchString(strings.ToLower(s.UUID)) {
			return errors.New("UUID 格式错误")
		}
		if s.Password == "" {
			s.Password = RandomPassword()
		}
		s.Method, s.Flow = "", ""
	case ProtocolSOCKS, ProtocolHTTP:
		s.Username = strings.TrimSpace(s.Username)
		if s.Username == "" {
			s.Username = "u" + RealityShortID()[:8]
		}
		if !naiveUserPattern.MatchString(s.Username) {
			return errors.New("用户名只能包含字母、数字和 -_，长度 1-32")
		}
		if s.Password == "" {
			s.Password = RandomPassword()
		}
		if !snellPSKPattern.MatchString(s.Password) {
			return errors.New("密码只能包含字母、数字和 -_，长度 8-64")
		}
		s.UUID, s.Method, s.Flow = "", "", ""
	case ProtocolWireGuard:
		s.UUID, s.Password, s.Method, s.Flow = "", "", "", ""
		if err := normalizeWireGuard(s); err != nil {
			return err
		}
	case ProtocolSnell:
		if s.Password == "" {
			s.Password = RandomPassword()
		}
		if !snellPSKPattern.MatchString(s.Password) {
			return errors.New("Snell PSK 只能包含字母、数字和 -_，长度 8-64")
		}
		if s.SnellVersion == 0 {
			s.SnellVersion = DefaultSnellVersion
		}
		if s.SnellVersion != 4 && s.SnellVersion != 5 && s.SnellVersion != 6 {
			return errors.New("Snell 版本只支持 4、5 或 6")
		}
		if s.SnellVersion == 6 && len(s.Password) < snellV6MinPSK {
			return fmt.Errorf("Snell v6 的 PSK 至少 %d 位", snellV6MinPSK)
		}
		s.SnellMode = strings.TrimSpace(s.SnellMode)
		if s.SnellMode == "default" || s.SnellVersion != 6 {
			s.SnellMode = ""
		}
		// unsafe-raw turns encryption off; not offered.
		if s.SnellMode != "" && s.SnellMode != SnellModeUnshaped {
			return errors.New("Snell v6 流量整形只支持默认或 unshaped")
		}
		s.UUID, s.Method, s.Flow = "", "", ""
	}
	if in.Protocol == ProtocolHysteria2 {
		lo, hi, err := parseHopRange(s.PortHopping)
		if err != nil {
			return err
		}
		s.PortHopping = ""
		if lo > 0 {
			if in.Port >= lo && in.Port <= hi {
				return errors.New("端口跳跃范围不能包含入站端口本身")
			}
			s.PortHopping = fmt.Sprintf("%d-%d", lo, hi)
		}
	} else {
		s.PortHopping = ""
	}
	if in.Protocol == ProtocolSnell && in.Security.Type == SecurityShadowTLS {
		if s.BackendPort == 0 {
			s.BackendPort = randomPort(20000, 60000)
		}
		if s.BackendPort < 1024 || s.BackendPort > 65535 || s.BackendPort == in.Port {
			return errors.New("ShadowTLS 后端端口无效")
		}
	} else {
		s.BackendPort = 0
	}
	return nil
}

// normalizeVLESSEncryption generates keys for "auto" and checks stored
// ones; Encryption is always derived from Decryption.
func normalizeVLESSEncryption(s *Settings) error {
	s.Decryption = strings.TrimSpace(s.Decryption)
	switch s.Decryption {
	case "", "none":
		s.Decryption = ""
		return nil
	case "auto":
		dec, enc, err := VLESSEncKeys()
		if err != nil {
			return fmt.Errorf("生成 VLESS Encryption 密钥失败: %v", err)
		}
		s.Decryption, s.Encryption = dec, enc
		return nil
	}
	s.Encryption = vlessEncryptionFor(s.Decryption)
	if s.Encryption == "" {
		return errors.New("VLESS Encryption 密钥格式错误，请重新生成")
	}
	return nil
}

func normalizeWireGuard(s *Settings) error {
	s.PrivateKey = strings.TrimSpace(s.PrivateKey)
	if s.PrivateKey == "" {
		priv, _, err := WireGuardKeyPair()
		if err != nil {
			return fmt.Errorf("生成 WireGuard 密钥失败: %v", err)
		}
		s.PrivateKey = priv
	}
	pub, err := WireGuardPublicKey(s.PrivateKey)
	if err != nil {
		return err
	}
	s.PublicKey = pub
	s.PeerPrivateKey = strings.TrimSpace(s.PeerPrivateKey)
	if s.PeerPrivateKey == "" {
		priv, _, err := WireGuardKeyPair()
		if err != nil {
			return fmt.Errorf("生成 WireGuard 密钥失败: %v", err)
		}
		s.PeerPrivateKey = priv
	}
	if s.PeerPublicKey, err = WireGuardPublicKey(s.PeerPrivateKey); err != nil {
		return err
	}
	s.PeerAddress = strings.TrimSpace(s.PeerAddress)
	if s.PeerAddress == "" {
		s.PeerAddress = DefaultWireGuardPeer
	}
	ip := net.ParseIP(s.PeerAddress).To4()
	if ip == nil || ip[3] < 2 || ip[3] == 255 {
		return errors.New("WireGuard 客户端地址应为 IPv4，例如 10.66.66.2")
	}
	if s.MTU == 0 {
		s.MTU = DefaultWireGuardMTU
	}
	if s.MTU < 1280 || s.MTU > 1500 {
		return errors.New("WireGuard MTU 应在 1280-1500 之间")
	}
	return nil
}

// WireGuardServerAddress is the server's tunnel address: the first host of
// the client's /24.
func (s Settings) WireGuardServerAddress() string {
	ip := net.ParseIP(s.PeerAddress).To4()
	if ip == nil {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d.1", ip[0], ip[1], ip[2])
}

var naiveUserPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

var semverPattern = regexp.MustCompile(`^\d{1,4}\.\d{1,4}\.\d{1,4}$`)

var snellPSKPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

// snellV6MinPSK is the shortest PSK Snell v6 accepts.
const snellV6MinPSK = 12

// SnellModeUnshaped keeps Snell v6's encryption but turns traffic shaping
// off (faster, easier to fingerprint).
const SnellModeUnshaped = "unshaped"

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func normalizeSecurity(in *Inbound) error {
	sec := &in.Security
	sec.SNI = strings.TrimSpace(sec.SNI)
	sec.Fingerprint = strings.TrimSpace(sec.Fingerprint)
	if sec.SNI != "" && !hostPattern.MatchString(sec.SNI) {
		return errors.New("SNI 格式错误")
	}

	switch sec.Type {
	case SecurityNone:
		*sec = Security{Type: SecurityNone}
		return nil
	case SecurityShadowTLS:
		*sec = Security{Type: SecurityShadowTLS, SNI: sec.SNI, Fingerprint: sec.Fingerprint, ShadowTLSPassword: strings.TrimSpace(sec.ShadowTLSPassword)}
		if sec.SNI == "" {
			sec.SNI = DefaultRealitySNI
		}
		if sec.Fingerprint == "" {
			sec.Fingerprint = DefaultFingerprint
		}
		if sec.ShadowTLSPassword == "" {
			sec.ShadowTLSPassword = RandomPassword()
		}
		if !snellPSKPattern.MatchString(sec.ShadowTLSPassword) {
			return errors.New("ShadowTLS 密码只能包含字母、数字和 -_，长度 8-64")
		}
		return nil
	case SecurityTLS:
		sec.RealityDest, sec.RealityPrivateKey, sec.RealityPublicKey, sec.RealityShortIDs = "", "", "", nil
		sec.RealityMinClientVer = ""
		sec.ShadowTLSPassword = ""
		quic := in.Protocol == ProtocolHysteria2 || in.Protocol == ProtocolTUIC
		if quic {
			sec.ALPN = []string{"h3"}
		}
		if sec.SNI == "" && (quic || in.Protocol == ProtocolAnyTLS) {
			sec.SNI = DefaultQUICSNI
		}
		if sec.Fingerprint == "" {
			sec.Fingerprint = DefaultFingerprint
		}
		if sec.CertMode == "" {
			sec.CertMode = DefaultTLSCertMode
		}
		if sec.CertMode != CertModeACME {
			sec.ACMEEmail, sec.ACMEDNS, sec.ACMEDNSKey, sec.ACMEDNSSecret = "", "", "", ""
		}
		switch sec.CertMode {
		case CertModeSelf:
			if sec.SNI == "" {
				return errors.New("TLS 需要填写域名 (SNI)")
			}
			sec.CertFile, sec.KeyFile = "", ""
			if sec.CertPEM == "" || sec.KeyPEM == "" || !certMatchesName(sec.CertPEM, sec.SNI) {
				cert, key, err := SelfSignedCert(sec.SNI)
				if err != nil {
					return fmt.Errorf("生成自签证书失败: %v", err)
				}
				sec.CertPEM, sec.KeyPEM = cert, key
			}
		case CertModeFile:
			sec.CertPEM, sec.KeyPEM = "", ""
			sec.CertFile = strings.TrimSpace(sec.CertFile)
			sec.KeyFile = strings.TrimSpace(sec.KeyFile)
			if !strings.HasPrefix(sec.CertFile, "/") || !strings.HasPrefix(sec.KeyFile, "/") {
				return errors.New("证书和私钥需要填写节点上的绝对路径")
			}
		case CertModeACME:
			sec.CertPEM, sec.KeyPEM, sec.CertFile, sec.KeyFile = "", "", "", ""
			return normalizeACME(sec)
		default:
			return fmt.Errorf("未知证书模式 %s", sec.CertMode)
		}
		return nil
	case SecurityReality:
		sec.CertMode, sec.CertPEM, sec.KeyPEM, sec.CertFile, sec.KeyFile = "", "", "", "", ""
		sec.ACMEEmail, sec.ACMEDNS, sec.ACMEDNSKey, sec.ACMEDNSSecret = "", "", "", ""
		sec.ShadowTLSPassword = ""
		if sec.SNI == "" {
			sec.SNI = DefaultRealitySNI
		}
		if sec.Fingerprint == "" {
			sec.Fingerprint = DefaultFingerprint
		}
		sec.RealityDest = strings.TrimSpace(sec.RealityDest)
		if sec.RealityDest == "" {
			sec.RealityDest = sec.SNI + ":443"
		}
		if host, port, err := net.SplitHostPort(sec.RealityDest); err != nil || host == "" {
			return errors.New("Reality 目标地址格式应为 host:port")
		} else if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
			return errors.New("Reality 目标端口错误")
		}
		sec.RealityPrivateKey = strings.TrimSpace(sec.RealityPrivateKey)
		if sec.RealityPrivateKey == "" {
			priv, pub, err := RealityKeyPair()
			if err != nil {
				return fmt.Errorf("生成 Reality 密钥失败: %v", err)
			}
			sec.RealityPrivateKey, sec.RealityPublicKey = priv, pub
		} else {
			pub, err := RealityPublicKey(sec.RealityPrivateKey)
			if err != nil {
				return err
			}
			sec.RealityPublicKey = pub
		}
		ids := make([]string, 0, len(sec.RealityShortIDs))
		for _, id := range sec.RealityShortIDs {
			id = strings.ToLower(strings.TrimSpace(id))
			if !shortIDPattern.MatchString(id) || len(id)%2 != 0 {
				return errors.New("Reality shortId 必须是偶数位十六进制（最多 16 位）")
			}
			ids = append(ids, id)
		}
		if len(ids) == 0 {
			ids = []string{RealityShortID()}
		}
		sec.RealityShortIDs = ids
		sec.RealityMinClientVer = strings.TrimSpace(sec.RealityMinClientVer)
		if sec.RealityMinClientVer == "" {
			sec.RealityMinClientVer = DefaultRealityMinClientVer
		}
		if !semverPattern.MatchString(sec.RealityMinClientVer) {
			return errors.New("最低客户端版本格式应为 x.y.z，例如 1.0.0")
		}
		return nil
	}
	return fmt.Errorf("未知安全层 %s", sec.Type)
}

var acmeEmailPattern = regexp.MustCompile(`^[^@\s"]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$`)

func normalizeACME(sec *Security) error {
	if sec.SNI == "" || net.ParseIP(sec.SNI) != nil || !strings.Contains(sec.SNI, ".") || sec.SNI == DefaultQUICSNI {
		return errors.New("自动申请证书需要填写解析到该节点的域名")
	}
	sec.ACMEEmail = strings.TrimSpace(sec.ACMEEmail)
	if sec.ACMEEmail != "" && !acmeEmailPattern.MatchString(sec.ACMEEmail) {
		return errors.New("ACME 邮箱格式错误")
	}
	sec.ACMEDNS = strings.ToLower(strings.TrimSpace(sec.ACMEDNS))
	sec.ACMEDNSKey = strings.TrimSpace(sec.ACMEDNSKey)
	sec.ACMEDNSSecret = strings.TrimSpace(sec.ACMEDNSSecret)
	switch sec.ACMEDNS {
	case "":
		sec.ACMEDNSKey, sec.ACMEDNSSecret = "", ""
	case ACMEDNSCloudflare:
		sec.ACMEDNSSecret = ""
		if sec.ACMEDNSKey == "" {
			return errors.New("请填写 Cloudflare API Token")
		}
	case ACMEDNSAliDNS:
		if sec.ACMEDNSKey == "" || sec.ACMEDNSSecret == "" {
			return errors.New("请填写阿里云 AccessKey ID 和 Secret")
		}
	default:
		return fmt.Errorf("不支持的 DNS 服务商 %s", sec.ACMEDNS)
	}
	return nil
}

// Regenerate replaces every secret of an inbound while keeping its shape.
func Regenerate(kernelType string, in *Inbound) error {
	in.Settings.UUID = ""
	in.Settings.Password = ""
	if in.Settings.Decryption != "" {
		in.Settings.Decryption = "auto"
	}
	in.Settings.PrivateKey = ""
	in.Settings.PeerPrivateKey = ""
	in.Security.RealityPrivateKey = ""
	in.Security.RealityPublicKey = ""
	in.Security.RealityShortIDs = nil
	in.Security.CertPEM = ""
	in.Security.KeyPEM = ""
	in.Security.ShadowTLSPassword = ""
	return Normalize(kernelType, in)
}

const maxAllowEntries = 256

// ParseAllowList validates a comma / whitespace separated list of IPs and
// CIDRs and returns the normalized entries (bare IPs become /32 or /128).
func ParseAllowList(raw string) ([]string, error) {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '，' || r == ';' || r == ' ' || r == '\n' || r == '\t' || r == '\r'
	})
	if len(fields) > maxAllowEntries {
		return nil, fmt.Errorf("白名单最多 %d 条", maxAllowEntries)
	}
	seen := make(map[string]bool, len(fields))
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		cidr, err := NormalizeAllowEntry(f)
		if err != nil {
			return nil, err
		}
		if !seen[cidr] {
			seen[cidr] = true
			out = append(out, cidr)
		}
	}
	return out, nil
}

// NormalizeAllowEntry turns an IP or CIDR into canonical CIDR form.
func NormalizeAllowEntry(entry string) (string, error) {
	entry = strings.TrimSpace(entry)
	if ip := net.ParseIP(entry); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			return v4.String() + "/32", nil
		}
		return ip.String() + "/128", nil
	}
	_, ipnet, err := net.ParseCIDR(entry)
	if err != nil {
		return "", fmt.Errorf("白名单条目 %q 不是合法的 IP 或网段", entry)
	}
	return ipnet.String(), nil
}
