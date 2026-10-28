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
	"go-backend/internal/store/model"
)

func TestForwardCreateBlockedWhenUserQuotaExceeded(t *testing.T) {
	secret := "contract-jwt-secret"
	router, repo := setupContractRouter(t, secret)
	now := time.Now()
	nowMs := now.UnixMilli()
	dayKey := int64(now.Year()*10000 + int(now.Month())*100 + now.Day())
	monthKey := int64(now.Year()*100 + int(now.Month()))

	if err := repo.DB().Exec(`
		INSERT INTO user(id, user, pwd, role_id, exp_time, flow, in_flow, out_flow, flow_reset_time, num, created_time, updated_time, status)
		VALUES(2, 'quota_user', 'pwd', 1, 2727251700000, 99999, 0, 0, 1, 99999, ?, ?, 1)
	`, nowMs, nowMs).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := repo.DB().Exec(`
		INSERT INTO tunnel(id, name, traffic_ratio, type, protocol, flow, created_time, updated_time, status, in_ip, inx)
		VALUES(1, 'quota_tunnel', 1.0, 1, 'tls', 1, ?, ?, 1, NULL, 0)
	`, nowMs, nowMs).Error; err != nil {
		t.Fatalf("insert tunnel: %v", err)
	}
	if err := repo.DB().Exec(`
		INSERT INTO user_tunnel(id, user_id, tunnel_id, speed_id, num, flow, in_flow, out_flow, flow_reset_time, exp_time, status)
		VALUES(10, 2, 1, NULL, 99999, 99999, 0, 0, 1, 2727251700000, 1)
	`).Error; err != nil {
		t.Fatalf("insert user_tunnel: %v", err)
	}
	if err := repo.DB().Exec(`
		INSERT INTO user_quota(user_id, daily_limit_gb, monthly_limit_gb, daily_used_bytes, monthly_used_bytes, day_key, month_key, disabled_by_quota, disabled_at, paused_forward_ids, created_time, updated_time)
		VALUES(2, 10, 0, ?, ?, ?, ?, 1, ?, '', ?, ?)
	`, 11*contractBytesPerGB, 11*contractBytesPerGB, dayKey, monthKey, nowMs, nowMs, nowMs).Error; err != nil {
		t.Fatalf("insert user_quota: %v", err)
	}

	token, err := auth.GenerateToken(2, "quota_user", 1, secret)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/forward/create", bytes.NewBufferString(`{"tunnelId":1,"name":"quota-forward","remoteAddr":"1.1.1.1:53"}`))
	req.Header.Set("Authorization", token)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()

	router.ServeHTTP(res, req)

	var out response.R
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Code == 0 {
		t.Fatalf("expected non-zero code when user quota exceeded")
	}
	if !strings.Contains(out.Msg, "配额") {
		t.Fatalf("expected quota error, got %q", out.Msg)
	}
}

func TestForwardResumeBlockedWhenUserQuotaExceeded(t *testing.T) {
	secret := "contract-jwt-secret"
	router, repo := setupContractRouter(t, secret)
	now := time.Now()
	nowMs := now.UnixMilli()
	dayKey := int64(now.Year()*10000 + int(now.Month())*100 + now.Day())
	monthKey := int64(now.Year()*100 + int(now.Month()))

	if err := repo.DB().Exec(`
		INSERT INTO user(id, user, pwd, role_id, exp_time, flow, in_flow, out_flow, flow_reset_time, num, created_time, updated_time, status)
		VALUES(2, 'quota_resume_user', 'pwd', 1, 2727251700000, 99999, 0, 0, 1, 99999, ?, ?, 1)
	`, nowMs, nowMs).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := repo.DB().Exec(`
		INSERT INTO tunnel(id, name, traffic_ratio, type, protocol, flow, created_time, updated_time, status, in_ip, inx)
		VALUES(1, 'quota_resume_tunnel', 1.0, 1, 'tls', 1, ?, ?, 1, NULL, 0)
	`, nowMs, nowMs).Error; err != nil {
		t.Fatalf("insert tunnel: %v", err)
	}
	if err := repo.DB().Exec(`
		INSERT INTO user_tunnel(id, user_id, tunnel_id, speed_id, num, flow, in_flow, out_flow, flow_reset_time, exp_time, status)
		VALUES(10, 2, 1, NULL, 99999, 99999, 0, 0, 1, 2727251700000, 1)
	`).Error; err != nil {
		t.Fatalf("insert user_tunnel: %v", err)
	}
	if err := repo.DB().Exec(`
		INSERT INTO forward(id, user_id, user_name, name, tunnel_id, remote_addr, strategy, in_flow, out_flow, created_time, updated_time, status, inx)
		VALUES(1, 2, 'quota_resume_user', 'quota_resume_forward', 1, '1.1.1.1:53', 'fifo', 0, 0, ?, ?, 0, 0)
	`, nowMs, nowMs).Error; err != nil {
		t.Fatalf("insert forward: %v", err)
	}
	if err := repo.DB().Exec(`
		INSERT INTO user_quota(user_id, daily_limit_gb, monthly_limit_gb, daily_used_bytes, monthly_used_bytes, day_key, month_key, disabled_by_quota, disabled_at, paused_forward_ids, created_time, updated_time)
		VALUES(2, 10, 0, ?, ?, ?, ?, 1, ?, '1', ?, ?)
	`, 11*contractBytesPerGB, 11*contractBytesPerGB, dayKey, monthKey, nowMs, nowMs, nowMs).Error; err != nil {
		t.Fatalf("insert user_quota: %v", err)
	}

	token, err := auth.GenerateToken(2, "quota_resume_user", 1, secret)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/forward/resume", bytes.NewBufferString(`{"id":1}`))
	req.Header.Set("Authorization", token)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()

	router.ServeHTTP(res, req)

	var out response.R
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Code == 0 {
		t.Fatalf("expected non-zero code when user quota exceeded")
	}
	if !strings.Contains(out.Msg, "配额") {
		t.Fatalf("expected quota error, got %q", out.Msg)
	}
	status := mustQueryInt(t, repo, `SELECT status FROM forward WHERE id = 1`)
	if status != 0 {
		t.Fatalf("expected forward to remain paused, got %d", status)
	}
}

func TestUserQuotaResetClearsDisableFlag(t *testing.T) {
	secret := "contract-jwt-secret"
	router, repo := setupContractRouter(t, secret)
	now := time.Now()
	nowMs := now.UnixMilli()
	dayKey := int64(now.Year()*10000 + int(now.Month())*100 + now.Day())
	monthKey := int64(now.Year()*100 + int(now.Month()))

	if err := repo.DB().Exec(`
		INSERT INTO user(id, user, pwd, role_id, exp_time, flow, in_flow, out_flow, flow_reset_time, num, created_time, updated_time, status)
		VALUES(2, 'quota_reset_user', 'pwd', 1, 2727251700000, 99999, 0, 0, 1, 99999, ?, ?, 1)
	`, nowMs, nowMs).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := repo.DB().Exec(`
		INSERT INTO user_quota(user_id, daily_limit_gb, monthly_limit_gb, daily_used_bytes, monthly_used_bytes, day_key, month_key, disabled_by_quota, disabled_at, paused_forward_ids, created_time, updated_time)
		VALUES(2, 10, 0, ?, ?, ?, ?, 1, ?, '', ?, ?)
	`, 11*contractBytesPerGB, 11*contractBytesPerGB, dayKey, monthKey, nowMs, nowMs, nowMs).Error; err != nil {
		t.Fatalf("insert user_quota: %v", err)
	}

	token, err := auth.GenerateToken(1, "admin", 0, secret)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/quota/reset", bytes.NewBufferString(`{"userId":2,"scope":"all"}`))
	req.Header.Set("Authorization", token)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()

	router.ServeHTTP(res, req)

	var out response.R
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Code != 0 {
		t.Fatalf("expected reset success, got code=%d msg=%q", out.Code, out.Msg)
	}
	quotaDisabled := mustQueryInt(t, repo, `SELECT disabled_by_quota FROM user_quota WHERE user_id = 2`)
	if quotaDisabled != 0 {
		t.Fatalf("expected quota disable flag cleared, got %d", quotaDisabled)
	}
	for _, column := range []string{"daily_used_bytes", "monthly_used_bytes"} {
		if used := mustQueryInt64(t, repo, `SELECT `+column+` FROM user_quota WHERE user_id = 2`); used != 0 {
			t.Errorf("expected %s cleared, got %d", column, used)
		}
	}
}

func TestUserQuotaResetRequiresAdmin(t *testing.T) {
	for _, tc := range []struct {
		name     string
		callerID int64
		targetID int64
		wantCode int
	}{
		{name: "unauthenticated", targetID: 2, wantCode: 401},
		{name: "own quota", callerID: 2, targetID: 2, wantCode: 403},
		{name: "another user's quota", callerID: 2, targetID: 3, wantCode: 403},
	} {
		for _, scope := range []string{"", "daily", "monthly", "all"} {
			t.Run(tc.name+"/scope="+scope, func(t *testing.T) {
				const secret = "contract-jwt-secret"
				router, r := setupContractRouter(t, secret)
				seedContractUser(t, r, 2, "quota_caller", 1, 1)
				seedContractUser(t, r, 3, "quota_other", 1, 1)
				now := time.Now()
				before := model.UserQuota{
					UserID: tc.targetID, DailyLimitGB: 10, MonthlyLimitGB: 10,
					DailyUsedBytes: 11 * contractBytesPerGB, MonthlyUsedBytes: 11 * contractBytesPerGB,
					DayKey:          int64(now.Year()*10000 + int(now.Month())*100 + now.Day()),
					MonthKey:        int64(now.Year()*100 + int(now.Month())),
					DisabledByQuota: 1, DisabledAt: now.UnixMilli(), PausedForwardIDs: "42",
					CreatedTime: now.UnixMilli(), UpdatedTime: now.UnixMilli(),
				}
				if err := r.DB().Create(&before).Error; err != nil {
					t.Fatalf("insert quota: %v", err)
				}
				var token string
				if tc.callerID != 0 {
					var err error
					token, err = auth.GenerateToken(tc.callerID, "quota_caller", 1, secret)
					if err != nil {
						t.Fatalf("generate token: %v", err)
					}
				}
				body, err := json.Marshal(map[string]interface{}{"userId": tc.targetID, "scope": scope})
				if err != nil {
					t.Fatalf("marshal request: %v", err)
				}
				out := kernelContractPost(t, router, token, "/api/v1/user/quota/reset", string(body))
				if out.Code != tc.wantCode {
					t.Errorf("expected code %d, got %d (%s)", tc.wantCode, out.Code, out.Msg)
				}
				var after model.UserQuota
				if err := r.DB().Where("user_id = ?", tc.targetID).First(&after).Error; err != nil {
					t.Fatalf("read quota: %v", err)
				}
				if after != before {
					t.Errorf("unauthorized reset changed quota: before=%+v after=%+v", before, after)
				}
			})
		}
	}
}
