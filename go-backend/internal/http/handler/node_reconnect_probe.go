package handler

import (
	"context"
	"log"
	"sort"
	"sync"
	"time"
)

// One worker merges reconnect bursts and keeps events arriving during a
// measurement for the next round. Its context belongs to the background jobs.
type nodeReconnectProbeQueue struct {
	mu    sync.Mutex
	nodes map[int64]struct{}
	wake  chan struct{}
}

func newNodeReconnectProbeQueue() *nodeReconnectProbeQueue {
	return &nodeReconnectProbeQueue{nodes: make(map[int64]struct{}), wake: make(chan struct{}, 1)}
}

func (q *nodeReconnectProbeQueue) add(nodeID int64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.nodes[nodeID] = struct{}{}
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *nodeReconnectProbeQueue) take() []int64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	ids := sortedProbeIDs(q.nodes)
	clear(q.nodes)
	select {
	case <-q.wake:
	default:
	}
	return ids
}

func sortedProbeIDs(ids map[int64]struct{}) []int64 {
	out := make([]int64, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (h *Handler) scheduleNodeReconnectProbe(nodeID int64) {
	if nodeID <= 0 {
		return
	}
	h.jobsMu.Lock()
	defer h.jobsMu.Unlock()
	if h.jobsStarted && h.reconnectProbes != nil {
		h.reconnectProbes.add(nodeID)
	}
}

// runNodeReconnectProbes probes reconnected nodes once reconnects have been
// quiet for quiet (at most 6×quiet after the first): when the panel restarts
// and every node reconnects, that is one batch rather than one per node.
func (h *Handler) runNodeReconnectProbes(ctx context.Context, queue *nodeReconnectProbeQueue, quiet time.Duration) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-queue.wake:
		}
		deadline := time.Now().Add(6 * quiet)
		for waiting := true; waiting; {
			timer := time.NewTimer(min(quiet, time.Until(deadline)))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				waiting = false
			case <-queue.wake:
				// Another reconnect: its node is already queued; wait for
				// quiet again.
				timer.Stop()
				waiting = time.Until(deadline) > 0
			}
		}
		nodes := queue.take()
		if len(nodes) == 0 {
			continue
		}
		if err := h.probeReconnectedNodes(ctx, nodes); err != nil && ctx.Err() == nil {
			log.Printf("node reconnect probe failed nodes=%v err=%v", nodes, err)
		}
	}
}

// Include all tunnel roles and the built landing, not just the entry. Refresh
// the entries' reference points first so relays cannot reuse an offline sample.
func (h *Handler) reconnectProbeTargets(nodes []int64) (relayIDs, nodeIDs, inboundIDs []int64, err error) {
	want := make(map[int64]struct{}, len(nodes))
	for _, id := range nodes {
		want[id] = struct{}{}
	}
	measure := make(map[int64]struct{}, len(want))
	for id := range want {
		measure[id] = struct{}{}
	}
	rows, err := h.repo.ListInboundRelays(0)
	if err != nil {
		return nil, nil, nil, err
	}
	inbounds, err := h.repo.ListNodeInbounds()
	if err != nil {
		return nil, nil, nil, err
	}
	landings := make(map[int64]int64, len(inbounds))
	for _, in := range inbounds {
		landings[in.ID] = in.NodeID
		if _, reconnected := want[in.NodeID]; reconnected && in.Enabled == 1 && !isExternalInbound(&in) {
			inboundIDs = append(inboundIDs, in.ID)
		}
	}
	chains := make(map[int64][]chainNodeRecord)
	for _, row := range rows {
		if !row.ForwardExists || row.TunnelID <= 0 {
			continue
		}
		chain, loaded := chains[row.TunnelID]
		if !loaded {
			chain, err = h.listChainNodesForTunnel(row.TunnelID)
			if err != nil {
				return nil, nil, nil, err
			}
			chains[row.TunnelID] = chain
		}
		_, affected := want[landings[row.InboundID]]
		for _, node := range chain {
			if _, ok := want[node.NodeID]; ok {
				affected = true
			}
		}
		if !affected {
			continue
		}
		relayIDs = append(relayIDs, row.ID)
		if entries, _, _ := splitChainNodeGroups(chain); len(entries) > 0 {
			measure[entries[0].NodeID] = struct{}{}
		}
	}
	return relayIDs, sortedProbeIDs(measure), inboundIDs, nil
}

func (h *Handler) probeReconnectedNodes(ctx context.Context, nodes []int64) error {
	if len(nodes) == 0 {
		return nil // Empty IDs mean "all" to the probe functions.
	}
	relayIDs, nodeIDs, inboundIDs, err := h.reconnectProbeTargets(nodes)
	if err != nil {
		return err
	}
	if _, err := h.probeNodeLatency(ctx, nodeIDs); err != nil {
		return err
	}
	if len(inboundIDs) > 0 {
		if _, err := h.probeInboundReach(ctx, inboundIDs); err != nil {
			return err
		}
	}
	if len(relayIDs) > 0 {
		if _, err := h.probeRelayLatency(ctx, relayIDs); err != nil {
			return err
		}
	}
	if ctx.Err() == nil {
		log.Printf("node reconnect probe complete nodes=%v measured_nodes=%v relays=%v inbounds=%v", nodes, nodeIDs, relayIDs, inboundIDs)
	}
	return ctx.Err()
}
