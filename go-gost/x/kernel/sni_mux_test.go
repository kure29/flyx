package kernel

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"fmt"
	"io"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

func selfSignedCert(t *testing.T, hosts ...string) tls.Certificate {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: hosts[0]},
		DNSNames:     hosts,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv}
}

func startTLSBackend(t *testing.T, label string, hosts ...string) (string, func()) {
	t.Helper()
	cert := selfSignedCert(t, hosts...)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				_, _ = io.WriteString(conn, "backend:"+label+"\n")
			}(c)
		}
	}()
	return ln.Addr().String(), func() { _ = ln.Close() }
}

func startRawEchoBackend(t *testing.T, label string) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
				r := bufio.NewReader(conn)
				line, _ := r.ReadString('\n')
				_, _ = fmt.Fprintf(conn, "%s|%s", label, line)
			}(c)
		}
	}()
	return ln.Addr().String(), func() { _ = ln.Close() }
}

func freeLocalTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func dialTLSAndRead(t *testing.T, addr, sni string) (string, error) {
	t.Helper()
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 2 * time.Second}, "tcp", addr, &tls.Config{
		ServerName:         sni,
		InsecureSkipVerify: true,
	})
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	b, err := io.ReadAll(conn)
	return strings.TrimSpace(string(b)), err
}

func TestSNIMuxRoutingWildcardFallbackWhitelistHotReload(t *testing.T) {
	b1, close1 := startTLSBackend(t, "reality", "www.microsoft.com")
	defer close1()
	b2, close2 := startTLSBackend(t, "wild", "app.example.com")
	defer close2()
	fb, closeFB := startRawEchoBackend(t, "fallback")
	defer closeFB()

	port := freeLocalTCPPort(t)
	mux := newSNIMux()
	defer mux.stop()

	st, err := mux.apply(MuxConfig{
		Enabled: true,
		Listen:  "127.0.0.1",
		Port:    port,
		Routes: []MuxRoute{
			{SNI: "WWW.Microsoft.com.", Target: b1},
			{SNI: "*.example.com", Target: b2},
		},
		FallbackTarget: fb,
		Hash:           "h1",
	})
	if err != nil {
		t.Fatalf("apply mux: %v", err)
	}
	if !st.Running || st.Port != port || st.ConfigHash != "h1" {
		t.Fatalf("unexpected state: %+v", st)
	}
	muxAddr := fmt.Sprintf("127.0.0.1:%d", port)

	// Exact SNI match (case-insensitive)
	got, err := dialTLSAndRead(t, muxAddr, "www.microsoft.com")
	if err != nil || got != "backend:reality" {
		t.Fatalf("exact match got=%q err=%v", got, err)
	}

	// Wildcard SNI match
	got, err = dialTLSAndRead(t, muxAddr, "app.example.com")
	if err != nil || got != "backend:wild" {
		t.Fatalf("wildcard match got=%q err=%v", got, err)
	}

	// Non-TLS fallback (plain HTTP) — should return immediately without waiting for peek timeout
	start := time.Now()
	c, err := net.DialTimeout("tcp", muxAddr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(c, "GET / HTTP/1.1\n")
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	rawResp, _ := io.ReadAll(c)
	_ = c.Close()
	if time.Since(start) > 2*time.Second {
		t.Fatalf("non-TLS fallback stalled: %v", time.Since(start))
	}
	if string(rawResp) != "fallback|GET / HTTP/1.1\n" {
		t.Fatalf("fallback response = %q", string(rawResp))
	}

	// Hot reload: add PROXY protocol on fallback and restrict b1 with allowlist denying 127.0.0.1
	st2, err := mux.apply(MuxConfig{
		Enabled: true,
		Listen:  "127.0.0.1",
		Port:    port,
		Routes: []MuxRoute{
			{SNI: "www.microsoft.com", Target: b1, Allow: []string{"10.0.0.1/32"}},
			{SNI: "*.example.com", Target: b2, Allow: []string{"127.0.0.0/8"}},
		},
		FallbackTarget: fb,
		FallbackProxy:  true,
		Hash:           "h2",
	})
	if err != nil || !st2.Running || st2.ConfigHash != "h2" {
		t.Fatalf("hot reload failed: st=%+v err=%v", st2, err)
	}

	// Denied by whitelist
	if _, err := dialTLSAndRead(t, muxAddr, "www.microsoft.com"); err == nil {
		t.Fatalf("expected whitelist rejection for www.microsoft.com")
	}
	// Allowed by whitelist
	if got, err := dialTLSAndRead(t, muxAddr, "app.example.com"); err != nil || got != "backend:wild" {
		t.Fatalf("allowed route failed: got=%q err=%v", got, err)
	}

	// PROXY protocol v1 header on fallback
	c, err = net.DialTimeout("tcp", muxAddr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(c, "HELLO\n")
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	ppResp, _ := io.ReadAll(c)
	_ = c.Close()
	if !strings.HasPrefix(string(ppResp), "fallback|PROXY TCP4 127.0.0.1 127.0.0.1 ") {
		t.Fatalf("expected PROXY v1 line, got %q", string(ppResp))
	}
}

func TestPeekFragmentedClientHello(t *testing.T) {
	// Build a minimal ClientHello handshake message carrying SNI "pq.example.org"
	// split across two TLS Handshake records.
	sni := "pq.example.org"
	var sniExt []byte
	sniExt = append(sniExt, 0, 0) // server_name_list len placeholder
	sniExt = append(sniExt, 0x00) // host_name
	sniExt = binary.BigEndian.AppendUint16(sniExt, uint16(len(sni)))
	sniExt = append(sniExt, sni...)
	binary.BigEndian.PutUint16(sniExt[0:2], uint16(len(sniExt)-2))

	var exts []byte
	exts = binary.BigEndian.AppendUint16(exts, 0x0000) // extType server_name
	exts = binary.BigEndian.AppendUint16(exts, uint16(len(sniExt)))
	exts = append(exts, sniExt...)

	var body []byte
	body = append(body, 0x03, 0x03)             // client_version TLS 1.2
	body = append(body, make([]byte, 32)...)    // random
	body = append(body, 0x00)                   // session_id len
	body = append(body, 0x00, 0x02, 0x13, 0x01) // cipher_suites
	body = append(body, 0x01, 0x00)             // compression_methods
	body = binary.BigEndian.AppendUint16(body, uint16(len(exts)))
	body = append(body, exts...)

	hs := make([]byte, 4+len(body))
	hs[0] = 0x01 // ClientHello
	hs[1] = byte(len(body) >> 16)
	hs[2] = byte(len(body) >> 8)
	hs[3] = byte(len(body))
	copy(hs[4:], body)

	split := len(hs) / 2
	makeRecord := func(payload []byte) []byte {
		r := make([]byte, 5+len(payload))
		r[0] = 0x16
		r[1] = 0x03
		r[2] = 0x01
		binary.BigEndian.PutUint16(r[3:5], uint16(len(payload)))
		copy(r[5:], payload)
		return r
	}
	rec1 := makeRecord(hs[:split])
	rec2 := makeRecord(hs[split:])

	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	go func() {
		_, _ = c1.Write(rec1)
		time.Sleep(10 * time.Millisecond)
		_, _ = c1.Write(rec2)
	}()

	gotSNI, raw, err := peekClientHelloSNI(c2)
	if err != nil {
		t.Fatalf("peek error: %v", err)
	}
	if gotSNI != sni {
		t.Fatalf("got SNI %q, want %q", gotSNI, sni)
	}
	if len(raw) != len(rec1)+len(rec2) {
		t.Fatalf("raw len %d != %d", len(raw), len(rec1)+len(rec2))
	}
}

func TestManagerMuxPersistAndRestore(t *testing.T) {
	dir := t.TempDir()
	port := freeLocalTCPPort(t)
	b1, close1 := startTLSBackend(t, "restored", "restore.example.com")
	defer close1()

	m1 := NewManager(dir)
	st, err := m1.ApplyMux(MuxConfig{
		Enabled: true,
		Listen:  "127.0.0.1",
		Port:    port,
		Routes:  []MuxRoute{{SNI: "restore.example.com", Target: b1}},
		Hash:    "persist-hash",
	})
	if err != nil || !st.Running {
		t.Fatalf("ApplyMux failed: st=%+v err=%v", st, err)
	}
	if status := m1.Status(""); status.Mux == nil || !status.Mux.Running || status.Mux.ConfigHash != "persist-hash" {
		t.Fatalf("unexpected status.Mux: %+v", status.Mux)
	}
	m1.Shutdown()

	// New manager restores mux from mux.json automatically
	m2 := NewManager(dir)
	defer m2.Shutdown()
	m2.Restore()

	got, err := dialTLSAndRead(t, fmt.Sprintf("127.0.0.1:%d", port), "restore.example.com")
	if err != nil || got != "backend:restored" {
		t.Fatalf("restored mux failed: got=%q err=%v", got, err)
	}

	// Disabling removes mux and frees port
	stOff, err := m2.ApplyMux(MuxConfig{Enabled: false, Hash: "off"})
	if err != nil || stOff.Running {
		t.Fatalf("disable mux failed: st=%+v err=%v", stOff, err)
	}
}
