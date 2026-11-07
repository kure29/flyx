package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"go-backend/internal/store/repo"
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

func TestBuildBestExitDisplayStateKeepsTopLevelWaitingWhenSomeOwnersPending(t *testing.T) {
	m := newBestExitManager()
	now := time.Unix(400, 0)
	m.setApplied(bestExitOwnerKey{TunnelID: 77, OwnerNodeID: 10}, 30, now)

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
	if state.Status != bestExitDisplayStatusWaiting || state.Summary != bestExitDisplaySummaryWait {
		t.Fatalf("expected top-level waiting for partial owner state, got %+v", state)
	}
	if len(state.Items) != 2 {
		t.Fatalf("expected two owner items, got %+v", state.Items)
	}
	if state.Items[0].ExitNodeID != 30 || state.Items[0].ExitNodeName != "香港节点" {
		t.Fatalf("expected first owner applied details to remain visible, got %+v", state.Items[0])
	}
	if state.Items[1].ExitNodeID != 0 || state.Items[1].ExitNodeName != bestExitDisplaySummaryWait {
		t.Fatalf("expected second owner waiting details, got %+v", state.Items[1])
	}
}

func TestBuildBestExitDisplayStateIgnoresAppliedExitRemovedFromTunnel(t *testing.T) {
	m := newBestExitManager()
	now := time.Unix(500, 0)
	m.setApplied(bestExitOwnerKey{TunnelID: 77, OwnerNodeID: 10}, 99, now)

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
	names := map[int64]string{10: "入口 A", 30: "香港节点", 31: "日本节点", 99: "已删除节点"}

	state, ok := buildBestExitDisplayState(tunnel, m, testBestExitNameLookup(names))
	if !ok {
		t.Fatalf("expected best exit state")
	}
	if state.Status != bestExitDisplayStatusWaiting || state.Summary != bestExitDisplaySummaryWait {
		t.Fatalf("expected waiting state for stale applied exit, got %+v", state)
	}
	if len(state.Items) != 1 {
		t.Fatalf("expected one item, got %+v", state.Items)
	}
	if state.Items[0].ExitNodeID != 0 || state.Items[0].ExitNodeName != bestExitDisplaySummaryWait {
		t.Fatalf("expected stale exit to be ignored, got %+v", state.Items[0])
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

func TestTunnelListAttachesBestExitStateOnlyForEligibleTunnels(t *testing.T) {
	h := setupBestExitTunnelHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tunnel/list", nil)
	res := httptest.NewRecorder()
	h.tunnelList(res, req)

	var payload struct {
		Code int              `json:"code"`
		Data []map[string]any `json:"data"`
	}
	decodeBestExitTunnelResponse(t, res, &payload)
	if payload.Code != 0 {
		t.Fatalf("expected success response, got code %d", payload.Code)
	}

	bestTunnel := findTunnelResponseItem(t, payload.Data, 77)
	if _, ok := bestTunnel["bestExitState"]; !ok {
		t.Fatalf("expected eligible best multi-exit tunnel to include bestExitState: %+v", bestTunnel)
	}

	singleExitTunnel := findTunnelResponseItem(t, payload.Data, 78)
	if _, ok := singleExitTunnel["bestExitState"]; ok {
		t.Fatalf("expected single-exit tunnel to omit bestExitState: %+v", singleExitTunnel)
	}

	nonBestTunnel := findTunnelResponseItem(t, payload.Data, 79)
	if _, ok := nonBestTunnel["bestExitState"]; ok {
		t.Fatalf("expected non-best tunnel to omit bestExitState: %+v", nonBestTunnel)
	}
}

func TestTunnelGetAttachesBestExitStateToSelectedTunnel(t *testing.T) {
	h := setupBestExitTunnelHandler(t)

	body := bytes.NewReader([]byte(`{"id":77}`))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tunnel/get", body)
	res := httptest.NewRecorder()
	h.tunnelGet(res, req)

	var payload struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	decodeBestExitTunnelResponse(t, res, &payload)
	if payload.Code != 0 {
		t.Fatalf("expected success response, got code %d", payload.Code)
	}
	if _, ok := payload.Data["bestExitState"]; !ok {
		t.Fatalf("expected selected best multi-exit tunnel to include bestExitState: %+v", payload.Data)
	}
}

func setupBestExitTunnelHandler(t *testing.T) *Handler {
	t.Helper()
	r, err := repo.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	h := New(r, "secret")
	now := time.Now().UnixMilli()

	insertNode := func(id int64, name string) {
		t.Helper()
		if err := r.DB().Exec(`
			INSERT INTO node(id, name, secret, server_ip, server_ip_v4, server_ip_v6, port, interface_name, version, http, tls, socks, created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, id, name, name+"-secret", "10.0.0.1", "10.0.0.1", "", "30000-30010", "", "v1", 1, 1, 1, now, now, 1, "[::]", "[::]", 0).Error; err != nil {
			t.Fatalf("insert node %s: %v", name, err)
		}
	}
	insertNode(10, "entry-a")
	insertNode(30, "exit-a")
	insertNode(31, "exit-b")
	insertNode(32, "exit-c")

	insertTunnel := func(id int64, name string) {
		t.Helper()
		if err := r.DB().Exec(`
			INSERT INTO tunnel(id, name, traffic_ratio, type, protocol, flow, created_time, updated_time, status, inx, ip_preference)
			VALUES(?, ?, 1, 1, 'tls', 1, ?, ?, 1, ?, '')
		`, id, name, now, now, id).Error; err != nil {
			t.Fatalf("insert tunnel %s: %v", name, err)
		}
	}
	insertTunnel(77, "best-multi")
	insertTunnel(78, "best-single")
	insertTunnel(79, "round-multi")

	insertChain := func(tunnelID int64, chainType string, nodeID int64, strategy string, inx int64) {
		t.Helper()
		if err := r.DB().Exec(`
			INSERT INTO chain_tunnel(tunnel_id, chain_type, node_id, port, strategy, inx, protocol)
			VALUES(?, ?, ?, 30001, ?, ?, 'tls')
		`, tunnelID, chainType, nodeID, strategy, inx).Error; err != nil {
			t.Fatalf("insert chain tunnel %d/%s/%d: %v", tunnelID, chainType, nodeID, err)
		}
	}
	insertChain(77, "1", 10, "round", 1)
	insertChain(77, "3", 30, tunnelStrategyBest, 1)
	insertChain(77, "3", 31, tunnelStrategyBest, 2)
	insertChain(78, "1", 10, "round", 1)
	insertChain(78, "3", 30, tunnelStrategyBest, 1)
	insertChain(79, "1", 10, "round", 1)
	insertChain(79, "3", 31, "round", 1)
	insertChain(79, "3", 32, "round", 2)

	h.bestExit.setApplied(bestExitOwnerKey{TunnelID: 77, OwnerNodeID: 10}, 30, time.UnixMilli(now))
	return h
}

func decodeBestExitTunnelResponse(t *testing.T, res *httptest.ResponseRecorder, v any) {
	t.Helper()
	if res.Code != http.StatusOK {
		t.Fatalf("expected HTTP %d, got %d", http.StatusOK, res.Code)
	}
	if err := json.NewDecoder(res.Body).Decode(v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func findTunnelResponseItem(t *testing.T, items []map[string]any, id float64) map[string]any {
	t.Helper()
	for _, item := range items {
		if item["id"] == id {
			return item
		}
	}
	t.Fatalf("tunnel %.0f not found in response: %+v", id, items)
	return nil
}

func testBestExitNameLookup(names map[int64]string) bestExitNodeNameLookup {
	return func(nodeID int64) (string, bool) {
		name := names[nodeID]
		return name, name != ""
	}
}
