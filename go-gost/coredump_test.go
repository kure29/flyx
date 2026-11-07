package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func elfHeader(eType byte) []byte {
	h := make([]byte, 64)
	copy(h, "\x7fELF")
	h[4], h[5], h[16] = 2, 1, eType // 64-bit, little-endian, e_type
	return h
}

func TestRemoveCoreDumpsDeletesOnlyCoreFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("core", elfHeader(4))
	write("core.12345", elfHeader(4))
	write("core.1", elfHeader(2))      // an executable, not a dump
	write("core.txt", elfHeader(4))    // name a crash never produces
	write("core.9", []byte("not elf")) // too short and not ELF
	write("config.json", elfHeader(4)) // unrelated name
	if err := os.Mkdir(filepath.Join(dir, "core.7"), 0o700); err != nil {
		t.Fatal(err)
	}

	removed, freed := removeCoreDumps(dir)
	if removed != 2 || freed != 128 {
		t.Fatalf("removed=%d freed=%d", removed, freed)
	}
	for _, name := range []string{"core", "core.12345"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s kept", name)
		}
	}
	for _, name := range []string{"core.1", "core.txt", "core.9", "config.json", "core.7"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("%s removed: %v", name, err)
		}
	}
}

func TestDisableCoreDumpsAppliesToChildren(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no core size limit")
	}
	if err := disableCoreDumps(); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("sh", "-c", "ulimit -c").Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != "0" {
		t.Fatalf("child core limit = %q", got)
	}
}
