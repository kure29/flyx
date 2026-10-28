package nftables

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Keep quota filtering outside the NAT table: changing a block must neither
// reset traffic counters nor leave established conntrack flows forwarding.
const quotaTable = `table inet flyx_quota {
    set blocked_ports { type inet_service; }
    chain incoming {
        type filter hook prerouting priority -110; policy accept;
        fib daddr type local meta l4proto { tcp, udp } th dport @blocked_ports drop
    }
    chain transit {
        type filter hook forward priority -10; policy accept;
        ct status dnat meta l4proto { tcp, udp } ct original proto-dst @blocked_ports drop
    }
    chain outgoing {
        type filter hook output priority -10; policy accept;
        ct status dnat meta l4proto { tcp, udp } ct original proto-dst @blocked_ports drop
    }
}`

func (r *SSHRunner) SetBlocked(ctx context.Context, cfg SSHConfig, port int, blocked bool) error {
	if port < 1 || port > 65535 {
		return errors.New("invalid nftables port")
	}
	return r.run(ctx, cfg, buildSetBlockedCommand(nftBinary(cfg), port, blocked))
}

func buildSetBlockedCommand(nft string, port int, blocked bool) string {
	command := "set -eu\n" + selectTableCommand(nft, "flyx_quota", "flvx_quota") +
		"if ! " + nft + " list table inet \"$table\" >/dev/null 2>&1; then\n" +
		nft + " -f - <<EOF\n" + strings.ReplaceAll(quotaTable, "flyx_quota", "$table") + "\nEOF\nfi\n"
	element := fmt.Sprintf(" element inet \"$table\" blocked_ports '{ %d }'", port)
	if blocked {
		return command + nft + " add" + element
	}
	return command + "if " + nft + " get" + element + " >/dev/null 2>&1; then\n" + nft + " delete" + element + "\nfi"
}

func (m *Manager) SetBlocked(ctx context.Context, cfg SSHConfig, port int, blocked bool) error {
	if err := m.ensureInitialized(); err != nil {
		return err
	}
	runner, ok := m.runner.(interface {
		SetBlocked(context.Context, SSHConfig, int, bool) error
	})
	if !ok {
		return errors.New("nftables runner does not support traffic blocking")
	}
	return runner.SetBlocked(ctx, cfg, port, blocked)
}

// Clearing nftables also removes quota filters so those ports can be reused.
func (r *SSHRunner) ClearBlocks(ctx context.Context, cfg SSHConfig) error {
	nft := nftBinary(cfg)
	return r.run(ctx, cfg, "set -eu\n"+selectTableCommand(nft, "flyx_quota", "flvx_quota")+"if "+nft+" list table inet \"$table\" >/dev/null 2>&1; then\n"+nft+" delete table inet \"$table\"\nfi")
}
