package handler

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"go-backend/internal/ws"
)

// An agent runs at most agentTCPPingLimit TcpPing commands at once. The
// panel queues for a slot before sending; newer agents also queue
// excess commands, while older agents refuse them with agentBusyMessage.
const (
	agentTCPPingLimit   = 8
	agentBusyMessage    = "TCP探测任务过多"
	agentBusyRetryDelay = 800 * time.Millisecond
)

var errTCPPingQueueTimeout = errors.New("服务器上的 TCP 探测排队超时")

// nodePingSlots hands out each node's TcpPing slots; nil means no limit.
type nodePingSlots struct {
	mu    sync.Mutex
	slots map[int64]chan struct{}
}

func newNodePingSlots() *nodePingSlots {
	return &nodePingSlots{slots: make(map[int64]chan struct{})}
}

func (s *nodePingSlots) node(nodeID int64) chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.slots[nodeID]
	if !ok {
		c = make(chan struct{}, agentTCPPingLimit)
		s.slots[nodeID] = c
	}
	return c
}

// acquire waits up to wait for one of the node's slots.
func (s *nodePingSlots) acquire(nodeID int64, wait time.Duration) bool {
	if wait <= 0 {
		return false
	}
	if s == nil {
		return true
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case s.node(nodeID) <- struct{}{}:
		return true
	case <-timer.C:
		return false
	}
}

func (s *nodePingSlots) release(nodeID int64) {
	if s == nil {
		return
	}
	select {
	case <-s.node(nodeID):
	default:
	}
}

// tcpPingSend sends one TcpPing command; tests replace it.
var tcpPingSend = func(h *Handler, nodeID int64, data interface{}, timeout time.Duration) (ws.CommandResult, error) {
	return h.sendNodeCommandWithTimeout(nodeID, "TcpPing", data, timeout, false, false)
}

// sendTCPPing runs a TcpPing on a node within its slot limit. The agent can
// still be busy with pings the panel gave up waiting for, or with another
// panel's (federation): one such refusal is retried after a short pause.
func (h *Handler) sendTCPPing(nodeID int64, data interface{}, timeout time.Duration) (ws.CommandResult, error) {
	if timeout <= 0 {
		timeout = defaultNodeCommandTimeout
	}
	deadline := time.Now().Add(timeout)
	if !h.pingSlots.acquire(nodeID, time.Until(deadline)) {
		return ws.CommandResult{}, errTCPPingQueueTimeout
	}
	defer h.pingSlots.release(nodeID)
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return ws.CommandResult{}, errTCPPingQueueTimeout
	}
	// Panel queueing, an optional retry and agent execution share one budget.
	res, err := tcpPingSend(h, nodeID, data, remaining)
	if err != nil && strings.Contains(err.Error(), agentBusyMessage) {
		if time.Until(deadline) <= agentBusyRetryDelay {
			return res, err
		}
		time.Sleep(agentBusyRetryDelay)
		if remaining = time.Until(deadline); remaining > 0 {
			res, err = tcpPingSend(h, nodeID, data, remaining)
		}
	}
	return res, err
}

// interleaveByNode orders jobs so each node's first job comes before any
// node's second, and so on: a round's parallel slots spread over the
// nodes instead of queueing on one node's limit. Order within a node stays.
func interleaveByNode[T any](jobs []T, nodeOf func(T) int64) []T {
	type ranked struct {
		job        T
		rank, node int64
	}
	seen := make(map[int64]int64, len(jobs))
	list := make([]ranked, len(jobs))
	for i, j := range jobs {
		n := nodeOf(j)
		list[i] = ranked{job: j, rank: seen[n], node: n}
		seen[n]++
	}
	sort.SliceStable(list, func(a, b int) bool {
		if list[a].rank != list[b].rank {
			return list[a].rank < list[b].rank
		}
		return list[a].node < list[b].node
	})
	out := make([]T, len(list))
	for i, r := range list {
		out[i] = r.job
	}
	return out
}
