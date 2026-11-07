package kernel

import (
	"fmt"
	"time"
)

// Notices are placeholder nodes whose names tell the user something the
// client would not show otherwise: the plan's expiry and traffic, or why a
// subscription no longer works. They point at 127.0.0.1:1, so selecting
// one connects nowhere.

// noticeEndpoints turns notice texts into placeholder Shadowsocks nodes.
// They are built directly: importing refuses a loopback address.
func noticeEndpoints(names []string) []Endpoint {
	out := make([]Endpoint, 0, len(names))
	for _, name := range names {
		p := Proxy{"name": name, "type": "ss", "server": "127.0.0.1", "port": 1, "cipher": "aes-128-gcm", "password": "notice"}
		out = append(out, Endpoint{Name: name, Original: name, Host: "127.0.0.1", Port: 1, Proxy: p, Direct: true})
	}
	return out
}

// withNotices puts the notices in front of the processed endpoints.
func withNotices(endpoints []Endpoint, opts SubOptions) []Endpoint {
	if len(opts.Notices) == 0 {
		return endpoints
	}
	return append(noticeEndpoints(opts.Notices), endpoints...)
}

// InfoNotices are a subscription's info nodes: expiry (unix ms, 0 = none)
// and traffic (bytes; total 0 = unlimited, which shows what was used).
func InfoNotices(expireMs, used, total int64) []string {
	expire := "⏳ 长期有效"
	if expireMs > 0 {
		expire = "⏳ 到期 " + time.UnixMilli(expireMs).Format("2006-01-02")
	}
	traffic := "📊 已用 " + noticeBytes(used)
	if total > 0 {
		left := total - used
		if left < 0 {
			left = 0
		}
		traffic = "📊 剩余 " + noticeBytes(left) + " / " + noticeBytes(total)
	}
	return []string{expire, traffic}
}

// noticeBytes formats a size without a space before the unit: a bare "GB"
// word would put the node in the UK region group (see RegionOf).
func noticeBytes(n int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	v, i := float64(n), 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%dB", n)
	}
	return fmt.Sprintf("%.2f%s", v, units[i])
}
