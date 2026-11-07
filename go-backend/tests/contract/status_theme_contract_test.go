package contract_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go-backend/internal/auth"
)

// The status page theme is public; managing themes is for the admin only.
func TestStatusThemeRoutesAuth(t *testing.T) {
	const secret = "status-theme-contract-secret"
	router, _ := setupContractRouter(t, secret)

	for _, path := range []string{"/status", "/status/", "/status/node/1"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		// 200 with the built-in theme embedded, 503 in a build without it;
		// never the API's login refusal.
		if rec.Code != http.StatusOK && rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: status %d", path, rec.Code)
		}
		if !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("%s: content type %q", path, rec.Header().Get("Content-Type"))
		}
	}

	for _, path := range []string{
		"/api/v1/status-theme/list",
		"/api/v1/status-theme/select",
		"/api/v1/status-theme/delete",
		"/api/v1/status-theme/upload",
		"/api/v1/status-theme/install",
		"/api/v1/status-theme/update",
		"/api/v1/status-theme/config/get",
		"/api/v1/status-theme/config/save",
	} {
		if out := passkeyPost(t, router, path, "", map[string]string{}); out.Code != 401 {
			t.Fatalf("%s without a token: code %d", path, out.Code)
		}
	}

	token, err := auth.GenerateToken(1, "admin_user", 0, secret)
	if err != nil {
		t.Fatal(err)
	}
	out := passkeyPost(t, router, "/api/v1/status-theme/list", token, map[string]string{})
	if out.Code != 0 {
		t.Fatalf("list as admin: code %d %s", out.Code, out.Msg)
	}
	// The theme's settings are public, like the page (404 while it is off,
	// never a login refusal).
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/public/status/theme-config", nil))
	if !strings.Contains(rec.Body.String(), `"code":404`) {
		t.Fatalf("public theme config: %s", rec.Body.String())
	}

	themes := out.Data.(map[string]interface{})["themes"].([]interface{})
	if len(themes) != 1 || themes[0].(map[string]interface{})["short"] != "default" {
		t.Fatalf("themes = %v", themes)
	}
}
