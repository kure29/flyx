package contract_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-backend/internal/auth"
	"go-backend/internal/http/response"
)

func TestStorageSummaryRequiresAdminAndReturnsSize(t *testing.T) {
	secret := "storage-contract-secret"
	router, r := setupContractRouter(t, secret)
	seedContractUser(t, r, 2, "normal_user", 1, 1)

	adminToken, err := auth.GenerateToken(1, "admin_user", 0, secret)
	if err != nil {
		t.Fatalf("generate admin token: %v", err)
	}
	userToken, err := auth.GenerateToken(2, "normal_user", 1, secret)
	if err != nil {
		t.Fatalf("generate user token: %v", err)
	}

	userReq := httptest.NewRequest(http.MethodGet, "/api/v1/system/storage", nil)
	userReq.Header.Set("Authorization", userToken)
	userRes := httptest.NewRecorder()
	router.ServeHTTP(userRes, userReq)

	var denied response.R
	if err := json.NewDecoder(userRes.Body).Decode(&denied); err != nil {
		t.Fatalf("decode denied response: %v", err)
	}
	if denied.Code != 403 {
		t.Fatalf("expected 403 for non-admin, got %d", denied.Code)
	}

	adminReq := httptest.NewRequest(http.MethodGet, "/api/v1/system/storage", nil)
	adminReq.Header.Set("Authorization", adminToken)
	adminRes := httptest.NewRecorder()
	router.ServeHTTP(adminRes, adminReq)

	var out response.R
	if err := json.NewDecoder(adminRes.Body).Decode(&out); err != nil {
		t.Fatalf("decode admin response: %v", err)
	}
	if out.Code != 0 {
		t.Fatalf("expected code 0, got %d: %s", out.Code, out.Msg)
	}
	data, ok := out.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected object data, got %T", out.Data)
	}
	if data["dbType"] == "" {
		t.Fatalf("expected dbType")
	}
	if _, ok := data["databaseSizeBytes"].(float64); !ok {
		t.Fatalf("expected numeric databaseSizeBytes, got %T", data["databaseSizeBytes"])
	}
	if data["databaseSizeText"] == "" {
		t.Fatalf("expected databaseSizeText")
	}
}
