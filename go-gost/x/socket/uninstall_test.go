package socket

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeInstall points every path the uninstaller touches into a temp dir.
func fakeInstall(t *testing.T, name string) (root, dir, binary string) {
	t.Helper()
	root = t.TempDir()
	dir = filepath.Join(root, name)
	binary = filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Join(dir, "xray", "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{binary, filepath.Join(dir, "config.json"), filepath.Join(dir, ".image_version"), filepath.Join(dir, "xray", "bin", "xray")} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	oldSystemd, oldInit := uninstallSystemdDir, uninstallInitDir
	uninstallSystemdDir, uninstallInitDir = filepath.Join(root, "systemd"), filepath.Join(root, "init.d")
	t.Cleanup(func() { uninstallSystemdDir, uninstallInitDir = oldSystemd, oldInit })
	return root, dir, binary
}

func TestAgentUninstallScriptRemovesServiceAndDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script")
	}
	for _, name := range []string{"flyx-agent", "flux_agent"} {
		t.Run(name, func(t *testing.T) {
			root, dir, binary := fakeInstall(t, name)
			if err := os.MkdirAll(uninstallSystemdDir, 0o755); err != nil {
				t.Fatal(err)
			}
			unit := filepath.Join(uninstallSystemdDir, name+".service")
			if err := os.WriteFile(unit, []byte("[Unit]\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(root, "bin")
			log := filepath.Join(root, "calls.log")
			if err := os.MkdirAll(bin, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte("#!/bin/sh\necho \"systemctl $*\" >> "+log+"\nexit 1\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			script, err := buildAgentUninstallScript(binary, dir)
			if err != nil {
				t.Fatal(err)
			}
			script = strings.Replace(script, "sleep 1; ", "", 1)
			cmd := exec.Command("sh", "-c", script)
			cmd.Env = append(os.Environ(), "PATH="+bin+":/usr/bin:/bin")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("script: %v\n%s", err, out)
			}
			calls, _ := os.ReadFile(log)
			for _, want := range []string{"systemctl stop " + name, "systemctl disable " + name, "systemctl daemon-reload"} {
				if !strings.Contains(string(calls), want) {
					t.Errorf("missing %q in:\n%s", want, calls)
				}
			}
			// A failing systemctl must not stop the cleanup.
			for _, gone := range []string{unit, dir} {
				if _, err := os.Stat(gone); !os.IsNotExist(err) {
					t.Errorf("%s still exists", gone)
				}
			}
		})
	}
}

func TestAgentUninstallScriptRejectsForeignDirectories(t *testing.T) {
	for _, tc := range []struct{ binary, dir string }{
		{"/usr/local/bin/flyx-agent", "/usr/local/bin"},
		{"/etc/flyx-agent/flyx-agent", "/etc"},
		{"/etc/flyx-agent/flyx-agent", "/"},
		{"/tmp/x/other-binary", "/tmp/x"},
		{"/etc/flyx-agent/flyx-agent", "relative/flyx-agent"},
	} {
		if _, err := buildAgentUninstallScript(tc.binary, tc.dir); err == nil {
			t.Errorf("%s in %s accepted", tc.binary, tc.dir)
		}
	}
}

func TestContainerUninstallKeepsOnlyMarkerAndBinary(t *testing.T) {
	_, dir, binary := fakeInstall(t, "flyx-agent")
	oldTune, oldKernels, oldExit := uninstallRestoreTuning, uninstallKernels, uninstallExit
	oldDir, oldBin, oldContainer, oldStart := uninstallDataDir, uninstallBinaryPath, uninstallInContainer, uninstallStartScript
	t.Cleanup(func() {
		uninstallRestoreTuning, uninstallKernels, uninstallExit = oldTune, oldKernels, oldExit
		uninstallDataDir, uninstallBinaryPath, uninstallInContainer, uninstallStartScript = oldDir, oldBin, oldContainer, oldStart
	})
	var steps []string
	uninstallRestoreTuning = func() error { steps = append(steps, "tuning"); return nil }
	uninstallKernels = func() error { steps = append(steps, "kernels"); return nil }
	exited := -1
	uninstallExit = func(code int) { exited = code }
	uninstallDataDir = func() string { return dir }
	uninstallBinaryPath = func() string { return binary }
	uninstallInContainer = func() bool { return true }
	uninstallStartScript = func(string) error { t.Fatal("container must not run the native uninstaller"); return nil }

	if AgentUninstalled() {
		t.Fatal("marked before uninstall")
	}
	runAgentUninstall()
	if strings.Join(steps, ",") != "tuning,kernels" || exited != 0 {
		t.Fatalf("steps=%v exit=%d", steps, exited)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var left []string
	for _, e := range entries {
		left = append(left, e.Name())
	}
	if strings.Join(left, ",") != ".image_version,.uninstalled,flyx-agent" {
		t.Fatalf("left in data dir: %v", left)
	}
	if !AgentUninstalled() {
		t.Fatal("marker missing")
	}
}

func TestNativeUninstallStartsScriptAfterCleanup(t *testing.T) {
	_, dir, binary := fakeInstall(t, "flyx-agent")
	oldTune, oldKernels, oldExit := uninstallRestoreTuning, uninstallKernels, uninstallExit
	oldDir, oldBin, oldContainer, oldStart := uninstallDataDir, uninstallBinaryPath, uninstallInContainer, uninstallStartScript
	t.Cleanup(func() {
		uninstallRestoreTuning, uninstallKernels, uninstallExit = oldTune, oldKernels, oldExit
		uninstallDataDir, uninstallBinaryPath, uninstallInContainer, uninstallStartScript = oldDir, oldBin, oldContainer, oldStart
	})
	var steps []string
	uninstallRestoreTuning = func() error { steps = append(steps, "tuning"); return os.ErrPermission }
	uninstallKernels = func() error { steps = append(steps, "kernels"); return nil }
	uninstallExit = func(int) { t.Fatal("native agent must be stopped by its service manager") }
	uninstallDataDir = func() string { return dir }
	uninstallBinaryPath = func() string { return binary }
	uninstallInContainer = func() bool { return false }
	uninstallStartScript = func(script string) error {
		steps = append(steps, "script")
		if !strings.Contains(script, "rm -rf '"+dir+"'") {
			t.Errorf("script does not remove %s: %s", dir, script)
		}
		return nil
	}
	runAgentUninstall()
	// A tuning restore failure is reported but does not block the uninstall.
	if strings.Join(steps, ",") != "tuning,kernels,script" {
		t.Fatalf("steps=%v", steps)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err != nil {
		t.Fatal("native files are removed by the detached script, not in-process")
	}
}
