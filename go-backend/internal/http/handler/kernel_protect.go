package handler

import (
	"context"
	"log"
	"net"
	"sort"
	"strings"
	"time"

	"go-backend/internal/kernel"
	"go-backend/internal/store/model"
)

// relayProxyProtocolVersion is what relay forwards send when the inbound
// wants the real client address. v1 is accepted by every kernel we support.
const relayProxyProtocolVersion = 1

var loopbackAllow = []string{"127.0.0.0/8", "::1/128"}

// inboundAllowList computes the sources allowed to reach a whitelisted
// inbound: loopback (tunnel exits dial locally), every address of the nodes
// that dial the landing node (entry nodes of port forwards, exit nodes of
// tunnel forwards), the nodes of kernels chained through it, and the user's
// extra entries.
func (h *Handler) inboundAllowList(row model.NodeInbound) []string {
	set := make(map[string]bool)
	for _, c := range loopbackAllow {
		set[c] = true
	}
	if extra, err := kernel.ParseAllowList(row.WhitelistExtra); err == nil {
		for _, c := range extra {
			set[c] = true
		}
	}

	relays, err := h.repo.ListInboundRelays(row.ID)
	if err != nil {
		log.Printf("whitelist relay lookup failed inbound_id=%d err=%v", row.ID, err)
	}
	seenNodes := make(map[int64]bool)
	for _, rel := range relays {
		if !rel.ForwardExists {
			continue
		}
		dialers, err := h.relayDialerNodeIDs(rel.TunnelID, rel.TunnelType)
		if err != nil {
			continue
		}
		for _, nodeID := range dialers {
			if seenNodes[nodeID] {
				continue
			}
			seenNodes[nodeID] = true
			node, err := h.repo.GetNodeByID(nodeID)
			if err != nil || node == nil {
				continue
			}
			for _, host := range nodeAddresses(node) {
				for _, c := range resolveAllowHost(host) {
					set[c] = true
				}
			}
		}
	}

	// Kernels chained through the inbound dial it from their own node.
	for _, nodeID := range h.chainDialerNodeIDs(row.ID) {
		if seenNodes[nodeID] {
			continue
		}
		seenNodes[nodeID] = true
		node, err := h.repo.GetNodeByID(nodeID)
		if err != nil || node == nil {
			continue
		}
		for _, host := range nodeAddresses(node) {
			for _, c := range resolveAllowHost(host) {
				set[c] = true
			}
		}
	}

	out := make([]string, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// relayDialerNodeIDs returns the nodes whose connections reach the landing
// node: a port-forward tunnel dials from its entry nodes, a tunnel forward
// from its exit nodes.
func (h *Handler) relayDialerNodeIDs(tunnelID int64, tunnelType int) ([]int64, error) {
	if tunnelType == 2 {
		return h.repo.TunnelExitNodeIDs(tunnelID)
	}
	return h.tunnelEntryNodeIDs(tunnelID)
}

func nodeAddresses(node *model.Node) []string {
	raw := []string{node.ServerIP, node.ServerIPV4.String, node.ServerIPV6.String, node.ExtraIPs.String}
	var out []string
	for _, r := range raw {
		out = append(out, strings.FieldsFunc(r, func(c rune) bool {
			return c == ',' || c == ';' || c == ' ' || c == '\n' || c == '\t'
		})...)
	}
	return out
}

// resolveAllowHost turns an IP or hostname into CIDRs; hostnames are resolved
// with a short timeout so an unreachable DNS server cannot stall an apply.
func resolveAllowHost(host string) []string {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" {
		return nil
	}
	if c, err := kernel.NormalizeAllowEntry(host); err == nil {
		return []string{c}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		log.Printf("whitelist: cannot resolve %q: %v", host, err)
		return nil
	}
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if c, err := kernel.NormalizeAllowEntry(a.IP.String()); err == nil {
			out = append(out, c)
		}
	}
	return out
}

// syncRelayProxyProtocol makes every managed relay forward of an inbound send
// (or stop sending) PROXY protocol to match the inbound setting.
func (h *Handler) syncRelayProxyProtocol(row model.NodeInbound) []string {
	want := 0
	if row.AcceptProxyProtocol == 1 {
		want = relayProxyProtocolVersion
	}
	relays, err := h.repo.ListInboundRelays(row.ID)
	if err != nil {
		return []string{err.Error()}
	}
	var problems []string
	for _, rel := range relays {
		if !rel.ForwardExists || rel.Managed != 1 {
			continue
		}
		fwd, err := h.getForwardRecord(rel.ForwardID)
		if err != nil || fwd == nil {
			continue
		}
		_, current := normalizeForwardProxyProtocol(fwd.ProxyProtocol, fwd.ProxyProtocolReceive, fwd.ProxyProtocolSend)
		if current == want {
			continue
		}
		if err := h.repo.UpdateForward(fwd.ID, fwd.Name, fwd.TunnelID, fwd.RemoteAddr, fwd.Strategy, time.Now().UnixMilli(),
			0, fwd.ProxyProtocolReceive, want); err != nil {
			problems = append(problems, rel.Name+": "+err.Error())
			continue
		}
		updated, err := h.getForwardRecord(fwd.ID)
		if err != nil || updated == nil {
			continue
		}
		if err := h.syncForwardServices(updated, "UpdateService", true); err != nil {
			problems = append(problems, rel.Name+": "+err.Error())
		}
	}
	return problems
}

// refreshInboundWhitelist re-applies the kernel when the relay set of a
// whitelisted inbound changed, so the allowed sources follow the relays.
func (h *Handler) refreshInboundWhitelist(inboundID int64) {
	row, err := h.repo.GetNodeInbound(inboundID)
	if err != nil || row == nil || row.RelayWhitelist != 1 {
		return
	}
	if err := h.applyKernel(row.KernelID, false); err != nil {
		log.Printf("whitelist refresh failed inbound_id=%d err=%v", inboundID, err)
	}
}
