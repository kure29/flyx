package handler

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/kernel"
	"go-backend/internal/store/model"
)

var (
	reachProbeInterval = 3 * time.Minute
	reachProbeTimeout  = 4 * time.Second
)

const reachProbeParallel = 16

// reachProbe is the last TCP reachability check of an inbound (one the panel
// built or an imported node), made from the panel server. Inbounds the panel
// cannot meaningfully dial are skipped: UDP-only protocols (Hysteria2,
// TUIC), loopback listeners, relay-whitelisted ports, and built inbounds
// whose node is offline or whose kernel is not running (those already show,
// and alert, as such).
type reachProbe struct {
	OK        bool   `json:"ok"`
	Skipped   bool   `json:"skipped,omitempty"`
	LatencyMs int64  `json:"latencyMs,omitempty"`
	Error     string `json:"error,omitempty"`
	CheckedAt int64  `json:"checkedAt"`
	// From is where the check ran when not from the panel server.
	From string `json:"from,omitempty"`
	// Points is a node's latency to each reference point (node_latency.go).
	Points []latencyPoint `json:"points,omitempty"`
}

// probeRounds remembers when the round that stored each result started
// (unix ns). Rounds overlap (scheduled, clicked, after a reconnect), and a
// slow round that started earlier must not overwrite a newer result with
// what it saw before, such as a node that was still offline.
type probeRounds map[int64]int64

// take reports whether a round that started at started may store id's
// result, and records it as the latest when so.
func (r probeRounds) take(id, started int64) bool {
	if r[id] > started {
		return false
	}
	r[id] = started
	return true
}

type reachProbeStore struct {
	mu      sync.RWMutex
	results map[int64]reachProbe
	rounds  probeRounds
	// failStreak counts consecutive failed probes; down marks nodes already
	// reported as unreachable, so alerts fire once per outage.
	failStreak map[int64]int
	down       map[int64]bool
}

func newReachProbeStore() *reachProbeStore {
	return &reachProbeStore{
		results:    make(map[int64]reachProbe),
		rounds:     make(probeRounds),
		failStreak: make(map[int64]int),
		down:       make(map[int64]bool),
	}
}

type probeTransition struct {
	row  model.NodeInbound
	down bool
	err  string
}

// track updates the outage state of enabled nodes and returns the ones that
// just went down (after notifyInboundDownStreak failures) or came back.
func (s *reachProbeStore) track(rows map[int64]model.NodeInbound, results map[int64]reachProbe) []probeTransition {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []probeTransition
	for id, p := range results {
		row, ok := rows[id]
		if !ok || p.Skipped || row.Enabled != 1 {
			delete(s.failStreak, id)
			delete(s.down, id)
			continue
		}
		if p.OK {
			delete(s.failStreak, id)
			if s.down[id] {
				delete(s.down, id)
				out = append(out, probeTransition{row: row})
			}
			continue
		}
		s.failStreak[id]++
		if s.failStreak[id] >= notifyInboundDownStreak && !s.down[id] {
			s.down[id] = true
			out = append(out, probeTransition{row: row, down: true, err: p.Error})
		}
	}
	return out
}

func (s *reachProbeStore) get(id int64) (reachProbe, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.results[id]
	return p, ok
}

// replace stores fresh results and drops entries of inbounds that no longer
// exist when the whole set was probed.
func (s *reachProbeStore) replace(results map[int64]reachProbe, all bool) {
	s.replaceRound(results, all, time.Now().UnixNano())
}

// replaceRound is replace for a round that started at started (unix ns):
// results of a newer round that already finished are kept.
func (s *reachProbeStore) replaceRound(results map[int64]reachProbe, all bool, started int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if all {
		for id := range s.results {
			if _, ok := results[id]; !ok && s.rounds.take(id, started) {
				delete(s.results, id)
				delete(s.rounds, id)
			}
		}
		for id := range s.failStreak {
			if _, ok := results[id]; !ok {
				delete(s.failStreak, id)
			}
		}
		for id := range s.down {
			if _, ok := results[id]; !ok {
				delete(s.down, id)
			}
		}
	}
	for id, p := range results {
		if s.rounds.take(id, started) {
			s.results[id] = p
		}
	}
}

// reachProbeHost returns the address clients use to reach the inbound, or ""
// when the panel should not probe it. Built inbounds are dialed at their
// share host, else their node's server IP (as chainEndpoint does); the panel
// is not on a relay whitelist, so whitelisted ports would always look down.
// builtNode reports a built inbound's node server IP and whether its node is
// online with the kernel running.
func reachProbeHost(row *model.NodeInbound, builtNode func(row *model.NodeInbound) (ip string, live bool)) string {
	if row.Protocol == kernel.ProtocolHysteria2 || row.Protocol == kernel.ProtocolTUIC {
		return ""
	}
	if isExternalInbound(row) {
		return row.Listen // imports store the node's address here
	}
	if row.RelayWhitelist == 1 || isLoopbackListen(row.Listen) {
		return ""
	}
	ip, live := builtNode(row)
	if !live {
		return ""
	}
	return firstNonEmpty(strings.TrimSpace(row.ShareHost), ip)
}

// reachDial dials an inbound from the panel server; tests replace it.
var reachDial = func(ctx context.Context, addr string) (net.Conn, error) {
	dialer := net.Dialer{Timeout: reachProbeTimeout}
	return dialer.DialContext(ctx, "tcp", addr)
}

// errPanelNoRoute: the panel server has no route to the address at all,
// typically an IPv6-only node while the panel (or its Docker network) has
// no IPv6. That says nothing about the node, so it is not reported down.
var errPanelNoRoute = errors.New("面板服务器没有到该地址的网络（通常是面板没有 IPv6）")

// panelNoRoute: no route to the network (Docker without IPv6), no usable
// source address, or IPv6 switched off on the panel host.
func panelNoRoute(err error) bool {
	return errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EADDRNOTAVAIL) ||
		errors.Is(err, syscall.EAFNOSUPPORT)
}

func probeInboundAt(ctx context.Context, host string, port int) (reachProbe, error) {
	now := time.Now()
	if host == "" {
		return reachProbe{Skipped: true, CheckedAt: now.UnixMilli()}, nil
	}
	conn, err := reachDial(ctx, net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		if panelNoRoute(err) {
			return reachProbe{Skipped: true, Error: errPanelNoRoute.Error(), CheckedAt: time.Now().UnixMilli()}, errPanelNoRoute
		}
		return reachProbe{Error: probeErrorText(err), CheckedAt: time.Now().UnixMilli()}, nil
	}
	_ = conn.Close()
	latency := time.Since(now).Milliseconds()
	if latency < 1 {
		latency = 1
	}
	return reachProbe{OK: true, LatencyMs: latency, CheckedAt: time.Now().UnixMilli()}, nil
}

// probeInboundFromNode is the fallback when the panel cannot route to a
// built inbound: its own node dials the address. That confirms the kernel
// listens there, not that outside clients get through a firewall.
func (h *Handler) probeInboundFromNode(nodeID int64, host string, port int) reachProbe {
	const from = "服务器自检（面板没有 IPv6）"
	data, err := nodeLatencyPing(h, nodeID, host, port)
	now := time.Now().UnixMilli()
	if err != nil {
		return reachProbe{Skipped: true, Error: errPanelNoRoute.Error(), From: from, CheckedAt: now}
	}
	if !asBool(data["success"], false) {
		return reachProbe{Error: "无法连接", From: from, CheckedAt: now}
	}
	latency := int64(asFloat(data["averageTime"], 0) + 0.5)
	if latency < 1 {
		latency = 1
	}
	return reachProbe{OK: true, LatencyMs: latency, From: from, CheckedAt: now}
}

func probeErrorText(err error) string {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "域名解析失败"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "连接超时"
	}
	return "无法连接"
}

// probeInboundReach checks the given inbounds (all of them when ids is
// empty) in parallel and records the results.
func (h *Handler) probeInboundReach(ctx context.Context, ids []int64) (map[int64]reachProbe, error) {
	started := time.Now().UnixNano()
	rows, err := h.repo.ListNodeInbounds()
	if err != nil {
		return nil, err
	}
	want := make(map[int64]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	targets := make([]model.NodeInbound, 0)
	for _, row := range rows {
		if len(ids) > 0 && !want[row.ID] {
			continue
		}
		targets = append(targets, row)
	}
	nodes := make(map[int64]*model.Node)
	kernelRunning := make(map[int64]bool)
	builtNode := func(row *model.NodeInbound) (string, bool) {
		node, ok := nodes[row.NodeID]
		if !ok {
			node, _ = h.repo.GetNodeByID(row.NodeID)
			nodes[row.NodeID] = node
		}
		running, ok := kernelRunning[row.KernelID]
		if !ok {
			k, _ := h.repo.GetNodeKernel(row.KernelID)
			running = k != nil && k.Status == kernelStatusRunning
			kernelRunning[row.KernelID] = running
		}
		if node == nil || node.Status != 1 || !running {
			return "", false
		}
		return inboundShareAddress(row, node), true
	}
	hosts := make(map[int64]string, len(targets))
	for _, row := range targets {
		hosts[row.ID] = reachProbeHost(&row, builtNode)
	}

	results := make(map[int64]reachProbe, len(targets))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, reachProbeParallel)
	for _, row := range targets {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(row model.NodeInbound) {
			defer wg.Done()
			defer func() { <-sem }()
			p, err := probeInboundAt(ctx, hosts[row.ID], row.Port)
			if errors.Is(err, errPanelNoRoute) && !isExternalInbound(&row) {
				p = h.probeInboundFromNode(row.NodeID, hosts[row.ID], row.Port)
			}
			mu.Lock()
			results[row.ID] = p
			mu.Unlock()
		}(row)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	h.reachProbes.replaceRound(results, len(ids) == 0, started)
	// Outage streaks count scheduled rounds only: a click or a reconnect
	// check (which can run before the kernel is back) is not one.
	if len(ids) > 0 {
		return results, nil
	}

	byID := make(map[int64]model.NodeInbound, len(targets))
	for _, row := range targets {
		byID[row.ID] = row
	}
	for _, t := range h.reachProbes.track(byID, results) {
		addr := net.JoinHostPort(hosts[t.row.ID], strconv.Itoa(t.row.Port))
		key := strconv.FormatInt(t.row.ID, 10)
		if t.down {
			from := firstNonEmpty(results[t.row.ID].From, "面板服务器")
			h.notifyIncident(t.row.NodeID, notifyEventInboundDown, []string{key}, "节点连不上",
				fmt.Sprintf("节点：%s\n地址：%s\n原因：%s（从%s检测）", t.row.Name, addr, t.err, from))
		} else if h.incidentAlerted(notifyEventInboundDown, key) {
			h.notifyForNode(t.row.NodeID, notifyEventInboundDown, "节点已恢复",
				fmt.Sprintf("节点：%s\n地址：%s", t.row.Name, addr))
		}
	}
	return results, nil
}

func (h *Handler) runReachProbeLoop(ctx context.Context) {
	defer h.jobsWG.Done()
	ticker := time.NewTicker(reachProbeInterval)
	defer ticker.Stop()
	for {
		if _, err := h.probeInboundReach(ctx, nil); err != nil && ctx.Err() == nil {
			log.Printf("inbound reachability probe failed: %v", err)
		}
		if _, err := h.probeNodeLatency(ctx, nil); err != nil && ctx.Err() == nil {
			log.Printf("node latency probe failed: %v", err)
		}
		if _, err := h.probeRelayLatency(ctx, nil); err != nil && ctx.Err() == nil {
			log.Printf("relay latency probe failed: %v", err)
		}
		if _, err := h.traceNodeRoutes(ctx, nil, false); err != nil && ctx.Err() == nil {
			log.Printf("node route trace failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// inboundProbe checks inbounds now: the given ids, or all of them.
func (h *Handler) inboundProbe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	results, err := h.probeInboundReach(r.Context(), req.IDs)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	rows, err := h.repo.ListNodeInbounds()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	// Built inbounds show their node's latency, so measure those nodes too.
	var nodeIDs []int64
	seen := make(map[int64]bool)
	for _, row := range rows {
		if _, ok := results[row.ID]; ok && !isExternalInbound(&row) && !seen[row.NodeID] {
			seen[row.NodeID] = true
			nodeIDs = append(nodeIDs, row.NodeID)
		}
	}
	if len(nodeIDs) > 0 && len(h.latencyPool()) > 0 {
		if len(req.IDs) == 0 {
			nodeIDs = nil
		}
		if _, err := h.probeNodeLatency(r.Context(), nodeIDs); err != nil {
			log.Printf("node latency probe failed: %v", err)
		}
	}
	out := make(map[string]reachProbe, len(results))
	for _, row := range rows {
		if _, ok := results[row.ID]; !ok {
			continue
		}
		if v := h.inboundProbeView(&row); v != nil {
			out[strconv.FormatInt(row.ID, 10)] = *v
		}
	}
	response.WriteJSON(w, response.OK(out))
}
