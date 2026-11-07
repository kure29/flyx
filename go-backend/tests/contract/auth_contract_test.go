package contract_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-backend/internal/auth"
	"go-backend/internal/http/middleware"
	"go-backend/internal/http/response"
	"go-backend/internal/security"
)

func TestJWTMiddlewareContracts(t *testing.T) {
	secret := "unit-test-secret"

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response.WriteJSON(w, response.OK("pass"))
	})

	wrapped := middleware.JWT(middleware.AuthOptions{JWTSecret: secret, GetUserAuthState: func(userID int64) (*auth.UserAuthState, error) {
		return &auth.UserAuthState{ID: userID, RoleID: 0, Status: 1, PasswordChangedAt: 0}, nil
	}})(next)

	t.Run("login path is excluded", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/user/login", nil)
		res := httptest.NewRecorder()
		wrapped.ServeHTTP(res, req)
		assertCode(t, res, 0)
	})

	t.Run("missing token returns 401 contract message", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tunnel/list", nil)
		res := httptest.NewRecorder()
		wrapped.ServeHTTP(res, req)
		assertCodeMsg(t, res, 401, "未登录或token已过期")
	})

	t.Run("invalid token returns 401 contract message", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tunnel/list", nil)
		req.Header.Set("Authorization", "invalid.token.value")
		res := httptest.NewRecorder()
		wrapped.ServeHTTP(res, req)
		assertCodeMsg(t, res, 401, "无效的token或token已过期")
	})

	t.Run("valid token reaches next", func(t *testing.T) {
		token, err := auth.GenerateToken(1, "admin_user", 0, secret)
		if err != nil {
			t.Fatalf("generate token: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tunnel/list", nil)
		req.Header.Set("Authorization", token)
		res := httptest.NewRecorder()
		wrapped.ServeHTTP(res, req)
		assertCode(t, res, 0)
	})

	t.Run("non-admin token is refused everywhere", func(t *testing.T) {
		token, err := auth.GenerateToken(2, "normal_user", 1, secret)
		if err != nil {
			t.Fatalf("generate token: %v", err)
		}
		wrapped := middleware.JWT(middleware.AuthOptions{JWTSecret: secret, GetUserAuthState: func(userID int64) (*auth.UserAuthState, error) {
			return &auth.UserAuthState{ID: userID, RoleID: 1, Status: 1, PasswordChangedAt: 0}, nil
		}})(next)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/config/update", nil)
		req.Header.Set("Authorization", token)
		res := httptest.NewRecorder()
		wrapped.ServeHTTP(res, req)
		assertCodeMsg(t, res, 401, "只有管理员可以登录")
	})
}

func TestLoginTokenValidatesThroughRouter(t *testing.T) {
	router, r := setupContractRouter(t, "contract-jwt-secret")
	seedLegacyUser(t, r, 9110, "router-login-user", "router-login-pass")

	body := bytes.NewBufferString(`{"username":"router-login-user","password":"router-login-pass","captchaId":""}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/login", body)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	var out response.R
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if out.Code != 0 {
		t.Fatalf("expected login code 0, got %d (%s)", out.Code, out.Msg)
	}
	data, ok := out.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected login data map, got %T", out.Data)
	}
	token, _ := data["token"].(string)
	if token == "" {
		t.Fatal("expected login token")
	}

	checkReq := httptest.NewRequest(http.MethodPost, "/api/v1/tunnel/user/tunnel", nil)
	checkReq.Header.Set("Authorization", token)
	checkResp := httptest.NewRecorder()
	router.ServeHTTP(checkResp, checkReq)
	assertCode(t, checkResp, 0)
}

func TestLegacyPasswordMigratesOnLogin(t *testing.T) {
	router, r := setupContractRouter(t, "contract-jwt-secret")
	legacyChangedAt := seedLegacyUser(t, r, 9101, "legacy-login-user", "legacy-login-pass")

	body := bytes.NewBufferString(`{"username":"legacy-login-user","password":"legacy-login-pass","captchaId":""}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/login", body)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)
	assertCode(t, resp, 0)
	assertUserPasswordIsBcrypt(t, r, "legacy-login-user", "legacy-login-pass")
	if changedAt := mustQueryPasswordChangedAtByUsername(t, r, "legacy-login-user"); changedAt <= legacyChangedAt {
		t.Fatalf("expected password_changed_at to advance on login migration, got %d <= %d", changedAt, legacyChangedAt)
	}
}

func TestDisabledLegacyPasswordIsRejectedWithoutMigration(t *testing.T) {
	router, r := setupContractRouter(t, "contract-jwt-secret")
	legacyChangedAt := seedLegacyUserWithStatus(t, r, 9105, "disabled-legacy-user", "disabled-legacy-pass", 0)

	body := bytes.NewBufferString(`{"username":"disabled-legacy-user","password":"disabled-legacy-pass","captchaId":""}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/user/login", body)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)
	assertCodeMsg(t, resp, -1, "账号被停用")

	user, err := r.GetUserByUsername("disabled-legacy-user")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if user == nil {
		t.Fatal("expected disabled user to exist")
	}
	if ok, migrated := security.VerifyPassword(user.Pwd, "disabled-legacy-pass"); !ok || !migrated {
		t.Fatalf("expected disabled user to remain legacy MD5, got (%v,%v) with hash %q", ok, migrated, user.Pwd)
	}
	if changedAt := mustQueryPasswordChangedAtByUsername(t, r, "disabled-legacy-user"); changedAt != legacyChangedAt {
		t.Fatalf("expected password_changed_at to remain unchanged, got %d want %d", changedAt, legacyChangedAt)
	}
}

func assertCode(t *testing.T, rec *httptest.ResponseRecorder, expected int) {
	t.Helper()
	var out response.R
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Code != expected {
		t.Fatalf("expected code %d, got %d", expected, out.Code)
	}
}

func assertCodeMsg(t *testing.T, rec *httptest.ResponseRecorder, expectedCode int, expectedMsg string) {
	t.Helper()
	var out response.R
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Code != expectedCode || out.Msg != expectedMsg {
		t.Fatalf("expected (%d,%q), got (%d,%q)", expectedCode, expectedMsg, out.Code, out.Msg)
	}
}
