# Best Exit Current Display Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show the currently applied `best` exit selection in the tunnel list information, including per-entry/per-final-hop details for multi-owner tunnels.

**Architecture:** Add a backend-only display layer that snapshots `bestExitManager` state and attaches `bestExitState` to existing `tunnelList`/`tunnelGet` responses. Render that state in the existing tunnel table/grid topology area using compact text and a native `title` detail tooltip. No routing, scoring, persistence, polling, or runtime update behavior changes.

**Tech Stack:** Go `net/http` handlers + existing repository methods, React/TypeScript in `vite-frontend/src/pages/tunnel.tsx`, Tailwind/shadcn bridge components already in the file.

---

## File Structure

- Create `go-backend/internal/http/handler/tunnel_best_exit_display.go`: response DTOs, manager snapshot method, tunnel-response parsing helpers, and `Handler.attachBestExitStates`.
- Create `go-backend/internal/http/handler/tunnel_best_exit_display_test.go`: backend display-state unit tests.
- Modify `go-backend/internal/http/handler/handler.go`: call `h.attachBestExitStatesOrLog(items)` in `tunnelList`.
- Modify `go-backend/internal/http/handler/mutations.go`: call `h.attachBestExitStatesOrLog(items)` before returning a single tunnel in `tunnelGet`.
- Modify `vite-frontend/src/pages/tunnel.tsx`: add `bestExitState` types, map API state, helper render functions, and table/grid display.

---

### Task 1: Backend Snapshot And Display-State Tests

**Files:**
- Create: `go-backend/internal/http/handler/tunnel_best_exit_display_test.go`

- [ ] **Step 1: Write failing backend display tests**

Create `go-backend/internal/http/handler/tunnel_best_exit_display_test.go`:

```go
package handler

import (
	"testing"
	"time"
)

func TestBestExitDecisionSnapshotIsDefensiveCopy(t *testing.T) {
	m := newBestExitManager()
	key := bestExitOwnerKey{TunnelID: 77, OwnerNodeID: 10}
	now := time.Unix(100, 0)
	score := scoreBestExitCandidate(10, chainNodeRecord{NodeID: 30, NodeName: "exit-a"}, 10, 0, 20, 0)

	m.observeScores(key, []bestExitCandidateScore{score}, now)
	snapshot, ok := m.snapshot(key)
	if !ok {
		t.Fatalf("expected snapshot")
	}
	if snapshot.AppliedExitNodeID != 30 || snapshot.UpdatedAt != now.UnixMilli() {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	if len(snapshot.Scores) != 1 {
		t.Fatalf("expected one score in snapshot, got %+v", snapshot.Scores)
	}
	snapshot.Scores[0].ExitNodeID = 99

	again, ok := m.snapshot(key)
	if !ok {
		t.Fatalf("expected second snapshot")
	}
	if again.Scores[0].ExitNodeID != 30 {
		t.Fatalf("snapshot score mutation leaked into manager state: %+v", again.Scores)
	}
}

func TestBuildBestExitDisplayStateForDirectMultiEntryOwners(t *testing.T) {
	m := newBestExitManager()
	now := time.Unix(100, 0)
	m.setApplied(bestExitOwnerKey{TunnelID: 77, OwnerNodeID: 10}, 30, now)
	m.setApplied(bestExitOwnerKey{TunnelID: 77, OwnerNodeID: 11}, 31, now.Add(time.Second))

	tunnel := map[string]interface{}{
		"id": int64(77),
		"inNodeId": []map[string]interface{}{
			{"nodeId": int64(10)},
			{"nodeId": int64(11)},
		},
		"outNodeId": []map[string]interface{}{
			{"nodeId": int64(30), "strategy": tunnelStrategyBest},
			{"nodeId": int64(31), "strategy": tunnelStrategyBest},
		},
		"chainNodes": [][]map[string]interface{}{},
	}
	names := map[int64]string{10: "入口 A", 11: "入口 B", 30: "香港节点", 31: "日本节点"}

	state, ok := buildBestExitDisplayState(tunnel, m, testBestExitNameLookup(names))
	if !ok {
		t.Fatalf("expected best exit state")
	}
	if !state.Enabled || state.Summary != "多个出口" || state.Status != "applied" {
		t.Fatalf("unexpected state summary: %+v", state)
	}
	if state.UpdatedAt != now.Add(time.Second).UnixMilli() {
		t.Fatalf("expected latest updatedAt, got %d", state.UpdatedAt)
	}
	if len(state.Items) != 2 {
		t.Fatalf("expected two owner items, got %+v", state.Items)
	}
	if state.Items[0].OwnerRole != "entry" || state.Items[0].OwnerNodeName != "入口 A" || state.Items[0].ExitNodeName != "香港节点" {
		t.Fatalf("unexpected first item: %+v", state.Items[0])
	}
	if state.Items[1].OwnerRole != "entry" || state.Items[1].OwnerNodeName != "入口 B" || state.Items[1].ExitNodeName != "日本节点" {
		t.Fatalf("unexpected second item: %+v", state.Items[1])
	}
}

func TestBuildBestExitDisplayStateForFinalChainHopOwners(t *testing.T) {
	m := newBestExitManager()
	now := time.Unix(200, 0)
	m.setApplied(bestExitOwnerKey{TunnelID: 88, OwnerNodeID: 20}, 30, now)
	m.setApplied(bestExitOwnerKey{TunnelID: 88, OwnerNodeID: 21}, 30, now.Add(time.Second))

	tunnel := map[string]interface{}{
		"id": int64(88),
		"inNodeId": []map[string]interface{}{
			{"nodeId": int64(10)},
		},
		"outNodeId": []map[string]interface{}{
			{"nodeId": int64(30), "strategy": tunnelStrategyBest},
			{"nodeId": int64(31), "strategy": tunnelStrategyBest},
		},
		"chainNodes": [][]map[string]interface{}{
			{{"nodeId": int64(15), "inx": int64(0)}},
			{{"nodeId": int64(20), "inx": int64(1)}, {"nodeId": int64(21), "inx": int64(1)}},
		},
	}
	names := map[int64]string{20: "中转 M1", 21: "中转 M2", 30: "香港节点", 31: "日本节点"}

	state, ok := buildBestExitDisplayState(tunnel, m, testBestExitNameLookup(names))
	if !ok {
		t.Fatalf("expected best exit state")
	}
	if state.Summary != "香港节点" || state.Status != "applied" {
		t.Fatalf("expected single-exit summary, got %+v", state)
	}
	if len(state.Items) != 2 {
		t.Fatalf("expected two final-hop owner items, got %+v", state.Items)
	}
	if state.Items[0].OwnerRole != "chain" || state.Items[0].OwnerNodeName != "中转 M1" || state.Items[0].ExitNodeName != "香港节点" {
		t.Fatalf("unexpected first chain owner item: %+v", state.Items[0])
	}
	if state.Items[1].OwnerRole != "chain" || state.Items[1].OwnerNodeName != "中转 M2" || state.Items[1].ExitNodeName != "香港节点" {
		t.Fatalf("unexpected second chain owner item: %+v", state.Items[1])
	}
}

func TestBuildBestExitDisplayStateWaitingWhenNoAppliedDecisionExists(t *testing.T) {
	tunnel := map[string]interface{}{
		"id": int64(77),
		"inNodeId": []map[string]interface{}{
			{"nodeId": int64(10)},
		},
		"outNodeId": []map[string]interface{}{
			{"nodeId": int64(30), "strategy": tunnelStrategyBest},
			{"nodeId": int64(31), "strategy": tunnelStrategyBest},
		},
		"chainNodes": [][]map[string]interface{}{},
	}
	names := map[int64]string{10: "入口 A", 30: "香港节点", 31: "日本节点"}

	state, ok := buildBestExitDisplayState(tunnel, newBestExitManager(), testBestExitNameLookup(names))
	if !ok {
		t.Fatalf("expected waiting best exit state")
	}
	if state.Summary != "等待探测" || state.Status != "waiting" {
		t.Fatalf("expected waiting state, got %+v", state)
	}
	if len(state.Items) != 1 || state.Items[0].ExitNodeID != 0 || state.Items[0].ExitNodeName != "等待探测" {
		t.Fatalf("unexpected waiting item: %+v", state.Items)
	}
}

func TestBuildBestExitDisplayStateSkipsNonBestAndSingleExitTunnels(t *testing.T) {
	nonBest := map[string]interface{}{
		"id":       int64(77),
		"inNodeId": []map[string]interface{}{{"nodeId": int64(10)}},
		"outNodeId": []map[string]interface{}{
			{"nodeId": int64(30), "strategy": "round"},
			{"nodeId": int64(31), "strategy": "round"},
		},
	}
	if state, ok := buildBestExitDisplayState(nonBest, newBestExitManager(), testBestExitNameLookup(nil)); ok || state != nil {
		t.Fatalf("expected non-best tunnel to skip state, got %+v", state)
	}

	singleExit := map[string]interface{}{
		"id":       int64(78),
		"inNodeId": []map[string]interface{}{{"nodeId": int64(10)}},
		"outNodeId": []map[string]interface{}{
			{"nodeId": int64(30), "strategy": tunnelStrategyBest},
		},
	}
	if state, ok := buildBestExitDisplayState(singleExit, newBestExitManager(), testBestExitNameLookup(nil)); ok || state != nil {
		t.Fatalf("expected single-exit tunnel to skip state, got %+v", state)
	}
}

func testBestExitNameLookup(names map[int64]string) bestExitNodeNameLookup {
	return func(nodeID int64) (string, bool) {
		name := names[nodeID]
		return name, name != ""
	}
}
```

- [ ] **Step 2: Run backend display tests to verify failure**

Run from `go-backend`:

```bash
go test ./internal/http/handler -run 'TestBestExitDecisionSnapshot|TestBuildBestExitDisplayState' -count=1
```

Expected: FAIL with undefined `snapshot`, `buildBestExitDisplayState`, and `bestExitNodeNameLookup`.

---

### Task 2: Backend Display State Implementation

**Files:**
- Create: `go-backend/internal/http/handler/tunnel_best_exit_display.go`
- Modify: `go-backend/internal/http/handler/tunnel_best_exit.go`
- Test: `go-backend/internal/http/handler/tunnel_best_exit_display_test.go`

- [ ] **Step 1: Implement display state and snapshot helpers**

Create `go-backend/internal/http/handler/tunnel_best_exit_display.go`:

```go
package handler

import (
	"log"
	"strings"
)

const (
	bestExitDisplayStatusApplied = "applied"
	bestExitDisplayStatusWaiting = "waiting"
	bestExitDisplaySummaryMulti  = "多个出口"
	bestExitDisplaySummaryWait   = "等待探测"
	bestExitUnknownExitName      = "未知出口"
	bestExitUnknownEntryName     = "未知入口"
	bestExitUnknownChainName     = "未知中转"
)

type bestExitDecisionSnapshot struct {
	AppliedExitNodeID int64
	UpdatedAt         int64
	Reason            string
	Scores            []bestExitCandidateScore
}

type bestExitDisplayState struct {
	Enabled   bool                  `json:"enabled"`
	Summary   string                `json:"summary"`
	Status    string                `json:"status"`
	UpdatedAt int64                 `json:"updatedAt,omitempty"`
	Reason    string                `json:"reason,omitempty"`
	Items     []bestExitDisplayItem `json:"items"`
}

type bestExitDisplayItem struct {
	OwnerNodeID   int64  `json:"ownerNodeId"`
	OwnerNodeName string `json:"ownerNodeName"`
	OwnerRole     string `json:"ownerRole"`
	ExitNodeID    int64  `json:"exitNodeId,omitempty"`
	ExitNodeName  string `json:"exitNodeName"`
	UpdatedAt     int64  `json:"updatedAt,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

type bestExitNodeNameLookup func(nodeID int64) (string, bool)

func (m *bestExitManager) snapshot(key bestExitOwnerKey) (bestExitDecisionSnapshot, bool) {
	if m == nil {
		return bestExitDecisionSnapshot{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.decisions[key]
	if d == nil {
		return bestExitDecisionSnapshot{}, false
	}
	updatedAt := int64(0)
	if !d.LastSwitchAt.IsZero() {
		updatedAt = d.LastSwitchAt.UnixMilli()
	}
	return bestExitDecisionSnapshot{
		AppliedExitNodeID: d.AppliedExitNodeID,
		UpdatedAt:         updatedAt,
		Reason:            d.LastReason,
		Scores:            cloneBestExitScores(d.Scores),
	}, true
}

func (h *Handler) attachBestExitStates(items []map[string]interface{}) {
	if h == nil || len(items) == 0 {
		return
	}
	lookup := h.bestExitNodeNameLookup()
	for _, item := range items {
		state, ok := buildBestExitDisplayState(item, h.bestExit, lookup)
		if !ok {
			delete(item, "bestExitState")
			continue
		}
		item["bestExitState"] = state
	}
}

func (h *Handler) bestExitNodeNameLookup() bestExitNodeNameLookup {
	cache := map[int64]string{}
	return func(nodeID int64) (string, bool) {
		if nodeID <= 0 || h == nil {
			return "", false
		}
		if name, ok := cache[nodeID]; ok {
			return name, name != ""
		}
		node, err := h.getNodeRecord(nodeID)
		if err != nil || node == nil {
			cache[nodeID] = ""
			return "", false
		}
		name := strings.TrimSpace(node.Name)
		cache[nodeID] = name
		return name, name != ""
	}
}

func buildBestExitDisplayState(tunnel map[string]interface{}, manager *bestExitManager, lookup bestExitNodeNameLookup) (*bestExitDisplayState, bool) {
	if tunnel == nil {
		return nil, false
	}
	tunnelID := asInt64(tunnel["id"], 0)
	outNodes := bestExitDisplayMapSlice(tunnel["outNodeId"])
	if tunnelID <= 0 || len(outNodes) <= 1 {
		return nil, false
	}
	if !isBestTunnelStrategy(asString(outNodes[0]["strategy"])) {
		return nil, false
	}

	owners, ownerRole := bestExitDisplayOwners(tunnel)
	state := &bestExitDisplayState{
		Enabled: true,
		Summary: bestExitDisplaySummaryWait,
		Status:  bestExitDisplayStatusWaiting,
		Items:   make([]bestExitDisplayItem, 0, len(owners)),
	}

	exitsByID := map[int64]map[string]interface{}{}
	for _, exit := range outNodes {
		if id := asInt64(exit["nodeId"], 0); id > 0 {
			exitsByID[id] = exit
		}
	}
	appliedExitIDs := map[int64]string{}
	appliedCount := 0
	latestUpdatedAt := int64(0)
	latestReason := ""
	for _, owner := range owners {
		ownerNodeID := asInt64(owner["nodeId"], 0)
		if ownerNodeID <= 0 {
			continue
		}
		item := bestExitDisplayItem{
			OwnerNodeID:   ownerNodeID,
			OwnerNodeName: bestExitDisplayNodeName(owner, ownerNodeID, lookup, bestExitUnknownOwnerName(ownerRole)),
			OwnerRole:     ownerRole,
			ExitNodeName:  bestExitDisplaySummaryWait,
			Reason:        bestExitDisplayStatusWaiting,
		}
		if snapshot, ok := manager.snapshot(bestExitOwnerKey{TunnelID: tunnelID, OwnerNodeID: ownerNodeID}); ok && snapshot.AppliedExitNodeID > 0 {
			item.ExitNodeID = snapshot.AppliedExitNodeID
			item.ExitNodeName = bestExitDisplayNodeName(exitsByID[snapshot.AppliedExitNodeID], snapshot.AppliedExitNodeID, lookup, bestExitUnknownExitName)
			item.UpdatedAt = snapshot.UpdatedAt
			item.Reason = snapshot.Reason
			appliedExitIDs[item.ExitNodeID] = item.ExitNodeName
			appliedCount++
			if snapshot.UpdatedAt > latestUpdatedAt {
				latestUpdatedAt = snapshot.UpdatedAt
				latestReason = snapshot.Reason
			}
		}
		state.Items = append(state.Items, item)
	}

	if appliedCount == 0 {
		return state, true
	}
	state.Status = bestExitDisplayStatusApplied
	state.UpdatedAt = latestUpdatedAt
	state.Reason = latestReason
	if len(appliedExitIDs) == 1 {
		for _, name := range appliedExitIDs {
			state.Summary = name
		}
	} else {
		state.Summary = bestExitDisplaySummaryMulti
	}
	return state, true
}

func bestExitDisplayOwners(tunnel map[string]interface{}) ([]map[string]interface{}, string) {
	chainGroups := bestExitDisplayChainGroups(tunnel["chainNodes"])
	if len(chainGroups) > 0 {
		return chainGroups[len(chainGroups)-1], "chain"
	}
	return bestExitDisplayMapSlice(tunnel["inNodeId"]), "entry"
}

func bestExitDisplayMapSlice(v interface{}) []map[string]interface{} {
	switch arr := v.(type) {
	case []map[string]interface{}:
		return arr
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(arr))
		for _, item := range arr {
			if m, ok := item.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func bestExitDisplayChainGroups(v interface{}) [][]map[string]interface{} {
	switch groups := v.(type) {
	case [][]map[string]interface{}:
		return groups
	case []interface{}:
		out := make([][]map[string]interface{}, 0, len(groups))
		for _, group := range groups {
			items := bestExitDisplayMapSlice(group)
			if len(items) > 0 {
				out = append(out, items)
			}
		}
		return out
	default:
		return nil
	}
}

func bestExitDisplayNodeName(source map[string]interface{}, nodeID int64, lookup bestExitNodeNameLookup, fallback string) string {
	if source != nil {
		for _, key := range []string{"nodeName", "name"} {
			if name := strings.TrimSpace(asString(source[key])); name != "" {
				return name
			}
		}
	}
	if lookup != nil {
		if name, ok := lookup(nodeID); ok && strings.TrimSpace(name) != "" {
			return strings.TrimSpace(name)
		}
	}
	return fallback
}

func bestExitUnknownOwnerName(role string) string {
	if role == "chain" {
		return bestExitUnknownChainName
	}
	return bestExitUnknownEntryName
}

func (h *Handler) attachBestExitStatesOrLog(items []map[string]interface{}) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("best_exit: attach display state failed: %v", recovered)
		}
	}()
	h.attachBestExitStates(items)
}
```

- [ ] **Step 2: Replace direct attach calls with panic-safe wrapper**

Keep `attachBestExitStates` for tests, and use `attachBestExitStatesOrLog` from handlers in Task 3. This step only creates the function above; no handler wiring yet.

- [ ] **Step 3: Run backend display tests**

Run from `go-backend`:

```bash
go test ./internal/http/handler -run 'TestBestExitDecisionSnapshot|TestBuildBestExitDisplayState' -count=1
```

Expected: PASS.

- [ ] **Step 4: Run gofmt**

```bash
gofmt -w internal/http/handler/tunnel_best_exit_display.go internal/http/handler/tunnel_best_exit_display_test.go
```

- [ ] **Step 5: Commit backend display implementation**

```bash
git add go-backend/internal/http/handler/tunnel_best_exit_display.go go-backend/internal/http/handler/tunnel_best_exit_display_test.go
git commit -m "feat: build best exit display state"
```

---

### Task 3: Attach Best-Exit State To Tunnel List And Get Responses

**Files:**
- Modify: `go-backend/internal/http/handler/handler.go`
- Modify: `go-backend/internal/http/handler/mutations.go`
- Test: `go-backend/internal/http/handler/tunnel_best_exit_display_test.go`

- [ ] **Step 1: Write failing handler attach tests**

Append to `go-backend/internal/http/handler/tunnel_best_exit_display_test.go`:

```go
func TestAttachBestExitStatesAddsStateToBestTunnelOnly(t *testing.T) {
	h := &Handler{bestExit: newBestExitManager()}
	now := time.Unix(300, 0)
	h.bestExit.setApplied(bestExitOwnerKey{TunnelID: 77, OwnerNodeID: 10}, 30, now)

	items := []map[string]interface{}{
		{
			"id":       int64(77),
			"inNodeId": []map[string]interface{}{{"nodeId": int64(10)}},
			"outNodeId": []map[string]interface{}{
				{"nodeId": int64(30), "strategy": tunnelStrategyBest},
				{"nodeId": int64(31), "strategy": tunnelStrategyBest},
			},
		},
		{
			"id":       int64(78),
			"inNodeId": []map[string]interface{}{{"nodeId": int64(12)}},
			"outNodeId": []map[string]interface{}{
				{"nodeId": int64(40), "strategy": "round"},
				{"nodeId": int64(41), "strategy": "round"},
			},
		},
	}

	h.attachBestExitStates(items)
	state, ok := items[0]["bestExitState"].(*bestExitDisplayState)
	if !ok {
		t.Fatalf("expected bestExitState on best tunnel, got %#v", items[0]["bestExitState"])
	}
	if state.Summary != bestExitUnknownExitName || state.Items[0].ExitNodeID != 30 {
		t.Fatalf("unexpected state with fallback names: %+v", state)
	}
	if _, exists := items[1]["bestExitState"]; exists {
		t.Fatalf("non-best tunnel should not have bestExitState: %+v", items[1])
	}
}
```

- [ ] **Step 2: Run attach test to verify failure**

Run from `go-backend`:

```bash
go test ./internal/http/handler -run TestAttachBestExitStatesAddsStateToBestTunnelOnly -count=1
```

Expected: PASS.

- [ ] **Step 3: Wire tunnel list response**

In `go-backend/internal/http/handler/handler.go`, change `tunnelList` from:

```go
	items, err := h.repo.ListTunnels()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(items))
```

to:

```go
	items, err := h.repo.ListTunnels()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	h.attachBestExitStatesOrLog(items)
	response.WriteJSON(w, response.OK(items))
```

- [ ] **Step 4: Wire single tunnel response**

In `go-backend/internal/http/handler/mutations.go`, change `tunnelGet` from:

```go
	items, err := h.repo.ListTunnels()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	for _, it := range items {
		if asInt64(it["id"], 0) == id {
			response.WriteJSON(w, response.OK(it))
			return
		}
	}
```

to:

```go
	items, err := h.repo.ListTunnels()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	h.attachBestExitStatesOrLog(items)
	for _, it := range items {
		if asInt64(it["id"], 0) == id {
			response.WriteJSON(w, response.OK(it))
			return
		}
	}
```

- [ ] **Step 5: Run focused backend tests**

Run from `go-backend`:

```bash
go test ./internal/http/handler -run 'TestBestExitDecisionSnapshot|TestBuildBestExitDisplayState|TestAttachBestExitStatesAddsStateToBestTunnelOnly' -count=1
```

Expected: PASS.

- [ ] **Step 6: Run gofmt**

```bash
gofmt -w internal/http/handler/handler.go internal/http/handler/mutations.go internal/http/handler/tunnel_best_exit_display.go internal/http/handler/tunnel_best_exit_display_test.go
```

- [ ] **Step 7: Commit response wiring**

```bash
git add go-backend/internal/http/handler/handler.go go-backend/internal/http/handler/mutations.go go-backend/internal/http/handler/tunnel_best_exit_display.go go-backend/internal/http/handler/tunnel_best_exit_display_test.go
git commit -m "feat: expose best exit display state"
```

---

### Task 4: Frontend Tunnel List Display

**Files:**
- Modify: `vite-frontend/src/pages/tunnel.tsx`

- [ ] **Step 1: Add TypeScript types**

In `vite-frontend/src/pages/tunnel.tsx`, add these interfaces after `interface ChainTunnel`:

```ts
interface BestExitStateItem {
  ownerNodeId: number;
  ownerNodeName: string;
  ownerRole: "entry" | "chain";
  exitNodeId?: number;
  exitNodeName: string;
  updatedAt?: number;
  reason?: string;
}

interface BestExitState {
  enabled: boolean;
  summary: string;
  status: "applied" | "waiting";
  updatedAt?: number;
  reason?: string;
  items: BestExitStateItem[];
}
```

Then add the optional field to `interface Tunnel`:

```ts
  bestExitState?: BestExitState | null;
```

- [ ] **Step 2: Preserve API state during mapping**

In `mapTunnelApiItems`, add `bestExitState` to the returned object:

```ts
    bestExitState:
      tunnel.bestExitState && typeof tunnel.bestExitState === "object"
        ? {
            ...tunnel.bestExitState,
            items: Array.isArray(tunnel.bestExitState.items)
              ? tunnel.bestExitState.items
              : [],
          }
        : null,
```

The mapped object should include this field before `createdTime` or immediately after it.

- [ ] **Step 3: Add render helpers**

Add these helper functions after `mapTunnelApiItems` and before `export default function TunnelPage()`:

```tsx
const bestExitOwnerRoleText = (role: BestExitStateItem["ownerRole"]) => {
  return role === "chain" ? "中转" : "入口";
};

const bestExitDetailTitle = (state?: BestExitState | null) => {
  if (!state?.enabled || !state.items?.length) {
    return "";
  }
  return state.items
    .map((item) => {
      const ownerName = item.ownerNodeName || `${bestExitOwnerRoleText(item.ownerRole)} ${item.ownerNodeId}`;
      const exitName = item.exitNodeName || "等待探测";
      return `${ownerName} -> ${exitName}`;
    })
    .join("\n");
};

const renderBestExitState = (state?: BestExitState | null) => {
  if (!state?.enabled) {
    return null;
  }
  const title = bestExitDetailTitle(state);
  const isWaiting = state.status === "waiting";

  return (
    <div
      className={`mt-1 text-[11px] leading-4 ${
        isWaiting
          ? "text-default-500"
          : "text-emerald-700 dark:text-emerald-300"
      }`}
      title={title || undefined}
    >
      最优出口：{state.summary || "等待探测"}
    </div>
  );
};
```

- [ ] **Step 4: Render in table topology cell**

In the table topology `<TableCell>` around line 1674, change the cell content from:

```tsx
                        <div className="flex items-center gap-1.5 text-xs">
                          <span className="font-semibold text-primary-700 dark:text-primary-400">
                            {tunnel.inNodeId?.length || 0}入口
                          </span>
                          <span className="text-default-400">→</span>
                          <span className="font-semibold text-secondary-700 dark:text-secondary-400">
                            {tunnel.type === 2
                              ? tunnel.chainNodes?.length || 0
                              : 0}
                            跳
                          </span>
                          <span className="text-default-400">→</span>
                          <span className="font-semibold text-success-700 dark:text-success-400">
                            {tunnel.type === 2
                              ? tunnel.outNodeId?.length || 0
                              : tunnel.inNodeId?.length || 0}
                            出口
                          </span>
                        </div>
```

to:

```tsx
                        <div>
                          <div className="flex items-center gap-1.5 text-xs">
                            <span className="font-semibold text-primary-700 dark:text-primary-400">
                              {tunnel.inNodeId?.length || 0}入口
                            </span>
                            <span className="text-default-400">→</span>
                            <span className="font-semibold text-secondary-700 dark:text-secondary-400">
                              {tunnel.type === 2
                                ? tunnel.chainNodes?.length || 0
                                : 0}
                              跳
                            </span>
                            <span className="text-default-400">→</span>
                            <span className="font-semibold text-success-700 dark:text-success-400">
                              {tunnel.type === 2
                                ? tunnel.outNodeId?.length || 0
                                : tunnel.inNodeId?.length || 0}
                              出口
                            </span>
                          </div>
                          {renderBestExitState(tunnel.bestExitState)}
                        </div>
```

- [ ] **Step 5: Render in grid card topology section**

In the grid card topology section, after the closing `</div>` for the topology row at the end of the block containing `出口` and before the enclosing border section closes, add:

```tsx
                                <div className="text-center">
                                  {renderBestExitState(tunnel.bestExitState)}
                                </div>
```

The result should put the best-exit summary under the entry -> hop -> exit row inside the topology section.

- [ ] **Step 6: Run frontend build**

Run from `vite-frontend`:

```bash
pnpm run build
```

Expected: PASS with `tsc && vite build` completing successfully.

- [ ] **Step 7: Commit frontend display**

```bash
git add vite-frontend/src/pages/tunnel.tsx
git commit -m "feat: show current best exit in tunnel list"
```

---

### Task 5: Full Verification And Review

**Files:**
- Verify only.

- [ ] **Step 1: Run backend tests**

Run from `go-backend`:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 2: Run frontend build**

Run from `vite-frontend`:

```bash
pnpm run build
```

Expected: PASS.

- [ ] **Step 3: Inspect final diff**

Run from repository root:

```bash
git diff --stat origin/main...HEAD
git diff -- go-backend/internal/http/handler/tunnel_best_exit_display.go go-backend/internal/http/handler/tunnel_best_exit_display_test.go go-backend/internal/http/handler/handler.go go-backend/internal/http/handler/mutations.go vite-frontend/src/pages/tunnel.tsx
```

Expected: Diff only adds best-exit display state, response attachment, frontend list display, and tests. It must not change best-exit scoring, switching, runtime chain update, or agent code.

- [ ] **Step 4: Request final code review**

Ask a reviewer to check:

```text
Review the best-exit current display implementation. Confirm it only exposes current in-memory best-exit state in tunnel list/get responses and renders it in the tunnel list. Verify it does not change routing, scoring, switching, persistence, or polling behavior.
```

Expected: No blocking findings.

---

## Self-Review

- Spec coverage: Backend response state is Task 2 and Task 3; direct vs final-hop owner semantics are covered by Task 1 tests; frontend list/grid display is Task 4; no polling and no routing changes are preserved by Task 5 review instructions.
- Placeholder scan: The plan contains concrete files, function names, code blocks, commands, and expected outcomes.
- Type consistency: `BestExitState`, `BestExitStateItem`, `bestExitDisplayState`, `bestExitDisplayItem`, `bestExitDecisionSnapshot`, and `bestExitNodeNameLookup` are defined before use and names match across tasks.
