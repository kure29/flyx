// Package kernel turns panel-side inbound definitions into proxy-kernel
// configuration (Xray, sing-box, Snell, ...). Everything here is pure so it can
// be covered by golden tests; the HTTP layer only persists and dispatches.
package kernel

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	TypeXray    = "xray"
	TypeSingbox = "singbox"
	TypeSnell   = "snell"
)

const (
	ProtocolVLESS       = "vless"
	ProtocolVMess       = "vmess"
	ProtocolTrojan      = "trojan"
	ProtocolShadowsocks = "shadowsocks"
	ProtocolHysteria2   = "hysteria2"
	ProtocolTUIC        = "tuic"
	ProtocolAnyTLS      = "anytls"
	ProtocolSnell       = "snell"
	ProtocolNaive       = "naive"
	ProtocolSOCKS       = "socks"
	ProtocolHTTP        = "http"
	ProtocolWireGuard   = "wireguard"
)

const (
	TransportTCP   = "tcp"
	TransportWS    = "ws"
	TransportGRPC  = "grpc"
	TransportXHTTP = "xhttp"
	TransportQUIC  = "quic"
	// TransportUDP is WireGuard's own UDP transport.
	TransportUDP = "udp"
)

const (
	SecurityNone      = "none"
	SecurityTLS       = "tls"
	SecurityReality   = "reality"
	SecurityShadowTLS = "shadowtls"
)

// TransportCap lists which security layers a transport accepts for a protocol.
type TransportCap struct {
	Type       string   `json:"type"`
	Securities []string `json:"securities"`
}

type ProtocolCap struct {
	Protocol   string         `json:"protocol"`
	Transports []TransportCap `json:"transports"`
}

// Download describes where an agent fetches the kernel binary. {VERSION} and
// {ARCH} are substituted by the agent; ArchMap translates GOARCH to the
// release asset naming of the kernel project.
type Download struct {
	URL         string            `json:"url"`
	LatestURL   string            `json:"latestUrl"`
	ChecksumURL string            `json:"checksumUrl"`
	LatestSum   string            `json:"latestChecksumUrl"`
	Archive     string            `json:"archive"`
	BinName     string            `json:"binName"`
	ArchMap     map[string]string `json:"archMap"`
	// Pinned SHA-256 per version and mapped arch, for projects that publish
	// no checksum files (Snell, shadow-tls). Only these versions install.
	Pinned map[string]map[string]string `json:"-"`
	// GitHub repo + asset template for digest lookup ("" when unused).
	GitHubRepo  string `json:"-"`
	GitHubAsset string `json:"-"`
	// Mirrorable: the GitHub download mirror (github_proxy_url) may be used.
	Mirrorable bool `json:"-"`
	// Legacy, when set, replaces this download for versions older than
	// LegacyBefore (release asset naming changed at that version).
	Legacy       *Download `json:"-"`
	LegacyBefore string    `json:"-"`
}

type Info struct {
	Type        string        `json:"type"`
	Name        string        `json:"name"`
	Implemented bool          `json:"implemented"`
	Protocols   []ProtocolCap `json:"protocols"`
	// Versions, when set, is the closed list of installable versions (the
	// first is the default); otherwise any version or "latest" is allowed.
	Versions []string `json:"versions,omitempty"`
	// ReleaseRepo is the GitHub repository whose releases list the
	// installable versions when Versions is empty.
	ReleaseRepo string `json:"-"`
	// CertModes lists the TLS certificate sources the kernel supports.
	CertModes []string  `json:"certModes"`
	Download  *Download `json:"-"`
	// Relay kernels run forwards, not inbounds: the panel installs them
	// itself for realm-engine tunnels.
	Relay bool `json:"relay,omitempty"`
}

var xrayDownload = &Download{
	URL:         "https://github.com/XTLS/Xray-core/releases/download/v{VERSION}/Xray-linux-{ARCH}.zip",
	LatestURL:   "https://github.com/XTLS/Xray-core/releases/latest/download/Xray-linux-{ARCH}.zip",
	ChecksumURL: "https://github.com/XTLS/Xray-core/releases/download/v{VERSION}/Xray-linux-{ARCH}.zip.dgst",
	LatestSum:   "https://github.com/XTLS/Xray-core/releases/latest/download/Xray-linux-{ARCH}.zip.dgst",
	Archive:     "zip",
	BinName:     "xray",
	ArchMap:     map[string]string{"amd64": "64", "arm64": "arm64-v8a", "386": "32", "arm": "arm32-v7a"},
	Mirrorable:  true,
}

// sing-box publishes no checksum files; the agent verifies against the
// GitHub release asset digest.
//
// Since 1.13 the unsuffixed Linux asset is a purego build: it is linked
// against glibc's dynamic loader, so on musl systems such as Alpine exec
// fails with "no such file or directory", and 386/armv7 have no unsuffixed
// asset at all. The -musl asset is statically linked and runs on any Linux.
// Older releases only have the unsuffixed asset, which was static then.
var singboxDownload = &Download{
	URL:          "https://github.com/SagerNet/sing-box/releases/download/v{VERSION}/sing-box-{VERSION}-linux-{ARCH}-musl.tar.gz",
	Archive:      "tar.gz",
	BinName:      "sing-box",
	ArchMap:      map[string]string{"amd64": "amd64", "arm64": "arm64", "arm": "armv7", "386": "386"},
	GitHubRepo:   "SagerNet/sing-box",
	GitHubAsset:  "sing-box-{VERSION}-linux-{ARCH}-musl.tar.gz",
	Mirrorable:   true,
	LegacyBefore: "1.13.0",
	Legacy: &Download{
		URL:         "https://github.com/SagerNet/sing-box/releases/download/v{VERSION}/sing-box-{VERSION}-linux-{ARCH}.tar.gz",
		Archive:     "tar.gz",
		BinName:     "sing-box",
		ArchMap:     map[string]string{"amd64": "amd64", "arm64": "arm64", "arm": "armv7", "386": "386"},
		GitHubRepo:  "SagerNet/sing-box",
		GitHubAsset: "sing-box-{VERSION}-linux-{ARCH}.tar.gz",
		Mirrorable:  true,
	},
}

// Snell is closed source and only distributed by Surge; the official site has
// no checksum files, so installable versions are pinned. Hashes were checked
// against the official downloads for every arch.
var snellDownload = &Download{
	URL:     "https://dl.nssurge.com/snell/snell-server-v{VERSION}-linux-{ARCH}.zip",
	Archive: "zip",
	BinName: "snell-server",
	ArchMap: map[string]string{"amd64": "amd64", "arm64": "aarch64", "arm": "armv7l"},
	Pinned: map[string]map[string]string{
		// 6.0.0 is a beta; Surge publishes no armv7 build of it.
		"6.0.0": {
			"amd64":   "8a9c4463ca87cfa5eaa37c6af0d37ab93ea275aa12391985bb2a375ca3abd7f2",
			"aarch64": "a0b2915cbc77dc3baf8fa069e741c20808d8a10c3a8a93e709a0a580645c3bd7",
		},
		"5.0.1": {
			"amd64":   "9bea1c2b9e35b73b31634856c04d18c393072b9e5dcde6a32781d8b8f908c539",
			"aarch64": "2f178bf5ac468ce1a130454efa40a0603fbbe4e47ecc4880a989f4abc7f824cf",
			"armv7l":  "14489f3e857569c8835dd3598b7ea6bca5371d4290ac7cf0f6c8dfb3381c1fb2",
		},
		"4.1.1": {
			"amd64":   "cc2271b79c7506888b34e651e8741b3aa7fc7d5f60aa65ef8bb096f3313a193b",
			"aarch64": "38d4cdc03dcdb3608af8594df83e1795265167fafc5d802f815148908902d758",
			"armv7l":  "d00b98ed803be4039f0f0630b810932cd3d3d87ee3e6ed224106fdc63347d8e6",
		},
	},
}

// ShadowTLSVersion is the ihciah/shadow-tls release used to front Snell.
const ShadowTLSVersion = "0.2.25"

var shadowTLSDownload = &Download{
	URL:     "https://github.com/ihciah/shadow-tls/releases/download/v{VERSION}/shadow-tls-{ARCH}",
	Archive: "raw",
	BinName: "shadow-tls",
	ArchMap: map[string]string{"amd64": "x86_64-unknown-linux-musl", "arm64": "aarch64-unknown-linux-musl", "arm": "armv7-unknown-linux-musleabihf"},
	Pinned: map[string]map[string]string{
		ShadowTLSVersion: {
			"x86_64-unknown-linux-musl":      "a173f5f2d57f45211b68e10ceeddc15b1791077b914fa89747bc705fddc71532",
			"aarch64-unknown-linux-musl":     "3295476b37f549a68906519d3eaecb74bf3b6eaf9094cebb16ee84f0151373c6",
			"armv7-unknown-linux-musleabihf": "e6f918a072557c50fd0ea950af9a156a9b102af72c1d010ff85d08d13006c54f",
		},
	},
	Mirrorable: true,
}

var catalog = []Info{
	{
		Type:        TypeXray,
		Name:        "Xray",
		Implemented: true,
		ReleaseRepo: "XTLS/Xray-core",
		CertModes:   []string{"self", "file"},
		Download:    xrayDownload,
		Protocols: []ProtocolCap{
			{Protocol: ProtocolVLESS, Transports: []TransportCap{
				{Type: TransportTCP, Securities: []string{SecurityReality, SecurityTLS, SecurityNone}},
				{Type: TransportXHTTP, Securities: []string{SecurityReality, SecurityTLS, SecurityNone}},
				{Type: TransportGRPC, Securities: []string{SecurityReality, SecurityTLS, SecurityNone}},
				{Type: TransportWS, Securities: []string{SecurityTLS, SecurityNone}},
			}},
			{Protocol: ProtocolVMess, Transports: []TransportCap{
				{Type: TransportTCP, Securities: []string{SecurityTLS, SecurityNone}},
				{Type: TransportWS, Securities: []string{SecurityTLS, SecurityNone}},
				{Type: TransportGRPC, Securities: []string{SecurityTLS, SecurityNone}},
			}},
			{Protocol: ProtocolTrojan, Transports: []TransportCap{
				{Type: TransportTCP, Securities: []string{SecurityTLS, SecurityReality}},
				{Type: TransportWS, Securities: []string{SecurityTLS}},
				{Type: TransportGRPC, Securities: []string{SecurityTLS, SecurityReality}},
			}},
			{Protocol: ProtocolShadowsocks, Transports: []TransportCap{
				{Type: TransportTCP, Securities: []string{SecurityNone}},
			}},
			{Protocol: ProtocolSOCKS, Transports: []TransportCap{{Type: TransportTCP, Securities: []string{SecurityNone}}}},
			{Protocol: ProtocolHTTP, Transports: []TransportCap{{Type: TransportTCP, Securities: []string{SecurityNone}}}},
			{Protocol: ProtocolWireGuard, Transports: []TransportCap{{Type: TransportUDP, Securities: []string{SecurityNone}}}},
		},
	},
	{
		Type:        TypeSingbox,
		Name:        "sing-box",
		Implemented: true,
		ReleaseRepo: "SagerNet/sing-box",
		CertModes:   []string{"self", "file", "acme"},
		Download:    singboxDownload,
		Protocols: []ProtocolCap{
			{Protocol: ProtocolHysteria2, Transports: []TransportCap{{Type: TransportQUIC, Securities: []string{SecurityTLS}}}},
			{Protocol: ProtocolTUIC, Transports: []TransportCap{{Type: TransportQUIC, Securities: []string{SecurityTLS}}}},
			{Protocol: ProtocolAnyTLS, Transports: []TransportCap{{Type: TransportTCP, Securities: []string{SecurityTLS}}}},
			{Protocol: ProtocolNaive, Transports: []TransportCap{{Type: TransportTCP, Securities: []string{SecurityTLS}}}},
			{Protocol: ProtocolVLESS, Transports: []TransportCap{
				{Type: TransportTCP, Securities: []string{SecurityReality, SecurityTLS, SecurityNone}},
				{Type: TransportWS, Securities: []string{SecurityTLS, SecurityNone}},
				{Type: TransportGRPC, Securities: []string{SecurityTLS, SecurityNone}},
			}},
			{Protocol: ProtocolVMess, Transports: []TransportCap{
				{Type: TransportTCP, Securities: []string{SecurityTLS, SecurityNone}},
				{Type: TransportWS, Securities: []string{SecurityTLS, SecurityNone}},
				{Type: TransportGRPC, Securities: []string{SecurityTLS, SecurityNone}},
			}},
			{Protocol: ProtocolTrojan, Transports: []TransportCap{
				{Type: TransportTCP, Securities: []string{SecurityTLS, SecurityReality}},
				{Type: TransportWS, Securities: []string{SecurityTLS}},
				{Type: TransportGRPC, Securities: []string{SecurityTLS}},
			}},
			{Protocol: ProtocolShadowsocks, Transports: []TransportCap{
				{Type: TransportTCP, Securities: []string{SecurityNone, SecurityShadowTLS}},
			}},
			// Built-in Snell server, sing-box 1.14+.
			{Protocol: ProtocolSnell, Transports: []TransportCap{{Type: TransportTCP, Securities: []string{SecurityNone, SecurityShadowTLS}}}},
			{Protocol: ProtocolSOCKS, Transports: []TransportCap{{Type: TransportTCP, Securities: []string{SecurityNone}}}},
			{Protocol: ProtocolHTTP, Transports: []TransportCap{{Type: TransportTCP, Securities: []string{SecurityNone}}}},
			{Protocol: ProtocolWireGuard, Transports: []TransportCap{{Type: TransportUDP, Securities: []string{SecurityNone}}}},
		},
	},
	{
		Type:        TypeSnell,
		Name:        "Snell",
		Implemented: true,
		CertModes:   []string{},
		Download:    snellDownload,
		// 5.0.1 stays the default: the 6.0.0 beta only serves v6 clients.
		Versions: []string{"5.0.1", "6.0.0", "4.1.1"},
		Protocols: []ProtocolCap{
			{Protocol: ProtocolSnell, Transports: []TransportCap{{Type: TransportTCP, Securities: []string{SecurityNone, SecurityShadowTLS}}}},
		},
	},
	{
		Type:        TypeRealm,
		Name:        "realm",
		Implemented: true,
		CertModes:   []string{},
		Download:    realmDownload,
		Versions:    []string{RealmVersion},
		Protocols:   []ProtocolCap{},
		Relay:       true,
	},
}

// ShadowTLSDownload is the helper binary used to front Snell with ShadowTLS.
func ShadowTLSDownload() *Download { return shadowTLSDownload }

// Catalog returns every known kernel; unimplemented ones are listed so the UI
// can show them as upcoming.
func Catalog() []Info {
	out := make([]Info, len(catalog))
	copy(out, catalog)
	return out
}

func Lookup(kernelType string) (Info, bool) {
	kernelType = strings.ToLower(strings.TrimSpace(kernelType))
	for _, info := range catalog {
		if info.Type == kernelType {
			return info, true
		}
	}
	return Info{}, false
}

func (info Info) protocol(name string) (ProtocolCap, bool) {
	for _, p := range info.Protocols {
		if p.Protocol == name {
			return p, true
		}
	}
	return ProtocolCap{}, false
}

func (p ProtocolCap) transport(name string) (TransportCap, bool) {
	for _, t := range p.Transports {
		if t.Type == name {
			return t, true
		}
	}
	return TransportCap{}, false
}

func containsString(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

// ResolveVersion maps a requested version to the one that will be installed:
// kernels with a closed version list use their default for "latest" and
// reject anything else they cannot verify.
func ResolveVersion(info Info, version string) (string, error) {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if len(info.Versions) == 0 {
		if version == "" {
			return "latest", nil
		}
		return version, nil
	}
	if version == "" || strings.EqualFold(version, "latest") {
		return info.Versions[0], nil
	}
	for _, v := range info.Versions {
		if v == version {
			return version, nil
		}
	}
	return "", fmt.Errorf("%s 仅支持版本 %s", info.Name, strings.Join(info.Versions, " / "))
}

// ResolveDownload builds the agent download spec for an installable version.
// proxy, when non-empty, prefixes GitHub URLs the same way agent upgrades are
// mirrored.
func ResolveDownload(info Info, version, proxy string) (map[string]interface{}, error) {
	if info.Download == nil {
		return nil, fmt.Errorf("%s 没有下载信息", info.Name)
	}
	version, err := ResolveVersion(info, version)
	if err != nil {
		return nil, err
	}
	return downloadSpec(info.Download, version, proxy)
}

// ExtraDownload resolves a helper binary such as shadow-tls.
func ExtraDownload(d *Download, version, proxy string) (map[string]interface{}, error) {
	return downloadSpec(d, version, proxy)
}

func downloadSpec(d *Download, version, proxy string) (map[string]interface{}, error) {
	if d.Legacy != nil && !strings.EqualFold(version, "latest") && version != "" && versionBefore(version, d.LegacyBefore) {
		d = d.Legacy
	}
	proxy = strings.TrimSuffix(strings.TrimSpace(proxy), "/")
	if !d.Mirrorable {
		proxy = ""
	}
	mirror := func(u string) string {
		if proxy == "" || u == "" {
			return u
		}
		return proxy + "/" + u
	}
	archMap := make(map[string]string, len(d.ArchMap))
	for k, v := range d.ArchMap {
		archMap[k] = v
	}
	spec := map[string]interface{}{
		"archive": d.Archive,
		"binName": d.BinName,
		"archMap": archMap,
	}
	latest := version == "" || strings.EqualFold(version, "latest")

	switch {
	case d.Pinned != nil:
		sums, ok := d.Pinned[version]
		if !ok {
			return nil, fmt.Errorf("版本 %s 没有内置校验值，无法安装", version)
		}
		pinned := make(map[string]string, len(sums))
		for k, v := range sums {
			pinned[k] = v
		}
		// Arches without a build of this version are left out, so the
		// agent reports an unsupported arch instead of a missing checksum.
		for goarch, arch := range archMap {
			if _, ok := sums[arch]; !ok {
				delete(archMap, goarch)
			}
		}
		spec["url"] = mirror(strings.ReplaceAll(d.URL, "{VERSION}", version))
		spec["sha256"] = pinned
	case d.GitHubRepo != "":
		gh := map[string]interface{}{"repo": d.GitHubRepo, "asset": d.GitHubAsset, "proxy": proxy}
		if latest {
			spec["url"] = d.URL // {VERSION} left for the agent to resolve
		} else {
			spec["url"] = mirror(strings.ReplaceAll(d.URL, "{VERSION}", version))
			gh["tag"] = "v" + version
		}
		spec["github"] = gh
	default:
		url, sum := d.LatestURL, d.LatestSum
		if !latest {
			url = strings.ReplaceAll(d.URL, "{VERSION}", version)
			sum = strings.ReplaceAll(d.ChecksumURL, "{VERSION}", version)
		}
		spec["url"] = mirror(url)
		spec["checksumUrl"] = mirror(sum)
	}
	return spec, nil
}

// versionBefore reports whether a dotted release version ("1.12.8",
// "1.13.0-beta.3") is older than ref, comparing the numeric parts only; a
// pre-release of ref counts as ref.
func versionBefore(version, ref string) bool {
	parse := func(v string) []int {
		v = strings.TrimPrefix(strings.TrimSpace(v), "v")
		if i := strings.IndexAny(v, "-+"); i >= 0 {
			v = v[:i]
		}
		var out []int
		for _, part := range strings.Split(v, ".") {
			n, err := strconv.Atoi(part)
			if err != nil {
				break
			}
			out = append(out, n)
		}
		return out
	}
	a, b := parse(version), parse(ref)
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return x < y
		}
	}
	return false
}
