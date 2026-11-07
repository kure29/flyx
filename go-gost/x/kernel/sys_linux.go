//go:build linux

package kernel

import "syscall"

// Kernels run in their own process group and die with the agent, so a
// crashed agent never leaves an orphan holding the inbound ports.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
}

func signalGroup(pid int, sig syscall.Signal) {
	if err := syscall.Kill(-pid, sig); err != nil {
		_ = syscall.Kill(pid, sig)
	}
}
