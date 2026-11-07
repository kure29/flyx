package socket

import (
	"encoding/binary"
	"net"
	"strings"
	"testing"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// timeExceeded builds the ICMP error a router sends for our echo to dst.
func timeExceeded(t *testing.T, id, seq int, dst net.IP) []byte {
	t.Helper()
	quoted := make([]byte, 28)
	quoted[0] = 0x45 // IPv4, 20-byte header
	copy(quoted[16:20], dst.To4())
	quoted[20] = byte(ipv4.ICMPTypeEcho)
	binary.BigEndian.PutUint16(quoted[24:26], uint16(id))
	binary.BigEndian.PutUint16(quoted[26:28], uint16(seq))
	b, err := (&icmp.Message{Type: ipv4.ICMPTypeTimeExceeded, Body: &icmp.TimeExceeded{Data: quoted}}).Marshal(nil)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestMatchTraceReply(t *testing.T) {
	dst := net.ParseIP("203.0.113.9")
	if seq, reached, ok := matchTraceReply(traceV4, timeExceeded(t, 42, 7, dst), 42, dst); !ok || reached || seq != 7 {
		t.Fatalf("time exceeded: seq=%d reached=%v ok=%v", seq, reached, ok)
	}
	if _, _, ok := matchTraceReply(traceV4, timeExceeded(t, 43, 7, dst), 42, dst); ok {
		t.Fatal("another trace's probe must not match")
	}
	if _, _, ok := matchTraceReply(traceV4, timeExceeded(t, 42, 7, net.ParseIP("203.0.113.10")), 42, dst); ok {
		t.Fatal("a probe to another target must not match")
	}
	reply, _ := (&icmp.Message{Type: ipv4.ICMPTypeEchoReply, Body: &icmp.Echo{ID: 42, Seq: 3}}).Marshal(nil)
	if seq, reached, ok := matchTraceReply(traceV4, reply, 42, dst); !ok || !reached || seq != 3 {
		t.Fatalf("echo reply: seq=%d reached=%v ok=%v", seq, reached, ok)
	}
}

func TestTracerouteLoopback(t *testing.T) {
	resp, err := traceroute("127.0.0.1", 5, 0)
	if err != nil {
		if strings.Contains(err.Error(), "NET_RAW") {
			t.Skip("no raw socket permission")
		}
		t.Fatal(err)
	}
	if !resp.Reached || len(resp.Hops) != 1 || resp.Hops[0].IP != "127.0.0.1" {
		t.Fatalf("loopback trace = %+v", resp)
	}
}

// timeExceeded6 builds the ICMPv6 error a router sends for our echo to dst.
func timeExceeded6(t *testing.T, id, seq int, dst net.IP) []byte {
	t.Helper()
	quoted := make([]byte, 48)
	quoted[0] = 0x60 // IPv6
	quoted[6] = 58   // next header: ICMPv6
	copy(quoted[24:40], dst.To16())
	quoted[40] = byte(ipv6.ICMPTypeEchoRequest)
	binary.BigEndian.PutUint16(quoted[44:46], uint16(id))
	binary.BigEndian.PutUint16(quoted[46:48], uint16(seq))
	b, err := (&icmp.Message{Type: ipv6.ICMPTypeTimeExceeded, Body: &icmp.TimeExceeded{Data: quoted}}).Marshal(nil)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestMatchTraceReplyV6(t *testing.T) {
	dst := net.ParseIP("2001:db8::9")
	if seq, reached, ok := matchTraceReply(traceV6, timeExceeded6(t, 42, 7, dst), 42, dst); !ok || reached || seq != 7 {
		t.Fatalf("time exceeded: seq=%d reached=%v ok=%v", seq, reached, ok)
	}
	if _, _, ok := matchTraceReply(traceV6, timeExceeded6(t, 42, 7, net.ParseIP("2001:db8::10")), 42, dst); ok {
		t.Fatal("a probe to another target must not match")
	}
	if _, _, ok := matchTraceReply(traceV6, timeExceeded6(t, 43, 7, dst), 42, dst); ok {
		t.Fatal("another trace's probe must not match")
	}
	reply, _ := (&icmp.Message{Type: ipv6.ICMPTypeEchoReply, Body: &icmp.Echo{ID: 42, Seq: 3}}).Marshal(nil)
	if seq, reached, ok := matchTraceReply(traceV6, reply, 42, dst); !ok || !reached || seq != 3 {
		t.Fatalf("echo reply: seq=%d reached=%v ok=%v", seq, reached, ok)
	}
}

func TestTraceAddr(t *testing.T) {
	orig := routable
	defer func() { routable = orig }()
	v4, v6 := net.ParseIP("203.0.113.9"), net.ParseIP("2001:db8::9")
	both := []net.IP{v6, v4}

	routable = func(net.IP) bool { return true }
	if ip, err := traceAddr("h", both, 0); err != nil || !ip.Equal(v4) {
		t.Fatalf("dual stack should trace IPv4: %v %v", ip, err)
	}
	// IPv6-only node.
	routable = func(ip net.IP) bool { return ip.To4() == nil }
	if ip, err := traceAddr("h", both, 0); err != nil || !ip.Equal(v6) {
		t.Fatalf("IPv6-only node should trace IPv6: %v %v", ip, err)
	}
	if _, err := traceAddr("h", []net.IP{v4}, 0); err == nil || !strings.Contains(err.Error(), "没有 IPv6 地址") {
		t.Fatalf("IPv4-only target from IPv6-only node: %v", err)
	}
	// IPv4-only node, IPv6-only target.
	routable = func(ip net.IP) bool { return ip.To4() != nil }
	if _, err := traceAddr("h", []net.IP{v6}, 0); err == nil || !strings.Contains(err.Error(), "没有 IPv4 地址") {
		t.Fatalf("IPv6-only target from IPv4-only node: %v", err)
	}
}

func TestTracerouteLoopbackV6(t *testing.T) {
	resp, err := traceroute("::1", 5, 0)
	if err != nil {
		if strings.Contains(err.Error(), "NET_RAW") || strings.Contains(err.Error(), "路由") {
			t.Skipf("no IPv6 raw socket here: %v", err)
		}
		t.Fatal(err)
	}
	if !resp.Reached || len(resp.Hops) != 1 || resp.Hops[0].IP != "::1" {
		t.Fatalf("loopback trace = %+v", resp)
	}
}

func TestTraceAddrFamily(t *testing.T) {
	orig := routable
	defer func() { routable = orig }()
	v4, v6 := net.ParseIP("203.0.113.9"), net.ParseIP("2001:db8::9")
	routable = func(net.IP) bool { return true }
	if ip, err := traceAddr("h", []net.IP{v4, v6}, 6); err != nil || !ip.Equal(v6) {
		t.Fatalf("family 6 must trace IPv6 on a dual-stack node: %v %v", ip, err)
	}
	if ip, err := traceAddr("h", []net.IP{v6, v4}, 4); err != nil || !ip.Equal(v4) {
		t.Fatalf("family 4 must trace IPv4: %v %v", ip, err)
	}
	if _, err := traceAddr("h", []net.IP{v4}, 6); err == nil || !strings.Contains(err.Error(), "没有 IPv6 地址") {
		t.Fatalf("IPv4-only target over IPv6: %v", err)
	}
	routable = func(ip net.IP) bool { return ip.To4() != nil }
	if _, err := traceAddr("h", []net.IP{v4, v6}, 6); err == nil || !strings.Contains(err.Error(), "没有 IPv6 路由") {
		t.Fatalf("IPv4-only node over IPv6: %v", err)
	}
}
