package handler

import (
	"encoding/json"
	"errors"
	"github.com/gorilla/websocket"
	"go-backend/internal/security"
	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
	"go-backend/internal/ws"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newSubscriptionRetryAgent(t *testing.T, failCommand string, fail *atomic.Bool) *cleanupAgent {
	t.Helper()
	r, err := repo.Open(filepath.Join(t.TempDir(), "cleanup.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	const secret = "cleanup-regression-node"
	if err := r.DB().Create(&model.Node{ID: 1, Name: "relay", Secret: secret, ServerIP: "127.0.0.1", Port: "30000", Status: 1}).Error; err != nil {
		t.Fatal(err)
	}
	server := ws.NewServer(r, "test-jwt-secret")
	online := make(chan struct{})
	server.SetNodeOnlineHook(func(int64) { close(online) })
	serverDone := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		defer close(serverDone)
		server.ServeHTTP(w, req)
	}))
	t.Cleanup(ts.Close)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"?type=1&secret="+secret, nil)
	if err != nil {
		t.Fatal(err)
	}
	crypto, err := security.NewAESCrypto(secret)
	if err != nil {
		t.Fatal(err)
	}
	a := &cleanupAgent{h: &Handler{repo: r, wsServer: server}}
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			_, payload, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var envelope struct {
				Encrypted bool   `json:"encrypted"`
				Data      string `json:"data"`
			}
			if err := json.Unmarshal(payload, &envelope); err != nil {
				t.Errorf("decode command envelope: %v", err)
				return
			}
			if envelope.Encrypted {
				payload, err = crypto.Decrypt(envelope.Data)
				if err != nil {
					t.Errorf("decrypt command: %v", err)
					return
				}
			}
			var command cleanupAgentCommand
			if err := json.Unmarshal(payload, &command); err != nil {
				t.Errorf("decode command: %v", err)
				return
			}
			a.mu.Lock()
			a.commands = append(a.commands, command)
			a.mu.Unlock()
			if err := conn.WriteJSON(map[string]interface{}{"type": command.Type, "requestId": command.RequestID, "success": !(command.Type == failCommand && fail.Load()), "message": "injected temporary agent failure"}); err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = conn.Close()
		<-readerDone
		<-serverDone
	})
	select {
	case <-online:
	case <-time.After(3 * time.Second):
		t.Fatal("mock node did not come online")
	}
	a.probe(t)
	return a
}

func TestSubscriptionStateRetriesAfterKernelFailure(t *testing.T) {
	for _, disable := range []bool{true, false} {
		name := "restore"
		if disable {
			name = "quota-cutoff"
		}
		t.Run(name, func(t *testing.T) {
			var fail atomic.Bool
			fail.Store(true)
			a := newSubscriptionRetryAgent(t, "KernelApply", &fail)
			h := a.h
			now := time.Now()
			k := &model.NodeKernel{NodeID: 1, KernelType: "xray", Version: "latest", Enabled: 1}
			if err := h.repo.CreateNodeKernel(k); err != nil {
				t.Fatal(err)
			}
			in := &model.NodeInbound{NodeID: 1, KernelID: k.ID, Name: "retry-in", Protocol: "trojan", Port: 443, Settings: `{"password":"owner-pw"}`, Enabled: 1}
			if err := h.repo.DB().Create(in).Error; err != nil {
				t.Fatal(err)
			}
			previous, want := 0, 1
			used := int64(0)
			if disable {
				previous, want, used = 1, 0, 200
			}
			s := &model.Subscription{Name: "retry-user", Kind: model.SubscriptionShare, Token: "retry-token", Password: "user-password", SourceAll: 1, Enabled: 1, AppliedActive: previous, TrafficLimit: 100, UploadBytes: used}
			if err := h.repo.SaveSubscription(s); err != nil {
				t.Fatal(err)
			}
			h.syncSubscriptions(now)
			first := len(a.commandsOfType("KernelApply"))
			if first == 0 {
				t.Fatal("kernel command not attempted")
			}
			got, err := h.repo.GetSubscription(s.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.AppliedActive != previous {
				t.Errorf("failed apply marked as complete: got %d want %d", got.AppliedActive, previous)
			}
			fail.Store(false)
			h.syncSubscriptions(now.Add(time.Minute))
			second := len(a.commandsOfType("KernelApply"))
			if second <= first {
				t.Fatalf("missing retry: %d -> %d", first, second)
			}
			got, err = h.repo.GetSubscription(s.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.AppliedActive != want {
				t.Fatalf("successful retry did not update applied state: %d", got.AppliedActive)
			}
			h.syncSubscriptions(now.Add(2 * time.Minute))
			if third := len(a.commandsOfType("KernelApply")); third != second {
				t.Fatalf("acknowledged state retried: %d -> %d", second, third)
			}
		})
	}
}

func TestApplySubscriptionKernelsPropagatesInboundLookupFailure(t *testing.T) {
	r, err := repo.Open(t.TempDir() + "/lookup.db")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	h := New(r, "test")
	injected := errors.New("temporary inbound lookup failure")
	if err := r.DB().Callback().Query().Before("gorm:query").Register("test:inbound-query", func(tx *gorm.DB) {
		if tx.Statement.Table == "node_inbound" {
			tx.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.applyKernelsForInbounds([]int64{1}); !errors.Is(err, injected) {
		t.Fatalf("lookup failure was acknowledged: %v", err)
	}
}
