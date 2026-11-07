package ws

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"go-backend/internal/auth"
	"go-backend/internal/store/repo"

	"github.com/gorilla/websocket"
)

func TestServeHTTPRejectsDisabledAdminToken(t *testing.T) {
	secret := "unit-test-secret"
	token, err := auth.GenerateToken(1, "admin_user", 0, secret)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	server := NewServer(nil, secret)
	server.SetUserAuthStateLookup(func(userID int64) (*auth.UserAuthState, error) {
		return &auth.UserAuthState{ID: userID, RoleID: 0, Status: 0, PasswordChangedAt: 0}, nil
	})

	req := httptest.NewRequest(http.MethodGet, "/system-info?type=0&secret="+url.QueryEscape(token), nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden for disabled admin token, got %d", rec.Code)
	}
}

func TestServeHTTPRejectsNonAdminToken(t *testing.T) {
	secret := "unit-test-secret"
	token, err := auth.GenerateToken(2, "normal_user", 1, secret)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	server := NewServer(nil, secret)
	server.SetUserAuthStateLookup(func(userID int64) (*auth.UserAuthState, error) {
		return &auth.UserAuthState{ID: userID, RoleID: 1, Status: 1, PasswordChangedAt: 0}, nil
	})

	req := httptest.NewRequest(http.MethodGet, "/system-info?type=0&secret="+url.QueryEscape(token), nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden for non-admin token, got %d", rec.Code)
	}
}

func TestValidateAdminSessionRejectsAuthStateChanges(t *testing.T) {
	secret := "unit-test-secret"
	token, err := auth.GenerateToken(1, "admin_user", 0, secret)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	claims, err := auth.ParseClaims(token, secret)
	if err != nil {
		t.Fatalf("parse claims: %v", err)
	}

	tests := []struct {
		name  string
		state *auth.UserAuthState
	}{
		{name: "disabled", state: &auth.UserAuthState{ID: 1, RoleID: 0, Status: 0, PasswordChangedAt: 0}},
		{name: "role changed", state: &auth.UserAuthState{ID: 1, RoleID: 1, Status: 1, PasswordChangedAt: 0}},
		{name: "password changed", state: &auth.UserAuthState{ID: 1, RoleID: 0, Status: 1, PasswordChangedAt: claims.IatMs + 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewServer(nil, secret)
			server.SetUserAuthStateLookup(func(userID int64) (*auth.UserAuthState, error) {
				return tt.state, nil
			})

			if ok := server.validateAdminSession(1, claims); ok {
				t.Fatalf("expected session validation to fail for %s state", tt.name)
			}
		})
	}
}

func TestValidateAdminSessionRejectsExpiredToken(t *testing.T) {
	secret := "unit-test-secret"
	token, err := auth.GenerateToken(1, "admin_user", 0, secret)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	claims, err := auth.ParseClaims(token, secret)
	if err != nil {
		t.Fatalf("parse claims: %v", err)
	}
	claims.Exp = 1

	server := NewServer(nil, secret)
	server.SetUserAuthStateLookup(func(userID int64) (*auth.UserAuthState, error) {
		return &auth.UserAuthState{ID: userID, RoleID: 0, Status: 1, PasswordChangedAt: 0}, nil
	})

	if ok := server.validateAdminSession(1, claims); ok {
		t.Fatal("expected expired token to be rejected")
	}
}

func TestConnWrapSerializesConcurrentWrites(t *testing.T) {
	serverConn, clientConn := websocketTestPipe(t)
	defer serverConn.Close()
	defer clientConn.Close()

	cw := &connWrap{conn: serverConn}
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for i := 0; i < 64; i++ {
			if _, _, err := clientConn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := writeWSMessage(cw, websocket.TextMessage, []byte("x")); err != nil {
				t.Errorf("writeWSMessage() error = %v", err)
			}
		}()
	}

	wg.Wait()
	_ = clientConn.Close()
	<-readerDone
}

func websocketTestPipe(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	serverConnCh := make(chan *websocket.Conn, 1)
	serverErrCh := make(chan error, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			serverErrCh <- err
			return
		}
		serverConnCh <- conn
	}))
	t.Cleanup(ts.Close)

	clientConn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}

	select {
	case err := <-serverErrCh:
		t.Fatalf("upgrade websocket: %v", err)
	case serverConn := <-serverConnCh:
		return serverConn, clientConn
	}

	return nil, nil
}

func TestTCPPingCommandCarriesQueueAndExecutionBudget(t *testing.T) {
	serverConn, agentConn := websocketTestPipe(t)
	defer serverConn.Close()
	defer agentConn.Close()
	server := NewServer(nil, "test-secret")
	server.nodes[7] = &nodeSession{nodeID: 7, conn: &connWrap{conn: serverConn}}
	done := make(chan error, 1)
	go func() {
		_, err := server.SendCommand(7, "TcpPing", map[string]interface{}{"ip": "127.0.0.1"}, 2*time.Second)
		done <- err
	}()
	agentConn.SetReadDeadline(time.Now().Add(time.Second))
	var command struct {
		Type      string `json:"type"`
		RequestID string `json:"requestId"`
		TimeoutMS int64  `json:"timeoutMs"`
	}
	if err := agentConn.ReadJSON(&command); err != nil {
		t.Fatal(err)
	}
	if command.Type != "TcpPing" || command.TimeoutMS != 2000 || command.RequestID == "" {
		t.Fatalf("unexpected command budget: %+v", command)
	}
	response, err := json.Marshal(commandResponse{Type: "TcpPingResponse", Success: true, RequestID: command.RequestID})
	if err != nil {
		t.Fatal(err)
	}
	server.tryResolvePending(7, string(response))
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRealtimeSessionGetsLatestNodeMetricOnConnect(t *testing.T) {
	secret := "unit-test-secret"
	token, err := auth.GenerateToken(1, "admin_user", 0, secret)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	r, err := repo.Open(t.TempDir() + "/metric.db")
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	defer r.Close()
	now := time.Now().UnixMilli()
	if err := r.DB().Exec(`
		INSERT INTO node(name, secret, server_ip, port, created_time, updated_time, status, tcp_listen_addr, udp_listen_addr, inx)
		VALUES('metric-node', 'metric-node-secret', '10.9.0.1', '40000-40010', ?, ?, 0, '[::]', '[::]', 0)
	`, now, now).Error; err != nil {
		t.Fatalf("insert node: %v", err)
	}

	server := NewServer(r, secret)
	server.SetUserAuthStateLookup(func(userID int64) (*auth.UserAuthState, error) {
		return &auth.UserAuthState{ID: userID, RoleID: 0, Status: 1, PasswordChangedAt: 0}, nil
	})
	ts := httptest.NewServer(server)
	defer ts.Close()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/system-info"

	dialAdmin := func() *websocket.Conn {
		t.Helper()
		conn, _, err := websocket.DefaultDialer.Dial(wsURL+"?type=0&secret="+url.QueryEscape(token), nil)
		if err != nil {
			t.Fatalf("dial admin websocket: %v", err)
		}
		return conn
	}
	waitLastMetric := func(want int) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for {
			server.mu.RLock()
			got := len(server.lastMetric)
			server.mu.RUnlock()
			if got == want {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("cached metrics = %d, want %d", got, want)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	node, _, err := websocket.DefaultDialer.Dial(wsURL+"?type=1&secret=metric-node-secret&version=v1", nil)
	if err != nil {
		t.Fatalf("dial node websocket: %v", err)
	}
	if err := node.WriteMessage(websocket.TextMessage, []byte(`{"type":"metric","data":{"cpu_usage":12.5}}`)); err != nil {
		t.Fatalf("send metric: %v", err)
	}
	waitLastMetric(1)

	admin := dialAdmin()
	_ = admin.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := admin.ReadMessage()
	_ = admin.Close()
	if err != nil {
		t.Fatalf("expected the cached metric on connect: %v", err)
	}
	if msg := string(raw); !strings.Contains(msg, `"type":"metric"`) || !strings.Contains(msg, "cpu_usage") {
		t.Fatalf("unexpected first message: %s", msg)
	}

	// Once the node disconnects its metric is stale and must not be replayed.
	_ = node.Close()
	waitLastMetric(0)
	admin = dialAdmin()
	defer admin.Close()
	// The node-offline status broadcast can reach the new session; only a
	// replayed metric is wrong.
	_ = admin.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	for {
		_, raw, err := admin.ReadMessage()
		if err != nil {
			break
		}
		if strings.Contains(string(raw), `"type":"metric"`) {
			t.Fatalf("expected no metric replay after the node went offline, got %s", raw)
		}
	}
}
