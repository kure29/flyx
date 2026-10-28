package socket

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/go-gost/x/identity"
)

// Node network tuning (节点调优). TuneStatus reports the current kernel
// parameters next to the recommended ones; TuneApply writes the chosen
// groups to a sysctl.d file (so they survive reboots) and applies them now.
// The value each key had before the first apply is kept, so unchecking a
// group, or TuneApply with restore, puts the original value back.

type tuneSetting struct {
	Key   string
	Value string
	// AtLeast marks limits that are only ever raised: a node already above
	// the recommendation keeps its value (compared on the last number, so
	// "4096 131072 67108864" goes by its maximum).
	AtLeast bool
}

type tuneGroup struct {
	ID       string
	Name     string
	Desc     string
	Settings []tuneSetting
}

// tuneParams describe the node's line: buffers are sized for its
// bandwidth-delay product, within what its memory allows.
type tuneParams struct {
	BandwidthMbps int `json:"bandwidthMbps"`
	RTTMs         int `json:"rttMs"`
}

// Only for compatibility with older panels. Automatic jobs always measure
// bandwidth before applying any parameters; they never use this fallback.
var defaultTuneParams = tuneParams{BandwidthMbps: 1000, RTTMs: 150}

func (p tuneParams) valid() bool {
	return p.BandwidthMbps >= 1 && p.BandwidthMbps <= 100000 && p.RTTMs >= 1 && p.RTTMs <= 2000
}

const mib = 1 << 20

// tuneSizes are the values derived from the line and the memory.
type tuneSizes struct {
	RAMMB  int64  `json:"ramMb"`
	BDP    int64  `json:"bdp"`
	BufMax int64  `json:"bufMax"`
	BufDef int64  `json:"bufDefault"`
	Reason string `json:"bufReason"`
	TCPMem string `json:"tcpMem"`
}

// tuneSizesFor sizes the buffers (the approach of tcpfit):
//   - per-socket maximum 2×BDP + 2 MiB, at least 4 MiB, and at most 1/32
//     of the memory (1/8 of the TCP budget below, so eight full-speed
//     flows fit) and 256 MiB;
//   - tcp_mem, the kernel's hard TCP budget, at 1/16, 1/8 and 1/4 of the
//     memory, so a small VPS cannot be run out of memory by sockets;
//   - a 1 MiB starting buffer: a proxy carries many connections, so they
//     start modest and grow when the line is fast.
func tuneSizesFor(p tuneParams, ramMB int64) tuneSizes {
	s := tuneSizes{RAMMB: ramMB, BufDef: mib}
	s.BDP = int64(p.BandwidthMbps) * 1_000_000 / 8 * int64(p.RTTMs) / 1000
	s.BufMax, s.Reason = 2*s.BDP+2*mib, "带宽 × 延迟 × 2 + 2MB"
	budget := ramMB * mib / 32
	if ramMB <= 0 {
		budget = 4 * mib // Missing memory information must never remove the cap.
	}
	if budget > 256*mib {
		budget = 256 * mib
	}
	if s.BufMax > budget {
		s.BufMax, s.Reason = budget, "受内存限制"
	}
	if s.BufMax < 4*mib {
		s.BufMax, s.Reason = 4*mib, "最低 4MB"
	}
	pages := ramMB * mib / tunePageSize
	low, pres, max := pages/16, pages/8, pages/4
	if low < 16*mib/tunePageSize {
		low = 16 * mib / tunePageSize
	}
	if pres < 32*mib/tunePageSize {
		pres = 32 * mib / tunePageSize
	}
	if max < 64*mib/tunePageSize {
		max = 64 * mib / tunePageSize
	}
	s.TCPMem = fmt.Sprintf("%d %d %d", low, pres, max)
	return s
}

func tuneGroupsFor(s tuneSizes) []tuneGroup {
	bufMax, bufDef := strconv.FormatInt(s.BufMax, 10), strconv.FormatInt(s.BufDef, 10)
	return []tuneGroup{
		{"bbr", "BBR 拥塞控制", "高延迟、有丢包的线路上明显提速（需要内核 4.9+）", []tuneSetting{
			{"net.core.default_qdisc", "fq", false},
			{"net.ipv4.tcp_congestion_control", "bbr", false},
		}},
		{"buffer", "按带宽和延迟设缓冲区", "单连接跑满线路；按内存限定上限，小内存机器不会被连接占满", []tuneSetting{
			{"net.core.rmem_max", bufMax, true},
			{"net.core.wmem_max", bufMax, true},
			{"net.core.rmem_default", bufDef, false},
			{"net.core.wmem_default", bufDef, false},
			{"net.ipv4.tcp_rmem", "4096 " + bufDef + " " + bufMax, true},
			{"net.ipv4.tcp_wmem", "4096 " + bufDef + " " + bufMax, true},
			{"net.ipv4.tcp_mem", s.TCPMem, true},
			{"net.ipv4.tcp_window_scaling", "1", false},
			{"net.ipv4.tcp_moderate_rcvbuf", "1", false},
			{"net.ipv4.tcp_adv_win_scale", "1", false},
			{"net.ipv4.udp_rmem_min", "16384", true},
			{"net.ipv4.udp_wmem_min", "16384", true},
		}},
		{"tcp", "TCP 连接优化", "Fast Open、空闲后不降速、自动探测 MTU、更快回收连接", []tuneSetting{
			{"net.ipv4.tcp_fastopen", "3", false},
			{"net.ipv4.tcp_slow_start_after_idle", "0", false},
			{"net.ipv4.tcp_mtu_probing", "1", false},
			{"net.ipv4.tcp_fin_timeout", "15", false},
			{"net.ipv4.tcp_tw_reuse", "1", false},
			{"net.ipv4.tcp_keepalive_time", "600", false},
			{"net.ipv4.tcp_syncookies", "1", false},
			{"net.ipv4.tcp_no_metrics_save", "0", false},
			{"net.ipv4.tcp_sack", "1", false},
			{"net.ipv4.tcp_dsack", "1", false},
			{"net.ipv4.tcp_timestamps", "1", false},
		}},
		{"backlog", "高并发", "连接多时不丢新连接，中转出站可用的端口更多", []tuneSetting{
			{"net.core.somaxconn", "32768", true},
			{"net.core.netdev_max_backlog", "16384", true},
			{"net.core.netdev_budget", "600", true},
			{"net.core.optmem_max", "65536", true},
			{"net.ipv4.tcp_max_syn_backlog", "16384", true},
			{"net.netfilter.nf_conntrack_max", "262144", true},
			// From 10000: well clear of the usual service ports.
			{"net.ipv4.ip_local_port_range", "10000 65535", false},
		}},
		{"forward", "IPv4 转发", "内核转发（nftables 端口转发）需要", []tuneSetting{
			{"net.ipv4.ip_forward", "1", false},
		}},
		{"files", "文件句柄上限", "系统允许打开更多连接和文件", []tuneSetting{
			{"fs.file-max", "1048576", true},
			{"fs.nr_open", "1048576", true},
		}},
	}
}

// Keys an older version set and this one no longer does (such as
// tcp_notsent_lowat, which throttled low-core machines) are in the backup
// and get their original value back on the next apply, like any key that
// is no longer chosen.
// Paths are variables so tests can use a temporary tree.
var (
	tunePageSize   = int64(os.Getpagesize())
	tuneProcRoot   = "/proc/sys"
	tuneConfPath   = "/etc/sysctl.d/99-flyx-tuning.conf"
	tuneBackupPath = filepath.Join(identity.DataDir(), "tuning-backup.json")
	tuneParamsPath = filepath.Join(identity.DataDir(), "tuning-params.json")
	tuneMeminfo    = "/proc/meminfo"
	// tuneBBRModule loads tcp_bbr at boot, before sysctl sets it.
	tuneBBRModule = "/etc/modules-load.d/flyx-bbr.conf"
	tuneModprobe  = func(module string) error { return exec.Command("modprobe", module).Run() }
	tuneWrite     = writeSysctl
)

func readRAMMB() int64 {
	b, err := os.ReadFile(tuneMeminfo)
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if f := strings.Fields(line); len(f) >= 2 && f[0] == "MemTotal:" {
			kb, _ := strconv.ParseInt(f[1], 10, 64)
			return kb / 1024
		}
	}
	return 0
}

// savedTuneParams are the line parameters of the last apply, or defaults.
func savedTuneParams() tuneParams {
	var p tuneParams
	if b, err := os.ReadFile(tuneParamsPath); err == nil && json.Unmarshal(b, &p) == nil && p.valid() {
		return p
	}
	return defaultTuneParams
}

func currentTuneGroups(p tuneParams) []tuneGroup {
	return tuneGroupsFor(tuneSizesFor(p, readRAMMB()))
}

// writeFileAtomic replaces path in one step, so a full disk or a crash
// never leaves a half-written file behind.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(name, mode)
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		_ = os.Remove(name)
	}
	return err
}

type tuneSettingStatus struct {
	Key       string `json:"key"`
	Current   string `json:"current"`
	Target    string `json:"target"`
	Supported bool   `json:"supported"`
	OK        bool   `json:"ok"` // already meets the recommendation
}

type tuneGroupStatus struct {
	ID       string              `json:"id"`
	Name     string              `json:"name"`
	Desc     string              `json:"desc"`
	Applied  bool                `json:"applied"`
	Settings []tuneSettingStatus `json:"settings"`
}

type tuneStatus struct {
	Automatic  bool              `json:"automatic"`
	Detection  *tuneDetection    `json:"detection,omitempty"`
	Job        *tuneJob          `json:"job,omitempty"`
	Params     tuneParams        `json:"params"`
	Sizes      tuneSizes         `json:"sizes"`
	Kernel     string            `json:"kernel"`
	Congestion string            `json:"congestion"`
	Available  string            `json:"available"`
	HasBackup  bool              `json:"hasBackup"`
	Groups     []tuneGroupStatus `json:"groups"`
	Warnings   []string          `json:"warnings,omitempty"`
}

// handleTuneCommand runs TuneStatus / TuneApply. Both take the line's
// bandwidth and RTT (the targets depend on them); without them the last
// applied ones, or the defaults, are used.
func handleTuneCommand(cmdType string, data interface{}) (interface{}, error) {
	var req struct {
		Groups       []string `json:"groups"`
		Restore      bool     `json:"restore"`
		Mode         string   `json:"mode"`
		MaxTrafficGB float64  `json:"maxTrafficGB"`
		tuneParams
	}
	if data != nil {
		raw, err := json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("序列化数据失败: %v", err)
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, fmt.Errorf("解析调优参数失败: %v", err)
		}
	}
	p := req.tuneParams
	if p.BandwidthMbps == 0 && p.RTTMs == 0 {
		p = savedTuneParams()
	} else if !p.valid() {
		return nil, fmt.Errorf("带宽需在 1–100000 Mbps，延迟需在 1–2000 ms")
	}
	switch cmdType {
	case "TuneStart":
		return startTuneJob(req.Mode, req.MaxTrafficGB)
	case "TuneCancel":
		cancelTuneJob()
		return readTuneStatus(p), nil
	case "TuneStatus":
		return readTuneStatus(p), nil
	case "TuneApply":
		if !tuningOperation.TryLock() {
			return nil, fmt.Errorf("已有调优任务正在运行")
		}
		defer tuningOperation.Unlock()
		if req.Restore {
			req.Groups = nil
		}
		return applyTuning(req.Groups, p)
	}
	return nil, fmt.Errorf("未知命令类型: %s", cmdType)
}

func sysctlPath(key string) string {
	return filepath.Join(tuneProcRoot, strings.ReplaceAll(key, ".", "/"))
}

// satisfied reports whether cur already meets s.
func (s tuneSetting) satisfied(cur string) bool {
	if cur == s.Value {
		return true
	}
	if !s.AtLeast {
		return false
	}
	c, err1 := lastNumber(cur)
	t, err2 := lastNumber(s.Value)
	return err1 == nil && err2 == nil && c >= t
}

func lastNumber(v string) (uint64, error) {
	f := strings.Fields(v)
	if len(f) == 0 {
		return 0, fmt.Errorf("empty")
	}
	return strconv.ParseUint(f[len(f)-1], 10, 64)
}

func readSysctl(key string) (string, bool) {
	b, err := os.ReadFile(sysctlPath(key))
	if err != nil {
		return "", false
	}
	return strings.Join(strings.Fields(string(b)), " "), true
}

func writeSysctl(key, value string) error {
	return os.WriteFile(sysctlPath(key), []byte(value+"\n"), 0o644)
}

func readTuneStatus(p tuneParams) tuneStatus {
	job := readTuneJob()
	st := tuneStatus{Automatic: true, Detection: loadTuneDetection(), Job: job, Params: p, Sizes: tuneSizesFor(p, readRAMMB())}
	if b, err := os.ReadFile(filepath.Join(tuneProcRoot, "kernel/osrelease")); err == nil {
		st.Kernel = strings.TrimSpace(string(b))
	}
	st.Congestion, _ = readSysctl("net.ipv4.tcp_congestion_control")
	st.Available, _ = readSysctl("net.ipv4.tcp_available_congestion_control")
	_, err := os.Stat(tuneBackupPath)
	st.HasBackup = err == nil
	for _, g := range tuneGroupsFor(st.Sizes) {
		gs := tuneGroupStatus{ID: g.ID, Name: g.Name, Desc: g.Desc, Applied: true}
		any := false
		for _, s := range g.Settings {
			cur, ok := readSysctl(s.Key)
			gs.Settings = append(gs.Settings, tuneSettingStatus{Key: s.Key, Current: cur, Target: s.Value, Supported: ok, OK: ok && s.satisfied(cur)})
			if ok {
				any = true
				if !s.satisfied(cur) {
					gs.Applied = false
				}
			}
		}
		gs.Applied = gs.Applied && any
		st.Groups = append(st.Groups, gs)
	}
	return st
}

func loadTuneBackup() map[string]string {
	backup := map[string]string{}
	if b, err := os.ReadFile(tuneBackupPath); err == nil {
		_ = json.Unmarshal(b, &backup)
	}
	return backup
}

// applyTuning makes the chosen groups the tuned set: their keys get the
// recommended values, keys tuned before but no longer chosen get their
// original value back. An empty choice restores everything.
func applyTuning(groupIDs []string, p tuneParams) (tuneStatus, error) {
	groups := currentTuneGroups(p)
	chosen := map[string]bool{}
	for _, id := range groupIDs {
		found := false
		for _, g := range groups {
			if g.ID == id {
				found = true
			}
		}
		if !found {
			return tuneStatus{}, fmt.Errorf("未知调优项: %s", id)
		}
		chosen[id] = true
	}

	var warnings []string
	if InContainer() && len(chosen) > 0 {
		// /etc/sysctl.d is the container's, not the host's.
		warnings = append(warnings, "节点运行在 Docker 中：调优需要容器以 --privileged 运行，宿主机重启后需在面板重新应用")
	}
	if chosen["bbr"] {
		if avail, _ := readSysctl("net.ipv4.tcp_available_congestion_control"); !strings.Contains(" "+avail+" ", " bbr ") {
			_ = tuneModprobe("tcp_bbr")
			if avail, _ = readSysctl("net.ipv4.tcp_available_congestion_control"); !strings.Contains(" "+avail+" ", " bbr ") {
				warnings = append(warnings, "内核不支持 BBR（需要 4.9 以上并带 tcp_bbr 模块），已跳过")
				delete(chosen, "bbr")
			}
		}
	}

	backup := map[string]string{}
	if b, err := os.ReadFile(tuneBackupPath); err == nil {
		if err = json.Unmarshal(b, &backup); err != nil {
			return tuneStatus{}, fmt.Errorf("原参数备份已损坏，已停止以保留恢复信息：%w", err)
		}
		if backup == nil {
			return tuneStatus{}, fmt.Errorf("原参数备份为空，已停止调优以保留恢复信息")
		}
	} else if !os.IsNotExist(err) {
		return tuneStatus{}, err
	}
	want := map[string]string{}
	var confKeys []string
	for _, g := range groups {
		if !chosen[g.ID] {
			continue
		}
		for _, s := range g.Settings {
			cur, ok := readSysctl(s.Key)
			if !ok {
				continue // not on this kernel (e.g. conntrack not loaded)
			}
			_, saved := backup[s.Key]
			if !saved && s.AtLeast && s.satisfied(cur) {
				continue // already above the recommendation: leave it alone
			}
			if !saved {
				backup[s.Key] = cur
			}
			want[s.Key] = s.Value
			confKeys = append(confKeys, s.Key)
		}
	}

	keys := make([]string, 0, len(backup))
	// Persist original values before touching /proc. A failed write or agent
	// restart must not destroy the only copy needed for rollback.
	if err := os.MkdirAll(filepath.Dir(tuneBackupPath), 0o700); err != nil {
		return tuneStatus{}, err
	}
	if len(backup) > 0 {
		b, err := json.MarshalIndent(backup, "", "  ")
		if err != nil {
			return tuneStatus{}, err
		}
		if err = writeFileAtomic(tuneBackupPath, b, 0o600); err != nil {
			return tuneStatus{}, err
		}
	}
	for k := range backup {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	failed := map[string]bool{}
	for _, k := range keys {
		value, tuned := want[k]
		if !tuned {
			value = backup[k] // no longer chosen: back to the original
		}
		if err := tuneWrite(k, value); err != nil {
			if os.IsNotExist(err) || errors.Is(err, syscall.EINVAL) {
				warnings = append(warnings, fmt.Sprintf("%s 不支持 %s，已跳过", k, value))
			} else {
				warnings = append(warnings, fmt.Sprintf("%s 设置失败: %v", k, err))
			}
			if tuned {
				failed[k] = true
				// Retain the original: this key may have been tuned by an
				// earlier apply, even if the current write failed.
			}
			continue
		}
		if !tuned {
			delete(backup, k)
		}
	}

	// Only what took effect goes into the file applied at boot.
	var conf []string
	for _, k := range confKeys {
		if !failed[k] {
			conf = append(conf, k+" = "+want[k])
		}
	}
	if err := os.MkdirAll(filepath.Dir(tuneBackupPath), 0o755); err != nil {
		return tuneStatus{}, err
	}
	if len(conf) > 0 {
		body := fmt.Sprintf("# Written by the FLYX panel (节点调优); remove via the panel to restore.\n# 带宽 %d Mbps, 延迟 %d ms\n%s\n",
			p.BandwidthMbps, p.RTTMs, strings.Join(conf, "\n"))
		if err := writeFileAtomic(tuneConfPath, []byte(body), 0o644); err != nil {
			return tuneStatus{}, fmt.Errorf("写入 %s 失败: %v", tuneConfPath, err)
		}
	} else if err := os.Remove(tuneConfPath); err != nil && !os.IsNotExist(err) {
		return tuneStatus{}, err
	}
	if chosen["bbr"] && !failed["net.ipv4.tcp_congestion_control"] {
		if err := os.MkdirAll(filepath.Dir(tuneBBRModule), 0o755); err == nil {
			_ = writeFileAtomic(tuneBBRModule, []byte("tcp_bbr\n"), 0o644)
		}
	} else {
		_ = os.Remove(tuneBBRModule)
	}
	if len(chosen) > 0 {
		b, _ := json.Marshal(p)
		_ = writeFileAtomic(tuneParamsPath, b, 0o644)
	} else {
		_ = os.Remove(tuneParamsPath)
	}

	if len(backup) > 0 {
		b, _ := json.MarshalIndent(backup, "", "  ")
		if err := writeFileAtomic(tuneBackupPath, b, 0o600); err != nil {
			return tuneStatus{}, err
		}
	} else if err := os.Remove(tuneBackupPath); err != nil && !os.IsNotExist(err) {
		return tuneStatus{}, err
	}

	st := readTuneStatus(p)
	st.Warnings = warnings
	return st, nil
}
