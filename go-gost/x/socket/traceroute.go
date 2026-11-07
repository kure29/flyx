package socket

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	tracerouteMaxHops     = 30
	tracerouteWait        = 3 * time.Second
	tracerouteRounds      = 2
	maxConcurrentTraces   = 4
	tracerouteICMPProtoV4 = 1
	tracerouteICMPProtoV6 = 58
)

// TracerouteRequest asks the node for the route to a host (ICMP; IPv4, or
// IPv6 when the node has no IPv4 route or the host no IPv4 address).
type TracerouteRequest struct {
	Host    string `json:"host"`
	MaxHops int    `json:"maxHops"`
	// Family 4 or 6 traces only over that IP version (dual-stack nodes
	// report both routes); 0 picks as above.
	Family int `json:"family,omitempty"`
}

// TracerouteHop is one TTL; IP is empty when no router answered.
type TracerouteHop struct {
	TTL   int     `json:"ttl"`
	IP    string  `json:"ip,omitempty"`
	RttMs float64 `json:"rttMs,omitempty"`
}

type TracerouteResponse struct {
	Host    string          `json:"host"`
	IP      string          `json:"ip"`
	Reached bool            `json:"reached"`
	Hops    []TracerouteHop `json:"hops"`
}

var traceSem = make(chan struct{}, maxConcurrentTraces)

func (w *WebSocketReporter) handleTraceroute(data interface{}) (TracerouteResponse, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return TracerouteResponse{}, fmt.Errorf("序列化路由追踪数据失败: %v", err)
	}
	var req TracerouteRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return TracerouteResponse{}, fmt.Errorf("解析路由追踪请求失败: %v", err)
	}
	host := strings.TrimSpace(req.Host)
	if net.ParseIP(host) == nil && !isValidHostname(host) {
		return TracerouteResponse{}, fmt.Errorf("无效的IP地址或主机名")
	}
	select {
	case traceSem <- struct{}{}:
		defer func() { <-traceSem }()
	default:
		return TracerouteResponse{}, fmt.Errorf("路由追踪任务过多，请稍后重试")
	}
	maxHops := req.MaxHops
	if maxHops <= 0 || maxHops > tracerouteMaxHops {
		maxHops = tracerouteMaxHops
	}
	if req.Family != 0 && req.Family != 4 && req.Family != 6 {
		return TracerouteResponse{}, fmt.Errorf("无效的 IP 版本: %d", req.Family)
	}
	return traceroute(host, maxHops, req.Family)
}

// traceAddr picks the address to trace: IPv4 when the node can route to
// it, else IPv6. routable reports whether the node has a route to an
// address; tests replace it.
func traceAddr(host string, addrs []net.IP, family int) (net.IP, error) {
	var v4, v6 net.IP
	for _, a := range addrs {
		if a.To4() != nil {
			if v4 == nil {
				v4 = a.To4()
			}
		} else if v6 == nil {
			v6 = a
		}
	}
	switch family {
	case 4:
		if v4 == nil {
			return nil, fmt.Errorf("目标没有 IPv4 地址")
		}
		if !routable(v4) {
			return nil, fmt.Errorf("节点没有 IPv4 路由")
		}
		return v4, nil
	case 6:
		if v6 == nil {
			return nil, fmt.Errorf("目标没有 IPv6 地址")
		}
		if !routable(v6) {
			return nil, fmt.Errorf("节点没有 IPv6 路由")
		}
		return v6, nil
	}
	switch {
	case v4 != nil && routable(v4):
		return v4, nil
	case v6 != nil && routable(v6):
		return v6, nil
	case v4 == nil && v6 == nil:
		return nil, fmt.Errorf("DNS解析失败: %s 没有地址", host)
	case v6 == nil:
		return nil, fmt.Errorf("节点没有 IPv4 路由，目标没有 IPv6 地址")
	case v4 == nil:
		return nil, fmt.Errorf("节点没有 IPv6 路由，目标没有 IPv4 地址")
	}
	return nil, fmt.Errorf("节点没有到目标的路由")
}

// routable connects a UDP socket, which sends nothing but fails without a
// route (no IPv6 / IPv4 on the node).
var routable = func(ip net.IP) bool {
	network := "udp4"
	if ip.To4() == nil {
		network = "udp6"
	}
	c, err := net.Dial(network, net.JoinHostPort(ip.String(), "33434"))
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// traceProto holds what differs between ICMPv4 and ICMPv6 probes.
type traceProto struct {
	network, listen string
	proto           int
	echo, reply     icmp.Type
	echoType        byte // echo request type in quoted probes
	setTTL          func(conn *icmp.PacketConn, ttl int) error
}

var (
	traceV4 = traceProto{
		network: "ip4:icmp", listen: "0.0.0.0", proto: tracerouteICMPProtoV4,
		echo: ipv4.ICMPTypeEcho, reply: ipv4.ICMPTypeEchoReply, echoType: byte(ipv4.ICMPTypeEcho),
		setTTL: func(c *icmp.PacketConn, ttl int) error { return c.IPv4PacketConn().SetTTL(ttl) },
	}
	traceV6 = traceProto{
		network: "ip6:ipv6-icmp", listen: "::", proto: tracerouteICMPProtoV6,
		echo: ipv6.ICMPTypeEchoRequest, reply: ipv6.ICMPTypeEchoReply, echoType: byte(ipv6.ICMPTypeEchoRequest),
		setTTL: func(c *icmp.PacketConn, ttl int) error { return c.IPv6PacketConn().SetHopLimit(ttl) },
	}
)

// traceroute sends ICMP echoes with TTL 1..maxHops at once (twice, for
// routers that drop one) and collects the Time Exceeded answers. It needs a
// raw socket: root or CAP_NET_RAW.
func traceroute(host string, maxHops, family int) (TracerouteResponse, error) {
	resp := TracerouteResponse{Host: host}
	addrs, err := net.LookupIP(host)
	if err != nil {
		return resp, fmt.Errorf("DNS解析失败: %v", err)
	}
	dst, err := traceAddr(host, addrs, family)
	if err != nil {
		return resp, err
	}
	resp.IP = dst.String()
	tp := traceV4
	if dst.To4() == nil {
		tp = traceV6
	}

	conn, err := icmp.ListenPacket(tp.network, tp.listen)
	if err != nil {
		return resp, fmt.Errorf("需要 root 或 NET_RAW 权限: %v", err)
	}
	defer conn.Close()

	id := rand.Intn(0xffff) + 1
	var mu sync.Mutex
	sent := make(map[int]time.Time, maxHops)
	hops := make(map[int]TracerouteHop, maxHops)
	reachedAt := 0

	send := func(ttl, round int) error {
		seq := round*tracerouteMaxHops + ttl
		msg := icmp.Message{
			Type: tp.echo,
			Body: &icmp.Echo{ID: id, Seq: seq, Data: []byte("flyx-trace")},
		}
		// The kernel fills in the ICMPv6 checksum.
		b, err := msg.Marshal(nil)
		if err != nil {
			return err
		}
		if err := tp.setTTL(conn, ttl); err != nil {
			return err
		}
		mu.Lock()
		sent[seq] = time.Now()
		mu.Unlock()
		_, err = conn.WriteTo(b, &net.IPAddr{IP: dst})
		return err
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 1500)
		deadline := time.Now().Add(time.Duration(tracerouteRounds) * tracerouteWait)
		_ = conn.SetReadDeadline(deadline)
		for {
			n, peer, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			seq, reached, ok := matchTraceReply(tp, buf[:n], id, dst)
			if !ok {
				continue
			}
			ttl := (seq-1)%tracerouteMaxHops + 1
			mu.Lock()
			if at, ok := sent[seq]; ok {
				if _, seen := hops[ttl]; !seen {
					hops[ttl] = TracerouteHop{
						TTL:   ttl,
						IP:    peerIP(peer),
						RttMs: float64(time.Since(at).Microseconds()) / 1000,
					}
				}
			}
			if reached && (reachedAt == 0 || ttl < reachedAt) {
				reachedAt = ttl
			}
			mu.Unlock()
		}
	}()

	for round := 0; round < tracerouteRounds; round++ {
		for ttl := 1; ttl <= maxHops; ttl++ {
			mu.Lock()
			_, seen := hops[ttl]
			stop := reachedAt != 0 && ttl > reachedAt
			mu.Unlock()
			if seen || stop {
				continue
			}
			if err := send(ttl, round); err != nil {
				return resp, fmt.Errorf("发送探测包失败: %v", err)
			}
			time.Sleep(2 * time.Millisecond)
		}
		// Wait for answers; stop early once every hop up to the target did.
		for end := time.Now().Add(tracerouteWait); time.Now().Before(end); {
			mu.Lock()
			complete := reachedAt != 0
			for ttl := 1; complete && ttl <= reachedAt; ttl++ {
				_, complete = hops[ttl]
			}
			mu.Unlock()
			if complete {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	_ = conn.SetReadDeadline(time.Now())
	<-done

	mu.Lock()
	defer mu.Unlock()
	last := maxHops
	if reachedAt != 0 {
		last = reachedAt
		resp.Reached = true
	} else {
		// Trim the silent tail after the last router that answered.
		for last > 0 {
			if _, ok := hops[last]; ok {
				break
			}
			last--
		}
	}
	resp.Hops = make([]TracerouteHop, 0, last)
	for ttl := 1; ttl <= last; ttl++ {
		if h, ok := hops[ttl]; ok {
			resp.Hops = append(resp.Hops, h)
		} else {
			resp.Hops = append(resp.Hops, TracerouteHop{TTL: ttl})
		}
	}
	return resp, nil
}

// matchTraceReply returns the probe sequence an ICMP message answers: an
// echo reply from the target, or a Time Exceeded / Unreachable quoting our
// echo to it.
func matchTraceReply(tp traceProto, b []byte, id int, dst net.IP) (seq int, reached bool, ok bool) {
	m, err := icmp.ParseMessage(tp.proto, b)
	if err != nil {
		return 0, false, false
	}
	switch body := m.Body.(type) {
	case *icmp.Echo:
		if m.Type == tp.reply && body.ID == id {
			return body.Seq, true, true
		}
	case *icmp.TimeExceeded:
		return quotedEcho(tp, body.Data, id, dst)
	case *icmp.DstUnreach:
		seq, _, ok := quotedEcho(tp, body.Data, id, dst)
		return seq, true, ok
	}
	return 0, false, false
}

// quotedEcho reads the original IP header and echo header an ICMP error
// carries.
func quotedEcho(tp traceProto, data []byte, id int, dst net.IP) (int, bool, bool) {
	var echo []byte
	if tp.proto == tracerouteICMPProtoV6 {
		// Fixed 40-byte IPv6 header; probes carry no extension headers.
		if len(data) < 48 || data[0]>>4 != 6 || data[6] != tracerouteICMPProtoV6 || !net.IP(data[24:40]).Equal(dst) {
			return 0, false, false
		}
		echo = data[40:]
	} else {
		if len(data) < 20 {
			return 0, false, false
		}
		ihl := int(data[0]&0x0f) * 4
		if ihl < 20 || len(data) < ihl+8 || !net.IP(data[16:20]).Equal(dst) {
			return 0, false, false
		}
		echo = data[ihl:]
	}
	if echo[0] != tp.echoType || int(binary.BigEndian.Uint16(echo[4:6])) != id {
		return 0, false, false
	}
	return int(binary.BigEndian.Uint16(echo[6:8])), false, true
}

func peerIP(a net.Addr) string {
	switch v := a.(type) {
	case *net.IPAddr:
		return v.IP.String()
	case *net.UDPAddr:
		return v.IP.String()
	}
	return a.String()
}
