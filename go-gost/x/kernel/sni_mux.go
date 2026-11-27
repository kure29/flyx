package kernel

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	maxMuxPeekBytes    = 32 * 1024
	muxPeekTimeout     = 5 * time.Second
	muxDialTimeout     = 5 * time.Second
	maxMuxAllowEntries = 512
)

// MuxRoute maps a TLS SNI (exact or "*.example.com") to a backend TCP target.
type MuxRoute struct {
	SNI           string `json:"sni"`
	Target        string `json:"target"`
	ProxyProtocol bool   `json:"proxyProtocol,omitempty"`
	// Allow, when non-nil, restricts this route to connections from these
	// source IPs / CIDRs (checked on the real client IP before dialing Target).
	// Nil means no restriction; an empty non-nil slice denies all traffic.
	Allow []string `json:"allow"`
}

// MuxConfig is the KernelMuxApply command payload.
type MuxConfig struct {
	Enabled        bool       `json:"enabled"`
	Listen         string     `json:"listen"`
	Port           int        `json:"port"`
	Routes         []MuxRoute `json:"routes"`
	FallbackTarget string     `json:"fallbackTarget,omitempty"`
	FallbackProxy  bool       `json:"fallbackProxy,omitempty"`
	Hash           string     `json:"hash"`
}

// MuxState is returned by KernelMuxApply and included in Status.Mux.
type MuxState struct {
	Enabled    bool   `json:"enabled"`
	Running    bool   `json:"running"`
	Listen     string `json:"listen"`
	Port       int    `json:"port"`
	ConfigHash string `json:"configHash"`
	Error      string `json:"error,omitempty"`
	RouteCount int    `json:"routeCount"`
}

type compiledMuxRoute struct {
	sni           string
	wildcard      bool   // true when pattern is "*.suffix"
	suffix        string // ".suffix" when wildcard is true
	target        string
	proxyProtocol bool
	hasAllow      bool
	allow         []netip.Prefix
}

type muxTable struct {
	exact          map[string]compiledMuxRoute
	wildcards      []compiledMuxRoute
	fallbackTarget string
	fallbackProxy  bool
}

func normalizeMuxSNI(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.TrimSuffix(s, ".")
}

func parseMuxAllow(entries []string) ([]netip.Prefix, error) {
	if len(entries) > maxMuxAllowEntries {
		return nil, fmt.Errorf("白名单条目过多（最多 %d 条）", maxMuxAllowEntries)
	}
	out := make([]netip.Prefix, 0, len(entries))
	for _, raw := range entries {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if strings.Contains(s, "/") {
			p, err := netip.ParsePrefix(s)
			if err != nil {
				return nil, fmt.Errorf("非法白名单 CIDR: %s", s)
			}
			out = append(out, p.Masked())
			continue
		}
		ip, err := netip.ParseAddr(s)
		if err != nil {
			return nil, fmt.Errorf("非法白名单 IP: %s", s)
		}
		ip = ip.Unmap()
		out = append(out, netip.PrefixFrom(ip, ip.BitLen()))
	}
	return out, nil
}

func validateMuxTarget(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil || strings.TrimSpace(host) == "" {
		return "", fmt.Errorf("非法转发目标地址: %s", addr)
	}
	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil || port < 1 || port > 65535 || fmt.Sprintf("%d", port) != portStr {
		return "", fmt.Errorf("非法转发目标端口: %s", addr)
	}
	return net.JoinHostPort(strings.TrimSpace(host), portStr), nil
}

func (c *MuxConfig) validateAndCompile() (*muxTable, error) {
	if !c.Enabled {
		return &muxTable{}, nil
	}
	if c.Port < 1 || c.Port > 65535 {
		return nil, fmt.Errorf("非法端口复用监听端口: %d", c.Port)
	}
	c.Listen = strings.TrimSpace(c.Listen)
	if c.Listen == "" {
		c.Listen = "::"
	}
	if net.ParseIP(c.Listen) == nil {
		return nil, fmt.Errorf("非法监听地址: %s", c.Listen)
	}

	tbl := &muxTable{
		exact: make(map[string]compiledMuxRoute, len(c.Routes)),
	}
	seen := make(map[string]struct{}, len(c.Routes))
	for _, r := range c.Routes {
		sni := normalizeMuxSNI(r.SNI)
		if sni == "" || strings.ContainsAny(sni, " \t\r\n/") {
			return nil, fmt.Errorf("非法 SNI 域名: %q", r.SNI)
		}
		if _, dup := seen[sni]; dup {
			return nil, fmt.Errorf("重复的 SNI 规则: %s", sni)
		}
		seen[sni] = struct{}{}

		target, err := validateMuxTarget(r.Target)
		if err != nil {
			return nil, err
		}
		var prefixes []netip.Prefix
		hasAllow := r.Allow != nil
		if hasAllow {
			prefixes, err = parseMuxAllow(r.Allow)
			if err != nil {
				return nil, err
			}
		}
		cr := compiledMuxRoute{
			sni:           sni,
			target:        target,
			proxyProtocol: r.ProxyProtocol,
			hasAllow:      hasAllow,
			allow:         prefixes,
		}
		if strings.HasPrefix(sni, "*.") {
			suffix := sni[1:] // ".example.com"
			if len(suffix) < 3 || strings.Contains(suffix, "*") {
				return nil, fmt.Errorf("非法通配符 SNI: %s", r.SNI)
			}
			cr.wildcard = true
			cr.suffix = suffix
			tbl.wildcards = append(tbl.wildcards, cr)
		} else {
			if strings.Contains(sni, "*") {
				return nil, fmt.Errorf("非法 SNI 域名: %s", r.SNI)
			}
			tbl.exact[sni] = cr
		}
	}

	if fb := strings.TrimSpace(c.FallbackTarget); fb != "" {
		target, err := validateMuxTarget(fb)
		if err != nil {
			return nil, fmt.Errorf("默认回落目标无效: %w", err)
		}
		tbl.fallbackTarget = target
		tbl.fallbackProxy = c.FallbackProxy
	}
	return tbl, nil
}

func (t *muxTable) match(sni string) (target string, proxyProto bool, allow []netip.Prefix, hasAllow bool, ok bool) {
	if t == nil {
		return "", false, nil, false, false
	}
	if sni != "" {
		if r, found := t.exact[sni]; found {
			return r.target, r.proxyProtocol, r.allow, r.hasAllow, true
		}
		for _, r := range t.wildcards {
			if strings.HasSuffix(sni, r.suffix) && len(sni) > len(r.suffix) {
				return r.target, r.proxyProtocol, r.allow, r.hasAllow, true
			}
		}
	}
	if t.fallbackTarget != "" {
		return t.fallbackTarget, t.fallbackProxy, nil, false, true
	}
	return "", false, nil, false, false
}

func allowAddrMatch(remote net.Addr, allow []netip.Prefix) bool {
	if len(allow) == 0 {
		return false
	}
	tcpAddr, ok := remote.(*net.TCPAddr)
	if !ok || tcpAddr == nil {
		return false
	}
	addr, ok := netip.AddrFromSlice(tcpAddr.IP)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	for _, p := range allow {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// sniMux multiplexes incoming TCP connections on a single port (typically 443)
// by peeking the TLS ClientHello SNI without terminating TLS.
type sniMux struct {
	mu      sync.Mutex
	cfg     MuxConfig
	ln      net.Listener
	running bool
	err     string
	table   atomic.Pointer[muxTable]
}

func newSNIMux() *sniMux {
	return &sniMux{}
}

func (s *sniMux) apply(cfg MuxConfig) (MuxState, error) {
	tbl, err := cfg.validateAndCompile()
	if err != nil {
		return s.state(), err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !cfg.Enabled {
		if s.ln != nil {
			_ = s.ln.Close()
			s.ln = nil
		}
		s.running = false
		s.err = ""
		s.cfg = cfg
		s.table.Store(&muxTable{})
		return s.stateLocked(), nil
	}

	// Hot-reload routing table in-place when listen address & port are unchanged.
	if s.running && s.ln != nil && s.cfg.Listen == cfg.Listen && s.cfg.Port == cfg.Port {
		s.table.Store(tbl)
		s.cfg = cfg
		s.err = ""
		return s.stateLocked(), nil
	}

	// Close previous listener before binding a new address/port.
	if s.ln != nil {
		_ = s.ln.Close()
		s.ln = nil
		s.running = false
	}

	ln, err := listenMuxTCP(cfg.Listen, cfg.Port)
	if err != nil {
		s.cfg = cfg
		s.running = false
		s.err = err.Error()
		s.table.Store(&muxTable{})
		return s.stateLocked(), fmt.Errorf("监听 %s 失败: %w", net.JoinHostPort(cfg.Listen, fmt.Sprintf("%d", cfg.Port)), err)
	}

	s.ln = ln
	s.cfg = cfg
	s.running = true
	s.err = ""
	s.table.Store(tbl)

	go s.serve(ln)
	return s.stateLocked(), nil
}

func listenMuxTCP(listen string, port int) (net.Listener, error) {
	addr := net.JoinHostPort(listen, fmt.Sprintf("%d", port))
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		return ln, nil
	}
	if listen == "::" && !ipv6Sockets() {
		return net.Listen("tcp", net.JoinHostPort("0.0.0.0", fmt.Sprintf("%d", port)))
	}
	return nil, err
}

func (s *sniMux) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln != nil {
		_ = s.ln.Close()
		s.ln = nil
	}
	s.running = false
	s.table.Store(&muxTable{})
}

func (s *sniMux) config() MuxConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

func (s *sniMux) state() MuxState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stateLocked()
}

func (s *sniMux) stateLocked() MuxState {
	return MuxState{
		Enabled:    s.cfg.Enabled,
		Running:    s.running,
		Listen:     s.cfg.Listen,
		Port:       s.cfg.Port,
		ConfigHash: s.cfg.Hash,
		Error:      s.err,
		RouteCount: len(s.cfg.Routes),
	}
}

func (s *sniMux) serve(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			s.mu.Lock()
			if s.ln == ln {
				s.ln = nil
				s.running = false
				if !errors.Is(err, net.ErrClosed) {
					s.err = err.Error()
				}
			}
			s.mu.Unlock()
			return
		}
		go s.handleConn(conn)
	}
}

func (s *sniMux) handleConn(client net.Conn) {
	defer client.Close()
	if tc, ok := client.(*net.TCPConn); ok {
		_ = tc.SetKeepAlive(true)
		_ = tc.SetKeepAlivePeriod(30 * time.Second)
	}

	sni, peeked, err := peekClientHelloSNI(client)
	if err != nil && len(peeked) == 0 {
		return
	}

	tbl := s.table.Load()
	target, proxyProto, allow, hasAllow, ok := tbl.match(sni)
	if !ok {
		return
	}
	if hasAllow && !allowAddrMatch(client.RemoteAddr(), allow) {
		return
	}

	backend, err := net.DialTimeout("tcp", target, muxDialTimeout)
	if err != nil {
		return
	}
	defer backend.Close()
	if tc, ok := backend.(*net.TCPConn); ok {
		_ = tc.SetKeepAlive(true)
		_ = tc.SetKeepAlivePeriod(30 * time.Second)
	}

	if proxyProto {
		if err := writeProxyV1Header(backend, client.RemoteAddr(), client.LocalAddr()); err != nil {
			return
		}
	}
	if len(peeked) > 0 {
		if _, err := backend.Write(peeked); err != nil {
			return
		}
	}

	relayBidirectional(client, backend)
}

func relayBidirectional(a, b net.Conn) {
	done := make(chan struct{}, 2)
	copyOne := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		if tc, ok := dst.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
		done <- struct{}{}
	}
	go copyOne(b, a)
	go copyOne(a, b)
	<-done
	<-done
}

// writeProxyV1Header writes a HAProxy PROXY protocol v1 line describing the
// original TCP client/server endpoints.
func writeProxyV1Header(w io.Writer, remote, local net.Addr) error {
	rTCP, rOK := remote.(*net.TCPAddr)
	lTCP, lOK := local.(*net.TCPAddr)
	if !rOK || !lOK || rTCP == nil || lTCP == nil {
		_, err := io.WriteString(w, "PROXY UNKNOWN\r\n")
		return err
	}
	rIP, rValid := netip.AddrFromSlice(rTCP.IP)
	lIP, lValid := netip.AddrFromSlice(lTCP.IP)
	if !rValid || !lValid {
		_, err := io.WriteString(w, "PROXY UNKNOWN\r\n")
		return err
	}
	rIP = rIP.Unmap()
	lIP = lIP.Unmap()
	if rIP.Is4() && lIP.Is4() {
		_, err := fmt.Fprintf(w, "PROXY TCP4 %s %s %d %d\r\n", rIP.String(), lIP.String(), rTCP.Port, lTCP.Port)
		return err
	}
	if rIP.Is6() && lIP.Is6() {
		_, err := fmt.Fprintf(w, "PROXY TCP6 %s %s %d %d\r\n", rIP.String(), lIP.String(), rTCP.Port, lTCP.Port)
		return err
	}
	_, err := io.WriteString(w, "PROXY UNKNOWN\r\n")
	return err
}

// peekClientHelloSNI reads the initial bytes of conn to extract the TLS
// ClientHello SNI while returning every raw byte read verbatim so the caller
// can replay them untouched to the selected backend.
func peekClientHelloSNI(conn net.Conn) (sni string, raw []byte, err error) {
	_ = conn.SetReadDeadline(time.Now().Add(muxPeekTimeout))
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()

	var rawBuf bytes.Buffer
	var hsBuf bytes.Buffer
	tmp := make([]byte, 4096)

	n, readErr := conn.Read(tmp)
	if n > 0 {
		rawBuf.Write(tmp[:n])
	}
	if n == 0 {
		return "", nil, readErr
	}
	// Fast path: non-TLS traffic (first byte is not TLS Handshake record 0x16)
	// returns immediately so fallback forwarding incurs zero latency.
	if rawBuf.Bytes()[0] != 0x16 {
		return "", rawBuf.Bytes(), nil
	}

	// Ensure we have enough bytes to parse one or more TLS Handshake records
	// containing the complete ClientHello message (handles fragmented PQ hellos).
	recOffset := 0
	for {
		data := rawBuf.Bytes()
		// Consume all complete TLS records currently in rawBuf.
		for recOffset+5 <= len(data) {
			recType := data[recOffset]
			recMajor := data[recOffset+1]
			recLen := int(binary.BigEndian.Uint16(data[recOffset+3 : recOffset+5]))
			if recType != 0x16 || recMajor != 0x03 || recLen == 0 || recLen > 16384+2048 {
				return "", data, nil
			}
			if recOffset+5+recLen > len(data) {
				break
			}
			hsBuf.Write(data[recOffset+5 : recOffset+5+recLen])
			recOffset += 5 + recLen
		}

		hs := hsBuf.Bytes()
		if len(hs) >= 4 {
			if hs[0] != 0x01 { // Not a ClientHello
				return "", rawBuf.Bytes(), nil
			}
			helloLen := int(hs[1])<<16 | int(hs[2])<<8 | int(hs[3])
			if helloLen <= 0 || helloLen > maxMuxPeekBytes {
				return "", rawBuf.Bytes(), nil
			}
			if len(hs) >= 4+helloLen {
				return parseClientHelloSNI(hs[4 : 4+helloLen]), rawBuf.Bytes(), nil
			}
		}

		if readErr != nil || rawBuf.Len() >= maxMuxPeekBytes {
			return "", rawBuf.Bytes(), nil
		}
		remain := maxMuxPeekBytes - rawBuf.Len()
		chunk := tmp
		if remain < len(chunk) {
			chunk = tmp[:remain]
		}
		n, readErr = conn.Read(chunk)
		if n > 0 {
			rawBuf.Write(chunk[:n])
		}
		if n == 0 && readErr != nil {
			return "", rawBuf.Bytes(), nil
		}
	}
}

// parseClientHelloSNI parses a TLS ClientHello body (after the 4-byte
// handshake header) and returns the normalized RFC 6066 host_name SNI.
func parseClientHelloSNI(body []byte) string {
	// client_version (2) + random (32)
	off := 2 + 32
	if off+1 > len(body) {
		return ""
	}
	// session_id
	sidLen := int(body[off])
	off += 1 + sidLen
	if off+2 > len(body) {
		return ""
	}
	// cipher_suites
	csLen := int(binary.BigEndian.Uint16(body[off : off+2]))
	off += 2 + csLen
	if off+1 > len(body) {
		return ""
	}
	// compression_methods
	cmLen := int(body[off])
	off += 1 + cmLen
	if off+2 > len(body) {
		return ""
	}
	// extensions
	extTotal := int(binary.BigEndian.Uint16(body[off : off+2]))
	off += 2
	extEnd := off + extTotal
	if extEnd > len(body) {
		extEnd = len(body)
	}
	for off+4 <= extEnd {
		extType := binary.BigEndian.Uint16(body[off : off+2])
		extLen := int(binary.BigEndian.Uint16(body[off+2 : off+4]))
		off += 4
		if off+extLen > extEnd {
			return ""
		}
		if extType == 0x0000 { // server_name
			ext := body[off : off+extLen]
			if len(ext) < 2 {
				return ""
			}
			listLen := int(binary.BigEndian.Uint16(ext[0:2]))
			if 2+listLen > len(ext) {
				listLen = len(ext) - 2
			}
			p := 2
			end := 2 + listLen
			for p+3 <= end {
				nameType := ext[p]
				nameLen := int(binary.BigEndian.Uint16(ext[p+1 : p+3]))
				p += 3
				if p+nameLen > end {
					return ""
				}
				if nameType == 0x00 { // host_name
					return normalizeMuxSNI(string(ext[p : p+nameLen]))
				}
				p += nameLen
			}
			return ""
		}
		off += extLen
	}
	return ""
}
