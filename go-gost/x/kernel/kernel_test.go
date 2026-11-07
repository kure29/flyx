package kernel

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeKernel prints its version, echoes the config path and then sleeps, or
// exits immediately when the config contains "crash".
const fakeKernel = `#!/bin/sh
if [ "$1" = "version" ]; then echo "Xray 9.9.9 (fake)"; exit 0; fi
echo "starting with $3"
if grep -q crash "$3"; then echo "config error"; exit 1; fi
exec sleep 300
`

func zipWith(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		hdr := &zip.FileHeader{Name: name, Method: zip.Deflate}
		hdr.SetMode(0o755)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type nftRecorder struct {
	mu      sync.Mutex
	scripts []string
	listOut string
}

func stubNft(t *testing.T) *nftRecorder {
	t.Helper()
	rec := &nftRecorder{}
	oldRunner, oldAvail := nftRunner, nftAvailable
	nftRunner = func(_ context.Context, stdin string, args ...string) ([]byte, error) {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		joined := strings.Join(args, " ")
		switch {
		case joined == "-f -":
			rec.scripts = append(rec.scripts, stdin)
			return nil, nil
		case joined == "list tables":
			if len(rec.scripts) == 0 {
				return nil, nil
			}
			table := nftTable
			if strings.Contains(rec.scripts[len(rec.scripts)-1], legacyNftTable) {
				table = legacyNftTable
			}
			return []byte("table inet " + table + "\n"), nil
		case strings.HasPrefix(joined, "list counters"):
			return []byte(rec.listOut), nil
		}
		return nil, fmt.Errorf("unexpected nft call %q", joined)
	}
	nftAvailable = func() error { return nil }
	t.Cleanup(func() { nftRunner, nftAvailable = oldRunner, oldAvail })
	return rec
}

func serveKernel(t *testing.T, payload []byte) (*httptest.Server, *int) {
	t.Helper()
	sum := sha256.Sum256(payload)
	downloads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Xray-linux-test.zip":
			downloads++
			_, _ = w.Write(payload)
		case "/Xray-linux-test.zip.dgst":
			fmt.Fprintf(w, "MD5= 00000000000000000000000000000000\nSHA2-256= %s\nSHA2-512= %s\n", hex.EncodeToString(sum[:]), strings.Repeat("ab", 64))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &downloads
}

func applyRequest(srv *httptest.Server, config string, enabled bool) ApplyRequest {
	return ApplyRequest{
		Type:    "xray",
		Version: "latest",
		Download: &Download{
			URL:         srv.URL + "/Xray-linux-{ARCH}.zip",
			ChecksumURL: srv.URL + "/Xray-linux-{ARCH}.zip.dgst",
			Archive:     "zip",
			BinName:     "xray",
			ArchMap:     map[string]string{runtime.GOARCH: "test"},
		},
		Files:   []File{{Name: "config.json", Content: config}},
		Args:    []string{"run", "-c", "{RUN_DIR}/config.json"},
		Hash:    hashString(config),
		Enabled: enabled,
		Ports:   []Port{{InboundID: 7, Port: 443, Network: "tcp"}, {InboundID: 8, Port: 8388, Network: "tcp,udp"}},
	}
}

func TestManagerLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as fake kernel")
	}
	rec := stubNft(t)
	srv, downloads := serveKernel(t, zipWith(t, map[string]string{
		"xray":        fakeKernel,
		"geoip.dat":   "geo",
		"../evil":     "nope",
		"LICENSE":     "x",
		"README.md":   "x",
		"sub/inner":   "flattened",
		"bad name!.x": "skipped",
	}))
	base := t.TempDir()
	m := NewManager(base)
	defer m.Shutdown()

	st, err := m.Apply(applyRequest(srv, `{"v":1}`, true))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !st.Running || st.Pid == 0 || st.Version != "Xray 9.9.9 (fake)" {
		t.Fatalf("state after apply: %+v", st)
	}
	if *downloads != 1 {
		t.Fatalf("downloads = %d", *downloads)
	}
	binDir := filepath.Join(base, "xray", "bin", "latest")
	for _, name := range []string{"xray", "geoip.dat", "inner"} {
		if _, err := os.Stat(filepath.Join(binDir, name)); err != nil {
			t.Fatalf("expected %s extracted: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(base, "xray", "bin", "evil")); err == nil {
		t.Fatal("path traversal entry escaped the bin dir")
	}
	cfg, _ := os.ReadFile(filepath.Join(base, "xray", "run", "config.json"))
	if string(cfg) != `{"v":1}` {
		t.Fatalf("config = %q", cfg)
	}
	if len(rec.scripts) != 1 || !strings.Contains(rec.scripts[0], "udp dport 8388 counter name in_8_udp") {
		t.Fatalf("nft script: %v", rec.scripts)
	}

	// Same hash: no restart, no new nft script, no re-download.
	pid := st.Pid
	st, err = m.Apply(applyRequest(srv, `{"v":1}`, true))
	if err != nil || st.Pid != pid {
		t.Fatalf("unchanged apply restarted the kernel: %+v %v", st, err)
	}
	if len(rec.scripts) != 1 || *downloads != 1 {
		t.Fatalf("unchanged apply must be a no-op: scripts=%d downloads=%d", len(rec.scripts), *downloads)
	}

	// New config: restart.
	st, err = m.Apply(applyRequest(srv, `{"v":2}`, true))
	if err != nil || st.Pid == pid || !st.Running {
		t.Fatalf("changed apply must restart: %+v %v", st, err)
	}

	// Broken config: reported with the kernel's output, and the kernel
	// goes back to the config it ran before.
	st, err = m.Apply(applyRequest(srv, `{"crash":true}`, true))
	if err == nil || !strings.Contains(err.Error(), "config error") || !strings.Contains(err.Error(), "已恢复") {
		t.Fatalf("crashing kernel must surface its log, got %v", err)
	}
	cfg, _ = os.ReadFile(filepath.Join(base, "xray", "run", "config.json"))
	if !st.Running || string(cfg) != `{"v":2}` {
		t.Fatalf("broken config must roll back: %+v %q", st, cfg)
	}
	// The rolled-back config is what runs now, so applying it again is a
	// no-op rather than another restart.
	pid = st.Pid
	if st, err = m.Apply(applyRequest(srv, `{"v":2}`, true)); err != nil || st.Pid != pid {
		t.Fatalf("re-applying the running config: %+v %v", st, err)
	}

	// Disable stops the process.
	st, err = m.Apply(applyRequest(srv, `{"v":3}`, false))
	if err != nil || st.Running {
		t.Fatalf("disabled kernel still running: %+v %v", st, err)
	}

	rec.listOut = `table inet flvx_kernel {
	counter in_7_tcp {
		packets 3 bytes 1200
	}
	counter out_7_tcp {
		packets 1 bytes 99
	}
}
table inet other {
	counter in_9_tcp {
		packets 1 bytes 5
	}
}
`
	status := m.Status("xray")
	if !status.NftAvailable || len(status.Counters) != 2 || status.Epoch == "" {
		t.Fatalf("status counters: %+v", status)
	}
	if status.Counters[0] != (Counter{InboundID: 7, Proto: "tcp", Dir: "in", Bytes: 1200}) {
		t.Fatalf("counter parse: %+v", status.Counters[0])
	}
	if len(status.Kernels) != 1 || len(status.Kernels[0].Logs) == 0 {
		t.Fatalf("logs missing: %+v", status.Kernels)
	}

	// Restore on a fresh manager re-uses the installed binary.
	m2 := NewManager(base)
	defer m2.Shutdown()
	m2.Restore()
	if *downloads != 1 {
		t.Fatalf("restore must not re-download: %d", *downloads)
	}
	if st := m2.Status("").Kernels; len(st) != 1 || st[0].ConfigHash != hashString(`{"v":3}`) || st[0].Running {
		t.Fatalf("restored state: %+v", st)
	}
	if _, err := m2.Control("xray", "start"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if st := m2.Status("").Kernels[0]; !st.Running {
		t.Fatalf("start did not run kernel: %+v", st)
	}

	if _, err := m2.Control("xray", "remove"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "xray")); !os.IsNotExist(err) {
		t.Fatal("remove must delete kernel files")
	}
	last := rec.scripts[len(rec.scripts)-1]
	if strings.Contains(last, "chain input") {
		t.Fatalf("after removing the only kernel the table must be dropped: %s", last)
	}
}

func TestChecksumMismatchRejected(t *testing.T) {
	stubNft(t)
	srv, _ := serveKernel(t, zipWith(t, map[string]string{"xray": fakeKernel}))
	req := applyRequest(srv, `{}`, true)
	req.Download.URL = srv.URL + "/Xray-linux-{ARCH}.zip"
	req.Download.ChecksumURL = srv.URL + "/missing.dgst"
	m := NewManager(t.TempDir())
	if _, err := m.Apply(req); err == nil || !strings.Contains(err.Error(), "校验") {
		t.Fatalf("missing checksum must fail: %v", err)
	}
}

func TestApplyValidation(t *testing.T) {
	m := NewManager(t.TempDir())
	base := ApplyRequest{Type: "xray", Version: "latest", Download: &Download{BinName: "xray", Archive: "zip"}}
	cases := map[string]func(r *ApplyRequest){
		"unknown type":  func(r *ApplyRequest) { r.Type = "gost" },
		"bad version":   func(r *ApplyRequest) { r.Version = "../../etc" },
		"file escape":   func(r *ApplyRequest) { r.Files = []File{{Name: "../x"}} },
		"bad bin name":  func(r *ApplyRequest) { r.Download.BinName = "a/b" },
		"bad archive":   func(r *ApplyRequest) { r.Download.Archive = "rar" },
		"bad port":      func(r *ApplyRequest) { r.Ports = []Port{{InboundID: 1, Port: 0}} },
		"no download":   func(r *ApplyRequest) { r.Download = nil },
		"hidden config": func(r *ApplyRequest) { r.Files = []File{{Name: ".."}} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := base
			d := *base.Download
			r.Download = &d
			mutate(&r)
			if _, err := m.Apply(r); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestParseChecksum(t *testing.T) {
	h := strings.Repeat("a1", 32)
	cases := map[string]string{
		h + "  Xray-linux-64.zip\n":                                         h,
		"MD5= d41d8cd98f00b204e9800998ecf8427e\nSHA2-256= " + h + "\n":      h,
		"SHA2-512= " + strings.Repeat("cd", 64) + "\nSHA2-256= " + h + "\n": h,
	}
	for in, want := range cases {
		if got, ok := parseChecksum(in); !ok || got != want {
			t.Fatalf("parseChecksum(%q) = %q %v", in, got, ok)
		}
	}
	if _, ok := parseChecksum("nothing"); ok {
		t.Fatal("expected no checksum")
	}
}

func TestNftScriptOnlyCounts(t *testing.T) {
	script := nftScript(expandPorts([]Port{{InboundID: 2, Port: 80, Network: "tcp,udp"}, {InboundID: 1, Port: 443, Network: "tcp"}}), nil, nil, nil)
	for _, bad := range []string{"drop", "reject", "policy drop"} {
		if strings.Contains(script, bad) {
			t.Fatalf("counting table must never filter: %s", script)
		}
	}
	if strings.Index(script, "in_1_tcp") > strings.Index(script, "in_2_tcp") {
		t.Fatal("rules must be sorted for a stable signature")
	}
	if !strings.HasPrefix(script, "table inet flyx_kernel {}\ndelete table inet flyx_kernel\n") {
		t.Fatalf("script must recreate the table atomically: %s", script)
	}
}

func TestProcessBackoffRestarts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh")
	}
	logs := &logRing{}
	p := newProcess("/bin/sh", []string{"-c", "echo boom; exit 3"}, t.TempDir(), logs)
	p.start()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, restarts, _, _ := p.snapshot(); restarts >= 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	p.stop()
	_, _, restarts, lastErr, _ := p.snapshot()
	if restarts < 2 || !strings.Contains(lastErr, "exit status 3") {
		t.Fatalf("restarts=%d lastErr=%q", restarts, lastErr)
	}
	if !strings.Contains(strings.Join(logs.tail(0), "\n"), "boom") {
		t.Fatal("process output not captured")
	}
}

func TestNftScriptWhitelist(t *testing.T) {
	ports := []Port{
		{InboundID: 1, Port: 443, Network: "tcp", Allow: []string{"127.0.0.0/8", "::1/128", "198.51.100.7/32", "127.0.0.1/32"}},
		{InboundID: 2, Port: 8388, Network: "tcp,udp"},
	}
	script := nftScript(expandPorts(ports), expandGuards(ports), expandHops(ports), nil)
	for _, want := range []string{
		"set allow4_1 {",
		"elements = { 127.0.0.0/8, 127.0.0.1/32, 198.51.100.7/32 }",
		"elements = { ::1/128 }",
		"meta nfproto ipv4 tcp dport 443 ip saddr != @allow4_1 drop",
		"meta nfproto ipv6 tcp dport 443 ip6 saddr != @allow6_1 drop",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("missing %q in:\n%s", want, script)
		}
	}
	if strings.Contains(script, "dport 8388 ip saddr") || strings.Contains(script, "allow4_2") {
		t.Fatalf("unguarded inbound must not be filtered:\n%s", script)
	}
	if strings.Index(script, "drop") > strings.Index(script, "counter name in_1_tcp") {
		t.Fatal("drop rules must come before the counters")
	}

	// Validate with the real nft parser when we can (root + nft installed).
	if _, err := exec.LookPath("nft"); err == nil && os.Geteuid() == 0 {
		cmd := exec.Command("nft", "-c", "-f", "-")
		cmd.Stdin = strings.NewReader(script)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("nft rejected the script: %v\n%s\n%s", err, out, script)
		}
	}
}

func TestApplyRejectsBadAllow(t *testing.T) {
	m := NewManager(t.TempDir())
	r := ApplyRequest{Type: "xray", Version: "latest", Download: &Download{BinName: "xray", Archive: "zip"},
		Ports: []Port{{InboundID: 1, Port: 443, Network: "tcp", Allow: []string{"1.2.3.4/32; drop"}}}}
	if _, err := m.Apply(r); err == nil || !strings.Contains(err.Error(), "白名单") {
		t.Fatalf("expected whitelist validation error, got %v", err)
	}
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestInstallIntegrityRules(t *testing.T) {
	payload := []byte("#!/bin/sh\nexec sleep 300\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bin-test":
			_, _ = w.Write(payload)
		case "/repos/o/r/releases/latest":
			fmt.Fprintf(w, `{"tag_name":"v1.2.3","assets":[{"name":"tool-1.2.3-test","digest":"sha256:%s","browser_download_url":"%s/bin-test"},{"name":"nodigest-1.2.3-test","browser_download_url":"x"}]}`, sha(payload), "http://"+r.Host)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := githubAPIBase
	githubAPIBase = srv.URL
	defer func() { githubAPIBase = old }()
	arch := map[string]string{runtime.GOARCH: "test"}

	cases := []struct {
		name    string
		d       Download
		wantErr string
	}{
		{"pinned ok", Download{URL: srv.URL + "/bin-{ARCH}", Archive: "raw", BinName: "tool", ArchMap: arch, SHA256: map[string]string{"test": sha(payload)}}, ""},
		{"pinned mismatch", Download{URL: srv.URL + "/bin-{ARCH}", Archive: "raw", BinName: "tool", ArchMap: arch, SHA256: map[string]string{"test": strings.Repeat("0", 64)}}, "校验失败"},
		{"no checksum refused", Download{URL: srv.URL + "/bin-{ARCH}", Archive: "raw", BinName: "tool", ArchMap: arch}, "拒绝安装"},
		{"github digest", Download{URL: "{VERSION}", Archive: "raw", BinName: "tool", ArchMap: arch, GitHub: &GitHubRelease{Repo: "o/r", Asset: "tool-{VERSION}-{ARCH}"}}, ""},
		{"github without digest", Download{Archive: "raw", BinName: "tool", ArchMap: arch, GitHub: &GitHubRelease{Repo: "o/r", Asset: "nodigest-{VERSION}-{ARCH}"}}, "校验值"},
		{"github missing asset", Download{Archive: "raw", BinName: "tool", ArchMap: arch, GitHub: &GitHubRelease{Repo: "o/r", Asset: "missing-{ARCH}"}}, "没有文件"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "bin")
			err := install(&tc.d, dir)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("install: %v", err)
				}
				if _, err := os.Stat(filepath.Join(dir, "tool")); err != nil {
					t.Fatal("binary not installed")
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want error %q, got %v", tc.wantErr, err)
			}
			if _, err := os.Stat(filepath.Join(dir, "tool")); err == nil {
				t.Fatal("failed install must not leave a binary")
			}
		})
	}
}

func TestManagerMultiProcessWithExtra(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts")
	}
	stubNft(t)
	main := []byte("#!/bin/sh\nif [ \"$1\" = \"-v\" ]; then echo 'snell-server v5.0.1'; exit 0; fi\necho \"main $2\"\nexec sleep 300\n")
	extra := []byte("#!/bin/sh\necho \"front $*\"\nexec sleep 300\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/main.zip":
			_, _ = w.Write(zipWith(t, map[string]string{"snell-server": string(main)}))
		case "/front":
			_, _ = w.Write(extra)
		}
	}))
	defer srv.Close()
	// Pin the hash of exactly what the server sends.
	resp, err := http.Get(srv.URL + "/main.zip")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	arch := map[string]string{runtime.GOARCH: "test"}

	mk := func(withFront bool, hash string) ApplyRequest {
		req := ApplyRequest{
			Type: "snell", Version: "5.0.1",
			Download: &Download{URL: srv.URL + "/main.zip", Archive: "zip", BinName: "snell-server", ArchMap: arch, SHA256: map[string]string{"test": sha(body)}},
			Files:    []File{{Name: "snell-1.conf", Content: "a"}, {Name: "snell-2.conf", Content: "b"}},
			Processes: []ProcessSpec{
				{Name: "snell-1", Args: []string{"-c", "{RUN_DIR}/snell-1.conf"}},
				{Name: "snell-2", Args: []string{"-c", "{RUN_DIR}/snell-2.conf"}},
			},
			Hash: hash, Enabled: true,
		}
		if withFront {
			req.Extras = []ExtraBinary{{Name: "shadow-tls", Version: "0.2.25", Download: &Download{URL: srv.URL + "/front", Archive: "raw", BinName: "shadow-tls", ArchMap: arch, SHA256: map[string]string{"test": sha(extra)}}}}
			req.Processes = append(req.Processes, ProcessSpec{Name: "stls-2", Bin: "shadow-tls", Args: []string{"--v3", "server"}})
		}
		return req
	}

	m := NewManager(t.TempDir())
	defer m.Shutdown()
	st, err := m.Apply(mk(true, "h1"))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !st.Running || !strings.Contains(st.Version, "5.0.1") {
		t.Fatalf("state: %+v", st)
	}
	logs := strings.Join(m.Status("snell").Kernels[0].Logs, "\n")
	for _, want := range []string{"[snell-1] main ", "[snell-2] main ", "[stls-2] front --v3 server"} {
		if !strings.Contains(logs, want) {
			t.Fatalf("missing %q in logs:\n%s", want, logs)
		}
	}
	// Dropping the front process restarts with two processes only.
	st, err = m.Apply(mk(false, "h2"))
	if err != nil || !st.Running {
		t.Fatalf("reapply: %+v %v", st, err)
	}
	m.mu.Lock()
	n := len(m.instances["snell"].procs)
	m.mu.Unlock()
	if n != 2 {
		t.Fatalf("want 2 processes, got %d", n)
	}
	bad := mk(false, "h3")
	bad.Processes[0].Bin = "nope"
	if _, err := m.Apply(bad); err == nil {
		t.Fatal("process referencing an undeclared binary must be rejected")
	}
}

func TestLogRingStripsANSI(t *testing.T) {
	r := &logRing{}
	r.add("[stls-3] \x1b[2m2026-10-14T16:14:14Z\x1b[0m \x1b[31mERROR\x1b[0m bind failed")
	if got := r.tail(1)[0]; got != "[stls-3] 2026-10-14T16:14:14Z ERROR bind failed" {
		t.Fatalf("got %q", got)
	}
}

func TestNftScriptPortHopping(t *testing.T) {
	ports := []Port{
		{InboundID: 3, Port: 443, Network: "udp", HopFrom: 20000, HopTo: 30000, Allow: []string{"127.0.0.0/8"}},
		{InboundID: 4, Port: 8443, Network: "udp"},
	}
	script := nftScript(expandPorts(ports), expandGuards(ports), expandHops(ports), nil)
	for _, want := range []string{
		"type nat hook prerouting priority -100;",
		"type nat hook output priority -100;",
		"fib daddr type local udp dport 20000-30000 redirect to :443",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("missing %q in:\n%s", want, script)
		}
	}
	if strings.Count(script, "redirect") != 2 {
		t.Fatalf("only the hopping inbound is redirected (once per hook):\n%s", script)
	}
	if strings.Contains(nftScript(expandPorts(ports[1:]), nil, expandHops(ports[1:]), nil), "hop_") {
		t.Fatal("no hop chains without hop ranges")
	}
	if _, err := exec.LookPath("nft"); err == nil && os.Geteuid() == 0 {
		cmd := exec.Command("nft", "-c", "-f", "-")
		cmd.Stdin = strings.NewReader(script)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("nft rejected the script: %v\n%s\n%s", err, out, script)
		}
	}

	bad := ApplyRequest{Type: "singbox", Ports: []Port{{InboundID: 1, Port: 25000, Network: "udp", HopFrom: 20000, HopTo: 30000}}}
	if err := bad.validatePorts(); err == nil {
		t.Fatal("hop range containing the inbound port must be rejected")
	}
}

func TestNftScriptUserMarks(t *testing.T) {
	ports := []Port{{InboundID: 1, Port: 443, Network: "tcp"}}
	users := sortedUsers([]int64{9, 7, 9, 0, 70000})
	if len(users) != 2 || users[0] != 7 {
		t.Fatalf("users: %v", users)
	}
	script := nftScript(expandPorts(ports), nil, nil, users)
	for _, want := range []string{
		"counter uup_7 {}", "counter udown_9 {}",
		"meta mark 0x46560007 ct mark set meta mark counter name uup_7",
		"ct mark 0x46560009 counter name udown_9",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("missing %q:\n%s", want, script)
		}
	}
	raw := `table inet flvx_kernel {
	counter uup_7 {
		packets 1 bytes 10
	}
	counter udown_7 {
		packets 2 bytes 20
	}
	counter in_1_tcp {
		packets 1 bytes 5
	}
}
`
	counters, us, err := parseCounters([]byte(raw))
	if err != nil || len(counters) != 1 || len(us) != 2 || us[0] != (UserCounter{ID: 7, Dir: "up", Bytes: 10}) || us[1].Dir != "down" {
		t.Fatalf("parse: %v %v %v", counters, us, err)
	}
	req := ApplyRequest{UserMarks: []int64{70000}}
	if err := req.validate(); err == nil {
		t.Fatal("expected an error")
	}
}

func TestResolveGitHubThroughPanelMirror(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		fmt.Fprintf(w, `{"tag_name":"v1.0.0","assets":[{"name":"tool-1.0.0","digest":"sha256:%s","browser_download_url":"https://github.com/o/r/releases/download/v1.0.0/tool-1.0.0"}]}`, strings.Repeat("a", 64))
	}))
	defer srv.Close()

	mirror := srv.URL + "/api/v1/open_api/gh"
	url, _, err := resolveGitHub(context.Background(), &GitHubRelease{Repo: "o/r", Asset: "tool-{VERSION}", Proxy: mirror}, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "/api/v1/open_api/gh/https://api.github.com/repos/o/r/releases/latest" {
		t.Fatalf("API looked up at %v", paths)
	}
	if url != mirror+"/https://github.com/o/r/releases/download/v1.0.0/tool-1.0.0" {
		t.Fatalf("download url %s", url)
	}
}

func TestRealmFixFileWithoutIPv6(t *testing.T) {
	orig := ipv6Sockets
	defer func() { ipv6Sockets = orig }()
	cfg := "[[endpoints]]\nlisten = \"[::]:31001\"\nremote = \"[2001:db8::1]:80\"\n"

	ipv6Sockets = func() bool { return true }
	if got := realmFixFile("config.toml", cfg); got != cfg {
		t.Fatalf("changed with IPv6: %q", got)
	}
	ipv6Sockets = func() bool { return false }
	want := "[[endpoints]]\nlisten = \"0.0.0.0:31001\"\nremote = \"[2001:db8::1]:80\"\n"
	if got := realmFixFile("config.toml", cfg); got != want {
		t.Fatalf("got %q", got)
	}
	if got := realmFixFile("other.txt", cfg); got != cfg {
		t.Fatalf("other files must stay: %q", got)
	}
}
