package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"go-backend/internal/auth"
	"go-backend/internal/http/middleware"
	"go-backend/internal/http/response"
	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
)

func setupUsersTestRouter(t *testing.T) (http.Handler, *Handler, *repo.Repository, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "users_test.db")
	r, err := repo.Open(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	secret := "users-test-secret"
	h := New(r, secret)
	mux := http.NewServeMux()
	h.Register(mux)
	wrapped := middleware.JWT(middleware.AuthOptions{
		JWTSecret:        secret,
		GetUserAuthState: h.GetUserAuthState,
	})(mux)
	return wrapped, h, r, secret
}

func postJSON(t *testing.T, router http.Handler, token, path string, body interface{}) response.R {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var out response.R
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode %s response (status %d): %v", path, rec.Code, err)
	}
	return out
}

func TestMultiUserCRUDAndAccessControl(t *testing.T) {
	router, _, r, secret := setupUsersTestRouter(t)
	adminToken, err := auth.GenerateToken(1, "admin_user", 0, secret)
	if err != nil {
		t.Fatalf("admin token: %v", err)
	}

	// Seed a tunnel and a subscription.
	now := time.Now().UnixMilli()
	if err := r.DB().Exec(`INSERT INTO tunnel(name, type, protocol, flow, status, created_time, updated_time, inx) VALUES('T1', 1, 'tls', 1, 1, ?, ?, 1)`, now, now).Error; err != nil {
		t.Fatalf("seed tunnel: %v", err)
	}
	tunnelID := mustLastInsertID(t, r, "tunnel")

	sub := &model.Subscription{
		Name:         "SubA",
		Kind:         model.SubscriptionShare,
		Token:        "tok_sub_a_123456",
		UUID:         "11111111-1111-1111-1111-111111111111",
		Password:     "pass_a",
		Enabled:      1,
		CreatedTime:  now,
		UpdatedTime:  now,
		TrafficSince: now,
	}
	if err := r.SaveSubscription(sub); err != nil {
		t.Fatalf("save sub: %v", err)
	}

	// Admin creates user alice with tunnel and subscription.
	createRes := postJSON(t, router, adminToken, "/api/v1/user/create", map[string]interface{}{
		"user":            "alice",
		"pwd":             "secret123",
		"status":          1,
		"expTime":         0,
		"flow":            10,
		"flowMiB":         0,
		"flowResetTime":   1,
		"num":             2,
		"tunnelIds":       []int64{tunnelID},
		"subscriptionIds": []int64{sub.ID},
	})
	if createRes.Code != 0 {
		t.Fatalf("create user failed: %+v", createRes)
	}

	// Alice logs in with password.
	loginRes := postJSON(t, router, "", "/api/v1/user/login", map[string]interface{}{
		"username": "alice",
		"password": "secret123",
	})
	if loginRes.Code != 0 {
		t.Fatalf("alice login failed: %+v", loginRes)
	}
	loginData, _ := loginRes.Data.(map[string]interface{})
	aliceToken, _ := loginData["token"].(string)
	if aliceToken == "" {
		t.Fatal("expected alice token")
	}

	// Alice cannot call admin endpoints.
	if out := postJSON(t, router, aliceToken, "/api/v1/user/list", nil); out.Code != 403 {
		t.Fatalf("expected 403 for alice on /user/list, got %+v", out)
	}
	if out := postJSON(t, router, aliceToken, "/api/v1/node/list", nil); out.Code != 403 {
		t.Fatalf("expected 403 for alice on /node/list, got %+v", out)
	}

	// Alice can view her package, tunnels, and subscriptions.
	pkgRes := postJSON(t, router, aliceToken, "/api/v1/user/package", nil)
	if pkgRes.Code != 0 {
		t.Fatalf("user/package failed: %+v", pkgRes)
	}
	tunnelsRes := postJSON(t, router, aliceToken, "/api/v1/tunnel/user/tunnel", nil)
	if tunnelsRes.Code != 0 {
		t.Fatalf("tunnel/user/tunnel failed: %+v", tunnelsRes)
	}
	tunnelsList, _ := tunnelsRes.Data.([]interface{})
	if len(tunnelsList) != 1 {
		t.Fatalf("expected 1 authorized tunnel, got %d", len(tunnelsList))
	}

	subsRes := postJSON(t, router, aliceToken, "/api/v1/user/subscriptions", nil)
	if subsRes.Code != 0 {
		t.Fatalf("user/subscriptions failed: %+v", subsRes)
	}
	subsList, _ := subsRes.Data.([]interface{})
	if len(subsList) != 1 {
		t.Fatalf("expected 1 assigned subscription, got %d", len(subsList))
	}

	// Admin lists users and deletes alice.
	listRes := postJSON(t, router, adminToken, "/api/v1/user/list", nil)
	if listRes.Code != 0 {
		t.Fatalf("user/list failed: %+v", listRes)
	}
	listItems, _ := listRes.Data.([]interface{})
	if len(listItems) != 1 {
		t.Fatalf("expected 1 non-admin user, got %d", len(listItems))
	}
	aliceID := int64(listItems[0].(map[string]interface{})["id"].(float64))

	if delRes := postJSON(t, router, adminToken, "/api/v1/user/delete", map[string]interface{}{"id": aliceID}); delRes.Code != 0 {
		t.Fatalf("user/delete failed: %+v", delRes)
	}
	// Subscription is unassigned, not deleted.
	reloadedSub, err := r.GetSubscription(sub.ID)
	if err != nil || reloadedSub == nil || reloadedSub.UserID != 0 {
		t.Fatalf("expected subscription unassigned after user delete, got %+v (%v)", reloadedSub, err)
	}
}

func TestUserQuotaExpiryAndAutoResumeOnReset(t *testing.T) {
	router, h, r, secret := setupUsersTestRouter(t)
	adminToken, _ := auth.GenerateToken(1, "admin_user", 0, secret)

	now := time.Now().UnixMilli()
	if err := r.DB().Exec(`INSERT INTO tunnel(name, type, protocol, flow, status, created_time, updated_time, inx) VALUES('T1', 1, 'tls', 1, 1, ?, ?, 1)`, now, now).Error; err != nil {
		t.Fatalf("seed tunnel: %v", err)
	}
	tunnelID := mustLastInsertID(t, r, "tunnel")

	// Create user bob with 10 MiB limit and max 1 rule.
	if out := postJSON(t, router, adminToken, "/api/v1/user/create", map[string]interface{}{
		"user":          "bob",
		"pwd":           "secret123",
		"status":        1,
		"expTime":       0,
		"flow":          0,
		"flowMiB":       10,
		"flowResetTime": 1,
		"num":           1,
		"tunnelIds":     []int64{tunnelID},
	}); out.Code != 0 {
		t.Fatalf("create bob: %+v", out)
	}
	bob, err := r.GetUserByUsername("bob")
	if err != nil || bob == nil {
		t.Fatalf("get bob: %v", err)
	}

	// Seed an active forward for bob.
	if err := r.DB().Exec(`INSERT INTO forward(user_id, user_name, name, tunnel_id, remote_addr, strategy, status, created_time, updated_time, inx) VALUES(?, 'bob', 'F1', ?, '1.1.1.1:80', 'fifo', 1, ?, ?, 1)`, bob.ID, tunnelID, now, now).Error; err != nil {
		t.Fatalf("seed forward: %v", err)
	}
	fwdID := mustLastInsertID(t, r, "forward")

	// Rule count limit blocks creating a 2nd forward.
	if err := h.checkUserForwardQuotaForCreate(bob.ID, 1, tunnelID); err == nil {
		t.Fatal("expected rule count limit error")
	}

	// Push usage over 10 MiB -> enforceUserForwardFlowLimit pauses F1.
	if err := r.DB().Model(&model.User{}).Where("id = ?", bob.ID).Updates(map[string]interface{}{
		"in_flow":  6 * 1024 * 1024,
		"out_flow": 5 * 1024 * 1024,
	}).Error; err != nil {
		t.Fatalf("update bob flow: %v", err)
	}
	h.enforceUserForwardFlowLimit(bob.ID)
	if status := mustQueryInt(t, r, "SELECT status FROM forward WHERE id = ?", fwdID); status != 0 {
		t.Fatalf("expected forward paused after over-quota, got status=%d", status)
	}
	if err := h.checkUserForwardQuotaForResume(bob.ID, 1, tunnelID); err == nil {
		t.Fatal("expected resume blocked while over quota")
	}

	// Admin resets bob's flow -> paused forward auto-resumes!
	if out := postJSON(t, router, adminToken, "/api/v1/user/reset", map[string]interface{}{"id": bob.ID}); out.Code != 0 {
		t.Fatalf("reset flow failed: %+v", out)
	}
	if status := mustQueryInt(t, r, "SELECT status FROM forward WHERE id = ?", fwdID); status != 1 {
		t.Fatalf("expected forward auto-resumed after flow reset, got status=%d", status)
	}

	// Expire bob -> runResetAndExpiryJob pauses F1.
	if err := r.DB().Model(&model.User{}).Where("id = ?", bob.ID).Update("exp_time", now-1000).Error; err != nil {
		t.Fatalf("expire bob: %v", err)
	}
	h.runResetAndExpiryJob(time.Now())
	if status := mustQueryInt(t, r, "SELECT status FROM forward WHERE id = ?", fwdID); status != 0 {
		t.Fatalf("expected forward paused after expiry, got status=%d", status)
	}
}
