package handler

import (
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
			exit, ok := exitsByID[snapshot.AppliedExitNodeID]
			if !ok {
				state.Items = append(state.Items, item)
				continue
			}
			item.ExitNodeID = snapshot.AppliedExitNodeID
			item.ExitNodeName = bestExitDisplayNodeName(exit, snapshot.AppliedExitNodeID, lookup, bestExitUnknownExitName)
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
	if appliedCount < len(state.Items) {
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
