package kernel

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestInfoNotices(t *testing.T) {
	expire := time.Date(2026, 12, 31, 23, 59, 0, 0, time.Local).UnixMilli()
	got := InfoNotices(expire, 30<<30, 100<<30)
	if strings.Join(got, "|") != "⏳ 到期 2026-12-31|📊 剩余 70.00GB / 100.00GB" {
		t.Fatalf("limited: %q", got)
	}
	got = InfoNotices(0, 1536, 0)
	if strings.Join(got, "|") != "⏳ 长期有效|📊 已用 1.50KB" {
		t.Fatalf("unlimited: %q", got)
	}
	if got := InfoNotices(0, 200, 100); got[1] != "📊 剩余 0B / 100B" {
		t.Fatalf("over the limit: %q", got)
	}
}

// Notices come first in every format, keep their names and stay out of
// the region groups.
func TestSubscriptionNotices(t *testing.T) {
	opts := SubOptions{RegionGroups: true, Notices: InfoNotices(0, 0, 100<<30)}
	eps := namedEndpoints("香港 01")
	for _, format := range []string{FormatClash, FormatStash, FormatSingbox, FormatSurge, FormatShadowrocket, FormatLoon, FormatQX} {
		body, _, err := Subscription(format, eps, opts)
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		a, b := strings.Index(body, "⏳ 长期有效"), strings.Index(body, "香港 01")
		if a < 0 || b < 0 || a > b || !strings.Contains(body, "📊 剩余 100.00GB / 100.00GB") {
			t.Fatalf("%s: notices first:\n%s", format, body)
		}
		if strings.Contains(body, "英国") {
			t.Fatalf("%s: a notice formed a region group:\n%s", format, body)
		}
	}
	body, _, _ := Subscription(FormatBase64, nil, SubOptions{Notices: []string{"⚠️ 订阅已过期，请联系管理员"}})
	raw, _ := base64.StdEncoding.DecodeString(body)
	if lines := strings.Split(strings.TrimSpace(string(raw)), "\n"); len(lines) != 1 || !strings.HasPrefix(lines[0], "ss://") || !strings.Contains(lines[0], "@127.0.0.1:1") {
		t.Fatalf("base64 notice: %q", raw)
	}
}
