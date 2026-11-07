# Best Exit Selection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `best` multi-exit tunnel strategy that routes new connections through the currently best end-to-end exit without interrupting existing connections.

**Architecture:** Store `best` in `chain_tunnel.strategy`, but render it to GOST as `fifo` plus panel-controlled node ordering. Extend the existing tunnel quality prober to score each chain owner node's candidate exits, debounce switch decisions, and send `UpdateChains` only when the best exit changes after confirmation.

**Tech Stack:** Go `net/http`, GORM, SQLite/PostgreSQL, GOST v3 fork, WebSocket node commands, Vite/React/TypeScript, existing shadcn bridge components.

---

## File Structure

- Create: `go-backend/internal/http/handler/tunnel_best_exit.go` for constants, scoring helpers, in-memory decision state, and target ordering.
- Create: `go-backend/internal/http/handler/tunnel_best_exit_test.go` for scoring, debounce, cooldown, and ordering tests.
- Modify: `go-backend/internal/http/handler/handler.go` to initialize the best-exit manager on `Handler`.
- Modify: `go-backend/internal/http/handler/mutations.go` to render `best` as GOST `fifo` and apply best-exit ordering during initial tunnel runtime creation and normal tunnel updates.
- Modify: `go-backend/internal/http/handler/tunnel_quality_prober.go` to evaluate all `best` exit candidates and apply confirmed chain updates.
- Create: `go-gost/x/socket/chain_test.go` for safe `UpdateChains` replacement tests.
- Modify: `go-gost/x/socket/chain.go` so parse failures do not unregister the currently active chain.
- Modify: `vite-frontend/src/pages/tunnel.tsx` to add the `最优` strategy option and keep form comments aligned.

Implementation must not create git commits unless the user explicitly requests them. If commits are requested later, commit at task boundaries.

---

### Task 1: Add Best-Exit Scoring And Decision State

**Files:**
- Create: `go-backend/internal/http/handler/tunnel_best_exit.go`
- Create: `go-backend/internal/http/handler/tunnel_best_exit_test.go`

- [ ] **Step 1: Write failing scoring tests**

Create `go-backend/internal/http/handler/tunnel_best_exit_test.go` with these tests:

```go
package handler

import (
	"testing"
	"time"
)

func TestBestExitScoreCombinesLatencyAndLoss(t *testing.T) {
	exit := chainNodeRecord{NodeID: 30, NodeName: "exit-a"}
	score := scoreBestExitCandidate(10, exit, 25, 2, 80, 3)

	if !score.Success {
		t.Fatalf("expected successful score")
	}
	if score.OwnerNodeID != 10 || score.ExitNodeID != 30 {
		t.Fatalf("unexpected owner/exit ids: %+v", score)
	}
	if score.TotalLatency != 105 {
		t.Fatalf("expected total latency 105, got %v", score.TotalLatency)
	}
	if score.TotalLoss < 4.9 || score.TotalLoss > 5.0 {
		t.Fatalf("expected combined loss about 4.94, got %v", score.TotalLoss)
	}
	if score.Score < 599 || score.Score > 600 {
		t.Fatalf("expected score about 599, got %v", score.Score)
	}
}

func TestBestExitScorePenalizesLoss(t *testing.T) {
	stable := scoreBestExitCandidate(10, chainNodeRecord{NodeID: 30}, 80, 0, 80, 0)
	lowLatencyLossy := scoreBestExitCandidate(10, chainNodeRecord{NodeID: 31}, 10, 5, 10, 5)

	if !bestExitScoreLess(stable, lowLatencyLossy) {
		t.Fatalf("expected stable exit to beat low-latency lossy exit: stable=%+v lossy=%+v", stable, lowLatencyLossy)
	}
}

func TestBestExitFailedCandidateSortsLast(t *testing.T) {
	failed := failedBestExitCandidate(10, chainNodeRecord{NodeID: 30}, "dial timeout")
	good := scoreBestExitCandidate(10, chainNodeRecord{NodeID: 31}, 100, 0, 100, 0)

	scores := []bestExitCandidateScore{failed, good}
	sortBestExitScores(scores)

	if scores[0].ExitNodeID != 31 || scores[1].ExitNodeID != 30 {
		t.Fatalf("expected good score first and failed score last, got %+v", scores)
	}
}

func TestBestExitDecisionRequiresConfirmationsAndCooldown(t *testing.T) {
	m := newBestExitManager()
	key := bestExitOwnerKey{TunnelID: 7, OwnerNodeID: 10}
	now := time.Unix(100, 0)
	current := scoreBestExitCandidate(10, chainNodeRecord{NodeID: 30}, 100, 0, 100, 0)
	candidate := scoreBestExitCandidate(10, chainNodeRecord{NodeID: 31}, 40, 0, 60, 0)

	m.setApplied(key, 30, now.Add(-time.Minute))

	if decision := m.observeScores(key, []bestExitCandidateScore{candidate, current}, now); decision.Switch {
		t.Fatalf("first observation should not switch: %+v", decision)
	}
	if decision := m.observeScores(key, []bestExitCandidateScore{candidate, current}, now.Add(time.Second)); decision.Switch {
		t.Fatalf("second observation should not switch: %+v", decision)
	}
	decision := m.observeScores(key, []bestExitCandidateScore{candidate, current}, now.Add(2*time.Second))
	if !decision.Switch || decision.ExitNodeID != 31 {
		t.Fatalf("third confirmed observation should switch to 31: %+v", decision)
	}

	betterAgain := scoreBestExitCandidate(10, chainNodeRecord{NodeID: 30}, 20, 0, 20, 0)
	if decision := m.observeScores(key, []bestExitCandidateScore{betterAgain, candidate}, now.Add(3*time.Second)); decision.Switch {
		t.Fatalf("cooldown should block immediate switch back: %+v", decision)
	}
}

func TestBestExitOrderingUsesAppliedDecision(t *testing.T) {
	m := newBestExitManager()
	key := bestExitOwnerKey{TunnelID: 7, OwnerNodeID: 10}
	m.setApplied(key, 31, time.Unix(100, 0))
	targets := []tunnelRuntimeNode{
		{NodeID: 30, Strategy: tunnelStrategyBest},
		{NodeID: 31, Strategy: tunnelStrategyBest},
		{NodeID: 32, Strategy: tunnelStrategyBest},
	}

	ordered := m.orderTargets(key, targets)
	if ordered[0].NodeID != 31 || ordered[1].NodeID != 30 || ordered[2].NodeID != 32 {
		t.Fatalf("unexpected order: %+v", ordered)
	}
	if targets[0].NodeID != 30 {
		t.Fatalf("orderTargets mutated input: %+v", targets)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run from `go-backend`:

```bash
go test ./internal/http/handler -run 'TestBestExit' -count=1
```

Expected: FAIL with undefined `scoreBestExitCandidate`, `failedBestExitCandidate`, `bestExitCandidateScore`, `sortBestExitScores`, `newBestExitManager`, `bestExitOwnerKey`, and `tunnelStrategyBest`.

- [ ] **Step 3: Implement scoring and manager**

Create `go-backend/internal/http/handler/tunnel_best_exit.go`:

```go
package handler

import (
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	tunnelStrategyBest               = "best"
	bestExitRuntimeStrategy          = "fifo"
	bestExitPublicTargetHost         = "www.bing.com"
	bestExitPublicTargetPort         = 443
	bestExitLossPenaltyMsPerPercent = 100.0
	bestExitConfirmationRounds       = 3
	bestExitSwitchCooldown           = 30 * time.Second
	bestExitMinLatencyAdvantageMs    = 20.0
	bestExitMinScoreAdvantageRatio   = 0.15
)

type bestExitOwnerKey struct {
	TunnelID    int64
	OwnerNodeID int64
}

type bestExitCandidateScore struct {
	OwnerNodeID int64
	ExitNodeID  int64
	ExitName    string

	OwnerToExitLatency float64
	ExitToBingLatency  float64
	OwnerToExitLoss    float64
	ExitToBingLoss     float64
	TotalLatency       float64
	TotalLoss          float64
	Score              float64
	Success            bool
	ErrorMessage       string
}

type bestExitSwitchDecision struct {
	Switch     bool
	ExitNodeID int64
	Reason     string
	Scores     []bestExitCandidateScore
}

type bestExitDecision struct {
	AppliedExitNodeID int64
	PendingExitNodeID int64
	PendingCount      int
	LastSwitchAt      time.Time
	LastReason        string
	Scores            []bestExitCandidateScore
}

type bestExitManager struct {
	mu        sync.Mutex
	decisions map[bestExitOwnerKey]*bestExitDecision
}

func newBestExitManager() *bestExitManager {
	return &bestExitManager{decisions: make(map[bestExitOwnerKey]*bestExitDecision)}
}

func isBestTunnelStrategy(strategy string) bool {
	return strings.EqualFold(strings.TrimSpace(strategy), tunnelStrategyBest)
}

func runtimeTunnelStrategy(strategy string) string {
	if isBestTunnelStrategy(strategy) {
		return bestExitRuntimeStrategy
	}
	return strategy
}

func scoreBestExitCandidate(ownerNodeID int64, exit chainNodeRecord, ownerLatency, ownerLoss, publicLatency, publicLoss float64) bestExitCandidateScore {
	totalLatency := ownerLatency + publicLatency
	totalLoss := combineLossPercent(ownerLoss, publicLoss)
	return bestExitCandidateScore{
		OwnerNodeID:        ownerNodeID,
		ExitNodeID:         exit.NodeID,
		ExitName:           exit.NodeName,
		OwnerToExitLatency: ownerLatency,
		ExitToBingLatency:  publicLatency,
		OwnerToExitLoss:    ownerLoss,
		ExitToBingLoss:     publicLoss,
		TotalLatency:       totalLatency,
		TotalLoss:          totalLoss,
		Score:              totalLatency + totalLoss*bestExitLossPenaltyMsPerPercent,
		Success:            true,
	}
}

func failedBestExitCandidate(ownerNodeID int64, exit chainNodeRecord, message string) bestExitCandidateScore {
	return bestExitCandidateScore{
		OwnerNodeID:  ownerNodeID,
		ExitNodeID:   exit.NodeID,
		ExitName:     exit.NodeName,
		Success:      false,
		ErrorMessage: message,
	}
}

func combineLossPercent(a, b float64) float64 {
	a = clampPercent(a)
	b = clampPercent(b)
	return (1 - (1-a/100.0)*(1-b/100.0)) * 100.0
}

func clampPercent(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func sortBestExitScores(scores []bestExitCandidateScore) {
	sort.SliceStable(scores, func(i, j int) bool {
		return bestExitScoreLess(scores[i], scores[j])
	})
}

func bestExitScoreLess(a, b bestExitCandidateScore) bool {
	if a.Success != b.Success {
		return a.Success
	}
	if !a.Success && !b.Success {
		return a.ExitNodeID < b.ExitNodeID
	}
	if a.Score != b.Score {
		return a.Score < b.Score
	}
	return a.ExitNodeID < b.ExitNodeID
}

func bestExitHasMinimumAdvantage(candidate, current bestExitCandidateScore) bool {
	if !candidate.Success {
		return false
	}
	if !current.Success {
		return true
	}
	improvement := current.Score - candidate.Score
	threshold := current.Score * bestExitMinScoreAdvantageRatio
	if threshold < bestExitMinLatencyAdvantageMs {
		threshold = bestExitMinLatencyAdvantageMs
	}
	return improvement >= threshold
}

func (m *bestExitManager) setApplied(key bestExitOwnerKey, exitNodeID int64, at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.decisionLocked(key)
	d.AppliedExitNodeID = exitNodeID
	d.PendingExitNodeID = 0
	d.PendingCount = 0
	d.LastSwitchAt = at
}

func (m *bestExitManager) observeScores(key bestExitOwnerKey, scores []bestExitCandidateScore, now time.Time) bestExitSwitchDecision {
	m.mu.Lock()
	defer m.mu.Unlock()

	ordered := append([]bestExitCandidateScore(nil), scores...)
	sortBestExitScores(ordered)
	d := m.decisionLocked(key)
	d.Scores = ordered

	if len(ordered) == 0 || !ordered[0].Success {
		d.LastReason = "all exits failed"
		return bestExitSwitchDecision{Reason: d.LastReason, Scores: ordered}
	}

	candidate := ordered[0]
	if d.AppliedExitNodeID == 0 {
		d.AppliedExitNodeID = candidate.ExitNodeID
		d.LastSwitchAt = now
		d.LastReason = "initial best exit"
		return bestExitSwitchDecision{Reason: d.LastReason, Scores: ordered}
	}
	if candidate.ExitNodeID == d.AppliedExitNodeID {
		d.PendingExitNodeID = 0
		d.PendingCount = 0
		d.LastReason = "current exit remains best"
		return bestExitSwitchDecision{Reason: d.LastReason, Scores: ordered}
	}
	if now.Sub(d.LastSwitchAt) < bestExitSwitchCooldown {
		d.LastReason = "cooldown"
		return bestExitSwitchDecision{Reason: d.LastReason, Scores: ordered}
	}

	current := findBestExitScore(ordered, d.AppliedExitNodeID)
	if !bestExitHasMinimumAdvantage(candidate, current) {
		d.PendingExitNodeID = 0
		d.PendingCount = 0
		d.LastReason = "insufficient advantage"
		return bestExitSwitchDecision{Reason: d.LastReason, Scores: ordered}
	}

	if d.PendingExitNodeID != candidate.ExitNodeID {
		d.PendingExitNodeID = candidate.ExitNodeID
		d.PendingCount = 1
		d.LastReason = "candidate pending confirmation"
		return bestExitSwitchDecision{Reason: d.LastReason, Scores: ordered}
	}
	d.PendingCount++
	if d.PendingCount < bestExitConfirmationRounds {
		d.LastReason = "candidate pending confirmation"
		return bestExitSwitchDecision{Reason: d.LastReason, Scores: ordered}
	}

	d.AppliedExitNodeID = candidate.ExitNodeID
	d.PendingExitNodeID = 0
	d.PendingCount = 0
	d.LastSwitchAt = now
	d.LastReason = "switch confirmed"
	return bestExitSwitchDecision{Switch: true, ExitNodeID: candidate.ExitNodeID, Reason: d.LastReason, Scores: ordered}
}

func findBestExitScore(scores []bestExitCandidateScore, exitNodeID int64) bestExitCandidateScore {
	for _, score := range scores {
		if score.ExitNodeID == exitNodeID {
			return score
		}
	}
	return failedBestExitCandidate(0, chainNodeRecord{NodeID: exitNodeID}, "current exit has no successful score")
}

func (m *bestExitManager) decisionLocked(key bestExitOwnerKey) *bestExitDecision {
	if d := m.decisions[key]; d != nil {
		return d
	}
	d := &bestExitDecision{}
	m.decisions[key] = d
	return d
}

func (m *bestExitManager) orderTargets(key bestExitOwnerKey, targets []tunnelRuntimeNode) []tunnelRuntimeNode {
	out := append([]tunnelRuntimeNode(nil), targets...)
	if m == nil || len(out) <= 1 {
		return out
	}
	m.mu.Lock()
	applied := int64(0)
	if d := m.decisions[key]; d != nil {
		applied = d.AppliedExitNodeID
	}
	m.mu.Unlock()
	if applied <= 0 {
		return out
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].NodeID == applied {
			return true
		}
		if out[j].NodeID == applied {
			return false
		}
		return false
	})
	return out
}
```

- [ ] **Step 4: Run tests to verify pass**

Run from `go-backend`:

```bash
go test ./internal/http/handler -run 'TestBestExit' -count=1
```

Expected: PASS.

---

### Task 2: Render `best` As FIFO With Applied Ordering

**Files:**
- Modify: `go-backend/internal/http/handler/handler.go`
- Modify: `go-backend/internal/http/handler/mutations.go`
- Modify: `go-backend/internal/http/handler/tunnel_best_exit_test.go`

- [ ] **Step 1: Write failing runtime rendering tests**

Append to `go-backend/internal/http/handler/tunnel_best_exit_test.go`:

```go
func TestBuildTunnelChainConfigMapsBestStrategyToFIFO(t *testing.T) {
	nodes := map[int64]*nodeRecord{
		10: {ID: 10, ServerIP: "10.0.0.10", ServerIPv4: "10.0.0.10", TCPListenAddr: "[::]"},
		30: {ID: 30, ServerIP: "10.0.0.30", ServerIPv4: "10.0.0.30", TCPListenAddr: "[::]"},
		31: {ID: 31, ServerIP: "10.0.0.31", ServerIPv4: "10.0.0.31", TCPListenAddr: "[::]"},
	}
	targets := []tunnelRuntimeNode{
		{NodeID: 30, Port: 30030, Protocol: "tls", Strategy: tunnelStrategyBest, ChainType: 3},
		{NodeID: 31, Port: 30031, Protocol: "tls", Strategy: tunnelStrategyBest, ChainType: 3},
	}

	chainData, err := buildTunnelChainConfig(77, 10, targets, nodes, "")
	if err != nil {
		t.Fatalf("build chain: %v", err)
	}
	hops := chainData["hops"].([]map[string]interface{})
	selector := hops[0]["selector"].(map[string]interface{})
	if selector["strategy"] != bestExitRuntimeStrategy {
		t.Fatalf("expected best to render as fifo, got %v", selector["strategy"])
	}
}

func TestHandlerOrdersBestExitTargetsForOwner(t *testing.T) {
	h := &Handler{bestExit: newBestExitManager()}
	key := bestExitOwnerKey{TunnelID: 77, OwnerNodeID: 10}
	h.bestExit.setApplied(key, 31, time.Unix(100, 0))
	targets := []tunnelRuntimeNode{
		{NodeID: 30, Port: 30030, Strategy: tunnelStrategyBest},
		{NodeID: 31, Port: 30031, Strategy: tunnelStrategyBest},
	}

	ordered := h.orderBestExitTargets(77, 10, targets)
	if ordered[0].NodeID != 31 || ordered[1].NodeID != 30 {
		t.Fatalf("unexpected ordered targets: %+v", ordered)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run from `go-backend`:

```bash
go test ./internal/http/handler -run 'TestBuildTunnelChainConfigMapsBestStrategyToFIFO|TestHandlerOrdersBestExitTargetsForOwner' -count=1
```

Expected: FAIL because `best` still renders as `best`, `Handler.bestExit` does not exist, and `orderBestExitTargets` does not exist.

- [ ] **Step 3: Initialize best-exit manager**

In `go-backend/internal/http/handler/handler.go`, add the field to `Handler`:

```go
	bestExit *bestExitManager
```

In `New`, initialize it in the struct literal:

```go
		bestExit:                 newBestExitManager(),
```

- [ ] **Step 4: Add ordering helper and render strategy mapping**

In `go-backend/internal/http/handler/mutations.go`, add this method near `buildTunnelChainConfig`:

```go
func (h *Handler) orderBestExitTargets(tunnelID, ownerNodeID int64, targets []tunnelRuntimeNode) []tunnelRuntimeNode {
	if len(targets) <= 1 || !isBestTunnelStrategy(targets[0].Strategy) {
		return append([]tunnelRuntimeNode(nil), targets...)
	}
	if h == nil || h.bestExit == nil {
		return append([]tunnelRuntimeNode(nil), targets...)
	}
	return h.bestExit.orderTargets(bestExitOwnerKey{TunnelID: tunnelID, OwnerNodeID: ownerNodeID}, targets)
}
```

In `buildTunnelChainConfig`, replace:

```go
	strategy := defaultString(strings.TrimSpace(targets[0].Strategy), "round")
```

with:

```go
	strategy := runtimeTunnelStrategy(defaultString(strings.TrimSpace(targets[0].Strategy), "round"))
```

In `applyTunnelRuntimeWithMode`, order targets before each `buildTunnelChainConfig` call. For entry nodes, replace the first chain build block with:

```go
	for _, inNode := range state.InNodes {
		targets := state.OutNodes
		if len(state.ChainHops) > 0 {
			targets = state.ChainHops[0]
		} else {
			targets = h.orderBestExitTargets(state.TunnelID, inNode.NodeID, targets)
		}
		chainData, err := buildTunnelChainConfig(state.TunnelID, inNode.NodeID, targets, state.Nodes, state.IPPreference)
```

For middle hop nodes, replace the `nextTargets` selection block with:

```go
		nextTargets := state.OutNodes
		if i+1 < len(state.ChainHops) {
			nextTargets = state.ChainHops[i+1]
		} else {
			nextTargets = h.orderBestExitTargets(state.TunnelID, chainNode.NodeID, nextTargets)
		}
```

- [ ] **Step 5: Run tests**

Run from `go-backend`:

```bash
go test ./internal/http/handler -run 'TestBestExit|TestBuildTunnelChainConfigMapsBestStrategyToFIFO|TestHandlerOrdersBestExitTargetsForOwner' -count=1
```

Expected: PASS.

---

### Task 3: Evaluate Best Exit Candidates In The Quality Prober

**Files:**
- Modify: `go-backend/internal/http/handler/tunnel_best_exit.go`
- Modify: `go-backend/internal/http/handler/tunnel_best_exit_test.go`
- Modify: `go-backend/internal/http/handler/tunnel_quality_prober.go`

- [ ] **Step 1: Write failing candidate evaluation tests**

Append to `go-backend/internal/http/handler/tunnel_best_exit_test.go`:

```go
func TestEvaluateBestExitOwnerScoresAllCandidates(t *testing.T) {
	owner := chainNodeRecord{NodeID: 10, NodeName: "entry"}
	exits := []chainNodeRecord{
		{NodeID: 30, NodeName: "exit-a", Port: 30030},
		{NodeID: 31, NodeName: "exit-b", Port: 30031},
	}
	nodes := map[int64]*nodeRecord{
		10: {ID: 10, ServerIP: "10.0.0.10", ServerIPv4: "10.0.0.10", TCPListenAddr: "[::]"},
		30: {ID: 30, ServerIP: "10.0.0.30", ServerIPv4: "10.0.0.30", TCPListenAddr: "[::]"},
		31: {ID: 31, ServerIP: "10.0.0.31", ServerIPv4: "10.0.0.31", TCPListenAddr: "[::]"},
	}
	pinger := func(nodeID int64, ip string, port int, _ diagnosisExecOptions) (float64, float64, error) {
		switch {
		case nodeID == 10 && port == 30030:
			return 60, 0, nil
		case nodeID == 10 && port == 30031:
			return 20, 0, nil
		case nodeID == 30 && ip == bestExitPublicTargetHost:
			return 60, 0, nil
		case nodeID == 31 && ip == bestExitPublicTargetHost:
			return 20, 0, nil
		default:
			t.Fatalf("unexpected ping node=%d ip=%s port=%d", nodeID, ip, port)
			return 0, 100, nil
		}
	}

	scores := evaluateBestExitOwner(owner, exits, nodes, "", diagnosisExecOptions{}, pinger)
	if len(scores) != 2 {
		t.Fatalf("expected two scores, got %+v", scores)
	}
	if scores[0].ExitNodeID != 31 {
		t.Fatalf("expected exit-b first, got %+v", scores)
	}
}

func TestEvaluateBestExitOwnerMarksCandidateFailedWhenOwnerToExitFails(t *testing.T) {
	owner := chainNodeRecord{NodeID: 10, NodeName: "entry"}
	exits := []chainNodeRecord{{NodeID: 30, NodeName: "exit-a", Port: 30030}}
	nodes := map[int64]*nodeRecord{
		10: {ID: 10, ServerIP: "10.0.0.10", ServerIPv4: "10.0.0.10", TCPListenAddr: "[::]"},
		30: {ID: 30, ServerIP: "10.0.0.30", ServerIPv4: "10.0.0.30", TCPListenAddr: "[::]"},
	}
	pinger := func(nodeID int64, ip string, port int, _ diagnosisExecOptions) (float64, float64, error) {
		return 0, 100, errBestExitProbeForTest
	}

	scores := evaluateBestExitOwner(owner, exits, nodes, "", diagnosisExecOptions{}, pinger)
	if len(scores) != 1 || scores[0].Success {
		t.Fatalf("expected failed candidate, got %+v", scores)
	}
}
```

Add imports to the test file:

```go
import (
	"errors"
	"testing"
	"time"
)

var errBestExitProbeForTest = errors.New("probe failed")
```

If the file already has an import block from Task 1, merge `errors` into that import block and place `var errBestExitProbeForTest` after imports.

- [ ] **Step 2: Run tests to verify failure**

Run from `go-backend`:

```bash
go test ./internal/http/handler -run 'TestEvaluateBestExitOwner' -count=1
```

Expected: FAIL with undefined `evaluateBestExitOwner`.

- [ ] **Step 3: Implement candidate evaluation helpers**

Append to `go-backend/internal/http/handler/tunnel_best_exit.go`:

```go
type bestExitProbeFunc func(nodeID int64, ip string, port int, options diagnosisExecOptions) (latency float64, loss float64, err error)

func evaluateBestExitOwner(owner chainNodeRecord, exits []chainNodeRecord, nodes map[int64]*nodeRecord, ipPreference string, options diagnosisExecOptions, ping bestExitProbeFunc) []bestExitCandidateScore {
	scores := make([]bestExitCandidateScore, 0, len(exits))
	if owner.NodeID <= 0 || len(exits) == 0 || ping == nil {
		return scores
	}
	ownerNode := nodes[owner.NodeID]
	for _, exit := range exits {
		exitNode := nodes[exit.NodeID]
		if exitNode == nil {
			scores = append(scores, failedBestExitCandidate(owner.NodeID, exit, "exit node unavailable"))
			continue
		}
		targetIP, targetPort, resolveErr := resolveChainProbeTarget(ownerNode, exitNode, exit.Port, ipPreference, exit.ConnectIP)
		if resolveErr != nil {
			scores = append(scores, failedBestExitCandidate(owner.NodeID, exit, resolveErr.Error()))
			continue
		}
		ownerLatency, ownerLoss, ownerErr := ping(owner.NodeID, targetIP, targetPort, options)
		if ownerErr != nil {
			scores = append(scores, failedBestExitCandidate(owner.NodeID, exit, ownerErr.Error()))
			continue
		}
		publicLatency, publicLoss, publicErr := ping(exit.NodeID, bestExitPublicTargetHost, bestExitPublicTargetPort, options)
		if publicErr != nil {
			scores = append(scores, failedBestExitCandidate(owner.NodeID, exit, publicErr.Error()))
			continue
		}
		scores = append(scores, scoreBestExitCandidate(owner.NodeID, exit, ownerLatency, ownerLoss, publicLatency, publicLoss))
	}
	sortBestExitScores(scores)
	return scores
}

func bestExitChainOwners(inNodes []chainNodeRecord, chainHops [][]chainNodeRecord) []chainNodeRecord {
	if len(chainHops) == 0 {
		return inNodes
	}
	return chainHops[len(chainHops)-1]
}
```

- [ ] **Step 4: Wire evaluation into the quality prober**

In `go-backend/internal/http/handler/tunnel_quality_prober.go`, after `inNodes, midNodesGrouped, outNodes := splitChainNodeGroups(chainRows)`, call a new method for `best` tunnels:

```go
	p.probeBestExitOwners(tunnelID, inNodes, midNodesGrouped, outNodes, ipPreference, options)
```

Append these methods to `tunnel_quality_prober.go`:

```go
func (p *tunnelQualityProber) probeBestExitOwners(tunnelID int64, inNodes []chainNodeRecord, chainHops [][]chainNodeRecord, outNodes []chainNodeRecord, ipPreference string, options diagnosisExecOptions) {
	if p == nil || p.handler == nil || p.handler.bestExit == nil || len(outNodes) <= 1 {
		return
	}
	if !isBestTunnelStrategy(outNodes[0].Strategy) {
		return
	}
	owners := bestExitChainOwners(inNodes, chainHops)
	if len(owners) == 0 {
		return
	}
	nodeMap := make(map[int64]*nodeRecord, len(owners)+len(outNodes))
	for _, owner := range owners {
		if node, err := p.handler.getNodeRecord(owner.NodeID); err == nil && node != nil {
			nodeMap[owner.NodeID] = node
		}
	}
	for _, exit := range outNodes {
		if node, err := p.handler.getNodeRecord(exit.NodeID); err == nil && node != nil {
			nodeMap[exit.NodeID] = node
		}
	}
	for _, owner := range owners {
		if nodeMap[owner.NodeID] == nil {
			continue
		}
		scores := evaluateBestExitOwner(owner, outNodes, nodeMap, ipPreference, options, p.tcpPingNode)
		decision := p.handler.bestExit.observeScores(bestExitOwnerKey{TunnelID: tunnelID, OwnerNodeID: owner.NodeID}, scores, time.Now())
		if decision.Switch {
			p.handler.applyBestExitChainOrder(tunnelID, owner.NodeID, outNodes, decision.Scores, ipPreference)
		}
	}
}
```

Append this method to `go-backend/internal/http/handler/mutations.go` near `applyTunnelChainOnNode`:

```go
func (h *Handler) applyBestExitChainOrder(tunnelID, ownerNodeID int64, outNodes []chainNodeRecord, scores []bestExitCandidateScore, ipPreference string) {
	if h == nil || tunnelID <= 0 || ownerNodeID <= 0 || len(outNodes) == 0 {
		return
	}
	targets := chainRecordsToRuntimeTargets(outNodes)
	orderedIDs := make([]int64, 0, len(scores))
	for _, score := range scores {
		if score.ExitNodeID > 0 {
			orderedIDs = append(orderedIDs, score.ExitNodeID)
		}
	}
	targets = orderRuntimeTargetsByNodeID(targets, orderedIDs)
	nodes := make(map[int64]*nodeRecord, len(targets)+1)
	if owner, err := h.getNodeRecord(ownerNodeID); err == nil && owner != nil {
		nodes[ownerNodeID] = owner
	}
	for _, target := range targets {
		if node, err := h.getNodeRecord(target.NodeID); err == nil && node != nil {
			nodes[target.NodeID] = node
		}
	}
	chainData, err := buildTunnelChainConfig(tunnelID, ownerNodeID, targets, nodes, ipPreference)
	if err != nil {
		log.Printf("best_exit: build chain failed tunnel=%d owner=%d err=%v", tunnelID, ownerNodeID, err)
		return
	}
	if err := h.applyTunnelChainOnNode(ownerNodeID, chainData, true); err != nil {
		log.Printf("best_exit: update chain failed tunnel=%d owner=%d err=%v", tunnelID, ownerNodeID, err)
		return
	}
	log.Printf("best_exit: updated chain tunnel=%d owner=%d best_exit=%d", tunnelID, ownerNodeID, targets[0].NodeID)
}
```

Add helper functions in `tunnel_best_exit.go`:

```go
func chainRecordsToRuntimeTargets(rows []chainNodeRecord) []tunnelRuntimeNode {
	out := make([]tunnelRuntimeNode, 0, len(rows))
	for _, row := range rows {
		out = append(out, tunnelRuntimeNode{
			NodeID:    row.NodeID,
			Protocol:  row.Protocol,
			Strategy:  row.Strategy,
			Inx:       int(row.Inx),
			ChainType: row.ChainType,
			Port:      row.Port,
			ConnectIP: row.ConnectIP,
		})
	}
	return out
}

func orderRuntimeTargetsByNodeID(targets []tunnelRuntimeNode, orderedIDs []int64) []tunnelRuntimeNode {
	out := append([]tunnelRuntimeNode(nil), targets...)
	if len(out) <= 1 || len(orderedIDs) == 0 {
		return out
	}
	positions := make(map[int64]int, len(orderedIDs))
	for i, id := range orderedIDs {
		if _, ok := positions[id]; !ok {
			positions[id] = i
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		pi, iok := positions[out[i].NodeID]
		pj, jok := positions[out[j].NodeID]
		if iok != jok {
			return iok
		}
		if iok && jok && pi != pj {
			return pi < pj
		}
		return false
	})
	return out
}
```

Add `log` to `mutations.go` imports if it is not already present.

- [ ] **Step 5: Run focused tests**

Run from `go-backend`:

```bash
go test ./internal/http/handler -run 'TestBestExit|TestEvaluateBestExitOwner|TestBuildTunnelChainConfigMapsBestStrategyToFIFO|TestHandlerOrdersBestExitTargetsForOwner' -count=1
```

Expected: PASS.

---

### Task 4: Preserve Existing Chain On Agent Update Parse Failure

**Files:**
- Create: `go-gost/x/socket/chain_test.go`
- Modify: `go-gost/x/socket/chain.go`

- [ ] **Step 1: Write failing agent tests**

Create `go-gost/x/socket/chain_test.go`:

```go
package socket

import (
	"testing"

	corelogger "github.com/go-gost/core/logger"
	"github.com/go-gost/x/config"
	xlogger "github.com/go-gost/x/logger"
	"github.com/go-gost/x/registry"
)

func TestUpdateChainParseFailureKeepsExistingChainRegistered(t *testing.T) {
	corelogger.SetDefault(xlogger.Nop())

	name := "chain_update_parse_failure_tdd"
	originalConfig := config.Global()
	defer config.Set(originalConfig)
	registry.ChainRegistry().Unregister(name)
	defer registry.ChainRegistry().Unregister(name)
	config.Set(&config.Config{})

	valid := config.ChainConfig{
		Name: name,
		Hops: []*config.HopConfig{{
			Name: "hop-valid",
			Nodes: []*config.NodeConfig{{
				Name:      "node-valid",
				Addr:      "127.0.0.1:443",
				Connector: &config.ConnectorConfig{Type: "relay"},
				Dialer:    &config.DialerConfig{Type: "tcp"},
			}},
		}},
	}
	if err := createChain(createChainRequest{Data: valid}); err != nil {
		t.Fatalf("create valid chain: %v", err)
	}
	before := registry.ChainRegistry().Get(name)
	if before == nil {
		t.Fatalf("expected chain registered before update")
	}

	invalid := config.ChainConfig{
		Hops: []*config.HopConfig{{
			Name: "hop-invalid",
			Nodes: []*config.NodeConfig{{
				Name:      "node-invalid",
				Addr:      "127.0.0.1:443",
				Connector: &config.ConnectorConfig{Type: "connector-does-not-exist"},
				Dialer:    &config.DialerConfig{Type: "tcp"},
			}},
		}},
	}
	err := updateChain(updateChainRequest{Chain: name, Data: invalid})
	if err == nil {
		t.Fatalf("expected invalid chain update to fail")
	}
	after := registry.ChainRegistry().Get(name)
	if after == nil {
		t.Fatalf("expected old chain to remain registered after failed update")
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run from `go-gost`:

```bash
go test ./x/socket -run TestUpdateChainParseFailureKeepsExistingChainRegistered -count=1
```

Expected: FAIL because the old chain is unregistered before parsing the invalid replacement.

- [ ] **Step 3: Parse before unregistering**

In `go-gost/x/socket/chain.go`, replace `updateChain` with:

```go
func updateChain(req updateChainRequest) error {
	name := strings.TrimSpace(req.Chain)
	if name == "" {
		name = strings.TrimSpace(req.Data.Name)
	}
	if name == "" {
		return errors.New("chain name is required")
	}

	req.Data.Name = name
	v, err := parser.ParseChain(&req.Data, logger.Default())
	if err != nil {
		return errors.New("create chain " + name + " failed: " + err.Error())
	}

	if registry.ChainRegistry().IsRegistered(name) {
		registry.ChainRegistry().Unregister(name)
	}
	if err := registry.ChainRegistry().Register(name, v); err != nil {
		return errors.New("chain " + name + " already exists")
	}

	return config.OnUpdate(func(c *config.Config) error {
		found := false
		for i := range c.Chains {
			if c.Chains[i].Name == name {
				c.Chains[i] = &req.Data
				found = true
				break
			}
		}
		if !found {
			c.Chains = append(c.Chains, &req.Data)
		}
		return nil
	})
}
```

- [ ] **Step 4: Run socket tests**

Run from `go-gost`:

```bash
go test ./x/socket -run 'TestUpdateChainParseFailureKeepsExistingChainRegistered|TestUpdateServicesSkipsUnchangedServiceWithoutRestart|TestCreateConnLimiterUpdatesGlobalConfig' -count=1
```

Expected: PASS.

---

### Task 5: Add Frontend `最优` Strategy Option

**Files:**
- Modify: `vite-frontend/src/pages/tunnel.tsx`

- [ ] **Step 1: Update tunnel strategy comment**

In `vite-frontend/src/pages/tunnel.tsx`, change the `ChainTunnel.strategy` comment to include `best`:

```ts
  strategy?: string; // 'fifo' | 'round' | 'rand' | 'best' - 仅转发链/多出口需要
```

- [ ] **Step 2: Add the select item**

In the exit “负载策略” selector, change the options block to:

```tsx
                                <SelectItem key="fifo">主备</SelectItem>
                                <SelectItem key="round">轮询</SelectItem>
                                <SelectItem key="rand">随机</SelectItem>
                                <SelectItem key="best">最优</SelectItem>
```

- [ ] **Step 3: Build frontend**

Run from `vite-frontend`:

```bash
pnpm run build
```

Expected: PASS with `tsc && vite build` completing successfully.

---

### Task 6: Full Verification

**Files:**
- Verify only.

- [ ] **Step 1: Run backend tests**

Run from `go-backend`:

```bash
go test ./...
```

Expected: PASS. Investigate any failure before continuing.

- [ ] **Step 2: Run agent tests**

Run from `go-gost`:

```bash
go test ./...
```

Expected: PASS. Investigate any failure before continuing.

- [ ] **Step 3: Run frontend build**

Run from `vite-frontend`:

```bash
pnpm run build
```

Expected: PASS.

- [ ] **Step 4: Inspect final diff**

Run from repository root:

```bash
git diff -- go-backend/internal/http/handler/tunnel_best_exit.go go-backend/internal/http/handler/tunnel_best_exit_test.go go-backend/internal/http/handler/handler.go go-backend/internal/http/handler/mutations.go go-backend/internal/http/handler/tunnel_quality_prober.go go-gost/x/socket/chain.go go-gost/x/socket/chain_test.go vite-frontend/src/pages/tunnel.tsx
```

Expected: Diff shows only the `best` strategy, quality decision, safe chain update, and UI option changes described in this plan.

---

## Self-Review

- Spec coverage: `best` UI option is Task 5; DB persistence is covered by reusing existing `strategy` handling and verified through runtime tests; runtime `fifo` mapping and ordering are Task 2; scoring, confirmation, cooldown, and all-failed behavior are Task 1; prober candidate evaluation and `UpdateChains` are Task 3; agent parse-failure safety is Task 4; verification is Task 6.
- Placeholder scan: The plan contains concrete file paths, constants, function names, code blocks, commands, and expected outcomes.
- Type consistency: `bestExitOwnerKey`, `bestExitCandidateScore`, `bestExitManager`, `runtimeTunnelStrategy`, and `evaluateBestExitOwner` are defined before they are used by later tasks.
