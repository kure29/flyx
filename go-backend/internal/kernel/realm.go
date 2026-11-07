package kernel

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// realm (zhboner/realm) runs the port forwards of tunnels whose engine is
// realm instead of the agent's own forwarder. It is installed and supervised
// like a kernel, but its config comes from forwards, not inbounds.

const (
	TypeRealm = "realm"
	// RealmVersion is the realm release the panel installs.
	RealmVersion = "2.9.6"
	// RealmCounterBase offsets forward ids in the agent's per-port counters,
	// which share an id space with inbounds: counter id = base + forward id.
	RealmCounterBase int64 = 1 << 40
)

// realm publishes no checksum files; hashes are pinned per arch, checked
// against the release downloads (musl builds run on any distro).
var realmDownload = &Download{
	URL:     "https://github.com/zhboner/realm/releases/download/v{VERSION}/realm-{ARCH}.tar.gz",
	Archive: "tar.gz",
	BinName: "realm",
	ArchMap: map[string]string{"amd64": "x86_64-unknown-linux-musl", "arm64": "aarch64-unknown-linux-musl", "arm": "armv7-unknown-linux-musleabihf"},
	Pinned: map[string]map[string]string{
		RealmVersion: {
			"x86_64-unknown-linux-musl":      "b1cc335547bea8bb2a88178bef12ec7f2363e36200e7ea1d4e1e67627929bf65",
			"aarch64-unknown-linux-musl":     "f4c0318dd86854da483dcb7645b4f39cae2cc3f91c688fef969d53220b949488",
			"armv7-unknown-linux-musleabihf": "ceedaf367c78f911eac227b2b88b3132f964d05a8b71f7a6a7e429dc78ec1862",
		},
	},
	Mirrorable: true,
}

// RealmEndpoint is one forward port realm listens on.
type RealmEndpoint struct {
	ForwardID int64
	// Listen is host:port ("[::]:31001").
	Listen string
	// Remotes are the targets; more than one balances between them.
	Remotes []string
	// IPHash keeps each client on one remote; otherwise remotes take turns
	// (realm has no failover or random mode, so those balance too).
	IPHash bool
	// Interface binds outgoing connections to a network interface.
	Interface string
	// AcceptProxy / SendProxy: PROXY protocol from clients / to targets.
	AcceptProxy bool
	SendProxy   int // version, 0 = off
}

// RealmCounterID is the counter id of a forward's realm port.
func RealmCounterID(forwardID int64) int64 { return RealmCounterBase + forwardID }

// RealmForwardID returns the forward a counter id belongs to (ok false for
// inbound counters).
func RealmForwardID(counterID int64) (int64, bool) {
	if counterID <= RealmCounterBase {
		return 0, false
	}
	return counterID - RealmCounterBase, true
}

func tomlString(s string) string { return strconv.Quote(s) }

// RenderRealm renders realm's config for a node's forward ports; with none
// the kernel is idle (applied disabled).
func RenderRealm(endpoints []RealmEndpoint) (Rendered, error) {
	sorted := append([]RealmEndpoint(nil), endpoints...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].ForwardID != sorted[j].ForwardID {
			return sorted[i].ForwardID < sorted[j].ForwardID
		}
		return sorted[i].Listen < sorted[j].Listen
	})
	var b strings.Builder
	b.WriteString("[log]\nlevel = \"warn\"\n\n[network]\nuse_udp = true\n")
	for _, e := range sorted {
		if e.Listen == "" || len(e.Remotes) == 0 {
			return Rendered{}, fmt.Errorf("转发 %d 缺少监听或目标地址", e.ForwardID)
		}
		fmt.Fprintf(&b, "\n[[endpoints]]\n# forward %d\nlisten = %s\nremote = %s\n", e.ForwardID, tomlString(e.Listen), tomlString(e.Remotes[0]))
		if len(e.Remotes) > 1 {
			extra := make([]string, len(e.Remotes)-1)
			weights := []string{"1"}
			for i, r := range e.Remotes[1:] {
				extra[i] = tomlString(r)
				weights = append(weights, "1")
			}
			mode := "roundrobin"
			if e.IPHash {
				mode = "iphash"
			}
			fmt.Fprintf(&b, "extra_remotes = [%s]\nbalance = %s\n", strings.Join(extra, ", "),
				tomlString(mode+": "+strings.Join(weights, ", ")))
		}
		if e.Interface != "" {
			fmt.Fprintf(&b, "interface = %s\n", tomlString(e.Interface))
		}
		if e.AcceptProxy || e.SendProxy > 0 {
			var opts []string
			if e.AcceptProxy {
				opts = append(opts, "accept_proxy = true")
			}
			if e.SendProxy > 0 {
				opts = append(opts, "send_proxy = true", fmt.Sprintf("send_proxy_version = %d", e.SendProxy))
			}
			fmt.Fprintf(&b, "network = { %s }\n", strings.Join(opts, ", "))
		}
	}
	out := Rendered{
		Files: []File{{Name: "config.toml", Content: b.String()}},
		Args:  []string{"-c", "{RUN_DIR}/config.toml"},
		Idle:  len(sorted) == 0,
	}
	out.Hash = hashRendered(out)
	return out, nil
}
