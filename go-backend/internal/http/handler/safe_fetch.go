package handler

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"
)

// fetchAllowLocalConfigKey lets subscription, template and icon-library
// downloads reach loopback and private addresses (a sub-store on the same
// host or LAN). Off by default: the panel would otherwise fetch internal
// services and show their response back.
const fetchAllowLocalConfigKey = "fetch_allow_local"

func (h *Handler) fetchAllowLocal() bool {
	cfg, err := h.repo.GetConfigByName(fetchAllowLocalConfigKey)
	return err == nil && cfg != nil && strings.TrimSpace(strings.ToLower(cfg.Value)) == "true"
}

// errFetchLocal names the setting that lifts the block.
var errFetchLocal = errors.New("是内网地址，如需拉取，请在「设置 → 安全与登录」打开「拉取订阅可访问内网地址」")

// blockedFetch is a refused address; fetchErr unwraps it from Go's
// request and dial errors.
type blockedFetch struct{ error }

// fetchBlockedIP reports whether a download may not dial ip. Link-local
// (cloud metadata), multicast and unspecified addresses are never fetched;
// loopback and private ones only when allowLocal.
func fetchBlockedIP(ip net.IP, allowLocal bool) error {
	switch {
	case ip.IsUnspecified(), ip.IsMulticast(), ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast():
		return blockedFetch{fmt.Errorf("%s 不能拉取", ip)}
	case allowLocal:
		return nil
	case ip.IsLoopback(), ip.IsPrivate(), cgnat.Contains(ip):
		return blockedFetch{fmt.Errorf("%s %w", ip, errFetchLocal)}
	}
	return nil
}

var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// fetchClient checks every address it dials, after DNS and on each
// redirect, so neither a hostname nor a redirect can point it inward.
// Behind an HTTP(S)_PROXY from the environment it dials only the proxy;
// the target host is then checked by resolving it before each request.
func fetchClient(allowLocal bool, timeout time.Duration) *http.Client {
	check := func(host string) error {
		ip := net.ParseIP(host)
		if ip == nil {
			return fmt.Errorf("无法识别地址 %s", host)
		}
		return fetchBlockedIP(ip, allowLocal)
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = func(req *http.Request) (*url.URL, error) {
		proxy, err := http.ProxyFromEnvironment(req)
		if proxy == nil || err != nil {
			return proxy, err
		}
		ips, err := net.DefaultResolver.LookupIP(req.Context(), "ip", req.URL.Hostname())
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if err := check(ip.String()); err != nil {
				return nil, err
			}
		}
		return proxy, nil
	}
	proxies := envProxyAddrs()
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if proxies[address] {
			return dialer.DialContext(ctx, network, address)
		}
		d := *dialer
		d.Control = func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			return check(host)
		}
		return d.DialContext(ctx, network, address)
	}
	return &http.Client{Timeout: timeout, Transport: transport}
}

// envProxyAddrs are the host:port of the HTTP(S)_PROXY variables: dials
// to them are the proxy, not the target.
func envProxyAddrs() map[string]bool {
	out := map[string]bool{}
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		v := strings.TrimSpace(os.Getenv(k))
		if v == "" {
			continue
		}
		if !strings.Contains(v, "://") {
			v = "http://" + v
		}
		u, err := url.Parse(v)
		if err != nil || u.Hostname() == "" {
			continue
		}
		port := u.Port()
		if port == "" {
			port = map[string]string{"https": "443", "socks5": "1080"}[u.Scheme]
			if port == "" {
				port = "80"
			}
		}
		out[net.JoinHostPort(u.Hostname(), port)] = true
	}
	return out
}

// fetchErr is a refused address's reason without Go's request wrapping
// ("Get \"url\": dial tcp ...: "), or err as is.
func fetchErr(err error) error {
	var b blockedFetch
	if errors.As(err, &b) {
		return b.error
	}
	return err
}
