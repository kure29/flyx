package handler

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/store/repo"
)

// Relay line latency: what a user behind a relay line sees, estimated as the
// entry node's latency to each reference point plus every hop the line
// takes to the landing (entry → tunnel hops → exit → landing). TCP connect
// times are one round trip each, so they add up to the line's round trip.

const relayPathPointName = "中转路径"

// relayHop is one leg of a relay line, measured from its first node.
type relayHop struct {
	From      string  `json:"from"`
	To        string  `json:"to"`
	OK        bool    `json:"ok"`
	LatencyMs int64   `json:"latencyMs,omitempty"`
	LossPct   float64 `json:"lossPct,omitempty"`
	Error     string  `json:"error,omitempty"`
}

// relayLatency is a relay line's estimated latency to each reference point
// (Points), with the legs it is made of.
type relayLatency struct {
	reachProbe
	Entry       string     `json:"entry,omitempty"`
	EntryNodeID int64      `json:"entryNodeId,omitempty"`
	Hops        []relayHop `json:"hops"`
	PathMs      int64      `json:"pathMs,omitempty"`
}

type relayLatencyStore struct {
	mu      sync.RWMutex
	results map[int64]relayLatency
	rounds  probeRounds
}

func newRelayLatencyStore() *relayLatencyStore {
	return &relayLatencyStore{results: make(map[int64]relayLatency), rounds: make(probeRounds)}
}

func (s *relayLatencyStore) get(relayID int64) (relayLatency, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.results[relayID]
	return r, ok
}

// replace stores fresh results; a full round also drops deleted relays.
func (s *relayLatencyStore) replace(results map[int64]relayLatency, all bool) {
	s.replaceRound(results, all, time.Now().UnixNano())
}

// replaceRound is replace for a round that started at started (unix ns):
// results of a newer round that already finished are kept.
func (s *relayLatencyStore) replaceRound(results map[int64]relayLatency, all bool, started int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if all {
		for id := range s.results {
			if _, ok := results[id]; !ok && s.rounds.take(id, started) {
				delete(s.results, id)
				delete(s.rounds, id)
			}
		}
	}
	for id, r := range results {
		if s.rounds.take(id, started) {
			s.results[id] = r
		}
	}
}

// relayLeg is one hop to measure: from a node to an address.
type relayLeg struct {
	fromID   int64
	fromName string
	toName   string
	host     string
	port     int
	// err is set when the leg cannot be measured (shared node, no address).
	err string
}

type relayPlan struct {
	entry chainNodeRecord
	legs  []relayLeg
	err   string
}

// planRelayPath lists the legs of a relay line: the tunnel's entry node to
// each hop (first node of each group) and its exit, then on to the
// forward's target unless the tunnel exits on the landing itself.
func (h *Handler) planRelayPath(rw repo.InboundRelayView, landing string, cache map[int64]*nodeRecord) relayPlan {
	if !rw.ForwardExists || rw.TunnelID <= 0 {
		return relayPlan{err: "中转转发不存在"}
	}
	tunnel, err := h.getTunnelRecord(rw.TunnelID)
	if err != nil {
		return relayPlan{err: err.Error()}
	}
	rows, err := h.listChainNodesForTunnel(rw.TunnelID)
	if err != nil {
		return relayPlan{err: err.Error()}
	}
	inNodes, hopGroups, outNodes := splitChainNodeGroups(rows)
	if len(inNodes) == 0 {
		return relayPlan{err: "隧道没有入口服务器"}
	}
	plan := relayPlan{entry: inNodes[0]}
	prev := inNodes[0]
	leg := func(to chainNodeRecord) relayLeg {
		l := relayLeg{fromID: prev.NodeID, fromName: prev.NodeName, toName: to.NodeName}
		fromNode, _ := h.cachedNode(cache, prev.NodeID)
		toNode, err := h.cachedNode(cache, to.NodeID)
		if err != nil {
			l.err = err.Error()
			return l
		}
		l.host, l.port, err = resolveChainProbeTarget(fromNode, toNode, to.Port, h.repo.GetTunnelIPPreference(rw.TunnelID), to.ConnectIP)
		if err != nil {
			l.err = err.Error()
		}
		return l
	}
	if tunnel.Type == 2 {
		var seq []chainNodeRecord
		for _, g := range hopGroups {
			if len(g) > 0 {
				seq = append(seq, g[0])
			}
		}
		if len(outNodes) > 0 {
			seq = append(seq, outNodes[0])
		}
		for _, n := range seq {
			plan.legs = append(plan.legs, leg(n))
			prev = n
		}
	}
	targets := splitRemoteTargets(rw.RemoteAddr)
	if len(targets) == 0 {
		plan.err = "中转转发没有目标地址"
		return plan
	}
	host, port, err := parseTargetAddress(targets[0])
	if err != nil {
		plan.err = "中转转发目标地址无效"
		return plan
	}
	// A tunnel exiting on the landing dials the inbound locally: no leg.
	if ip := net.ParseIP(host); tunnel.Type == 2 && ip != nil && ip.IsLoopback() {
		return plan
	}
	plan.legs = append(plan.legs, relayLeg{fromID: prev.NodeID, fromName: prev.NodeName, toName: landing, host: host, port: port})
	return plan
}

// combineLoss is the loss of two legs in a row, in percent.
func combineLoss(a, b float64) float64 {
	return float64(int((1-(1-a/100)*(1-b/100))*100 + 0.5))
}

// relayLatencyFrom adds the measured legs to the entry node's latency to
// each reference point. Without reference points the path alone is shown.
func relayLatencyFrom(plan relayPlan, hops []relayHop, entry *reachProbe, now int64) relayLatency {
	out := relayLatency{Hops: hops, Entry: plan.entry.NodeName, EntryNodeID: plan.entry.NodeID}
	out.CheckedAt = now
	if plan.err != "" {
		out.Error = plan.err
		return out
	}
	pathOK, pathLoss, pathErr := true, 0.0, ""
	for _, hop := range hops {
		if !hop.OK {
			pathOK = false
			if pathErr == "" {
				pathErr = fmt.Sprintf("%s → %s %s", hop.From, hop.To, firstNonEmpty(hop.Error, "不通"))
			}
			continue
		}
		out.PathMs += hop.LatencyMs
		pathLoss = combineLoss(pathLoss, hop.LossPct)
	}
	var points []latencyPoint
	if entry != nil && len(entry.Points) > 0 {
		for _, pt := range entry.Points {
			p := latencyPoint{Name: pt.Name, Pinned: pt.Pinned, Route: pt.Route}
			switch {
			case !pt.OK:
				p.Error = pt.Error
			case !pathOK:
				p.Error = "线路不通"
			default:
				p.OK = true
				p.LatencyMs = pt.LatencyMs + out.PathMs
				p.LossPct = combineLoss(pt.LossPct, pathLoss)
			}
			points = append(points, p)
		}
	} else {
		p := latencyPoint{Name: relayPathPointName, OK: pathOK, Error: pathErr}
		if pathOK {
			p.LatencyMs, p.LossPct = out.PathMs, pathLoss
			if p.LatencyMs < 1 {
				p.LatencyMs = 1
			}
		}
		points = []latencyPoint{p}
	}
	probe := latencyProbe(points, now)
	if !pathOK {
		probe.Error = pathErr
	}
	out.reachProbe = probe
	return out
}

// probeRelayLatency measures the given relay lines (all when ids is empty).
// Entry nodes' reference point latency comes from the last node round.
func (h *Handler) probeRelayLatency(ctx context.Context, ids []int64) (map[int64]relayLatency, error) {
	started := time.Now().UnixNano()
	rows, err := h.repo.ListInboundRelays(0)
	if err != nil {
		return nil, err
	}
	inbounds, err := h.repo.ListNodeInbounds()
	if err != nil {
		return nil, err
	}
	landing := make(map[int64]string, len(inbounds))
	for _, in := range inbounds {
		landing[in.ID] = in.Name
	}
	want := make(map[int64]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	cache := map[int64]*nodeRecord{}
	plans := make(map[int64]relayPlan)
	hops := make(map[int64][]relayHop)
	for _, rw := range rows {
		if len(ids) > 0 && !want[rw.ID] {
			continue
		}
		plan := h.planRelayPath(rw, firstNonEmpty(landing[rw.InboundID], "节点"), cache)
		for i := range plan.legs {
			l := &plan.legs[i]
			if l.err != "" {
				continue
			}
			if n, err := h.cachedNode(cache, l.fromID); err != nil {
				l.err = err.Error()
			} else if n.IsRemote == 1 {
				l.err = "共享服务器不检测"
			} else if n.Status != 1 {
				l.err = "服务器离线"
			}
		}
		plans[rw.ID] = plan
		hops[rw.ID] = make([]relayHop, len(plan.legs))
	}

	type job struct {
		id  int64
		i   int
		l   relayLeg
		hop relayHop
	}
	var jobs []job
	for id, plan := range plans {
		for i, l := range plan.legs {
			hop := relayHop{From: l.fromName, To: l.toName, Error: l.err}
			if l.err != "" {
				hops[id][i] = hop
				continue
			}
			jobs = append(jobs, job{id, i, l, hop})
		}
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, latencyParallel)
	for _, j := range interleaveByNode(jobs, func(j job) int64 { return j.l.fromID }) {
		if ctx.Err() != nil {
			hops[j.id][j.i] = j.hop
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(j job) {
			defer wg.Done()
			defer func() { <-sem }()
			data, err := nodeLatencyPing(h, j.l.fromID, j.l.host, j.l.port)
			p := latencyPointFrom(latencyTarget{}, data, err)
			hop := j.hop
			hop.OK, hop.LatencyMs, hop.LossPct, hop.Error = p.OK, p.LatencyMs, p.LossPct, p.Error
			// Each goroutine writes its own slot.
			hops[j.id][j.i] = hop
		}(j)
	}
	wg.Wait()

	now := time.Now().UnixMilli()
	results := make(map[int64]relayLatency, len(plans))
	for id, plan := range plans {
		var entry *reachProbe
		if plan.entry.NodeID > 0 {
			if p, ok := h.nodeLatency.get(plan.entry.NodeID); ok {
				p = h.withRoutes(plan.entry.NodeID, p)
				entry = &p
			}
		}
		results[id] = relayLatencyFrom(plan, hops[id], entry, now)
	}
	if ctx.Err() == nil {
		h.relayLatency.replaceRound(results, len(ids) == 0, started)
	}
	return results, nil
}

// inboundRelayProbe measures relay lines now: the given ids, or all of them.
// Their entry nodes are measured first so the totals are current.
func (h *Handler) inboundRelayProbe(w http.ResponseWriter, r *http.Request) {
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
	if len(h.latencyPool()) > 0 {
		var entries []int64
		if len(req.IDs) > 0 {
			rows, err := h.repo.ListInboundRelays(0)
			if err != nil {
				response.WriteJSON(w, response.Err(-2, err.Error()))
				return
			}
			want := make(map[int64]bool, len(req.IDs))
			for _, id := range req.IDs {
				want[id] = true
			}
			seen := make(map[int64]bool)
			for _, rw := range rows {
				if !want[rw.ID] || rw.TunnelID <= 0 {
					continue
				}
				if nodes, err := h.listChainNodesForTunnel(rw.TunnelID); err == nil {
					if in, _, _ := splitChainNodeGroups(nodes); len(in) > 0 && !seen[in[0].NodeID] {
						seen[in[0].NodeID] = true
						entries = append(entries, in[0].NodeID)
					}
				}
			}
		}
		if len(req.IDs) == 0 || len(entries) > 0 {
			if _, err := h.probeNodeLatency(r.Context(), entries); err != nil {
				response.WriteJSON(w, response.Err(-2, err.Error()))
				return
			}
		}
	}
	results, err := h.probeRelayLatency(r.Context(), req.IDs)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	out := make(map[string]relayLatency, len(results))
	for id, res := range results {
		out[strconv.FormatInt(id, 10)] = res
	}
	response.WriteJSON(w, response.OK(out))
}

// relayLatencyView is a relay's last measurement for the relay list.
func (h *Handler) relayLatencyView(relayID int64) *relayLatency {
	if r, ok := h.relayLatency.get(relayID); ok {
		return &r
	}
	return nil
}
