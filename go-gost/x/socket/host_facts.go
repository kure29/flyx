package socket

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
)

// HostFacts describes the machine rather than its load, after the "facts"
// monitor's agent sends (github.com/monitor-probe/agent): it is collected on
// connect and every hostFactsRecheck, and sent only on the first report of a
// connection and when it changes.
type HostFacts struct {
	OS             string `json:"os"`
	Kernel         string `json:"kernel"`
	Arch           string `json:"arch"`
	CPUName        string `json:"cpu_name"`
	Virtualization string `json:"virtualization"`
}

const hostFactsRecheck = 60 * time.Second

// hostFactsTracker decides which report carries the facts. It is used from
// the report loop only.
type hostFactsTracker struct {
	collect   func() HostFacts
	facts     HostFacts
	checkedAt time.Time
	sent      bool
}

func newHostFactsTracker() *hostFactsTracker {
	return &hostFactsTracker{collect: collectHostFacts}
}

// reset is called for each new connection: its first report carries the
// facts again.
func (t *hostFactsTracker) reset() {
	if t != nil {
		t.sent = false
	}
}

// next returns the facts to attach to the report made at now, or nil.
func (t *hostFactsTracker) next(now time.Time) *HostFacts {
	if t == nil {
		return nil
	}
	if t.checkedAt.IsZero() || now.Sub(t.checkedAt) >= hostFactsRecheck {
		fresh := t.collect()
		t.checkedAt = now
		if fresh != t.facts {
			t.facts = fresh
			t.sent = false
		}
	}
	if t.sent {
		return nil
	}
	t.sent = true
	f := t.facts
	return &f
}

func collectHostFacts() HostFacts {
	var f HostFacts
	if info, err := host.Info(); err == nil && info != nil {
		f.Kernel = info.KernelVersion
		f.Arch = info.KernelArch
		f.OS = strings.TrimSpace(info.Platform + " " + info.PlatformVersion)
	}
	if pretty := osPrettyName("/"); pretty != "" {
		f.OS = pretty
	}
	if infos, err := cpu.Info(); err == nil && len(infos) > 0 {
		f.CPUName = strings.Join(strings.Fields(infos[0].ModelName), " ")
	}
	f.Virtualization = virtualization()
	return f
}

// osPrettyName reads PRETTY_NAME from os-release(5) under root.
func osPrettyName(root string) string {
	for _, p := range []string{"etc/os-release", "usr/lib/os-release"} {
		data, err := os.ReadFile(filepath.Join(root, p))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(line), "PRETTY_NAME="); ok {
				return strings.Trim(strings.TrimSpace(v), `"'`)
			}
		}
		return ""
	}
	return ""
}

// virtualization names the container or hypervisor this system runs in,
// "none" on bare metal. A container is named rather than the machine whose
// kernel it shares.
func virtualization() string {
	mounts, _ := os.ReadFile("/proc/self/mounts")
	if c := containerKind("/", string(mounts)); c != "" {
		return c
	}
	if system, role, err := host.Virtualization(); err == nil && role == "guest" && system != "" {
		return system
	}
	return "none"
}

// containerKind judges the container from files any process can read, as
// monitor's agent does:
//   - OpenVZ: /proc/vz without the /proc/bc only the host has.
//   - What systemd, as PID 1 in a container, records in /run/systemd/container.
//   - The marker files Docker and Podman place in each container.
//   - LXCFS mounted over /proc (Proxmox and LXD containers, including ones
//     booted by OpenRC, which record nothing).
func containerKind(root, mounts string) string {
	has := func(p string) bool {
		_, err := os.Stat(filepath.Join(root, p))
		return err == nil
	}
	if has("proc/vz") && !has("proc/bc") {
		return "openvz"
	}
	if data, err := os.ReadFile(filepath.Join(root, "run/systemd/container")); err == nil {
		if name := strings.TrimSpace(string(data)); name != "" {
			return name
		}
	}
	if has(".dockerenv") {
		return "docker"
	}
	if has("run/.containerenv") {
		return "podman"
	}
	for _, line := range strings.Split(mounts, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[2] == "fuse.lxcfs" && strings.HasPrefix(fields[1], "/proc/") {
			return "lxc"
		}
	}
	return ""
}

// getSwapInfo returns used and total swap in bytes.
func getSwapInfo() (used, total uint64) {
	s, err := mem.SwapMemory()
	if err != nil || s == nil {
		return 0, 0
	}
	return s.Used, s.Total
}

// getProcessCount returns the number of processes, 0 when unknown.
func getProcessCount() int64 {
	m, err := load.Misc()
	if err != nil || m == nil || m.ProcsTotal <= 0 {
		return 0
	}
	return int64(m.ProcsTotal)
}
