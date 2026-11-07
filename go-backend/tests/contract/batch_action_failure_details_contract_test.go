package contract_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-backend/internal/auth"
	"go-backend/internal/http/response"
	"go-backend/internal/store/repo"
)

func TestForwardBatchDeleteReturnsFailureReasonsContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, _ := setupContractRouter(t, secret)
	adminToken := mustAdminToken(t, secret)

	out := postBatchRequest(t, router, adminToken, "/api/v1/forward/batch-delete", `{"ids":[999]}`)
	result := mustBatchResult(t, out)
	assertBatchFailureReasonContains(t, result, "转发不存在")
}

func TestForwardBatchPauseReturnsFailureReasonsContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, _ := setupContractRouter(t, secret)
	adminToken := mustAdminToken(t, secret)

	out := postBatchRequest(t, router, adminToken, "/api/v1/forward/batch-pause", `{"ids":[999]}`)
	result := mustBatchResult(t, out)
	assertBatchFailureReasonContains(t, result, "转发不存在")
}

func TestForwardBatchResumeReturnsFailureReasonsContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, _ := setupContractRouter(t, secret)
	adminToken := mustAdminToken(t, secret)

	out := postBatchRequest(t, router, adminToken, "/api/v1/forward/batch-resume", `{"ids":[999]}`)
	result := mustBatchResult(t, out)
	assertBatchFailureReasonContains(t, result, "转发不存在")
}

func TestForwardBatchChangeTunnelReturnsFailureReasonsContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, repo := setupContractRouter(t, secret)
	adminToken := mustAdminToken(t, secret)
	now := time.Now().UnixMilli()

	forwardID := seedForwardForBatchAction(t, repo, batchForwardSeedOptions{
		Now:         now,
		TunnelName:  "change-detail-tunnel",
		ForwardName: "change-detail-forward",
	})
	tunnelID := mustQueryInt64(t, repo, `SELECT tunnel_id FROM forward WHERE id = ?`, forwardID)

	payload := `{"forwardIds":[` + jsonNumber(forwardID) + `],"targetTunnelId":` + jsonNumber(tunnelID) + `}`
	out := postBatchRequest(t, router, adminToken, "/api/v1/forward/batch-change-tunnel", payload)
	result := mustBatchResult(t, out)
	assertBatchFailureNameAndReason(t, result, "change-detail-forward", "规则已在目标隧道中")
}

func TestTunnelBatchDeleteReturnsFailureReasonsContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, _ := setupContractRouter(t, secret)
	adminToken := mustAdminToken(t, secret)

	out := postBatchRequest(t, router, adminToken, "/api/v1/tunnel/batch-delete", `{"ids":[999]}`)
	result := mustBatchResult(t, out)
	assertBatchFailureReasonContains(t, result, "隧道不存在")
}

type batchForwardSeedOptions struct {
	Now              int64
	TunnelName       string
	ForwardName      string
	CreateUserTunnel bool
	UserTunnelStatus int
}

func mustAdminToken(t *testing.T, secret string) string {
	t.Helper()
	token, err := auth.GenerateToken(1, "admin_user", 0, secret)
	if err != nil {
		t.Fatalf("generate admin token: %v", err)
	}
	return token
}

func postBatchRequest(t *testing.T, router http.Handler, token, path, payload string) response.R {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(payload))
	req.Header.Set("Authorization", token)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	var out response.R
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Code != 0 {
		t.Fatalf("expected API success envelope, got code=%d msg=%q", out.Code, out.Msg)
	}
	return out
}

func mustBatchResult(t *testing.T, out response.R) map[string]interface{} {
	t.Helper()
	result, ok := out.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", out.Data)
	}
	if int(result["failCount"].(float64)) != 1 {
		t.Fatalf("expected failCount=1, got %v", result["failCount"])
	}
	return result
}

func assertBatchFailureReasonContains(t *testing.T, result map[string]interface{}, snippet string) {
	t.Helper()
	failures, ok := result["failures"].([]interface{})
	if !ok || len(failures) != 1 {
		t.Fatalf("expected exactly one failure detail, got %#v", result["failures"])
	}
	first, ok := failures[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected failure detail object, got %T", failures[0])
	}
	reason, _ := first["reason"].(string)
	if !strings.Contains(reason, snippet) {
		t.Fatalf("expected failure reason to contain %q, got %q", snippet, reason)
	}
}

func assertBatchFailureNameAndReason(t *testing.T, result map[string]interface{}, expectedName, reasonSnippet string) {
	t.Helper()
	failures, ok := result["failures"].([]interface{})
	if !ok || len(failures) != 1 {
		t.Fatalf("expected exactly one failure detail, got %#v", result["failures"])
	}
	first, ok := failures[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected failure detail object, got %T", failures[0])
	}
	gotName, _ := first["name"].(string)
	if strings.TrimSpace(gotName) != expectedName {
		t.Fatalf("expected failure name %q, got %q", expectedName, gotName)
	}
	reason, _ := first["reason"].(string)
	if !strings.Contains(reason, reasonSnippet) {
		t.Fatalf("expected failure reason to contain %q, got %q", reasonSnippet, reason)
	}
}

func seedForwardForBatchAction(t *testing.T, repo *repo.Repository, opts batchForwardSeedOptions) int64 {
	t.Helper()
	if err := repo.DB().Exec(`
		INSERT INTO user(id, user, pwd, role_id, exp_time, flow, in_flow, out_flow, flow_reset_time, num, created_time, updated_time, status)
		VALUES(2, 'batch_action_user', '3c85cdebade1c51cf64ca9f3c09d182d', 1, 2727251700000, 99999, 0, 0, 1, 99999, ?, ?, 1)
	`, opts.Now, opts.Now).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}

	if err := repo.DB().Exec(`
		INSERT INTO tunnel(name, traffic_ratio, type, protocol, flow, created_time, updated_time, status, in_ip, inx)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, opts.TunnelName, 1.0, 1, "tls", 99999, opts.Now, opts.Now, 1, nil, 0).Error; err != nil {
		t.Fatalf("insert tunnel: %v", err)
	}
	tunnelID := mustLastInsertID(t, repo, opts.TunnelName)

	if opts.CreateUserTunnel {
		if err := repo.DB().Exec(`
			INSERT INTO user_tunnel(id, user_id, tunnel_id, speed_id, num, flow, in_flow, out_flow, flow_reset_time, exp_time, status)
			VALUES(20, 2, ?, NULL, 999, 99999, 0, 0, 1, 2727251700000, ?)
		`, tunnelID, opts.UserTunnelStatus).Error; err != nil {
			t.Fatalf("insert user_tunnel: %v", err)
		}
	}

	if err := repo.DB().Exec(`
		INSERT INTO forward(user_id, user_name, name, tunnel_id, remote_addr, strategy, in_flow, out_flow, created_time, updated_time, status, inx)
		VALUES(2, 'batch_action_user', ?, ?, '1.1.1.1:443', 'fifo', 0, 0, ?, ?, 1, 0)
	`, opts.ForwardName, tunnelID, opts.Now, opts.Now).Error; err != nil {
		t.Fatalf("insert forward: %v", err)
	}

	return mustLastInsertID(t, repo, opts.ForwardName)
}
