package contract_test

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go-backend/internal/auth"
	"go-backend/internal/store/repo"
)

type notifySink struct {
	mu       sync.Mutex
	telegram []map[string]interface{}
	webhook  []map[string]interface{}
	paths    []string
}

func (s *notifySink) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]interface{}
		_ = json.Unmarshal(raw, &body)
		s.mu.Lock()
		s.paths = append(s.paths, r.URL.Path)
		if strings.HasPrefix(r.URL.Path, "/bot") {
			s.telegram = append(s.telegram, body)
		} else {
			s.webhook = append(s.webhook, body)
		}
		s.mu.Unlock()
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (s *notifySink) waitWebhook(t *testing.T, n int) []map[string]interface{} {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		if len(s.webhook) >= n {
			out := append([]map[string]interface{}(nil), s.webhook...)
			s.mu.Unlock()
			return out
		}
		s.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t.Fatalf("expected %d webhook calls, got %d", n, len(s.webhook))
	return nil
}

func setConfig(t *testing.T, r *repo.Repository, name, value string) {
	t.Helper()
	if err := r.DB().Exec(`
		INSERT INTO vite_config(name, value, time) VALUES(?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET value = excluded.value, time = excluded.time`,
		name, value, time.Now().UnixMilli()).Error; err != nil {
		t.Fatal(err)
	}
}

func TestNotifyTestEndpointContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, _ := setupContractRouter(t, secret)
	admin, _ := auth.GenerateToken(1, "admin_user", 0, secret)
	user, _ := auth.GenerateToken(2, "normal_user", 1, secret)
	sink := &notifySink{}
	srv := sink.server(t)

	if out := kernelContractPost(t, router, user, "/api/v1/notify/test", `{}`); out.Code == 0 {
		t.Fatalf("non-admins must be refused: %+v", out)
	}
	if out := kernelContractPost(t, router, admin, "/api/v1/notify/test", `{}`); out.Code == 0 {
		t.Fatalf("nothing configured must be an error: %+v", out)
	}

	// Unsaved form values are used as given.
	out := kernelContractPost(t, router, admin, "/api/v1/notify/test",
		`{"telegramToken":"123:abc","telegramChatId":"42","telegramApiBase":"`+srv.URL+`","webhookUrl":"`+srv.URL+`/hook"}`)
	if out.Code != 0 {
		t.Fatalf("test send: %+v", out)
	}
	if len(sink.telegram) != 1 || sink.paths[0] != "/bot123:abc/sendMessage" || sink.telegram[0]["chat_id"] != "42" ||
		!strings.Contains(sink.telegram[0]["text"].(string), "测试通知") {
		t.Fatalf("telegram call: %v %v", sink.paths, sink.telegram)
	}
	if len(sink.webhook) != 1 || sink.webhook[0]["event"] != "test" || sink.webhook[0]["title"] != "测试通知" {
		t.Fatalf("webhook call: %v", sink.webhook)
	}

	// A failing endpoint reports why without leaking the bot token.
	out = kernelContractPost(t, router, admin, "/api/v1/notify/test",
		`{"telegramToken":"123:secret-token","telegramChatId":"42","telegramApiBase":"http://127.0.0.1:1"}`)
	if out.Code == 0 || !strings.Contains(out.Msg, "Telegram") || strings.Contains(out.Msg, "secret-token") {
		t.Fatalf("failure message: %+v", out)
	}

	// Credentials stay hidden from non-admins and out of backups.
	for _, key := range []string{"notify_telegram_bot_token", "notify_webhook_url"} {
		if !repo.IsSensitiveConfigKey(key) {
			t.Fatalf("%s must be sensitive", key)
		}
	}
}

func TestNotifyImportedNodeDownContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, r := setupContractRouter(t, secret)
	admin, _ := auth.GenerateToken(1, "admin_user", 0, secret)
	sink := &notifySink{}
	srv := sink.server(t)
	setConfig(t, r, "notify_enabled", "true")
	setConfig(t, r, "notify_webhook_url", srv.URL+"/hook")
	setConfig(t, r, "notify_events", "inbound_down")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	link := "ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:secret")) + "@198.51.100.50:1001#落地"
	if out := kernelContractPost(t, router, admin, "/api/v1/inbound/import", `{"links":"`+link+`"}`); out.Code != 0 {
		t.Fatalf("import: %+v", out)
	}
	if err := r.DB().Exec(`UPDATE node_inbound SET listen = '127.0.0.1', port = ?`, port).Error; err != nil {
		t.Fatal(err)
	}

	kernelContractPost(t, router, admin, "/api/v1/inbound/probe", `{}`)
	time.Sleep(200 * time.Millisecond)
	if len(sink.webhook) != 0 {
		t.Fatal("one failed probe must not alert yet")
	}
	kernelContractPost(t, router, admin, "/api/v1/inbound/probe", `{}`)
	calls := sink.waitWebhook(t, 1)
	if calls[0]["event"] != "inbound_down" || !strings.Contains(calls[0]["message"].(string), "落地") {
		t.Fatalf("down alert: %v", calls[0])
	}
	kernelContractPost(t, router, admin, "/api/v1/inbound/probe", `{}`)
	time.Sleep(200 * time.Millisecond)
	if len(sink.webhook) != 1 {
		t.Fatal("an ongoing outage must alert only once")
	}

	ln, err = net.Listen("tcp", "127.0.0.1:"+jsonInt64(int64(port)))
	if err != nil {
		t.Skipf("port reused: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	kernelContractPost(t, router, admin, "/api/v1/inbound/probe", `{}`)
	calls = sink.waitWebhook(t, 2)
	if calls[1]["title"] != "节点已恢复" {
		t.Fatalf("recovery alert: %v", calls[1])
	}

	// Events left out of notify_events stay quiet.
	setConfig(t, r, "notify_events", "node_offline")
	_ = ln.Close()
	kernelContractPost(t, router, admin, "/api/v1/inbound/probe", `{}`)
	kernelContractPost(t, router, admin, "/api/v1/inbound/probe", `{}`)
	time.Sleep(200 * time.Millisecond)
	if len(sink.webhook) != 2 {
		t.Fatal("unselected events must not be sent")
	}
}
