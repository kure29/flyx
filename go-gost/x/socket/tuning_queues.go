package socket

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func tuneShellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

// tc's display forms ("limit 10240p", "quantum 3028b", pfifo_fast's priomap)
// are not accepted back as options, so only the queue kinds and their
// placement are replayed; the kernel supplies the defaults. Refuse external
// classful shapers and filters instead of destroying their topology.
func tuneQueueReplay(iface, out string) (string, error) {
	var lines [][]string
	root := ""
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || f[0] != "qdisc" {
			continue
		}
		if f[1] == "ingress" || f[1] == "clsact" {
			continue
		}
		if f[3] == "root" {
			root = f[1]
		}
		switch f[1] {
		case "mq", "fq", "fq_codel", "pfifo_fast", "noqueue":
		default:
			return "", fmt.Errorf("网卡 %s 使用外部 %s 队列，无法保证完整恢复，已停止深度优化", iface, f[1])
		}
		lines = append(lines, f)
	}
	if root == "" {
		return "", fmt.Errorf("无法读取网卡原始队列")
	}
	script := "#!/bin/sh\nset -eu\n# Original queues, captured before FLYX deep tuning.\ntc qdisc del dev " + tuneShellQuote(iface) + " root 2>/dev/null || true\n"
	// Hardware mq roots with handle 0 are recreated by the kernel on delete.
	// Query the resulting major handle so leaves bind to the actual queues.
	if root == "mq" {
		script += "mqmajor=$(tc qdisc show dev " + tuneShellQuote(iface) + " | awk '$1==\"qdisc\" && $2==\"mq\" {for(i=1;i<=NF;i++) if($i==\"root\") {sub(/:$/,\"\",$3); print $3; exit}}')\n"
		script += "if [ -z \"$mqmajor\" ]; then tc qdisc replace dev " + tuneShellQuote(iface) + " root handle 1: mq; mqmajor=1; fi\n"
	}
	for _, f := range lines {
		if f[1] == "noqueue" || f[1] == "mq" {
			continue
		}
		args := []string{"tc", "qdisc", "replace", "dev", iface}
		for i := 3; i < len(f); i++ {
			if f[i] == "refcnt" {
				i++
				continue
			}
			if f[i] == "root" || f[i] == "parent" {
				args = append(args, f[i])
				if f[i] == "parent" && i+1 < len(f) {
					i++
					args = append(args, f[i])
				}
				break
			}
		}
		if f[2] != "0:" {
			args = append(args, "handle", f[2])
		}
		args = append(args, f[1])
		quoted := make([]string, len(args))
		for i, a := range args {
			quoted[i] = tuneShellQuote(a)
			if root == "mq" && i > 0 && args[i-1] == "parent" {
				_, minor, ok := strings.Cut(a, ":")
				if !ok {
					return "", fmt.Errorf("无法解析 mq 叶子队列")
				}
				quoted[i] = "\"${mqmajor}:\"" + tuneShellQuote(minor)
			}
		}
		script += strings.Join(quoted, " ") + "\n"
	}
	return script, nil
}

func captureTuneQueues(ctx context.Context, d *tuneDetection) error {
	iface, err := tuneInterface(ctx, tunePeer{host: d.Peer, port: d.Port, family: d.Family})
	if err != nil {
		return err
	}
	if iface != d.Interface {
		return fmt.Errorf("测速出口已变化，请重新执行自动调优")
	}
	ifacePath := filepath.Join(tuneEngineDir(), "queue-interface")
	if old, e := os.ReadFile(ifacePath); e == nil && string(old) != iface {
		return fmt.Errorf("出口已变化，请先恢复原配置，再对新出口调优")
	} else if e != nil && !os.IsNotExist(e) {
		return e
	}
	filters, err := tuneCommand(ctx, "tc", "filter", "show", "dev", iface, "root")
	if err != nil {
		return err
	}
	if strings.TrimSpace(filters) != "" {
		return fmt.Errorf("出口存在自定义流量分类，已停止深度优化以保留原配置")
	}
	out, err := tuneCommand(ctx, "tc", "qdisc", "show", "dev", iface)
	if err != nil {
		return err
	}
	var replay string
	if strings.Contains(out, "qdisc htb ") && d.ShapeMbps > 0 {
		b, e := os.ReadFile(filepath.Join(tuneEngineDir(), "shape.sh"))
		if e != nil {
			return fmt.Errorf("找不到原整形配置，已停止深度优化")
		}
		replay = string(b)
	} else {
		replay, err = tuneQueueReplay(iface, out)
		if err != nil {
			return err
		}
	}
	if err = writeFileAtomic(filepath.Join(tuneEngineDir(), "current-qdisc.sh"), []byte(replay), 0o700); err != nil {
		return err
	}
	original := filepath.Join(tuneEngineDir(), "original-qdisc.sh")
	if _, err = os.Stat(original); os.IsNotExist(err) {
		if err = writeFileAtomic(ifacePath, []byte(iface), 0o600); err != nil {
			return err
		}
		return writeFileAtomic(original, []byte(replay), 0o700)
	} else if err != nil {
		return err
	}
	return nil
}

func restoreTuneQueues(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := tuneCommand(ctx, "bash", filepath.Join(tuneEngineDir(), name))
	return err
}

var tuneReadTraffic = tuneTrafficBytes

// tuneTrafficBaseline is how long the interface's own traffic is sampled
// before a scan, so forwarding traffic is not charged to the scan budget.
var tuneTrafficBaseline = 5 * time.Second

func tuneTrafficBytes(iface string) (uint64, error) {
	var total uint64
	for _, name := range []string{"rx_bytes", "tx_bytes"} {
		b, err := os.ReadFile(filepath.Join("/sys/class/net", iface, "statistics", name))
		if err != nil {
			return 0, err
		}
		n, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
		if err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}

func executeDeepTuning(ctx context.Context, d *tuneDetection, maxGB float64) (err error) {
	if d == nil {
		return fmt.Errorf("请先完成自动调优")
	}
	if err = captureTuneQueues(ctx, d); err != nil {
		return err
	}
	committed := false
	// keepSummary saves d's summary again after the files are rolled back.
	keepSummary := false
	files, err := captureTuneShapeFiles()
	if err != nil {
		return err
	}
	if err = saveTuneJSON(tuneStatePath("tuning-transaction.json"), files); err != nil {
		return err
	}
	defer func() {
		recovered := true
		if !committed {
			if e := restoreTuneShapeFiles(files); e != nil {
				recovered = false
				err = fmt.Errorf("%v；整形持久配置恢复失败：%w", err, e)
			}
			if e := restoreTuneQueues("current-qdisc.sh"); e != nil {
				recovered = false
				err = fmt.Errorf("%v；原队列恢复失败：%w", err, e)
			}
			if recovered && keepSummary {
				if e := saveTuneJSON(tuneStatePath("tuning-result.json"), d); e != nil {
					err = fmt.Errorf("%v；保存扫描结果失败：%w", err, e)
				}
			}
		}
		if recovered {
			if e := os.Remove(tuneStatePath("tuning-transaction.json")); e != nil && !os.IsNotExist(e) {
				err = fmt.Errorf("清理恢复记录失败：%w", e)
			}
		}
	}()
	tuneProgress("scanning", 32, "测量出口现有流量，避免把转发流量计入测速预算")
	start, err := tuneReadTraffic(d.Interface)
	if err != nil {
		return err
	}
	var background float64 // bytes per second
	if tuneTrafficBaseline > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(tuneTrafficBaseline):
		}
		n, e := tuneReadTraffic(d.Interface)
		if e != nil {
			return e
		}
		if n >= start {
			background = float64(n-start) / tuneTrafficBaseline.Seconds()
		}
		start = n
	}
	began := time.Now()
	scanCtx, cancel := context.WithCancelCause(ctx)
	trafficDone := make(chan struct{})
	defer func() { cancel(nil); <-trafficDone }()
	go func() {
		defer close(trafficDone)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-scanCtx.Done():
				return
			case <-ticker.C:
				n, e := tuneReadTraffic(d.Interface)
				if e != nil {
					cancel(fmt.Errorf("无法计量测速流量，已停止深度优化"))
					return
				}
				used := float64(n-start) - background*time.Since(began).Seconds()
				if n >= start && used > maxGB*1024*1024*1024 {
					cancel(fmt.Errorf("已达到本次 %.1f GB 流量预算，已停止扫描并恢复原队列", maxGB))
					return
				}
			}
		}
	}()
	tuneProgress("scanning", 35, fmt.Sprintf("扫描限速器拐点，流量预算 %.1f GB；可能需要数分钟", maxGB))
	out, scanErr := callTuneEngine(scanCtx, "sweep", d, strconv.Itoa(d.BandwidthMbps), strconv.FormatFloat(maxGB, 'f', 1, 64))
	if scanCtx.Err() != nil {
		return context.Cause(scanCtx)
	}
	values := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "FLYX_SWEEP_") {
			kv := strings.SplitN(strings.TrimPrefix(line, "FLYX_SWEEP_"), "=", 2)
			if len(kv) == 2 {
				values[kv[0]] = kv[1]
			}
		}
	}
	rate, _ := strconv.Atoi(values["RECOMMEND"])
	knee, _ := strconv.Atoi(values["KNEE"])
	if values["INCONCLUSIVE"] == "1" || values["OUT_OF_RANGE"] == "1" || values["CANCELLED"] == "1" || values["ABOVE_CAP"] != "" {
		d.DeepSummary = "本次未能可靠判断限速器拐点，保留原整形配置"
		keepSummary = true
		return fmt.Errorf("%s；可稍后重试，详情见任务日志", d.DeepSummary)
	}
	if values["NO_KNEE"] == "1" {
		d.DeepSummary = "本次未发现明确限速器拐点，保留原队列与整形配置"
		if err = restoreTuneQueues("current-qdisc.sh"); err != nil {
			return err
		}
		if err = saveTuneJSON(tuneStatePath("tuning-result.json"), d); err != nil {
			return err
		}
		committed = true
		return nil
	}
	if scanErr != nil || rate <= 0 || knee <= 0 || rate > knee || rate > 100000 {
		return fmt.Errorf("拐点扫描未取得可靠结果，未更改整形配置：%v", scanErr)
	}
	tuneProgress("shaping", 75, fmt.Sprintf("已定位拐点 %d Mbps，应用 %d Mbps 出向整形并验证", knee, rate))
	if err = persistTuneShape(scanCtx, d, rate); err != nil {
		return err
	}
	// Verify before committing the new shaper. Failure restores its predecessor.
	d.Single, err = tuneMeasure(scanCtx, d, 1)
	if err == nil {
		d.Parallel, err = tuneMeasure(scanCtx, d, 4)
	}
	if err != nil {
		return fmt.Errorf("整形后验证失败，已回退原队列：%w", err)
	}
	baseline := float64(d.BandwidthMbps)
	if d.Before != nil {
		baseline = d.Before.Mbps
	}
	if d.Parallel.Mbps < math.Min(float64(rate), baseline)*0.7 || d.Parallel.LossPercent > 1 {
		return fmt.Errorf("整形后验证吞吐偏低或重传偏高，已回退原队列；可在对端空闲时重试")
	}
	d.ShapeMbps = rate
	d.DeepSummary = fmt.Sprintf("拐点 %d Mbps，出向整形 %d Mbps", knee, rate)
	if err = saveTuneJSON(tuneStatePath("tuning-result.json"), d); err != nil {
		return err
	}
	committed = true
	return nil
}

type tuneShapeFile struct {
	Path   string
	Data   []byte
	Exists bool
}

func captureTuneShapeFiles() ([]tuneShapeFile, error) {
	var saved []tuneShapeFile
	for _, path := range []string{filepath.Join(tuneEngineDir(), "shape.sh"), tuneShapeUnitPath(), tuneStatePath("tuning-result.json")} {
		b, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		saved = append(saved, tuneShapeFile{Path: path, Data: b, Exists: err == nil})
	}
	return saved, nil
}
func restoreTuneShapeFiles(saved []tuneShapeFile) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, f := range saved {
		if f.Exists {
			mode := os.FileMode(0o600)
			if strings.HasSuffix(f.Path, ".sh") {
				mode = 0o700
			}
			if f.Path == tuneShapeUnitPath() {
				mode = 0o644
			}
			if err := writeFileAtomic(f.Path, f.Data, mode); err != nil {
				return err
			}
		} else {
			if f.Path == tuneShapeUnitPath() {
				if _, err := os.Stat(f.Path); err == nil && !InContainer() {
					if _, err = tuneCommand(ctx, "systemctl", "disable", "--now", "flyx-tuning-qdisc.service"); err != nil {
						return err
					}
				}
			}
			if err := os.Remove(f.Path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	// Reload after removing a unit too, so systemd drops the stale definition.
	if !InContainer() && commandExists("systemctl") {
		_, err := tuneCommand(ctx, "systemctl", "daemon-reload")
		return err
	}
	return nil
}

func recoverTuneTransaction() error {
	path := tuneStatePath("tuning-transaction.json")
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var files []tuneShapeFile
	if err = json.Unmarshal(b, &files); err != nil {
		return err
	}
	if err = restoreTuneShapeFiles(files); err != nil {
		return err
	}
	if err = restoreTuneQueues("current-qdisc.sh"); err != nil {
		return err
	}
	return os.Remove(path)
}

func tuneShapeScript(iface string, rate int) string {
	q := tuneShellQuote(iface)
	r := strconv.Itoa(rate) + "mbit"
	burst := rate * 500
	if burst < 32768 {
		burst = 32768
	}
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntc qdisc replace dev %s root handle 1: htb default 10\ntc class replace dev %s parent 1: classid 1:10 htb rate %s ceil %s burst %d cburst %d quantum 1514\ntc qdisc replace dev %s parent 1:10 handle 10: fq limit 40960 flow_limit 8192 maxrate %s\n", q, q, r, r, burst, burst, q, r)
}

var tuneShapeUnitPath = func() string { return "/etc/systemd/system/flyx-tuning-qdisc.service" }

func persistTuneShape(ctx context.Context, d *tuneDetection, rate int) error {
	_, err := callTuneEngine(ctx, "shape", d, strconv.Itoa(rate))
	if err != nil {
		return err
	}
	script := filepath.Join(tuneEngineDir(), "shape.sh")
	if err = writeFileAtomic(script, []byte(tuneShapeScript(d.Interface, rate)), 0o700); err != nil {
		return err
	}
	// Replace the engine's test shaper so the verified queue is the persisted one.
	if _, err = tuneCommand(ctx, "bash", script); err != nil {
		return err
	}
	if InContainer() {
		return nil
	}
	if !commandExists("systemctl") {
		return fmt.Errorf("缺少 systemd，无法持久保存整形配置")
	}
	unit := "[Unit]\nDescription=FLYX egress tuning\nAfter=network-online.target\nWants=network-online.target\n[Service]\nType=oneshot\nRemainAfterExit=yes\nExecStart=" + script + "\n[Install]\nWantedBy=multi-user.target\n"
	if err = writeFileAtomic(tuneShapeUnitPath(), []byte(unit), 0o644); err != nil {
		return err
	}
	if _, err = tuneCommand(ctx, "systemctl", "daemon-reload"); err != nil {
		return err
	}
	_, err = tuneCommand(ctx, "systemctl", "enable", "flyx-tuning-qdisc.service")
	return err
}

func restoreAutomaticTuning(ctx context.Context) error {
	tuneProgress("restoring", 35, "恢复首次调优前的内核参数与出口队列")
	original := filepath.Join(tuneEngineDir(), "original-qdisc.sh")
	_, queueErr := os.Stat(original)
	if queueErr != nil && !os.IsNotExist(queueErr) {
		return queueErr
	}
	if os.IsNotExist(queueErr) {
		if _, err := os.Stat(filepath.Join(tuneEngineDir(), "shape.sh")); err == nil {
			return fmt.Errorf("找不到整形前的队列备份，已保留现有配置，请检查 Agent 数据目录")
		}
		if _, err := os.Stat(tuneShapeUnitPath()); err == nil {
			return fmt.Errorf("找不到整形前的队列备份，已保留整形服务配置")
		}
	}
	if queueErr == nil {
		var err error
		if err = restoreTuneQueues("original-qdisc.sh"); err != nil {
			return err
		}
		if !InContainer() {
			if _, e := os.Stat(tuneShapeUnitPath()); e == nil {
				if _, err = tuneCommand(ctx, "systemctl", "disable", "--now", "flyx-tuning-qdisc.service"); err != nil {
					return err
				}
				if err = os.Remove(tuneShapeUnitPath()); err != nil {
					return err
				}
				if _, err = tuneCommand(ctx, "systemctl", "daemon-reload"); err != nil {
					return err
				}
			}
		}
	}
	st, err := applyTuning(nil, savedTuneParams())
	if err != nil {
		return err
	}
	if len(st.Warnings) > 0 {
		return fmt.Errorf("部分参数恢复失败：%s", strings.Join(st.Warnings, "；"))
	}
	for _, name := range []string{"shape.sh", "original-qdisc.sh", "current-qdisc.sh", "queue-interface"} {
		if err = os.Remove(filepath.Join(tuneEngineDir(), name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err = os.Remove(tuneStatePath("tuning-result.json")); err != nil && !os.IsNotExist(err) {
		return err
	}
	// The original queues are back, so an unfinished scan has nothing to undo.
	if err = os.Remove(tuneStatePath("tuning-transaction.json")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
