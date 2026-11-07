package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"go-backend/internal/http/response"
	"go-backend/internal/security"
	"go-backend/internal/store/repo"
)

// fakeGithub answers the OAuth token exchange and the user lookup: code
// "octo" belongs to GitHub user 42.
func fakeGithub(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			_ = r.ParseForm()
			if r.Form.Get("client_secret") != "shh" || r.Form.Get("code") != "octo" || r.Form.Get("redirect_uri") == "" {
				_, _ = w.Write([]byte(`{"error":"bad_verification_code"}`))
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"tok42"}`))
		case "/user":
			if r.Header.Get("Authorization") != "Bearer tok42" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"id":42,"login":"octocat"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	oldBase, oldAPI := githubOAuthBase, githubUserAPI
	githubOAuthBase, githubUserAPI = srv.URL, srv.URL+"/user"
	t.Cleanup(func() { githubOAuthBase, githubUserAPI = oldBase, oldAPI })
}

func githubPost(t *testing.T, router http.Handler, token, path, body string) response.R {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var out response.R
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("%s: decode: %v", path, err)
	}
	return out
}

// stateOf pulls the state out of the authorize URL a begin call returned.
func stateOf(t *testing.T, out response.R) string {
	t.Helper()
	data, _ := out.Data.(map[string]interface{})
	raw, _ := data["url"].(string)
	u, err := url.Parse(raw)
	if out.Code != 0 || err != nil || u.Query().Get("state") == "" || u.Query().Get("redirect_uri") != "https://panel.example.com/" {
		t.Fatalf("begin: %+v", out)
	}
	return u.Query().Get("state")
}

func TestGithubLoginBindAndLogin(t *testing.T) {
	fakeGithub(t)
	router, r := setupConfigAccessTestRouter(t)
	admin := mustGenerateConfigAccessToken(t, 1, "admin_user", 0)
	begin := `{"redirectUri":"https://panel.example.com/","password":"admin_user"}`

	status := githubPost(t, router, "", "/api/v1/user/github/status", `{}`)
	if status.Data.(map[string]interface{})["enabled"] != false {
		t.Fatalf("unconfigured login must be off: %+v", status)
	}
	seedConfigValue(t, r, "github_login_enabled", "true")
	seedConfigValue(t, r, "github_oauth_client_id", "cid")
	seedConfigValue(t, r, "github_oauth_secret", "shh")
	if !repo.IsSensitiveConfigKey("github_oauth_secret") {
		t.Fatal("client secret must be a sensitive config")
	}
	status = githubPost(t, router, "", "/api/v1/user/github/status", `{}`)
	if status.Data.(map[string]interface{})["enabled"] != true {
		t.Fatalf("status: %+v", status)
	}

	t.Run("unbound account is refused", func(t *testing.T) {
		state := stateOf(t, githubPost(t, router, "", "/api/v1/user/github/login/begin", begin))
		out := githubPost(t, router, "", "/api/v1/user/github/login/finish", `{"code":"octo","state":"`+state+`"}`)
		if out.Code == 0 || !strings.Contains(out.Msg, "未绑定") {
			t.Fatalf("unbound login: %+v", out)
		}
	})

	t.Run("binding needs the password", func(t *testing.T) {
		out := githubPost(t, router, admin, "/api/v1/user/github/bind/begin", `{"redirectUri":"https://panel.example.com/","password":"nope"}`)
		if out.Code == 0 {
			t.Fatalf("wrong password accepted: %+v", out)
		}
		if out := githubPost(t, router, admin, "/api/v1/user/github/bind/begin", `{"redirectUri":"javascript:alert(1)","password":"admin_user"}`); out.Code == 0 {
			t.Fatalf("bad redirect accepted: %+v", out)
		}
	})

	state := stateOf(t, githubPost(t, router, admin, "/api/v1/user/github/bind/begin", begin))
	if out := githubPost(t, router, "", "/api/v1/user/github/login/finish", `{"code":"octo","state":"`+state+`"}`); out.Code == 0 {
		t.Fatal("a bind state must not log in")
	}
	state = stateOf(t, githubPost(t, router, admin, "/api/v1/user/github/bind/begin", begin))
	if out := githubPost(t, router, admin, "/api/v1/user/github/bind/finish", `{"code":"bad","state":"`+state+`"}`); out.Code == 0 {
		t.Fatal("bad code must not bind")
	}
	state = stateOf(t, githubPost(t, router, admin, "/api/v1/user/github/bind/begin", begin))
	if out := githubPost(t, router, admin, "/api/v1/user/github/bind/finish", `{"code":"octo","state":"`+state+`"}`); out.Code != 0 {
		t.Fatalf("bind: %+v", out)
	}
	info := githubPost(t, router, admin, "/api/v1/user/github/info", `{}`)
	if d := info.Data.(map[string]interface{}); d["bound"] != true || d["login"] != "octocat" {
		t.Fatalf("info: %+v", info)
	}

	state = stateOf(t, githubPost(t, router, "", "/api/v1/user/github/login/begin", begin))
	out := githubPost(t, router, "", "/api/v1/user/github/login/finish", `{"code":"octo","state":"`+state+`"}`)
	if d, _ := out.Data.(map[string]interface{}); out.Code != 0 || d["name"] != "admin_user" || d["token"] == "" {
		t.Fatalf("login: %+v", out)
	}
	if again := githubPost(t, router, "", "/api/v1/user/github/login/finish", `{"code":"octo","state":"`+state+`"}`); again.Code == 0 {
		t.Fatal("state must be single use")
	}

	if out := githubPost(t, router, admin, "/api/v1/user/github/unbind", `{"password":"admin_user"}`); out.Code != 0 {
		t.Fatalf("unbind: %+v", out)
	}
	state = stateOf(t, githubPost(t, router, "", "/api/v1/user/github/login/begin", begin))
	if out := githubPost(t, router, "", "/api/v1/user/github/login/finish", `{"code":"octo","state":"`+state+`"}`); out.Code == 0 {
		t.Fatal("unbound account must not log in")
	}
}

func TestAdminPasswordLoginCanBeDisabledOnlyWithGithub(t *testing.T) {
	fakeGithub(t)
	router, r := setupConfigAccessTestRouter(t)
	admin := mustGenerateConfigAccessToken(t, 1, "admin_user", 0)
	pwd, err := security.HashPassword("user_pass")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.DB().Exec(`INSERT INTO user(id, user, pwd, role_id, exp_time, flow, flow_reset_time, num, created_time, status) VALUES(2, 'alice', ?, 1, 0, 0, 0, 0, 0, 1)`, pwd).Error; err != nil {
		t.Fatal(err)
	}
	adminLogin := func() response.R {
		return githubPost(t, router, "", "/api/v1/user/login", `{"username":"admin_user","password":"admin_user"}`)
	}
	userLogin := func() response.R {
		return githubPost(t, router, "", "/api/v1/user/login", `{"username":"alice","password":"user_pass"}`)
	}

	if out := githubPost(t, router, admin, "/api/v1/config/update", `{"admin_password_login_disabled":"true"}`); out.Code == 0 {
		t.Fatal("must not disable admin password login without GitHub login")
	}
	if out := githubPost(t, router, admin, "/api/v1/config/update",
		`{"github_login_enabled":"true","github_oauth_client_id":"cid","github_oauth_secret":"shh","admin_password_login_disabled":"true"}`); out.Code == 0 || !strings.Contains(out.Msg, "admin_user") {
		t.Fatalf("must not disable it while an admin has no GitHub bound: %+v", out)
	}
	if adminLogin().Code != 0 {
		t.Fatal("a refused save must leave admin password login on")
	}

	seedConfigValue(t, r, "github_login_enabled", "true")
	seedConfigValue(t, r, "github_oauth_client_id", "cid")
	seedConfigValue(t, r, "github_oauth_secret", "shh")
	state := stateOf(t, githubPost(t, router, admin, "/api/v1/user/github/bind/begin", `{"redirectUri":"https://panel.example.com/","password":"admin_user"}`))
	if out := githubPost(t, router, admin, "/api/v1/user/github/bind/finish", `{"code":"octo","state":"`+state+`"}`); out.Code != 0 {
		t.Fatalf("bind: %+v", out)
	}
	if out := githubPost(t, router, admin, "/api/v1/config/update-single", `{"name":"admin_password_login_disabled","value":"true"}`); out.Code != 0 {
		t.Fatalf("disable: %+v", out)
	}

	if out := adminLogin(); out.Code == 0 || !strings.Contains(out.Msg, "禁止密码登录") {
		t.Fatalf("admin password login must be refused: %+v", out)
	}
	if out := githubPost(t, router, "", "/api/v1/user/login", `{"username":"admin_user","password":"wrong"}`); strings.Contains(out.Msg, "禁止") {
		t.Fatal("a wrong password must not reveal the account is an admin")
	}
	if out := userLogin(); out.Code == 0 || !strings.Contains(out.Msg, "只有管理员") {
		t.Fatalf("only the admin may log in: %+v", out)
	}
	methods := func() map[string]bool {
		t.Helper()
		out := githubPost(t, router, "", "/api/v1/user/login/methods", `{}`)
		raw, _ := json.Marshal(out.Data)
		var m map[string]bool
		if err := json.Unmarshal(raw, &m); err != nil || out.Code != 0 {
			t.Fatalf("login methods: %+v (%v)", out, err)
		}
		return m
	}
	if m := methods(); m["password"] || !m["github"] {
		t.Fatalf("with password login off the page must offer GitHub only: %v", m)
	}
	if out := githubPost(t, router, admin, "/api/v1/user/github/unbind", `{"password":"admin_user"}`); out.Code == 0 {
		t.Fatal("an admin must not unbind while admin password login is off")
	}

	// Turning GitHub login off brings admin password login back.
	seedConfigValue(t, r, "github_login_enabled", "false")
	if out := adminLogin(); out.Code != 0 {
		t.Fatalf("admin password login must return with GitHub login off: %+v", out)
	}
	if m := methods(); !m["password"] || m["github"] {
		t.Fatalf("with GitHub login off the page must offer the password form: %v", m)
	}
}
