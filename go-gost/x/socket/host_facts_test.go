package socket

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHostFactsSentOnConnectAndOnChange(t *testing.T) {
	facts := HostFacts{OS: "Debian GNU/Linux 12 (bookworm)", Arch: "x86_64", Virtualization: "kvm"}
	calls := 0
	tr := &hostFactsTracker{collect: func() HostFacts { calls++; return facts }}
	t0 := time.Unix(1_800_000_000, 0)

	if got := tr.next(t0); got == nil || *got != facts {
		t.Fatalf("first report must carry the facts, got %+v", got)
	}
	if got := tr.next(t0.Add(5 * time.Second)); got != nil {
		t.Fatalf("unchanged facts must not be resent, got %+v", got)
	}
	if calls != 1 {
		t.Fatalf("facts must be collected once per recheck, collected %d times", calls)
	}

	// A new connection sends them again without collecting again.
	tr.reset()
	if got := tr.next(t0.Add(10 * time.Second)); got == nil {
		t.Fatal("a new connection's first report must carry the facts")
	}
	if calls != 1 {
		t.Fatalf("reconnecting must not recollect, collected %d times", calls)
	}

	// Rechecked after a minute: unchanged sends nothing, changed sends once.
	if got := tr.next(t0.Add(hostFactsRecheck)); got != nil || calls != 2 {
		t.Fatalf("unchanged recheck: got %+v after %d collections", got, calls)
	}
	facts.Kernel = "6.1.0-27-amd64"
	if got := tr.next(t0.Add(2 * hostFactsRecheck)); got == nil || got.Kernel != facts.Kernel {
		t.Fatalf("changed facts must be sent, got %+v", got)
	}
	if got := tr.next(t0.Add(2*hostFactsRecheck + 5*time.Second)); got != nil {
		t.Fatalf("changed facts must be sent once, got %+v", got)
	}

	var none *hostFactsTracker
	none.reset()
	if none.next(t0) != nil {
		t.Fatal("a reporter without a tracker sends no facts")
	}
}

func TestContainerKind(t *testing.T) {
	cases := []struct {
		name   string
		files  map[string]string
		mounts string
		want   string
	}{
		{"bare", nil, "", ""},
		{"openvz guest", map[string]string{"proc/vz/x": ""}, "", "openvz"},
		{"openvz host", map[string]string{"proc/vz/x": "", "proc/bc/x": ""}, "", ""},
		{"systemd container", map[string]string{"run/systemd/container": "lxc\n"}, "", "lxc"},
		{"docker", map[string]string{".dockerenv": ""}, "", "docker"},
		{"podman", map[string]string{"run/.containerenv": ""}, "", "podman"},
		{"lxcfs", nil, "lxcfs /proc/cpuinfo fuse.lxcfs rw 0 0\n", "lxc"},
		{"lxcfs elsewhere", nil, "lxcfs /var/lib/lxcfs fuse.lxcfs rw 0 0\n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			for name, body := range c.files {
				p := filepath.Join(root, name)
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if got := containerKind(root, c.mounts); got != c.want {
				t.Fatalf("containerKind = %q, want %q", got, c.want)
			}
		})
	}
}

func TestOSPrettyName(t *testing.T) {
	root := t.TempDir()
	if got := osPrettyName(root); got != "" {
		t.Fatalf("no os-release: %q", got)
	}
	_ = os.MkdirAll(filepath.Join(root, "usr/lib"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "usr/lib/os-release"), []byte("NAME=\"Alpine\"\nPRETTY_NAME='Alpine Linux v3.22'\n"), 0o644)
	if got := osPrettyName(root); got != "Alpine Linux v3.22" {
		t.Fatalf("usr/lib fallback: %q", got)
	}
	_ = os.MkdirAll(filepath.Join(root, "etc"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "etc/os-release"), []byte("PRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\"\n"), 0o644)
	if got := osPrettyName(root); got != "Debian GNU/Linux 12 (bookworm)" {
		t.Fatalf("etc first: %q", got)
	}
}

func TestCollectHostFactsAndSwapOnThisMachine(t *testing.T) {
	f := collectHostFacts()
	if f.Arch == "" || f.Kernel == "" || f.Virtualization == "" {
		t.Fatalf("facts on this machine: %+v", f)
	}
	if n := getProcessCount(); n <= 0 {
		t.Fatalf("process count = %d", n)
	}
	used, total := getSwapInfo()
	if used > total {
		t.Fatalf("swap used %d > total %d", used, total)
	}
	t.Logf("facts %+v, procs %d, swap %d/%d", f, getProcessCount(), used, total)
}
