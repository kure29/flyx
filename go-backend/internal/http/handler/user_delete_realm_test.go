package handler

import (
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/store/model"
	"go-backend/internal/ws"
)

func TestUserDeleteReconcilesRealm(t *testing.T) {
	for _, mode := range []string{"last forwards", "shared node", "offline", "rejected apply"} {
		t.Run(mode, func(t *testing.T) {
			var reject atomic.Bool
			var state map[string]interface{}
			a := newCleanupAgentWithReply(t, func(cmd cleanupAgentCommand) ws.CommandResult {
				result := ws.CommandResult{Success: true}
				switch cmd.Type {
				case "KernelApply":
					if reject.Load() {
						return ws.CommandResult{Success: false, Message: "simulated realm apply failure"}
					}
					var req struct {
						Type    string `json:"type"`
						Hash    string `json:"hash"`
						Enabled bool   `json:"enabled"`
					}
					if err := json.Unmarshal(cmd.Data, &req); err != nil {
						t.Errorf("decode apply: %v", err)
					}
					state = map[string]interface{}{"type": req.Type, "configHash": req.Hash, "running": req.Enabled, "enabled": req.Enabled}
					result.Data = state
				case "KernelStatus":
					result.Data = map[string]interface{}{"kernels": []interface{}{state}}
				}
				return result
			})
			h, r := a.h, a.h.repo
			now := time.Now().UnixMilli()
			if err := r.DB().Exec(`INSERT INTO user(id,user,pwd,role_id,exp_time,flow,in_flow,out_flow,flow_reset_time,num,created_time,updated_time,status)
			VALUES(2,'realm-user','pwd',1,2727251700000,99999,0,0,1,99999,?,?,1)`, now, now).Error; err != nil {
				t.Fatal(err)
			}
			tid := seedTunnelForNftables(t, h, "realm-delete", 1)
			if err := r.DB().Exec(`UPDATE tunnel SET engine='realm' WHERE id=?`, tid).Error; err != nil {
				t.Fatal(err)
			}
			for i, remote := range []string{"203.0.113.5:443", "203.0.113.6:443"} {
				if _, err := r.CreateForwardTx(2, "realm-user", "realm-forward", tid, remote, "fifo", now, 1, []int64{1}, 20000+i, "", nil, 0, 0, nil, 0, 0, 0); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "shared node" {
				if _, err := r.CreateForwardTx(1, "admin", "keep-forward", tid, "203.0.113.9:443", "fifo", now, 1, []int64{1}, 20002, "", nil, 0, 0, nil, 0, 0, 0); err != nil {
					t.Fatal(err)
				}
			}
			k, err := h.ensureRealmKernel(1, true)
			if err != nil {
				t.Fatal(err)
			}
			if err := h.applyKernel(k.ID, false); err != nil {
				t.Fatal(err)
			}
			deleteUser := func() response.R {
				t.Helper()
				res := postJSONToHandler(t, h.userDelete, map[string]interface{}{"id": 2})
				var out response.R
				if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
					t.Fatal(err)
				}
				return out
			}
			if mode == "rejected apply" {
				reject.Store(true)
				if out := deleteUser(); out.Code == 0 {
					t.Fatal("rejected apply must fail deletion")
				}
				var count int64
				if err := r.DB().Model(&model.User{}).Where("id=2").Count(&count).Error; err != nil || count != 1 {
					t.Fatalf("user must remain retryable: %d %v", count, err)
				}
				if err := r.DB().Model(&model.Forward{}).Where("user_id=2").Count(&count).Error; err != nil || count != 2 {
					t.Fatalf("forward bindings must remain: %d %v", count, err)
				}
				reject.Store(false)
			}
			online := h.wsServer
			if mode == "offline" {
				h.wsServer = ws.NewServer(r, "test-jwt-secret")
			}
			out := deleteUser()
			if out.Code != 0 {
				t.Fatalf("delete: %+v", out)
			}
			if mode == "offline" {
				if out.Data == nil || len(out.Data.(map[string]interface{})["warnings"].([]interface{})) == 0 {
					t.Fatal("offline cleanup must report a warning")
				}
				pending, err := r.GetNodeKernel(k.ID)
				if err != nil || pending.Status != kernelStatusPending || pending.ConfigHash == pending.AppliedHash {
					t.Fatalf("missing pending cleanup: %+v %v", pending, err)
				}
				h.wsServer = online
				h.reconcileNodeKernels(1)
			}
			commands := a.commandsOfType("KernelApply")
			wantCommands := 2
			if mode == "rejected apply" {
				wantCommands++
			}
			if len(commands) != wantCommands {
				t.Fatalf("expected one cleanup per node: got %d commands", len(commands))
			}
			var payload struct {
				Enabled bool `json:"enabled"`
				Files   []struct {
					Content string `json:"content"`
				} `json:"files"`
			}
			if err := json.Unmarshal(commands[len(commands)-1].Data, &payload); err != nil {
				t.Fatal(err)
			}
			config := payload.Files[0].Content
			if strings.Contains(config, "203.0.113.5") || strings.Contains(config, "203.0.113.6") {
				t.Fatalf("deleted endpoints remain: %s", config)
			}
			if mode == "shared node" {
				if !payload.Enabled || !strings.Contains(config, "203.0.113.9") {
					t.Fatalf("other user's forward was removed: %s", config)
				}
			} else if payload.Enabled {
				t.Fatal("empty realm must be stopped")
			}
			var count int64
			if err := r.DB().Model(&model.User{}).Where("id=2").Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("user remains: %d %v", count, err)
			}
			if err := r.DB().Model(&model.Forward{}).Where("user_id=2").Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("forwards remain: %d %v", count, err)
			}
			stored, err := r.GetNodeKernel(k.ID)
			if err != nil {
				t.Fatal(err)
			}
			_, hash, err := h.buildKernelApplyPayload(stored)
			if err != nil || hash != stored.AppliedHash {
				t.Fatalf("agent config differs from deleted DB: %v", err)
			}
		})
	}
}
