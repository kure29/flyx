package socket

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// fakeProcSys builds a /proc/sys stand-in holding every tunable key (minus
// skip) at a stock value, and points the tuning paths at it.
func fakeProcSys(t *testing.T, available string, skip ...string) string {
	t.Helper()
	oldPageSize := tunePageSize
	tunePageSize = 4096
	t.Cleanup(func() { tunePageSize = oldPageSize })
	dir := t.TempDir()
	oldProc, oldConf, oldBackup, oldModprobe := tuneProcRoot, tuneConfPath, tuneBackupPath, tuneModprobe
	oldParams, oldMem, oldBBR := tuneParamsPath, tuneMeminfo, tuneBBRModule
	tuneProcRoot = filepath.Join(dir, "proc")
	tuneConfPath = filepath.Join(dir, "sysctl.d", "99-flyx-tuning.conf")
	tuneBackupPath = filepath.Join(dir, "agent", "tuning-backup.json")
	tuneParamsPath = filepath.Join(dir, "agent", "tuning-params.json")
	tuneMeminfo = filepath.Join(dir, "meminfo")
	tuneBBRModule = filepath.Join(dir, "modules-load.d", "flyx-bbr.conf")
	tuneModprobe = func(string) error { return nil }
	t.Cleanup(func() {
		tuneProcRoot, tuneConfPath, tuneBackupPath, tuneModprobe = oldProc, oldConf, oldBackup, oldModprobe
		tuneParamsPath, tuneMeminfo, tuneBBRModule = oldParams, oldMem, oldBBR
	})
	// 2 GiB of memory.
	if err := os.WriteFile(tuneMeminfo, []byte("MemTotal:        2097152 kB\nMemFree:  1000 kB\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Dir(tuneConfPath), 0o755)

	skipped := map[string]bool{}
	for _, k := range skip {
		skipped[k] = true
	}
	write := func(key, value string) {
		p := sysctlPath(key)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(value+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, g := range tuneGroupsFor(tuneSizesFor(defaultTuneParams, 2048)) {
		for _, s := range g.Settings {
			if !skipped[s.Key] {
				write(s.Key, "0")
			}
		}
	}
	write("net.ipv4.tcp_congestion_control", "cubic")
	write("net.ipv4.tcp_available_congestion_control", available)
	write("net.ipv4.tcp_rmem", "4096\t131072\t6291456")
	write("kernel.osrelease", "6.1.0-test")
	return dir
}

func TestTuneApplyAndRestore(t *testing.T) {
	fakeProcSys(t, "reno cubic bbr", "net.netfilter.nf_conntrack_max")

	st, err := applyTuning([]string{"bbr", "buffer", "backlog"}, defaultTuneParams)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := readSysctl("net.ipv4.tcp_congestion_control"); v != "bbr" {
		t.Fatalf("congestion: %q", v)
	}
	// 1000 Mbps × 150 ms: 2 × 18.75 MB + 2 MiB, under the 64 MiB memory cap.
	if v, _ := readSysctl("net.ipv4.tcp_rmem"); v != "4096 1048576 39597152" {
		t.Fatalf("tcp_rmem: %q", v)
	}
	if v, _ := readSysctl("net.ipv4.tcp_mem"); v != "32768 65536 131072" {
		t.Fatalf("tcp_mem: %q", v)
	}
	if b, _ := os.ReadFile(tuneBBRModule); string(b) != "tcp_bbr\n" {
		t.Fatalf("bbr module not loaded at boot: %q", b)
	}
	conf, _ := os.ReadFile(tuneConfPath)
	if !strings.Contains(string(conf), "net.ipv4.tcp_congestion_control = bbr") || strings.Contains(string(conf), "nf_conntrack_max") {
		t.Fatalf("conf:\n%s", conf)
	}
	applied := map[string]bool{}
	for _, g := range st.Groups {
		applied[g.ID] = g.Applied
	}
	if !applied["bbr"] || !applied["buffer"] || !applied["backlog"] || applied["tcp"] || !st.HasBackup {
		t.Fatalf("status: %+v", st)
	}

	// Unchecking a group puts its keys back; the rest stay tuned.
	if _, err := applyTuning([]string{"buffer"}, defaultTuneParams); err != nil {
		t.Fatal(err)
	}
	if v, _ := readSysctl("net.ipv4.tcp_congestion_control"); v != "cubic" {
		t.Fatalf("bbr not restored: %q", v)
	}
	if _, err := os.Stat(tuneBBRModule); !os.IsNotExist(err) {
		t.Fatal("bbr module file left behind")
	}
	if v, _ := readSysctl("net.core.rmem_max"); v != "39597152" {
		t.Fatalf("buffer lost: %q", v)
	}

	// Another line resizes the tuned buffers, even downwards.
	if _, err := applyTuning([]string{"buffer"}, tuneParams{BandwidthMbps: 100, RTTMs: 50}); err != nil {
		t.Fatal(err)
	}
	if v, _ := readSysctl("net.core.rmem_max"); v != "4194304" {
		t.Fatalf("small line: %q", v)
	}
	if savedTuneParams() != (tuneParams{BandwidthMbps: 100, RTTMs: 50}) {
		t.Fatalf("params not kept: %+v", savedTuneParams())
	}

	// Restore puts back the values found before the first apply.
	st, err = applyTuning(nil, defaultTuneParams)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := readSysctl("net.ipv4.tcp_rmem"); v != "4096 131072 6291456" {
		t.Fatalf("tcp_rmem not restored: %q", v)
	}
	if _, err := os.Stat(tuneConfPath); !os.IsNotExist(err) {
		t.Fatal("conf file left behind")
	}
	if _, err := os.Stat(tuneBackupPath); !os.IsNotExist(err) || st.HasBackup {
		t.Fatal("backup left behind")
	}
	if _, err := os.Stat(tuneParamsPath); !os.IsNotExist(err) {
		t.Fatal("params left behind")
	}
}

func TestTuneSkipsBBRWithoutKernelSupport(t *testing.T) {
	fakeProcSys(t, "reno cubic")
	st, err := applyTuning([]string{"bbr", "forward"}, defaultTuneParams)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := readSysctl("net.ipv4.tcp_congestion_control"); v != "cubic" {
		t.Fatalf("bbr set anyway: %q", v)
	}
	if v, _ := readSysctl("net.ipv4.ip_forward"); v != "1" {
		t.Fatalf("forward: %q", v)
	}
	if len(st.Warnings) != 1 || !strings.Contains(st.Warnings[0], "BBR") {
		t.Fatalf("warnings: %v", st.Warnings)
	}
}

func TestTuneCommandRejectsUnknownGroup(t *testing.T) {
	fakeProcSys(t, "cubic bbr")
	if _, err := handleTuneCommand("TuneApply", map[string]interface{}{"groups": []string{"rm-rf"}}); err == nil {
		t.Fatal("expected an error")
	}
	out, err := handleTuneCommand("TuneStatus", nil)
	st := out.(tuneStatus)
	if err != nil || st.Kernel != "6.1.0-test" || len(st.Groups) != 6 || st.Params != defaultTuneParams || st.Sizes.RAMMB != 2048 {
		t.Fatalf("status: %+v %v", out, err)
	}
	// The targets follow the line given.
	out, _ = handleTuneCommand("TuneStatus", map[string]interface{}{"bandwidthMbps": 100, "rttMs": 50})
	if s := out.(tuneStatus).Sizes; s.BufMax != 4*mib || s.Reason != "最低 4MB" {
		t.Fatalf("sizes: %+v", s)
	}
	if _, err := handleTuneCommand("TuneStatus", map[string]interface{}{"bandwidthMbps": 0, "rttMs": 5000}); err == nil {
		t.Fatal("an out-of-range line must be rejected")
	}
}

func TestTuneNeverLowersLimits(t *testing.T) {
	fakeProcSys(t, "cubic bbr")
	_ = writeSysctl("fs.file-max", "9999999")
	_ = writeSysctl("net.ipv4.tcp_rmem", "4096 131072 134217728")

	st, err := applyTuning([]string{"files", "buffer"}, defaultTuneParams)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := readSysctl("fs.file-max"); v != "9999999" {
		t.Fatalf("file-max lowered to %q", v)
	}
	if v, _ := readSysctl("net.ipv4.tcp_rmem"); v != "4096 131072 134217728" {
		t.Fatalf("tcp_rmem lowered to %q", v)
	}
	if v, _ := readSysctl("fs.nr_open"); v != "1048576" {
		t.Fatalf("nr_open not raised: %q", v)
	}
	conf, _ := os.ReadFile(tuneConfPath)
	if strings.Contains(string(conf), "file-max") {
		t.Fatalf("conf pins a value the node already beat:\n%s", conf)
	}
	for _, g := range st.Groups {
		if g.ID == "files" && !g.Applied {
			t.Fatalf("files should count as applied: %+v", g)
		}
	}
	// Restoring leaves the untouched key alone.
	if _, err := applyTuning(nil, defaultTuneParams); err != nil {
		t.Fatal(err)
	}
	if v, _ := readSysctl("fs.file-max"); v != "9999999" {
		t.Fatalf("file-max changed on restore: %q", v)
	}
}

func TestTuneLeavesFailedSettingsOutOfConf(t *testing.T) {
	fakeProcSys(t, "cubic bbr")
	// A kernel without the fq qdisc answers ENOENT when it is written.
	tuneWrite = func(key, value string) error {
		if key == "net.core.default_qdisc" {
			return &os.PathError{Op: "write", Path: key, Err: syscall.ENOENT}
		}
		return writeSysctl(key, value)
	}
	t.Cleanup(func() { tuneWrite = writeSysctl })

	st, err := applyTuning([]string{"bbr"}, defaultTuneParams)
	if err != nil {
		t.Fatal(err)
	}
	conf, _ := os.ReadFile(tuneConfPath)
	if strings.Contains(string(conf), "default_qdisc") || !strings.Contains(string(conf), "tcp_congestion_control = bbr") {
		t.Fatalf("conf:\n%s", conf)
	}
	if len(st.Warnings) != 1 || !strings.Contains(st.Warnings[0], "default_qdisc") {
		t.Fatalf("warnings: %v", st.Warnings)
	}
}

func TestTuneSizes(t *testing.T) {
	oldPageSize := tunePageSize
	tunePageSize = 4096
	t.Cleanup(func() { tunePageSize = oldPageSize })
	for _, c := range []struct {
		name   string
		p      tuneParams
		ram    int64
		max    int64
		reason string
		tcpMem string
	}{
		// 2 × 18.75 MB + 2 MiB.
		{"gigabit cross-border", tuneParams{1000, 150}, 4096, 39597152, "带宽 × 延迟 × 2 + 2MB", "65536 131072 262144"},
		// 512 MB: a socket gets at most 1/32 of the memory.
		{"small VPS", tuneParams{1000, 150}, 512, 16 * mib, "受内存限制", "8192 16384 32768"},
		{"slow line", tuneParams{10, 20}, 2048, 4 * mib, "最低 4MB", "32768 65536 131072"},
		// 2 × 250 MB + 2 MiB, over the 256 MiB ceiling.
		{"10G long-haul", tuneParams{10000, 200}, 65536, 256 * mib, "受内存限制", "1048576 2097152 4194304"},
		// The tcp_mem floors on a tiny machine.
		{"tiny", tuneParams{100, 100}, 128, 4 * mib, "受内存限制", "4096 8192 16384"},
	} {
		s := tuneSizesFor(c.p, c.ram)
		if s.BufMax != c.max || s.Reason != c.reason || s.TCPMem != c.tcpMem || s.BufDef != mib {
			t.Errorf("%s: %+v", c.name, s)
		}
	}
}
