package socket

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/go-gost/x/identity"
	"github.com/go-gost/x/kernel"
)

// uninstallMarker stays in a container's data volume after an uninstall. The
// agent cannot remove its own container, so the restart policy brings it back;
// the marker makes that restarted agent idle instead of reconnecting.
const uninstallMarker = ".uninstalled"

var uninstallStarted atomic.Bool

// Overridden by tests, which must never touch a real installation.
var (
	uninstallRestoreTuning = restoreTuningForUninstall
	uninstallKernels       = func() error { return kernel.Default().Uninstall() }
	uninstallExit          = os.Exit
	uninstallDataDir       = identity.DataDir
	uninstallBinaryPath    = identity.BinaryPath
	uninstallInContainer   = InContainer
	uninstallStartScript   = func(script string) error { return agentRestartCommand(script).Start() }
	uninstallSystemdDir    = "/etc/systemd/system"
	uninstallInitDir       = "/etc/init.d"
)

// AgentUninstalled reports whether the panel uninstalled this agent.
func AgentUninstalled() bool {
	_, err := os.Stat(filepath.Join(uninstallDataDir(), uninstallMarker))
	return err == nil
}

// handleUninstallAgent acknowledges first and uninstalls afterwards, so the
// panel receives the reply before the connection goes away.
func (w *WebSocketReporter) handleUninstallAgent() error {
	if !uninstallStarted.CompareAndSwap(false, true) {
		return nil
	}
	go func() {
		time.Sleep(time.Second)
		runAgentUninstall()
	}()
	return nil
}

func runAgentUninstall() {
	fmt.Println("🗑️ 面板已删除此服务器，开始卸载 agent...")
	if err := uninstallRestoreTuning(); err != nil {
		fmt.Printf("⚠️ 恢复调优前的系统参数失败: %v\n", err)
	}
	if err := uninstallKernels(); err != nil {
		fmt.Printf("⚠️ 清理内核和 nftables 规则失败: %v\n", err)
	}
	dir := uninstallDataDir()
	if uninstallInContainer() {
		if err := wipeDataDirForUninstall(dir, filepath.Base(uninstallBinaryPath())); err != nil {
			fmt.Printf("⚠️ 清理数据目录失败: %v\n", err)
		}
		fmt.Println("✅ 已卸载。容器无法删除自己，请在宿主机上执行 docker rm -f 删除这个容器")
		uninstallExit(0)
		return
	}
	script, err := buildAgentUninstallScript(uninstallBinaryPath(), dir)
	if err != nil {
		fmt.Printf("❌ 卸载失败: %v\n", err)
		return
	}
	if err := uninstallStartScript(script); err != nil {
		fmt.Printf("❌ 启动卸载脚本失败: %v\n", err)
		return
	}
	fmt.Println("✅ 卸载脚本已启动，agent 即将停止")
}

func restoreTuningForUninstall() error {
	cancelTuneJob()
	tuningOperation.Lock()
	defer tuningOperation.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return restoreAutomaticTuning(ctx)
}

// wipeDataDirForUninstall removes the configuration (including the panel
// secret), kernels and state. It keeps the marker, plus the running binary and
// image version stamp so the entrypoint restarts this binary, which honours
// the marker, rather than an older one from the image.
func wipeDataDirForUninstall(dir, binary string) error {
	if err := os.WriteFile(filepath.Join(dir, uninstallMarker), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600); err != nil {
		return err
	}
	keep := map[string]bool{uninstallMarker: true, binary: true, ".image_version": true}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var firstErr error
	for _, entry := range entries {
		if keep[entry.Name()] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// buildAgentUninstallScript stops and removes the native service, then the
// installation directory. Every step runs even if an earlier one fails.
func buildAgentUninstallScript(binaryPath, dir string) (string, error) {
	name := filepath.Base(binaryPath)
	if name != identity.AgentName && name != "flux_agent" {
		return "", fmt.Errorf("无法识别的 agent 安装：%s", binaryPath)
	}
	// Only ever an installation directory of its own, never e.g. /usr/local/bin.
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != filepath.Dir(binaryPath) ||
		(filepath.Base(dir) != identity.AgentName && filepath.Base(dir) != "flux_agent") {
		return "", fmt.Errorf("拒绝删除无法识别的目录：%s", dir)
	}
	q := tuneShellQuote
	unit := filepath.Join(uninstallSystemdDir, name+".service")
	initScript := filepath.Join(uninstallInitDir, name)
	return "sleep 1; " +
		"if command -v rc-service >/dev/null 2>&1 && [ -f /etc/alpine-release ]; then " +
		"rc-service " + q(name) + " stop; rc-update del " + q(name) + " default; rm -f " + q(initScript) + "; " +
		"else systemctl stop " + q(name) + "; systemctl disable " + q(name) + "; rm -f " + q(unit) + "; systemctl daemon-reload; fi; " +
		"rm -rf " + q(dir), nil
}
