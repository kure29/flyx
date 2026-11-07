package kernel

import (
	"debug/elf"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// dynamicELF returns a dynamically linked binary of this machine and the
// path of its loader, or skips.
func dynamicELF(t *testing.T) (string, string) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("ELF checks only apply on Linux")
	}
	for _, p := range []string{"/bin/sh", "/bin/ls", "/usr/bin/env"} {
		f, err := elf.Open(p)
		if err != nil {
			continue
		}
		for _, prog := range f.Progs {
			if prog.Type == elf.PT_INTERP {
				buf := make([]byte, prog.Filesz)
				_, _ = prog.ReadAt(buf, 0)
				f.Close()
				return p, strings.TrimRight(string(buf), "\x00")
			}
		}
		f.Close()
	}
	t.Skip("no dynamically linked system binary found")
	return "", ""
}

// withMissingLoader copies a dynamic binary and points its PT_INTERP at a
// loader that does not exist, like a glibc build on Alpine.
func withMissingLoader(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	f, err := elf.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, prog := range f.Progs {
		if prog.Type == elf.PT_INTERP {
			fake := []byte("/nonexistent/ld")
			if uint64(len(fake)) >= prog.Filesz {
				t.Skip("loader path too short to patch")
			}
			region := data[prog.Off : prog.Off+prog.Filesz]
			for i := range region {
				region[i] = 0
			}
			copy(region, fake)
		}
	}
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestCheckELF(t *testing.T) {
	bin, loader := dynamicELF(t)
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }

	if err := checkELF(bin, runtime.GOARCH, exists); err != nil {
		t.Fatalf("system binary must be runnable: %v", err)
	}
	err := checkELF(bin, runtime.GOARCH, func(p string) bool { return p != loader })
	if err == nil || !strings.Contains(err.Error(), loader) {
		t.Fatalf("missing loader must be reported: %v", err)
	}
	other := "arm64"
	if runtime.GOARCH == "arm64" {
		other = "amd64"
	}
	if err := checkELF(bin, other, exists); err == nil {
		t.Fatal("binary of another CPU must be rejected")
	}
	script := filepath.Join(t.TempDir(), "kernel")
	if err := os.WriteFile(script, []byte(fakeKernel), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := checkELF(script, runtime.GOARCH, exists); err != nil {
		t.Fatalf("non-ELF files are left to exec: %v", err)
	}
}

// A kernel binary that cannot run here (as sing-box's glibc build on Alpine)
// used to stay installed forever, since an existing file was never
// re-downloaded. It must be replaced on the next apply.
func TestEnsureBinaryReplacesUnrunnableCopy(t *testing.T) {
	bin, _ := dynamicELF(t)
	stubNft(t)
	srv, downloads := serveKernel(t, zipWith(t, map[string]string{"xray": fakeKernel}))
	base := t.TempDir()
	binDir := filepath.Join(base, "xray", "bin", "latest")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	withMissingLoader(t, bin, filepath.Join(binDir, "xray"))

	m := NewManager(base)
	defer m.Shutdown()
	st, err := m.Apply(applyRequest(srv, `{"v":1}`, true))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if *downloads != 1 {
		t.Fatalf("unrunnable binary was not re-downloaded (downloads = %d)", *downloads)
	}
	if !st.Running {
		t.Fatalf("kernel not running after reinstall: %+v", st)
	}
	var logs []string
	for _, k := range m.Status("xray").Kernels {
		logs = append(logs, k.Logs...)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "无法在本机运行") {
		t.Fatalf("reinstall reason missing from logs: %v", logs)
	}
}
