//go:build unix

package main

import "syscall"

// disableCoreDumps sets a zero core size limit, which child processes inherit.
func disableCoreDumps() error {
	return syscall.Setrlimit(syscall.RLIMIT_CORE, &syscall.Rlimit{})
}
