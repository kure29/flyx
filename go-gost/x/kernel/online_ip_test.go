package kernel

import (
	"testing"
	"time"
)

func TestOnlineIPTrackerXray(t *testing.T) {
	tr := newOnlineIPTracker()
	now := time.Unix(1700000000, 0)

	// Routine accepted lines with email (both u<id>@flyx and u<id> for SOCKS/HTTP).
	if !tr.ObserveLine("xray", "2026/01/02 15:04:05.123456 from 203.0.113.10:54321 accepted tcp:example.com:443 [in-1 -> user-5] email: u5@flyx", now) {
		t.Fatal("expected xray accepted line to be suppressed from logRing")
	}
	if !tr.ObserveLine("xray", "2026/01/02 15:04:06 from tcp:[2001:db8:1:2::11]:44321 accepted tcp:example.com:443 [in-1 -> user-5] email: u5@flyx", now.Add(time.Second)) {
		t.Fatal("expected xray IPv6 accepted line to be suppressed")
	}
	// Same /64 IPv6 privacy address rotates suffix: must deduplicate to 1 entry!
	tr.ObserveLine("xray", "2026/01/02 15:04:07 from udp:[2001:db8:1:2::99]:55555 accepted udp:1.1.1.1:53 [in-1 -> user-5] email: u5@flyx", now.Add(2*time.Second))
	// SOCKS/HTTP email without @flyx
	tr.ObserveLine("xray", "2026/01/02 15:04:08 from 198.51.100.7:10800 accepted tcp:github.com:443 [in-2 -> user-9] email: u9", now.Add(3*time.Second))
	// Rejected scan or warning must NOT be tracked and NOT suppressed from logRing.
	if tr.ObserveLine("xray", "2026/01/02 15:04:09 from 192.0.2.1:9999 rejected proxy/vless: invalid user", now.Add(4*time.Second)) {
		t.Fatal("rejected line should stay in logRing")
	}
	if tr.ObserveLine("xray", "2026/01/02 15:04:10 [Warning] core: Xray 25.1.1 started", now.Add(5*time.Second)) {
		t.Fatal("startup warning should stay in logRing")
	}
	// Loopback must be ignored.
	tr.ObserveLine("xray", "2026/01/02 15:04:11 from 127.0.0.1:40000 accepted tcp:example.com:80 [in-1 -> user-5] email: u5@flyx", now.Add(6*time.Second))

	snap := tr.Snapshot(now.Add(10 * time.Second))
	if len(snap) != 3 {
		t.Fatalf("expected 3 deduplicated (user,ip) entries, got %+v", snap)
	}
	if snap[0].ID != 5 || snap[0].IP != "2001:db8:1:2::99" {
		t.Fatalf("unexpected entry 0: %+v", snap[0])
	}
	if snap[1].ID != 5 || snap[1].IP != "203.0.113.10" {
		t.Fatalf("unexpected entry 1: %+v", snap[1])
	}
	if snap[2].ID != 9 || snap[2].IP != "198.51.100.7" {
		t.Fatalf("unexpected entry 2: %+v", snap[2])
	}

	// Sliding window expiry.
	expired := tr.Snapshot(now.Add(onlineIPWindow + 10*time.Second))
	if len(expired) != 0 {
		t.Fatalf("expected entries to expire after window, got %+v", expired)
	}
}

func TestOnlineIPTrackerSingbox(t *testing.T) {
	tr := newOnlineIPTracker()
	now := time.Unix(1700000000, 0)

	// Two-line flow (standard sing-box VLESS/VMess/Trojan with ANSI color codes):
	line1 := "\x1b[36mINFO\x1b[0m[0001] [987654321 0ms] inbound/vless[in-1]: inbound connection from 203.0.113.20:51234"
	line2 := "\x1b[36mINFO\x1b[0m[0001] [987654321 2ms] inbound/vless[in-1]: [u7] inbound connection to example.com:443"
	if !tr.ObserveLine("singbox", line1, now) {
		t.Fatal("expected sing-box inbound connection from line to be suppressed")
	}
	if !tr.ObserveLine("singbox", line2, now.Add(time.Millisecond)) {
		t.Fatal("expected sing-box inbound connection to line to be suppressed")
	}

	// Single-line flow (e.g. Hysteria2 / TUIC packet connection with user already authenticated):
	line3 := "+0800 2026-01-02 15:04:05 INFO [111222333 0ms] inbound/hysteria2[in-2]: [u7] inbound packet connection from [2001:db8:abcd:1::42]:60000"
	if !tr.ObserveLine("singbox", line3, now.Add(time.Second)) {
		t.Fatal("expected sing-box single-line packet connection to be suppressed")
	}

	// Error / startup lines must NOT be suppressed.
	if tr.ObserveLine("singbox", "+0800 2026-01-02 15:04:05 INFO sing-box started (0.04s)", now.Add(2*time.Second)) {
		t.Fatal("sing-box startup line must stay in logRing")
	}
	if tr.ObserveLine("singbox", "+0800 2026-01-02 15:04:05 ERROR [987654321 5ms] inbound/vless[in-1]: process connection: EOF", now.Add(3*time.Second)) {
		t.Fatal("sing-box ERROR line must stay in logRing")
	}

	snap := tr.Snapshot(now.Add(5 * time.Second))
	if len(snap) != 2 {
		t.Fatalf("expected 2 entries for u7, got %+v", snap)
	}
	if snap[0].ID != 7 || snap[0].IP != "2001:db8:abcd:1::42" || snap[1].IP != "203.0.113.20" {
		t.Fatalf("unexpected sing-box snapshot: %+v", snap)
	}
}
