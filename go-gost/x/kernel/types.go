// Package kernel runs third-party proxy kernels (Xray, sing-box, Snell, ...)
// next to the agent. The panel renders every configuration file; this package
// only installs binaries, writes files, supervises processes and counts
// per-inbound traffic with nftables.
package kernel

import (
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"strings"
)

// Download describes how to fetch one binary. Integrity is mandatory: the
// digest comes from SHA256 (pinned, keyed by the mapped arch), a checksum file,
// or the GitHub release asset digest; without one the install is refused.
type Download struct {
	URL         string            `json:"url"`
	ChecksumURL string            `json:"checksumUrl"`
	Archive     string            `json:"archive"` // zip | tar.gz | raw
	BinName     string            `json:"binName"`
	ArchMap     map[string]string `json:"archMap"`
	SHA256      map[string]string `json:"sha256,omitempty"`
	GitHub      *GitHubRelease    `json:"github,omitempty"`
}

// GitHubRelease lets the agent resolve a release itself when the panel could
// not (URL still holds {VERSION}, or no digest for this arch).
type GitHubRelease struct {
	Repo  string `json:"repo"`  // owner/name
	Tag   string `json:"tag"`   // "" = latest
	Asset string `json:"asset"` // file name template with {VERSION} and {ARCH}
	Proxy string `json:"proxy"` // optional mirror prefix for the download URL
}

// ExtraBinary is a helper program a kernel needs next to its main binary
// (e.g. shadow-tls in front of Snell).
type ExtraBinary struct {
	Name     string    `json:"name"`
	Version  string    `json:"version"`
	Download *Download `json:"download"`
}

// ProcessSpec is one supervised process of a kernel. Bin is "" for the main
// binary or the Name of an extra binary.
type ProcessSpec struct {
	Name string   `json:"name"`
	Bin  string   `json:"bin"`
	Args []string `json:"args"`
}

type File struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type Port struct {
	InboundID int64  `json:"inboundId"`
	Port      int    `json:"port"`
	Network   string `json:"network"` // "tcp", "udp" or "tcp,udp"
	// Allow, when present, restricts the port to these source CIDRs; every
	// other packet to it is dropped. Absent means no restriction.
	Allow []string `json:"allow"`
	// HopFrom..HopTo (UDP) are redirected to Port: Hysteria2 port hopping.
	HopFrom int `json:"hopFrom,omitempty"`
	HopTo   int `json:"hopTo,omitempty"`
}

// ApplyRequest is the KernelApply command body.
type ApplyRequest struct {
	Type     string    `json:"type"`
	Version  string    `json:"version"`
	Download *Download `json:"download"`
	Files    []File    `json:"files"`
	Args     []string  `json:"args"`
	// Processes, when set, replaces Args with several processes (Snell runs
	// one per inbound, plus shadow-tls fronts).
	Processes []ProcessSpec `json:"processes,omitempty"`
	Extras    []ExtraBinary `json:"extras,omitempty"`
	Hash      string        `json:"hash"`
	Enabled   bool          `json:"enabled"`
	Ports     []Port        `json:"ports"`
	// UserMarks are subscription users whose connections the kernel marks
	// with userMark(id); their traffic is counted per user.
	UserMarks []int64 `json:"userMarks,omitempty"`
	// GeoData are data files kept current next to the main binary (Xray's
	// geosite.dat / geoip.dat).
	GeoData []GeoFile `json:"geoData,omitempty"`
}

type State struct {
	Type       string   `json:"type"`
	Running    bool     `json:"running"`
	Enabled    bool     `json:"enabled"`
	Pid        int      `json:"pid"`
	Version    string   `json:"version"`
	ConfigHash string   `json:"configHash"`
	Error      string   `json:"error"`
	Restarts   int      `json:"restarts"`
	StartedAt  int64    `json:"startedAt"`
	Logs       []string `json:"logs,omitempty"`
}

type Counter struct {
	InboundID int64  `json:"inboundId"`
	Proto     string `json:"proto"`
	Dir       string `json:"dir"` // "in" = client upload, "out" = client download
	Bytes     int64  `json:"bytes"`
}

// UserCounter is one user's traffic in one direction ("up" = the user's
// upload, "down" = download) since the counters' epoch.
type UserCounter struct {
	ID    int64  `json:"id"`
	Dir   string `json:"dir"`
	Bytes int64  `json:"bytes"`
}

type Status struct {
	Kernels      []State       `json:"kernels"`
	Counters     []Counter     `json:"counters"`
	Users        []UserCounter `json:"users,omitempty"`
	Epoch        string        `json:"epoch"`
	NftAvailable bool          `json:"nftAvailable"`
	NftError     string        `json:"nftError,omitempty"`
}

type adapter struct {
	versionArgs []string
	// fixFile adjusts a rendered file to this host before it is written.
	fixFile func(name, content string) string
}

var adapters = map[string]adapter{
	"xray":    {versionArgs: []string{"version"}},
	"singbox": {versionArgs: []string{"version"}},
	"snell":   {versionArgs: []string{"-v"}},
	// realm runs the forwards of realm-engine tunnels.
	"realm": {versionArgs: []string{"-v"}, fixFile: realmFixFile},
}

// realmFixFile listens on IPv4 only on hosts without IPv6 sockets: realm,
// unlike Go, cannot bind [::] there.
func realmFixFile(name, content string) string {
	if name != "config.toml" || ipv6Sockets() {
		return content
	}
	return strings.ReplaceAll(content, `listen = "[::]:`, `listen = "0.0.0.0:`)
}

var ipv6Sockets = func() bool {
	c, err := net.ListenPacket("udp6", "[::]:0")
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

var (
	safeNamePattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	safeVersionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{0,39}$`)
)

func safeName(name string) bool {
	return safeNamePattern.MatchString(name) && !strings.Contains(name, "..")
}

func (r *ApplyRequest) validate() error {
	if _, ok := adapters[r.Type]; !ok {
		return fmt.Errorf("不支持的内核类型: %s", r.Type)
	}
	if r.Version == "" {
		r.Version = "latest"
	}
	if !safeVersionPattern.MatchString(r.Version) || strings.Contains(r.Version, "..") {
		return fmt.Errorf("版本号格式错误: %s", r.Version)
	}
	if err := r.Download.validate(); err != nil {
		return err
	}
	extras := make(map[string]bool, len(r.Extras))
	for _, e := range r.Extras {
		if !safeName(e.Name) || !safeVersionPattern.MatchString(e.Version) || strings.Contains(e.Version, "..") {
			return fmt.Errorf("非法附加程序: %s", e.Name)
		}
		if err := e.Download.validate(); err != nil {
			return err
		}
		extras[e.Name] = true
	}
	seen := make(map[string]bool, len(r.Processes))
	for _, p := range r.Processes {
		if !safeName(p.Name) || seen[p.Name] {
			return fmt.Errorf("非法进程名: %s", p.Name)
		}
		seen[p.Name] = true
		if p.Bin != "" && !extras[p.Bin] {
			return fmt.Errorf("进程 %s 引用了未声明的程序 %s", p.Name, p.Bin)
		}
	}
	for _, f := range r.Files {
		if !safeName(f.Name) || filepath.Base(f.Name) != f.Name {
			return fmt.Errorf("非法配置文件名: %s", f.Name)
		}
	}
	for _, g := range r.GeoData {
		if err := g.validate(); err != nil {
			return err
		}
	}
	for _, id := range r.UserMarks {
		if id <= 0 || id > userMarkMax {
			return fmt.Errorf("非法用户标记: %d", id)
		}
	}
	return r.validatePorts()
}

func (r *ApplyRequest) validatePorts() error {
	for _, p := range r.Ports {
		if p.Port < 1 || p.Port > 65535 || p.InboundID <= 0 {
			return fmt.Errorf("非法端口: %d", p.Port)
		}
		if p.HopFrom != 0 || p.HopTo != 0 {
			if p.HopFrom < 1 || p.HopTo > 65535 || p.HopFrom >= p.HopTo || (p.Port >= p.HopFrom && p.Port <= p.HopTo) {
				return fmt.Errorf("非法端口跳跃范围: %d-%d", p.HopFrom, p.HopTo)
			}
		}
		if len(p.Allow) > 512 {
			return fmt.Errorf("白名单条目过多")
		}
		for _, c := range p.Allow {
			if _, _, err := net.ParseCIDR(c); err != nil {
				return fmt.Errorf("非法白名单条目: %q", c)
			}
		}
	}
	return nil
}

func (d *Download) validate() error {
	if d == nil || !safeName(d.BinName) {
		return fmt.Errorf("缺少下载信息")
	}
	switch d.Archive {
	case "zip", "tar.gz", "raw":
	default:
		return fmt.Errorf("不支持的压缩格式: %s", d.Archive)
	}
	for arch, sum := range d.SHA256 {
		if !sha256HexPattern.MatchString(sum) || len(sum) != 64 || !safeName(arch) {
			return fmt.Errorf("非法校验值")
		}
	}
	if g := d.GitHub; g != nil {
		if !githubRepoPattern.MatchString(g.Repo) || (g.Tag != "" && !safeVersionPattern.MatchString(strings.TrimPrefix(g.Tag, "v"))) {
			return fmt.Errorf("非法 GitHub 发布信息")
		}
	}
	return nil
}

var githubRepoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// processSpecs returns the processes to run, falling back to the single
// main-binary process described by Args.
func (r ApplyRequest) processSpecs() []ProcessSpec {
	if len(r.Processes) > 0 {
		return r.Processes
	}
	return []ProcessSpec{{Name: r.Type, Args: r.Args}}
}
