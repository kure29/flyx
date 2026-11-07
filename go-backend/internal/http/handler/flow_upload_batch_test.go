package handler

import (
	"path/filepath"
	"testing"
	"time"

	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
)

func TestBuildFlowUploadBatchAggregatesForwardPeerShareAndCleanupTargets(t *testing.T) {
	h := &Handler{}
	metas := map[int64]repo.FlowUploadForwardMeta{
		20: {
			ForwardID:    20,
			UserID:       2,
			UserTunnelID: 10,
			TunnelID:     1,
			TrafficRatio: 2,
			TunnelFlow:   2,
		},
	}

	batch := h.buildFlowUploadBatch([]flowItem{
		{N: "20_2_10", U: 70, D: 50},
		{N: "20_2_10_tcp", U: 40, D: 30},
		{N: "99_2_10", U: 12, D: 8},
		{N: "fed_svc_17", U: 9, D: 1},
	}, metas)

	if len(batch.flowDeltas) != 1 {
		t.Fatalf("expected 1 flow delta, got %d", len(batch.flowDeltas))
	}
	delta := batch.flowDeltas[0]
	// The tunnel's legacy ratio and direction no longer matter: both
	// directions count as they are, and no user tunnel is charged.
	if delta.ForwardID != 20 || delta.UserID != 2 || delta.UserTunnelID != 0 {
		t.Fatalf("unexpected flow delta identity: %#v", delta)
	}
	if delta.InFlow != 80 || delta.OutFlow != 110 {
		t.Fatalf("expected raw flow in=80 out=110, got in=%d out=%d", delta.InFlow, delta.OutFlow)
	}
	traffic := batch.forwardTraffic[20]
	if traffic.bytesIn != 80 || traffic.bytesOut != 110 {
		t.Fatalf("expected raw traffic in=80 out=110, got in=%d out=%d", traffic.bytesIn, traffic.bytesOut)
	}
	if _, ok := batch.orphanServices["99_2_10"]; !ok {
		t.Fatalf("expected orphan service cleanup target for 99_2_10")
	}
	if item, ok := batch.peerShareForwardItems["99_2_10"]; !ok || item.U != 12 || item.D != 8 {
		t.Fatalf("expected orphan forward to remain eligible for peer-share accounting, got %#v ok=%v", item, ok)
	}
	if item, ok := batch.peerShareForwardItems["20_2_10"]; !ok || item.U != 110 || item.D != 80 {
		t.Fatalf("expected merged peer-share forward item, got %#v ok=%v", item, ok)
	}
	if item, ok := batch.peerShareRuntimeItems[17]; !ok || item.U != 9 || item.D != 1 {
		t.Fatalf("expected merged peer-share runtime item, got %#v ok=%v", item, ok)
	}
}

func TestApplyFlowUploadBatchRollsBackWhenFlowBatchFails(t *testing.T) {
	r, err := repo.Open(filepath.Join(t.TempDir(), "flow-upload-batch-flow-fail.db"))
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	defer r.Close()

	now := time.Now()
	nowMs := now.UnixMilli()
	if err := r.DB().Create(&model.User{ID: 2, User: "flow-user", Pwd: "pwd", RoleID: 1, ExpTime: 2727251700000, Flow: 99999, Num: 99999, CreatedTime: nowMs, Status: 1}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := r.DB().Create(&model.Tunnel{ID: 1, Name: "tunnel-1", TrafficRatio: 1, Type: 1, Protocol: "tls", Flow: 1, CreatedTime: nowMs, UpdatedTime: nowMs, Status: 1}).Error; err != nil {
		t.Fatalf("seed tunnel: %v", err)
	}
	if err := r.DB().Create(&model.UserTunnel{ID: 10, UserID: 2, TunnelID: 1, Num: 99999, Flow: 0, ExpTime: 2727251700000, Status: 1}).Error; err != nil {
		t.Fatalf("seed user tunnel: %v", err)
	}
	if err := r.DB().Create(&model.Forward{ID: 20, UserID: 2, UserName: "flow-user", Name: "forward-20", TunnelID: 1, RemoteAddr: "1.1.1.1:80", Strategy: "fifo", CreatedTime: nowMs, UpdatedTime: nowMs, Status: 1}).Error; err != nil {
		t.Fatalf("seed forward: %v", err)
	}
	if err := r.DB().Create(&model.Forward{ID: 21, UserID: 2, UserName: "flow-user", Name: "forward-21", TunnelID: 1, RemoteAddr: "1.1.1.1:81", Strategy: "fifo", CreatedTime: nowMs, UpdatedTime: nowMs, Status: 1}).Error; err != nil {
		t.Fatalf("seed second forward: %v", err)
	}
	if err := r.CreatePeerShare(&repo.PeerShare{Name: "share", NodeID: 1, Token: "token", MaxBandwidth: 0, CurrentFlow: 0, PortRangeStart: 31000, PortRangeEnd: 31010, IsActive: 1, CreatedTime: nowMs, UpdatedTime: nowMs}); err != nil {
		t.Fatalf("create peer share: %v", err)
	}
	share, err := r.GetPeerShareByToken("token")
	if err != nil || share == nil {
		t.Fatalf("load peer share: %v", err)
	}
	if err := r.DB().Exec(`
		INSERT INTO peer_share_runtime(share_id, node_id, reservation_id, resource_key, binding_id, role, chain_name, service_name, protocol, strategy, port, target, applied, status, created_time, updated_time)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, share.ID, 1, "svc-r1", "svc-rk1", "", "forward", "", "20_2_10", "tcp", "fifo", 31001, "", 1, 1, nowMs, nowMs).Error; err != nil {
		t.Fatalf("insert peer share runtime: %v", err)
	}
	if err := r.DB().Exec(`
		CREATE TRIGGER fail_forward_flow_update
		BEFORE UPDATE ON forward
		WHEN NEW.id = 21 AND (NEW.in_flow != OLD.in_flow OR NEW.out_flow != OLD.out_flow)
		BEGIN
			SELECT RAISE(FAIL, 'forward flow update blocked for test');
		END;
	`).Error; err != nil {
		t.Fatalf("create flow failure trigger: %v", err)
	}

	h := &Handler{repo: r}
	h.applyFlowUploadBatch(1, flowUploadBatch{
		flowDeltas: []repo.FlowUploadCounterDelta{
			{ForwardID: 20, UserID: 2, InFlow: 80, OutFlow: 120},
			{ForwardID: 21, UserID: 2, InFlow: 30, OutFlow: 40},
		},
		peerShareForwardItems: map[string]flowItem{"20_2_10": {N: "20_2_10", U: 120, D: 80}},
	}, now)

	if got := mustQueryInt(t, r, `SELECT status FROM forward WHERE id = 20`); got != 1 {
		t.Fatalf("expected no flow-policy side effect to pause forward after flow batch failure, got status=%d", got)
	}
	updatedShare, err := r.GetPeerShare(share.ID)
	if err != nil || updatedShare == nil {
		t.Fatalf("reload peer share: %v", err)
	}
	if updatedShare.CurrentFlow != 0 {
		t.Fatalf("expected peer-share flow accounting to roll back after flow batch failure, got %d", updatedShare.CurrentFlow)
	}
	if got := mustQueryInt(t, r, `SELECT in_flow FROM forward WHERE id = 20`); got != 0 {
		t.Fatalf("expected transaction to roll back forward 20, got %d", got)
	}
	if got := mustQueryInt(t, r, `SELECT in_flow FROM forward WHERE id = 21`); got != 0 {
		t.Fatalf("expected failed forward 21 delta to remain unapplied, got %d", got)
	}
	if got := mustQueryInt(t, r, `SELECT in_flow FROM user WHERE id = 2`); got != 0 {
		t.Fatalf("expected user totals to roll back, got %d", got)
	}
}
