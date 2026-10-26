package socket

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fakeTuneAutomation(t *testing.T) {
	t.Helper()
	fakeProcSys(t, "reno cubic bbr")
	oldExecute, oldPlatform, oldCommand := tuneAutoExecute, tunePlatform, tuneCommand
	oldPrepare, oldPeers, oldMeasure := tunePrepare, tuneFindPeers, tuneMeasure
	oldTraffic, oldUnit, oldBaseline := tuneReadTraffic, tuneShapeUnitPath, tuneTrafficBaseline
	tunePlatform = "linux"
	tunePrepare = func(context.Context) error { return os.MkdirAll(tuneEngineDir(), 0o700) }
	tuneFindPeers = func(context.Context) []tunePeer { return []tunePeer{{host: "127.0.0.1", port: 5201, family: "4"}} }
	tuneCommand = func(_ context.Context, name string, args ...string) (string, error) {
		if name == "ip" {
			return "127.0.0.1 dev eth0 src 192.0.2.1", nil
		}
		if name == "tc" && len(args) > 1 && args[0] == "qdisc" {
			return "qdisc fq_codel 0: root refcnt 2 limit 10240p flows 1024 quantum 1514 target 5ms interval 100ms memory_limit 32Mb ecn", nil
		}
		return "", nil
	}
	tuneMeasure = func(context.Context, *tuneDetection, int) (*tuneSample, error) { return &tuneSample{Mbps: 200}, nil }
	tuneReadTraffic = func(string) (uint64, error) { return 0, nil }
	tuneTrafficBaseline = 0
	unit := filepath.Join(filepath.Dir(tuneBackupPath), "flyx-tuning-qdisc.service")
	tuneShapeUnitPath = func() string { return unit }
	t.Setenv("FLUX_AGENT_CONTAINER", "1")
	tuningTasks.Lock()
	tuningTasks.job = nil
	tuningTasks.cancel = nil
	tuningTasks.path = ""
	tuningTasks.Unlock()
	t.Cleanup(func() {
		tuneAutoExecute, tunePlatform, tuneCommand = oldExecute, oldPlatform, oldCommand
		tunePrepare, tuneFindPeers, tuneMeasure = oldPrepare, oldPeers, oldMeasure
		tuneReadTraffic, tuneShapeUnitPath, tuneTrafficBaseline = oldTraffic, oldUnit, oldBaseline
		tuningTasks.Lock()
		tuningTasks.job = nil
		tuningTasks.cancel = nil
		tuningTasks.path = ""
		tuningTasks.Unlock()
	})
}

func TestTuneAutomaticUsesMeasuredBandwidthAndVerifies(t *testing.T) {
	fakeTuneAutomation(t)
	var calls []int
	tuneMeasure = func(_ context.Context, d *tuneDetection, n int) (*tuneSample, error) {
		calls = append(calls, n)
		return &tuneSample{Mbps: 237.4}, nil
	}
	if err := executeTuneJob(context.Background(), "auto", 0); err != nil {
		t.Fatal(err)
	}
	d := loadTuneDetection()
	if d == nil || d.BandwidthMbps != 237 || d.Single == nil || d.Parallel == nil || !d.Applied {
		t.Fatalf("result: %+v", d)
	}
	if fmt.Sprint(calls) != "[4 1 4]" {
		t.Fatalf("measurement order: %v", calls)
	}
	if p := savedTuneParams(); p.BandwidthMbps != 237 || p.RTTMs != 150 {
		t.Fatalf("used assumed bandwidth: %+v", p)
	}
	if v, _ := readSysctl("net.ipv4.tcp_moderate_rcvbuf"); v != "1" {
		t.Fatal("receive auto tuning not enabled")
	}
	if len(loadTuneBackup()) == 0 {
		t.Fatal("original values not retained")
	}
	if err := restoreAutomaticTuning(context.Background()); err != nil {
		t.Fatal(err)
	}
	if v, _ := readSysctl("net.ipv4.tcp_rmem"); v != "4096 131072 6291456" {
		t.Fatalf("original lost: %s", v)
	}
	if loadTuneDetection() != nil {
		t.Fatal("restored results remain")
	}
}

func TestTuneProbeFailureDoesNotApplyDefaults(t *testing.T) {
	fakeTuneAutomation(t)
	tuneMeasure = func(context.Context, *tuneDetection, int) (*tuneSample, error) { return nil, errors.New("peer busy") }
	if err := executeTuneJob(context.Background(), "auto", 0); err == nil {
		t.Fatal("measurement should fail")
	}
	if v, _ := readSysctl("net.ipv4.tcp_congestion_control"); v != "cubic" {
		t.Fatal("changed parameters without measurement")
	}
	if len(loadTuneBackup()) != 0 || loadTuneDetection() != nil {
		t.Fatal("invented a successful tuning result")
	}
}

func TestTuneVerificationFailureKeepsAppliedResultAndOriginal(t *testing.T) {
	fakeTuneAutomation(t)
	n := 0
	tuneMeasure = func(context.Context, *tuneDetection, int) (*tuneSample, error) {
		n++
		if n > 1 {
			return nil, errors.New("peer busy")
		}
		return &tuneSample{Mbps: 300}, nil
	}
	if err := executeTuneJob(context.Background(), "auto", 0); err == nil || !strings.Contains(err.Error(), "已应用") {
		t.Fatalf("error: %v", err)
	}
	d := loadTuneDetection()
	if d == nil || !d.Applied || d.Single != nil {
		t.Fatalf("partial result: %+v", d)
	}
	if len(loadTuneBackup()) == 0 {
		t.Fatal("backup lost after verification failure")
	}
}

func TestTuneJobIsAsyncExclusiveAndCancellable(t *testing.T) {
	fakeTuneAutomation(t)
	entered := make(chan struct{})
	tuneAutoExecute = func(ctx context.Context, _ string, _ float64) error { close(entered); <-ctx.Done(); return ctx.Err() }
	st, err := startTuneJob("auto", 0)
	if err != nil {
		t.Fatal(err)
	}
	if st.Job == nil || st.Job.State != "running" {
		t.Fatalf("start must return immediately: %+v", st)
	}
	<-entered
	if _, err = startTuneJob("auto", 0); err == nil {
		t.Fatal("allowed concurrent mutation")
	}
	if _, err = handleTuneCommand("TuneApply", map[string]interface{}{"groups": []string{"buffer"}}); err == nil {
		t.Fatal("legacy command bypassed task lock")
	}
	cancelTuneJob()
	deadline := time.Now().Add(2 * time.Second)
	for {
		j := readTuneJob()
		if j.State != "running" {
			if j.State != "cancelled" {
				t.Fatalf("job: %+v", j)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cancel did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	// Completion releases the operation lock after persisting the final job.
	tuningOperation.Lock()
	tuningOperation.Unlock()
}

func TestTuneDeepRequiresRecentMeasurementAndTrafficBudget(t *testing.T) {
	fakeTuneAutomation(t)
	if _, err := startTuneJob("deep", 20); err == nil {
		t.Fatal("deep accepted without a measurement")
	}
	d := &tuneDetection{Applied: true, MeasuredAt: time.Now().UnixMilli(), EstimatedGB: 30}
	if err := saveTuneJSON(tuneStatePath("tuning-result.json"), d); err != nil {
		t.Fatal(err)
	}
	if _, err := startTuneJob("deep", 29); err == nil {
		t.Fatal("deep accepted an unconfirmed traffic estimate")
	}
	d.MeasuredAt = time.Now().Add(-25 * time.Hour).UnixMilli()
	_ = saveTuneJSON(tuneStatePath("tuning-result.json"), d)
	if _, err := startTuneJob("deep", 40); err == nil {
		t.Fatal("deep accepted stale measurement")
	}
}

func TestTuneSampleParsingUsesReceiverAndRejectsInvalidData(t *testing.T) {
	s, err := parseTuneSample("log\nFLYX_SAMPLE=1000 12 940\n")
	if err != nil || s.Mbps != 940 || s.Retransmits != 12 || s.LossPercent <= 0 {
		t.Fatalf("sample: %+v %v", s, err)
	}
	for _, line := range []string{"0 0", "100 -1", "NaN 0 20", "100 0 NaN", "100 0 nope", "10 0 0", "+Inf 1", "oops"} {
		if _, err = parseTuneSample("FLYX_SAMPLE=" + line); err == nil {
			t.Errorf("accepted invalid sample %q", line)
		}
	}
	if s, err = parseTuneSample("FLYX_SAMPLE=100 0"); err != nil || s.Mbps != 100 {
		t.Fatalf("old iperf fallback: %+v %v", s, err)
	}
}

func TestTuneQueueReplayKeepsKindsAndMQ(t *testing.T) {
	for queue, want := range map[string]string{
		"qdisc fq_codel 0: root refcnt 2 limit 10240p flows 1024 quantum 1514 target 5ms interval 100ms memory_limit 32Mb ecn": "'tc' 'qdisc' 'replace' 'dev' 'eth0' 'root' 'fq_codel'\n",
		"qdisc fq 8001: root refcnt 2 limit 10000p flow_limit 100p buckets 1024 quantum 3028b initial_quantum 15140b":          "'tc' 'qdisc' 'replace' 'dev' 'eth0' 'root' 'handle' '8001:' 'fq'\n",
		"qdisc pfifo_fast 0: root refcnt 2 bands 3 priomap 1 2 2 2 1 2 0 0 1 1 1 1 1 1 1 1":                                    "'tc' 'qdisc' 'replace' 'dev' 'eth0' 'root' 'pfifo_fast'\n",
		"qdisc mq 0: root\nqdisc fq_codel 0: parent :1 limit 10240p flows 1024 quantum 1514 target 5ms interval 100ms ecn":     "'tc' 'qdisc' 'replace' 'dev' 'eth0' 'parent' \"${mqmajor}:\"'1' 'fq_codel'\n",
	} {
		script, err := tuneQueueReplay("eth0", queue)
		if err != nil {
			t.Fatal(err)
		}
		// tc rejects display forms such as 10240p, 3028b and pfifo_fast options.
		if !strings.HasSuffix(script, want) {
			t.Fatalf("replay of %q:\n%s\nwant suffix %s", queue, script, want)
		}
	}
	for _, kind := range []string{"htb", "cake", "hfsc"} {
		if _, err := tuneQueueReplay("eth0", "qdisc "+kind+" 1: root"); err == nil {
			t.Fatal("external shaper accepted")
		}
	}
}

func fakeDeepResult(t *testing.T) *tuneDetection {
	t.Helper()
	if err := os.MkdirAll(tuneEngineDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	d := &tuneDetection{Applied: true, BandwidthMbps: 200, Interface: "eth0", Peer: "127.0.0.1", Port: 5201, Family: "4", MeasuredAt: time.Now().UnixMilli(), EstimatedGB: 6}
	if err := saveTuneJSON(tuneStatePath("tuning-result.json"), d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestTuneDeepInconclusiveNeverAppliesShape(t *testing.T) {
	fakeTuneAutomation(t)
	d := fakeDeepResult(t)
	base := tuneCommand
	shaped := false
	restored := false
	tuneCommand = func(ctx context.Context, name string, args ...string) (string, error) {
		if name == "bash" && len(args) > 2 {
			if args[2] == "sweep" {
				return "FLYX_SWEEP_INCONCLUSIVE=1\n", errors.New("exit 3")
			}
			if args[2] == "shape" {
				shaped = true
			}
		}
		if name == "bash" && len(args) == 1 {
			restored = true
		}
		return base(ctx, name, args...)
	}
	if err := executeDeepTuning(context.Background(), d, 10); err == nil {
		t.Fatal("uncertainty reported as success")
	}
	if shaped || !restored {
		t.Fatalf("shaped %v restored %v", shaped, restored)
	}
}

func TestTuneDeepVerificationFailureRestoresPersistentShape(t *testing.T) {
	fakeTuneAutomation(t)
	d := fakeDeepResult(t)
	old := "#!/bin/sh\necho original-shape\n"
	shape := filepath.Join(tuneEngineDir(), "shape.sh")
	if err := os.WriteFile(shape, []byte(old), 0o700); err != nil {
		t.Fatal(err)
	}
	base := tuneCommand
	tuneCommand = func(ctx context.Context, name string, args ...string) (string, error) {
		if name == "bash" && len(args) > 2 && args[2] == "sweep" {
			return "FLYX_SWEEP_KNEE=220\nFLYX_SWEEP_RECOMMEND=210\n", nil
		}
		return base(ctx, name, args...)
	}
	tuneMeasure = func(context.Context, *tuneDetection, int) (*tuneSample, error) {
		return nil, errors.New("verification failed")
	}
	if err := executeDeepTuning(context.Background(), d, 10); err == nil {
		t.Fatal("expected failed validation")
	}
	b, err := os.ReadFile(shape)
	if err != nil || string(b) != old {
		t.Fatalf("persisted a failed shaper: %q %v", b, err)
	}
	if got := loadTuneDetection(); got.ShapeMbps != 0 {
		t.Fatalf("published failed result %+v", got)
	}
}

func TestTuneDeepCommitsOnlyHealthyShaping(t *testing.T) {
	for _, mbps := range []float64{200, 20} {
		t.Run(fmt.Sprint(mbps), func(t *testing.T) {
			fakeTuneAutomation(t)
			d := fakeDeepResult(t)
			base := tuneCommand
			tuneCommand = func(ctx context.Context, name string, args ...string) (string, error) {
				if name == "bash" && len(args) > 2 && args[2] == "sweep" {
					return "FLYX_SWEEP_KNEE=220\nFLYX_SWEEP_RECOMMEND=210\n", nil
				}
				return base(ctx, name, args...)
			}
			tuneMeasure = func(context.Context, *tuneDetection, int) (*tuneSample, error) { return &tuneSample{Mbps: mbps}, nil }
			err := executeDeepTuning(context.Background(), d, 10)
			if mbps == 200 {
				if err != nil {
					t.Fatal(err)
				}
				if got := loadTuneDetection(); got.ShapeMbps != 210 || got.Parallel == nil {
					t.Fatalf("missing verified shaper: %+v", got)
				}
			} else {
				if err == nil {
					t.Fatal("committed severe throughput regression")
				}
				if got := loadTuneDetection(); got.ShapeMbps != 0 {
					t.Fatalf("failed shaper published: %+v", got)
				}
				if _, err = os.Stat(filepath.Join(tuneEngineDir(), "shape.sh")); !os.IsNotExist(err) {
					t.Fatal("failed shaper retained at boot")
				}
			}
		})
	}
}

func TestTuneDeepBudgetStopsScanAndRestoresQueues(t *testing.T) {
	fakeTuneAutomation(t)
	d := fakeDeepResult(t)
	var count atomic.Int32
	tuneReadTraffic = func(string) (uint64, error) {
		if count.Add(1) > 1 {
			return 20 << 30, nil
		}
		return 0, nil
	}
	base := tuneCommand
	restored := false
	tuneCommand = func(ctx context.Context, name string, args ...string) (string, error) {
		if name == "bash" && len(args) > 2 && args[2] == "sweep" {
			<-ctx.Done()
			return "", ctx.Err()
		}
		if name == "bash" && len(args) == 1 {
			restored = true
		}
		return base(ctx, name, args...)
	}
	if err := executeDeepTuning(context.Background(), d, 10); err == nil || !strings.Contains(err.Error(), "预算") {
		t.Fatalf("error: %v", err)
	}
	if !restored {
		t.Fatal("queues left at scan rate")
	}
}

func TestTuneInterruptedTransactionRecoversBeforeReporting(t *testing.T) {
	fakeTuneAutomation(t)
	_ = fakeDeepResult(t)
	files, err := captureTuneShapeFiles()
	if err != nil {
		t.Fatal(err)
	}
	if err = saveTuneJSON(tuneStatePath("tuning-transaction.json"), files); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(tuneEngineDir(), "current-qdisc.sh"), []byte("#!/bin/sh\ntrue\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err = saveTuneJSON(tuneStatePath("tuning-job.json"), tuneJob{State: "running"}); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(tuneEngineDir(), "shape.sh"), []byte("bad state"), 0o700); err != nil {
		t.Fatal(err)
	}
	j := readTuneJob()
	if j == nil || j.State != "failed" || !strings.Contains(j.Message, "重启") {
		t.Fatalf("job: %+v", j)
	}
	if _, err = os.Stat(filepath.Join(tuneEngineDir(), "shape.sh")); !os.IsNotExist(err) {
		t.Fatal("interrupted shaper remained persistent")
	}
	if _, err = os.Stat(tuneStatePath("tuning-transaction.json")); !os.IsNotExist(err) {
		t.Fatal("recovery not completed")
	}
}

// Exercise the actual vendored bash bridge with fake system tools: no network
// traffic and no host queue/sysctl writes. This catches library integration
// errors that mocked Go functions cannot detect.
func TestTuneTCPFitBridgeMeasurementAndCleanScan(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"tcpfit.sh", "bridge.sh"} {
		b, err := tuneEngine.ReadFile("tcpfit/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, name), b, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	tools := map[string]string{
		"id":      "#!/bin/sh\nprintf '0\\n'\n",
		"timeout": "#!/bin/sh\n[ \"$1\" != --foreground ] || shift\nshift\nexec \"$@\"\n",
		"pkill":   "#!/bin/sh\nexit 1\n",
		"iperf3":  "#!/bin/sh\nprintf '[SUM] 0.00-10.00 sec 1.10 GBytes 1000 Mbits/sec 0 sender\\n[SUM] 0.00-10.00 sec 1.10 GBytes 950 Mbits/sec receiver\\n'\n",
		"tc":      "#!/bin/sh\nprintf 'qdisc fq 0: root refcnt 2 limit 10000p flow_limit 100p\\n'\n",
		"ip":      "#!/bin/sh\nprintf 'default via 192.0.2.1 dev eth0\\n'\n",
	}
	for name, body := range tools {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	marker := filepath.Join(dir, "restored")
	if err := os.WriteFile(filepath.Join(dir, "current-qdisc.sh"), []byte("#!/bin/sh\nprintf restored > "+tuneShellQuote(marker)+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	run := func(action string, args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		a := []string{filepath.Join(dir, "bridge.sh"), dir, action, "eth0", "127.0.0.1", "5201", "-4"}
		cmd := exec.CommandContext(ctx, "bash", append(a, args...)...)
		cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "LC_ALL=C")
		b, err := cmd.CombinedOutput()
		return string(b), err
	}
	out, err := run("sample", "4")
	if err != nil {
		t.Fatalf("bridge: %v\n%s", err, out)
	}
	s, err := parseTuneSample(out)
	if err != nil || s.Mbps != 950 {
		t.Fatalf("measurement: %+v %v\n%s", s, err, out)
	}
	out, err = run("sweep", "1000", "50")
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 3 || !strings.Contains(out, "FLYX_SWEEP_NO_KNEE=1") {
		t.Fatalf("clean scan: %v\n%s", err, out)
	}
	if _, err = os.Stat(marker); err != nil {
		t.Fatal("bridge did not restore captured queues")
	}
}

func TestTuneMissingMemoryCannotCreateUnboundedBuffer(t *testing.T) {
	if s := tuneSizesFor(tuneParams{100000, 2000}, 0); s.BufMax != 4*mib {
		t.Fatalf("unsafe buffer without RAM information: %+v", s)
	}
}

func TestTuneBudgetUsesKernelPageSize(t *testing.T) {
	old := tunePageSize
	t.Cleanup(func() { tunePageSize = old })
	tunePageSize = 65536
	if s := tuneSizesFor(tuneParams{1000, 150}, 2048); s.TCPMem != "2048 4096 8192" {
		t.Fatalf("64 KiB page budget: %+v", s)
	}
}

func TestTuneCorruptBackupBlocksMutation(t *testing.T) {
	fakeTuneAutomation(t)
	if err := os.MkdirAll(filepath.Dir(tuneBackupPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tuneBackupPath, []byte("broken original backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := applyTuning([]string{"bbr"}, defaultTuneParams); err == nil {
		t.Fatal("overwrote corrupt original snapshot")
	}
	if v, _ := readSysctl("net.ipv4.tcp_congestion_control"); v != "cubic" {
		t.Fatal("changed the system without a recoverable snapshot")
	}
}

func TestTuneDeepInconclusiveKeepsSummary(t *testing.T) {
	fakeTuneAutomation(t)
	d := fakeDeepResult(t)
	base := tuneCommand
	tuneCommand = func(ctx context.Context, name string, args ...string) (string, error) {
		if name == "bash" && len(args) > 2 && args[2] == "sweep" {
			return "FLYX_SWEEP_INCONCLUSIVE=1\n", errors.New("exit 3")
		}
		return base(ctx, name, args...)
	}
	if err := executeDeepTuning(context.Background(), d, 10); err == nil {
		t.Fatal("uncertainty reported as success")
	}
	if got := loadTuneDetection(); got == nil || !strings.Contains(got.DeepSummary, "未能可靠判断") || got.ShapeMbps != 0 {
		t.Fatalf("summary lost on rollback: %+v", got)
	}
}

func TestTuneDeepBudgetExcludesBackgroundTraffic(t *testing.T) {
	fakeTuneAutomation(t)
	d := fakeDeepResult(t)
	tuneTrafficBaseline = 200 * time.Millisecond
	began := time.Now()
	// 1 GB/s of forwarding traffic and no scan traffic at all.
	tuneReadTraffic = func(string) (uint64, error) { return uint64(time.Since(began).Seconds() * (1 << 30)), nil }
	base := tuneCommand
	tuneCommand = func(ctx context.Context, name string, args ...string) (string, error) {
		if name == "bash" && len(args) > 2 && args[2] == "sweep" {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(1500 * time.Millisecond):
			}
			return "FLYX_SWEEP_NO_KNEE=1\n", errors.New("exit 3")
		}
		return base(ctx, name, args...)
	}
	if err := executeDeepTuning(context.Background(), d, 0.5); err != nil {
		t.Fatalf("forwarding traffic charged to the scan budget: %v", err)
	}
}

func TestTuneRestoreClearsStuckTransaction(t *testing.T) {
	fakeTuneAutomation(t)
	_ = fakeDeepResult(t)
	files, err := captureTuneShapeFiles()
	if err != nil {
		t.Fatal(err)
	}
	if err = saveTuneJSON(tuneStatePath("tuning-transaction.json"), files); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(tuneEngineDir(), "original-qdisc.sh"), []byte("#!/bin/sh\ntrue\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	base := tuneCommand
	tuneCommand = func(ctx context.Context, name string, args ...string) (string, error) {
		if name == "bash" && len(args) == 1 && strings.HasSuffix(args[0], "current-qdisc.sh") {
			return "", errors.New("tc failed")
		}
		return base(ctx, name, args...)
	}
	if _, err = startTuneJob("deep", 10); err == nil || !strings.Contains(err.Error(), "恢复原配置") {
		t.Fatalf("deep tuning over a stuck transaction: %v", err)
	}
	done := make(chan error, 1)
	tuneAutoExecute = func(ctx context.Context, mode string, maxGB float64) error {
		err := executeTuneJob(ctx, mode, maxGB)
		done <- err
		return err
	}
	if _, err = startTuneJob("restore", 0); err != nil {
		t.Fatalf("restore blocked: %v", err)
	}
	if err = <-done; err != nil {
		t.Fatalf("restore: %v", err)
	}
	if _, err = os.Stat(tuneStatePath("tuning-transaction.json")); !os.IsNotExist(err) {
		t.Fatal("stuck transaction kept after restore")
	}
}

func TestTuneAutoKeepsUnshapedBandwidthUnderShaper(t *testing.T) {
	fakeTuneAutomation(t)
	d := fakeDeepResult(t)
	d.BandwidthMbps, d.ShapeMbps = 1000, 800
	if err := saveTuneJSON(tuneStatePath("tuning-result.json"), d); err != nil {
		t.Fatal(err)
	}
	tuneMeasure = func(context.Context, *tuneDetection, int) (*tuneSample, error) { return &tuneSample{Mbps: 790}, nil }
	if err := executeTuneJob(context.Background(), "auto", 0); err != nil {
		t.Fatal(err)
	}
	got := loadTuneDetection()
	if got == nil || got.BandwidthMbps != 1000 || got.ShapeMbps != 800 || got.EstimatedGB != estimateTuneTraffic(1000) {
		t.Fatalf("shaped rate stored as line rate: %+v", got)
	}
}
