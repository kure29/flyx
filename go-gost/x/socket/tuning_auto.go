package socket

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed tcpfit/*.sh tcpfit/LICENSE
var tuneEngine embed.FS

type tuneSample struct {
	Mbps        float64 `json:"mbps"`
	Retransmits int64   `json:"retransmits"`
	LossPercent float64 `json:"lossPercent"`
}

type tuneDetection struct {
	Warnings        []string    `json:"warnings,omitempty"`
	BandwidthMbps   int         `json:"bandwidthMbps"`
	BandwidthSource string      `json:"bandwidthSource"`
	RTTSource       string      `json:"rttSource"`
	Interface       string      `json:"interface"`
	Peer            string      `json:"peer"`
	Port            int         `json:"port"`
	Family          string      `json:"family"`
	MeasuredAt      int64       `json:"measuredAt"`
	Before          *tuneSample `json:"before,omitempty"`
	Single          *tuneSample `json:"single,omitempty"`
	Parallel        *tuneSample `json:"parallel,omitempty"`
	Applied         bool        `json:"applied"`
	ShapeMbps       int         `json:"shapeMbps"`
	DeepSummary     string      `json:"deepSummary,omitempty"`
	EstimatedGB     float64     `json:"estimatedGB"`
}

type tuneJob struct {
	ID         string `json:"id"`
	Mode       string `json:"mode"`
	State      string `json:"state"`
	Stage      string `json:"stage"`
	Percent    int    `json:"percent"`
	Message    string `json:"message"`
	StartedAt  int64  `json:"startedAt"`
	FinishedAt int64  `json:"finishedAt,omitempty"`
	Logs       string `json:"logs,omitempty"`
}

var tuningTasks = struct {
	sync.Mutex
	job    *tuneJob
	cancel context.CancelFunc
	path   string
}{}

// Tuning operations are serialized, including requests from old panels.
var tuningOperation sync.Mutex
var tuneAutoExecute = executeTuneJob
var tunePlatform = runtime.GOOS
var tuneCommand = runTuneCommand
var tunePrepare = prepareTuneEngine
var tuneFindPeers = findTunePeers
var tuneMeasure = measureTuneSample

func tuneStatePath(name string) string { return filepath.Join(filepath.Dir(tuneBackupPath), name) }
func tuneEngineDir() string            { return tuneStatePath("tcpfit") }

func saveTuneJSON(path string, v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeFileAtomic(path, b, 0o600)
}

func loadTuneDetection() *tuneDetection {
	var d tuneDetection
	b, err := os.ReadFile(tuneStatePath("tuning-result.json"))
	if err != nil || json.Unmarshal(b, &d) != nil {
		return nil
	}
	return &d
}

func readTuneJob() *tuneJob {
	tuningTasks.Lock()
	if tuningTasks.path == tuneStatePath("tuning-job.json") && tuningTasks.job != nil {
		j := *tuningTasks.job
		tuningTasks.Unlock()
		return &j
	}
	tuningTasks.Unlock()
	var j tuneJob
	b, err := os.ReadFile(tuneStatePath("tuning-job.json"))
	if err != nil || json.Unmarshal(b, &j) != nil {
		return nil
	}
	if j.State == "running" {
		j.State, j.Message, j.FinishedAt = "failed", "Agent 已重启，任务中断；可重新调优或恢复原配置", time.Now().UnixMilli()
		if tuningOperation.TryLock() {
			if err := recoverTuneTransaction(); err != nil {
				j.Message += "；自动恢复失败：" + err.Error()
			}
			tuningOperation.Unlock()
		}
		_ = saveTuneJSON(tuneStatePath("tuning-job.json"), j)
	}
	return &j
}

func startTuneJob(mode string, maxGB float64) (tuneStatus, error) {
	if mode != "auto" && mode != "deep" && mode != "restore" {
		return tuneStatus{}, fmt.Errorf("未知调优模式")
	}
	if tunePlatform != "linux" {
		return tuneStatus{}, fmt.Errorf("自动调优仅支持 Linux")
	}
	if !tuningOperation.TryLock() {
		return tuneStatus{}, fmt.Errorf("已有调优任务正在运行")
	}
	// Restoring replays the original queues itself, so a stuck transaction
	// must not block it; it is cleared once the restore succeeds.
	if err := recoverTuneTransaction(); err != nil && mode != "restore" {
		tuningOperation.Unlock()
		return tuneStatus{}, fmt.Errorf("先前任务的队列尚未恢复，请先恢复原配置：%w", err)
	}
	if mode == "deep" {
		d := loadTuneDetection()
		if d == nil || !d.Applied || time.Now().UnixMilli()-d.MeasuredAt > int64(24*time.Hour/time.Millisecond) {
			tuningOperation.Unlock()
			return tuneStatus{}, fmt.Errorf("请先完成自动调优，24 小时内的测速结果才能用于深度优化")
		}
		if math.IsNaN(maxGB) || math.IsInf(maxGB, 0) || maxGB < d.EstimatedGB || maxGB > 1000 {
			tuningOperation.Unlock()
			return tuneStatus{}, fmt.Errorf("请先确认本次深度优化的预计流量（约 %.1f GB）", d.EstimatedGB)
		}
	}
	var token [12]byte
	if _, err := rand.Read(token[:]); err != nil {
		tuningOperation.Unlock()
		return tuneStatus{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	j := &tuneJob{ID: hex.EncodeToString(token[:]), Mode: mode, State: "running", Stage: "preparing", Percent: 2, Message: "准备调优环境", StartedAt: time.Now().UnixMilli()}
	path := tuneStatePath("tuning-job.json")
	if err := saveTuneJSON(path, j); err != nil {
		cancel()
		tuningOperation.Unlock()
		return tuneStatus{}, err
	}
	tuningTasks.Lock()
	tuningTasks.job, tuningTasks.cancel, tuningTasks.path = j, cancel, path
	tuningTasks.Unlock()
	go func() {
		defer tuningOperation.Unlock()
		defer cancel()
		err := tuneAutoExecute(ctx, mode, maxGB)
		tuningTasks.Lock()
		defer tuningTasks.Unlock()
		j.FinishedAt = time.Now().UnixMilli()
		j.State, j.Stage, j.Message, j.Percent = "succeeded", "done", "调优完成", 100
		if mode == "restore" {
			j.Message = "已恢复调优前的配置"
		}
		if err != nil {
			j.State, j.Stage, j.Message = "failed", "failed", err.Error()
			if ctx.Err() == context.Canceled {
				j.State, j.Message = "cancelled", "任务已停止；已应用的基础参数保留，可恢复原配置"
			}
			if ctx.Err() == context.DeadlineExceeded {
				j.Message = "调优超时，已停止测速；可重试或恢复原配置"
			}
		}
		tuningTasks.cancel = nil
		_ = saveTuneJSON(path, j)
	}()
	return readTuneStatus(savedTuneParams()), nil
}

func cancelTuneJob() {
	tuningTasks.Lock()
	defer tuningTasks.Unlock()
	if tuningTasks.cancel != nil && tuningTasks.path == tuneStatePath("tuning-job.json") {
		tuningTasks.cancel()
	}
}

func tuneProgress(stage string, percent int, message string) {
	tuningTasks.Lock()
	defer tuningTasks.Unlock()
	if tuningTasks.job == nil {
		return
	}
	tuningTasks.job.Stage, tuningTasks.job.Percent, tuningTasks.job.Message = stage, percent, message
	_ = saveTuneJSON(tuningTasks.path, tuningTasks.job)
}

type tuneLogWriter struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (w *tuneLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	// The full output goes to the job log; keep only a short tail for errors.
	w.buffer.Write(p)
	if w.buffer.Len() > 2048 {
		b := append([]byte(nil), w.buffer.Bytes()[w.buffer.Len()-1024:]...)
		w.buffer.Reset()
		w.buffer.Write(b)
	}
	// iperf output can be verbose; retain a bounded tail in the persisted job.
	tuningTasks.Lock()
	if tuningTasks.job != nil {
		tuningTasks.job.Logs += string(p)
		if len(tuningTasks.job.Logs) > 32768 {
			tuningTasks.job.Logs = tuningTasks.job.Logs[len(tuningTasks.job.Logs)-16384:]
		}
	}
	tuningTasks.Unlock()
	return len(p), nil
}

// tail returns the last lines of output, short enough for a status message.
func (w *tuneLogWriter) tail() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	lines := strings.Split(strings.TrimSpace(w.buffer.String()), "\n")
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	out := strings.Join(lines, "\n")
	if r := []rune(out); len(r) > 300 {
		out = "…" + string(r[len(r)-300:])
	}
	return out
}

func runTuneCommand(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	prepareTuneProcess(cmd)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "DEBIAN_FRONTEND=noninteractive", "TCPFIT_NO_TELEMETRY=1")
	var out bytes.Buffer
	logs := &tuneLogWriter{}
	cmd.Stdout, cmd.Stderr = io.MultiWriter(&out, logs), logs
	err := cmd.Run()
	if err != nil {
		if ctx.Err() != nil {
			return out.String(), ctx.Err()
		}
		return out.String(), fmt.Errorf("%s 执行失败: %v；%s", name, err, logs.tail())
	}
	return out.String(), nil
}

func prepareTuneEngine(ctx context.Context) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("调优需要以 root 运行 Agent")
	}
	missing := false
	for _, name := range []string{"bash", "iperf3", "ip", "tc", "timeout", "awk", "pkill"} {
		if _, err := exec.LookPath(name); err != nil {
			missing = true
		}
	}
	if missing {
		tuneProgress("dependencies", 5, "安装测速依赖 iperf3 和 iproute2")
		var err error
		switch {
		case commandExists("apt-get"):
			_, err = tuneCommand(ctx, "apt-get", "-o", "DPkg::Lock::Timeout=30", "update")
			if err == nil {
				_, err = tuneCommand(ctx, "apt-get", "-o", "DPkg::Lock::Timeout=30", "install", "-y", "--no-install-recommends", "bash", "iperf3", "iproute2", "coreutils", "procps", "gawk")
			}
		case commandExists("dnf"):
			_, err = tuneCommand(ctx, "dnf", "install", "-y", "bash", "iperf3", "iproute", "coreutils", "procps-ng", "gawk")
		case commandExists("yum"):
			_, err = tuneCommand(ctx, "yum", "install", "-y", "bash", "iperf3", "iproute", "coreutils", "procps-ng", "gawk")
		case commandExists("apk"):
			_, err = tuneCommand(ctx, "apk", "add", "--no-cache", "bash", "iperf3", "iproute2", "coreutils", "procps", "gawk")
		default:
			return fmt.Errorf("缺少测速依赖，请安装 bash、iperf3、iproute2、coreutils 和 procps")
		}
		if err != nil {
			return fmt.Errorf("安装调优依赖失败: %w", err)
		}
	}
	if readRAMMB() <= 0 {
		return fmt.Errorf("无法读取服务器内存，已停止调优")
	}
	if err := os.MkdirAll(tuneEngineDir(), 0o700); err != nil {
		return err
	}
	for _, name := range []string{"tcpfit.sh", "bridge.sh", "LICENSE"} {
		b, err := tuneEngine.ReadFile("tcpfit/" + name)
		if err != nil {
			return err
		}
		if err = writeFileAtomic(filepath.Join(tuneEngineDir(), name), b, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func commandExists(name string) bool { _, err := exec.LookPath(name); return err == nil }

// Public test peers and alternate ports follow tcpfit 0.5.9. TCP handshake
// latency selects candidates without requiring ICMP or installing ping.
var tunePeerHosts = []string{
	"speedtest.hkg12.hk.leaseweb.net", "speedtest.sin1.sg.leaseweb.net", "sgp.proof.ovh.net",
	"speedtest.tyo11.jp.leaseweb.net", "speedtest.syd12.au.leaseweb.net", "speedtest.fra1.de.leaseweb.net",
	"speedtest.ams2.nl.leaseweb.net", "ams.speedtest.clouvider.net", "speedtest.lon12.uk.leaseweb.net",
	"lon.speedtest.clouvider.net", "speedtest.lax12.us.leaseweb.net", "speedtest.sfo12.us.leaseweb.net",
	"speedtest.sea11.us.leaseweb.net", "speedtest.dal13.us.leaseweb.net", "speedtest.chi11.us.leaseweb.net",
	"speedtest.nyc1.us.leaseweb.net", "speedtest.mia11.us.leaseweb.net", "speedtest.mtl2.ca.leaseweb.net",
}

type tunePeer struct {
	host    string
	port    int
	family  string
	latency time.Duration
}

func findTunePeers(ctx context.Context) []tunePeer {
	var mu sync.Mutex
	var peers []tunePeer
	var wg sync.WaitGroup
	for _, host := range tunePeerHosts {
		for _, family := range []string{"4", "6"} {
			wg.Add(1)
			go func(host, family string) {
				defer wg.Done()
				for _, port := range []int{5201, 5202, 5203, 5200} {
					start := time.Now()
					conn, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp"+family, net.JoinHostPort(host, strconv.Itoa(port)))
					if err != nil {
						continue
					}
					conn.Close()
					mu.Lock()
					peers = append(peers, tunePeer{host: host, port: port, family: family, latency: time.Since(start)})
					mu.Unlock()
					return
				}
			}(host, family)
		}
	}
	wg.Wait()
	sort.Slice(peers, func(i, j int) bool { return peers[i].latency < peers[j].latency })
	return peers
}

func tuneInterface(ctx context.Context, p tunePeer) (string, error) {
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip"+p.family, p.host)
	if err != nil || len(ips) == 0 {
		return "", fmt.Errorf("无法解析测速对端")
	}
	out, err := tuneCommand(ctx, "ip", "-"+p.family, "route", "get", ips[0].String())
	if err != nil {
		return "", err
	}
	f := strings.Fields(out)
	for i := 0; i+1 < len(f); i++ {
		if f[i] == "dev" {
			return f[i+1], nil
		}
	}
	return "", fmt.Errorf("无法确定测速出口网卡")
}

func callTuneEngine(ctx context.Context, action string, d *tuneDetection, args ...string) (string, error) {
	a := []string{filepath.Join(tuneEngineDir(), "bridge.sh"), tuneEngineDir(), action, d.Interface, d.Peer, strconv.Itoa(d.Port), "-" + d.Family}
	return tuneCommand(ctx, "bash", append(a, args...)...)
}

func parseTuneSample(out string) (*tuneSample, error) {
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "FLYX_SAMPLE=") {
			continue
		}
		f := strings.Fields(strings.TrimPrefix(line, "FLYX_SAMPLE="))
		if len(f) < 2 {
			break
		}
		sent, e1 := strconv.ParseFloat(f[0], 64)
		rt, e2 := strconv.ParseInt(f[1], 10, 64)
		received := sent
		if len(f) > 2 {
			var err error
			received, err = strconv.ParseFloat(f[2], 64)
			if err != nil {
				return nil, fmt.Errorf("无法解析接收端测速结果")
			}
		}
		if e1 != nil || e2 != nil || sent <= 0 || received <= 0 || rt < 0 || math.IsNaN(sent) || math.IsNaN(received) || math.IsInf(sent, 0) || math.IsInf(received, 0) {
			break
		}
		return &tuneSample{Mbps: received, Retransmits: rt, LossPercent: float64(rt) * 100 / (sent * 1e6 * 10 / 8 / 1448)}, nil
	}
	return nil, fmt.Errorf("iperf3 未返回有效测速结果")
}

func measureTuneSample(ctx context.Context, d *tuneDetection, parallel int) (*tuneSample, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	out, err := callTuneEngine(ctx, "sample", d, strconv.Itoa(parallel))
	if err != nil {
		return nil, err
	}
	return parseTuneSample(out)
}

func recoverAutomaticTuningOnStart() {
	if !tuningOperation.TryLock() {
		return
	}
	defer tuningOperation.Unlock()
	if err := recoverTuneTransaction(); err != nil {
		fmt.Printf("FLYX 调优恢复失败：%v\n", err)
		return
	}
	// Containers have no systemd; reapply a committed shaper when the agent
	// starts after a host/container restart.
	if InContainer() {
		if d := loadTuneDetection(); d != nil && d.ShapeMbps > 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if _, err := tuneCommand(ctx, "bash", filepath.Join(tuneEngineDir(), "shape.sh")); err != nil {
				fmt.Printf("FLYX 整形恢复失败：%v\n", err)
			}
		}
	}
}

func estimateTuneTraffic(bw int) float64 {
	step := math.Max(20, math.Floor(float64(bw)/300+0.5)*10)
	steps := math.Floor(float64(bw)*0.4/step) + 1
	mb := float64(bw)*10/8 + float64(bw)*0.4 + (steps+3)*float64(bw)*12/8 + float64(bw)*10/8*2
	return math.Ceil(mb/1024*10) / 10
}

func executeTuneJob(ctx context.Context, mode string, maxGB float64) error {
	if mode == "restore" {
		return restoreAutomaticTuning(ctx)
	}
	if err := tunePrepare(ctx); err != nil {
		return err
	}
	if mode == "deep" {
		return executeDeepTuning(ctx, loadTuneDetection(), maxGB)
	}
	tuneProgress("selecting", 12, "自动选择可用的公共测速对端")
	peers := tuneFindPeers(ctx)
	if len(peers) == 0 {
		return fmt.Errorf("公共测速对端不可达，未更改系统参数，请稍后重试")
	}
	var d *tuneDetection
	for i, p := range peers {
		if i >= 4 {
			break
		}
		iface, err := tuneInterface(ctx, p)
		if err != nil {
			continue
		}
		tuneProgress("measuring", 25, fmt.Sprintf("实测出口带宽：%s（对端 %s）", iface, p.host))
		candidate := &tuneDetection{Interface: iface, Peer: p.host, Port: p.port, Family: p.family}
		sample, err := tuneMeasure(ctx, candidate, 4)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			continue
		}
		candidate.Before = sample
		candidate.BandwidthMbps = int(math.Round(sample.Mbps))
		if candidate.BandwidthMbps < 1 {
			candidate.BandwidthMbps = 1
		}
		if candidate.BandwidthMbps > 100000 {
			candidate.BandwidthMbps = 100000
		}
		candidate.BandwidthSource = "iperf3 四并发实测"
		candidate.RTTSource = "按 tcpfit 使用 150 ms 路径估值（非测速对端 RTT）"
		candidate.MeasuredAt = time.Now().UnixMilli()
		// Preserve an existing FLYX shaper when re-running basic tuning.
		if old := loadTuneDetection(); old != nil {
			if old.ShapeMbps > 0 && old.Interface != candidate.Interface {
				return fmt.Errorf("出口已变化，请先恢复原配置以撤销旧出口整形，再重新调优")
			}
			candidate.ShapeMbps = old.ShapeMbps
			candidate.DeepSummary = old.DeepSummary
			// The sample went through the shaper; keep the unshaped line rate so
			// buffers and the next scan are not sized to the shaped rate.
			if old.ShapeMbps > 0 && old.BandwidthMbps > candidate.BandwidthMbps {
				candidate.BandwidthMbps = old.BandwidthMbps
				candidate.BandwidthSource = "沿用整形前实测（当前测速受出向整形限制）"
			}
		}
		candidate.EstimatedGB = estimateTuneTraffic(candidate.BandwidthMbps)
		d = candidate
		break
	}
	if d == nil {
		return fmt.Errorf("公共测速对端忙或未返回有效吞吐，未更改系统参数，请稍后重试")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tuneProgress("applying", 55, "按实测带宽和内存计算并应用基础参数")
	p := tuneParams{BandwidthMbps: d.BandwidthMbps, RTTMs: 150}
	var groups []string
	for _, g := range currentTuneGroups(p) {
		groups = append(groups, g.ID)
	}
	st, err := applyTuning(groups, p)
	if err != nil {
		return err
	}
	d.Applied = true
	d.Warnings = st.Warnings
	if err = saveTuneJSON(tuneStatePath("tuning-result.json"), d); err != nil {
		return err
	}
	if len(st.Warnings) > 0 {
		tuneProgress("applying", 60, strings.Join(st.Warnings, "；"))
	}
	tuneProgress("verifying", 75, "验证调优后的单连接和多连接吞吐")
	d.Single, err = tuneMeasure(ctx, d, 1)
	if err != nil {
		return fmt.Errorf("基础参数已应用，但单连接验证失败：%w", err)
	}
	if err = saveTuneJSON(tuneStatePath("tuning-result.json"), d); err != nil {
		return err
	}
	d.Parallel, err = tuneMeasure(ctx, d, 4)
	if err != nil {
		return fmt.Errorf("基础参数已应用，但多连接验证失败：%w", err)
	}
	return saveTuneJSON(tuneStatePath("tuning-result.json"), d)
}
