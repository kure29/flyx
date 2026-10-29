package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"go-backend/internal/http/response"
	"go-backend/internal/security"
	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
	"go-backend/internal/ws"
)

func decodeNodeDelete(t *testing.T, rec *httptest.ResponseRecorder, into interface{}) {
	t.Helper()
	var out response.R
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Code != 0 {
		t.Fatalf("delete failed: %+v", out)
	}
	raw, err := json.Marshal(out.Data)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatal(err)
	}
}

func assertNodeGone(t *testing.T, r *repo.Repository, id int64) {
	t.Helper()
	if node, err := r.GetNodeByID(id); err != nil || node != nil {
		t.Fatalf("node %d remains: %+v %v", id, node, err)
	}
}

func TestNodeDeleteUninstallsOnlineAgent(t *testing.T) {
	for _, tc := range []struct {
		name      string
		uninstall interface{}
		reply     func(cleanupAgentCommand) ws.CommandResult
		want      string
		commands  int
	}{
		{name: "confirmed", uninstall: true, want: nodeUninstallDone, commands: 1},
		{name: "string flag", uninstall: "true", want: nodeUninstallDone, commands: 1},
		{name: "agent too old", uninstall: true, want: nodeUninstallUnsupported, commands: 1,
			reply: func(c cleanupAgentCommand) ws.CommandResult {
				if c.Type == "UninstallAgent" {
					return ws.CommandResult{Message: "未知命令类型: UninstallAgent"}
				}
				return ws.CommandResult{Success: true}
			}},
		{name: "not requested", uninstall: false, want: "", commands: 0},
		{name: "flag omitted", uninstall: nil, want: "", commands: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newCleanupAgentWithReply(t, tc.reply)
			h := a.h
			body := map[string]interface{}{"id": 1}
			if tc.uninstall != nil {
				body["uninstall"] = tc.uninstall
			}
			var got nodeDeleteResult
			decodeNodeDelete(t, postJSONToHandler(t, h.nodeDelete, body), &got)
			if got.Uninstall != tc.want {
				t.Fatalf("uninstall=%q want %q (%+v)", got.Uninstall, tc.want, got)
			}
			if n := len(a.commandsOfType("UninstallAgent")); n != tc.commands {
				t.Fatalf("UninstallAgent sent %d times", n)
			}
			assertNodeGone(t, h.repo, 1)
			if pending, err := h.repo.GetNodeUninstallBySecret("cleanup-regression-node"); err != nil || pending != nil {
				t.Fatalf("an online outcome must not stay pending: %+v %v", pending, err)
			}
		})
	}
}

func newOfflineNodeHandler(t *testing.T) *Handler {
	t.Helper()
	r, err := repo.Open(filepath.Join(t.TempDir(), "offline.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	for _, n := range []model.Node{
		{ID: 1, Name: "tokyo", Secret: "offline-secret-1", ServerIP: "198.51.100.1", Port: "30000"},
		{ID: 2, Name: "osaka", Secret: "offline-secret-2", ServerIP: "198.51.100.2", Port: "30000"},
	} {
		if err := r.DB().Create(&n).Error; err != nil {
			t.Fatal(err)
		}
	}
	return &Handler{repo: r, wsServer: ws.NewServer(r, "test-jwt-secret")}
}

func TestNodeDeleteRemembersOfflineAgent(t *testing.T) {
	h := newOfflineNodeHandler(t)
	var got nodeDeleteResult
	decodeNodeDelete(t, postJSONToHandler(t, h.nodeDelete, map[string]interface{}{"id": 1, "uninstall": true}), &got)
	if got.Uninstall != nodeUninstallPending {
		t.Fatalf("result %+v", got)
	}
	assertNodeGone(t, h.repo, 1)
	pending, err := h.repo.GetNodeUninstallBySecret("offline-secret-1")
	if err != nil || pending == nil || pending.NodeName != "tokyo" {
		t.Fatalf("pending: %+v %v", pending, err)
	}
	// Deleting without uninstall leaves nothing behind.
	decodeNodeDelete(t, postJSONToHandler(t, h.nodeDelete, map[string]interface{}{"id": 2}), &got)
	if pending, _ := h.repo.GetNodeUninstallBySecret("offline-secret-2"); pending != nil {
		t.Fatal("uninstall was not requested")
	}
}

func TestNodeBatchDeleteSummarizesUninstalls(t *testing.T) {
	h := newOfflineNodeHandler(t)
	var got nodeBatchDeleteResult
	decodeNodeDelete(t, postJSONToHandler(t, h.nodeBatchDelete, map[string]interface{}{"ids": []interface{}{1, "2"}, "uninstall": true}), &got)
	if got.Deleted != 2 || got.Pending != 2 || got.Failed != 0 || got.Uninstalled != 0 {
		t.Fatalf("summary %+v", got)
	}
	assertNodeGone(t, h.repo, 1)
	assertNodeGone(t, h.repo, 2)
}

// The agent of a server deleted while offline reconnects with its old secret.
func TestDeletedAgentIsUninstalledWhenItReconnects(t *testing.T) {
	for _, tc := range []struct {
		name        string
		success     bool
		message     string
		keepPending bool
	}{
		{name: "uninstalled", success: true},
		{name: "too old", message: "未知命令类型: UninstallAgent"},
		{name: "failed", message: "boom", keepPending: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := repo.Open(filepath.Join(t.TempDir(), "reconnect.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = r.Close() })
			const secret = "deleted-node-secret"
			if err := r.AddNodeUninstall(secret, "tokyo", time.Now()); err != nil {
				t.Fatal(err)
			}
			server := ws.NewServer(r, "test-jwt-secret")
			done := make(chan struct{})
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				defer close(done)
				server.ServeHTTP(w, req)
			}))
			t.Cleanup(ts.Close)
			url := "ws" + strings.TrimPrefix(ts.URL, "http") + "?type=1&secret="
			if _, resp, err := websocket.DefaultDialer.Dial(url+"unknown-secret", nil); err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
				t.Fatalf("unknown secrets must still be refused: %v", err)
			}
			<-done
			done = make(chan struct{})

			conn, _, err := websocket.DefaultDialer.Dial(url+secret, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			// Like the agent, report metrics first; the server must skip them.
			if err := conn.WriteJSON(map[string]interface{}{"type": "info", "data": map[string]interface{}{"uptime": 1}}); err != nil {
				t.Fatal(err)
			}
			_, payload, err := conn.ReadMessage()
			if err != nil {
				t.Fatal(err)
			}
			var envelope struct {
				Encrypted bool   `json:"encrypted"`
				Data      string `json:"data"`
			}
			if err := json.Unmarshal(payload, &envelope); err != nil || !envelope.Encrypted {
				t.Fatalf("command must be encrypted: %s", payload)
			}
			crypto, err := security.NewAESCrypto(secret)
			if err != nil {
				t.Fatal(err)
			}
			plain, err := crypto.Decrypt(envelope.Data)
			if err != nil {
				t.Fatal(err)
			}
			var cmd cleanupAgentCommand
			if err := json.Unmarshal(plain, &cmd); err != nil || cmd.Type != "UninstallAgent" || cmd.RequestID == "" {
				t.Fatalf("command %s", plain)
			}
			if err := conn.WriteJSON(map[string]interface{}{"type": "UninstallAgentResponse", "requestId": cmd.RequestID, "success": tc.success, "message": tc.message}); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("server kept the connection of a deleted node")
			}
			pending, err := r.GetNodeUninstallBySecret(secret)
			if err != nil || (pending != nil) != tc.keepPending {
				t.Fatalf("pending=%+v err=%v, want kept=%v", pending, err, tc.keepPending)
			}
			if server.IsNodeOnline(0) {
				t.Fatal("a deleted node must not be registered")
			}
		})
	}
}
