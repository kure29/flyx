//go:build windows

package kernel

import (
	"os"
	"syscall"
)

func sysProcAttr() *syscall.SysProcAttr { return nil }

func signalGroup(pid int, _ syscall.Signal) {
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Kill()
	}
}
