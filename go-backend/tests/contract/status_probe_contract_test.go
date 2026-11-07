package contract_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// An agent's report reaches the database and the public page; the history
// routes are public. The agent here is a plain WebSocket client sending what
// go-gost's reporter sends.
func TestAgentHostFactsAndSwapReachStatusPage(t *testing.T) {
	router, repo := setupContractRouter(t, "status-probe-secret")
	server := httptest.NewServer(router)
	defer server.Close()

	now := time.Now().UnixMilli()
	if err := repo.DB().Exec(`
		INSERT INTO node(name, secret, server_ip, port, created_time, status, tcp_listen_addr, udp_listen_addr)
		VALUES('probe-node', 'probe-node-secret', '192.0.2.9', '1000', ?, 0, '[::]', '[::]')`, now).Error; err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{"status_page_enabled": "true", "status_page_show_host": "true"} {
		if err := repo.UpsertConfig(k, v, now); err != nil {
			t.Fatal(err)
		}
	}

	u, _ := url.Parse(server.URL)
	u.Scheme, u.Path = "ws", "/system-info"
	u.RawQuery = url.Values{"type": {"1"}, "secret": {"probe-node-secret"}, "version": {"v1"}}.Encode()
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	metric := map[string]interface{}{
		"type": "metric",
		"data": map[string]interface{}{
			"cpu_usage": 12.5, "memory_usage": 40, "disk_usage": 30,
			"swap_used": 1 << 29, "swap_total": 1 << 30, "process_count": 123,
			"host": map[string]string{
				"os": "Debian GNU/Linux 12 (bookworm)", "kernel": "6.1.0-27-amd64", "arch": "x86_64",
				"cpu_name": "AMD EPYC 7B13", "virtualization": "kvm",
			},
		},
	}
	raw, _ := json.Marshal(metric)
	if err := conn.WriteMessage(websocket.TextMessage, raw); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		var os, kernel string
		_ = repo.DB().Raw(`SELECT host_os, host_kernel FROM node WHERE name = 'probe-node'`).Row().Scan(&os, &kernel)
		if os == "Debian GNU/Linux 12 (bookworm)" && kernel == "6.1.0-27-amd64" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("host facts not stored: os=%q kernel=%q", os, kernel)
		}
		time.Sleep(20 * time.Millisecond)
	}

	var page struct {
		Code int `json:"code"`
		Data struct {
			Nodes []map[string]interface{} `json:"nodes"`
		} `json:"data"`
	}
	for {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/public/status", nil))
		_ = json.Unmarshal(rec.Body.Bytes(), &page)
		if page.Code == 0 && len(page.Data.Nodes) == 1 && page.Data.Nodes[0]["swap"] != nil {
			if strings.Contains(rec.Body.String(), "6.1.0-27") {
				t.Fatalf("the kernel version is never public: %s", rec.Body.String())
			}
			break
		}
		if time.Now().After(deadline.Add(5 * time.Second)) {
			t.Fatalf("status page did not show the report: %s", rec.Body.String())
		}
		time.Sleep(200 * time.Millisecond)
	}
	n := page.Data.Nodes[0]
	if n["swap"].(float64) != 50 || n["procs"].(float64) != 123 || n["os"] != "Debian GNU/Linux 12 (bookworm)" || n["virtualization"] != "kvm" {
		t.Fatalf("node on the page = %v", n)
	}

	for _, path := range []string{"/api/v1/public/status/history?node=1&range=1h", "/api/v1/public/status/ping-history?node=1&range=1h"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		var out struct {
			Code int `json:"code"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if out.Code != 0 {
			t.Fatalf("%s without a login: %s", path, rec.Body.String())
		}
	}
}
