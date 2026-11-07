package handler

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go-backend/internal/auth"
	"go-backend/internal/http/response"
	"go-backend/internal/security"
	"go-backend/internal/store/model"
)

// GitHub login (OAuth App). Users bind a GitHub account in their profile
// and can then log in with it; an unbound GitHub account is refused, so
// this never creates users. The browser comes back to the panel's own page
// (the login page, or the profile page when binding) with ?code&state, and
// that page hands both to the matching finish endpoint.

const githubStateTTL = 10 * time.Minute

// Overridden in tests.
var (
	githubOAuthBase = "https://github.com"
	githubUserAPI   = "https://api.github.com/user"
	githubClient    = &http.Client{Timeout: 15 * time.Second}
)

type githubState struct {
	kind        string // "login" or "bind"
	userID      int64
	redirectURI string
	expires     time.Time
}

type githubAccount struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}

// githubOAuthConfig returns the OAuth App credentials when GitHub login is
// switched on and fully configured.
func (h *Handler) githubOAuthConfig() (clientID, secret string, ok bool) {
	get := func(name string) string {
		cfg, err := h.repo.GetConfigByName(name)
		if err != nil || cfg == nil {
			return ""
		}
		return strings.TrimSpace(cfg.Value)
	}
	if get("github_login_enabled") != "true" {
		return "", "", false
	}
	clientID, secret = get("github_oauth_client_id"), get("github_oauth_secret")
	return clientID, secret, clientID != "" && secret != ""
}

func validGithubRedirectURI(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil && u.Fragment == ""
}

func (h *Handler) putGithubState(s githubState) (string, bool) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", false
	}
	id := base64.RawURLEncoding.EncodeToString(buf)
	h.githubMu.Lock()
	defer h.githubMu.Unlock()
	now := time.Now()
	for k, v := range h.githubPending {
		if now.After(v.expires) {
			delete(h.githubPending, k)
		}
	}
	if len(h.githubPending) >= 1000 {
		return "", false
	}
	s.expires = now.Add(githubStateTTL)
	h.githubPending[id] = s
	return id, true
}

// takeGithubState consumes a state once; a replayed or foreign state fails.
func (h *Handler) takeGithubState(id, kind string, userID int64) (githubState, bool) {
	h.githubMu.Lock()
	s, ok := h.githubPending[id]
	delete(h.githubPending, id)
	h.githubMu.Unlock()
	if !ok || s.kind != kind || s.userID != userID || time.Now().After(s.expires) {
		return githubState{}, false
	}
	return s, true
}

func (h *Handler) githubAuthorizeURL(clientID string, s githubState) (string, bool) {
	state, ok := h.putGithubState(s)
	if !ok {
		return "", false
	}
	q := url.Values{}
	q.Set("client_id", clientID)
	q.Set("redirect_uri", s.redirectURI)
	q.Set("state", state)
	q.Set("allow_signup", "false")
	return githubOAuthBase + "/login/oauth/authorize?" + q.Encode(), true
}

// githubExchange turns the authorization code into the GitHub account that
// approved it.
func githubExchange(clientID, secret, code, redirectURI string) (githubAccount, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("client_secret", secret)
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	req, err := http.NewRequest(http.MethodPost, githubOAuthBase+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return githubAccount{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := githubClient.Do(req)
	if err != nil {
		return githubAccount{}, fmt.Errorf("连接 GitHub 失败: %v", err)
	}
	defer resp.Body.Close()
	var token struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 64*1024)).Decode(&token); err != nil {
		return githubAccount{}, errors.New("GitHub 返回异常")
	}
	if token.AccessToken == "" {
		return githubAccount{}, fmt.Errorf("GitHub 授权失败: %s", firstNonEmpty(token.Description, token.Error, "未返回令牌"))
	}

	req, err = http.NewRequest(http.MethodGet, githubUserAPI, nil)
	if err != nil {
		return githubAccount{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err = githubClient.Do(req)
	if err != nil {
		return githubAccount{}, fmt.Errorf("连接 GitHub 失败: %v", err)
	}
	defer resp.Body.Close()
	var account githubAccount
	if resp.StatusCode != http.StatusOK || json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 256*1024)).Decode(&account) != nil || account.ID <= 0 {
		return githubAccount{}, errors.New("读取 GitHub 账号失败")
	}
	return account, nil
}

type githubBeginRequest struct {
	RedirectURI string `json:"redirectUri"`
	Password    string `json:"password"`
}

type githubFinishRequest struct {
	Code  string `json:"code"`
	State string `json:"state"`
}

func (h *Handler) githubStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	_, _, ok := h.githubOAuthConfig()
	response.WriteJSON(w, response.OK(map[string]bool{"enabled": ok}))
}

// loginMethods tells the login page which ways in to offer, without a
// login: the password form only while password login is allowed.
func (h *Handler) loginMethods(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	_, _, github := h.githubOAuthConfig()
	wa, _ := passkeyConfig()
	response.WriteJSON(w, response.OK(map[string]bool{
		"password": !h.adminPasswordLoginDisabled(),
		"github":   github,
		"passkey":  wa != nil,
	}))
}

// adminPasswordLoginDisabled reports whether the admin must log in with
// GitHub or a passkey, the login page then offering no password form. It only
// holds while GitHub login is on and configured, so turning GitHub login
// off (or breaking its config) brings admin password login back instead of
// locking admins out. `paneld enable-password-login` clears it.
func (h *Handler) adminPasswordLoginDisabled() bool {
	cfg, err := h.repo.GetConfigByName("admin_password_login_disabled")
	if err != nil || cfg == nil || strings.TrimSpace(cfg.Value) != "true" {
		return false
	}
	_, _, ok := h.githubOAuthConfig()
	return ok
}

// checkPasswordLoginSwitch refuses to turn admin password login off unless
// the result has GitHub login configured and every enabled admin has a
// GitHub account bound, so no admin loses the only way they can get in.
func (h *Handler) checkPasswordLoginSwitch(payload map[string]string) error {
	if strings.TrimSpace(payload["admin_password_login_disabled"]) != "true" {
		return nil
	}
	value := func(name string) string {
		if v, ok := payload[name]; ok {
			return strings.TrimSpace(v)
		}
		if cfg, err := h.repo.GetConfigByName(name); err == nil && cfg != nil {
			return strings.TrimSpace(cfg.Value)
		}
		return ""
	}
	if value("github_login_enabled") != "true" || value("github_oauth_client_id") == "" || value("github_oauth_secret") == "" {
		return errors.New("请先启用并配置 GitHub 登录，再禁止密码登录")
	}
	unbound, err := h.repo.ListAdminsWithoutGithub()
	if err != nil {
		return err
	}
	if len(unbound) > 0 {
		return fmt.Errorf("管理员 %s 还没有绑定 GitHub，请先在个人资料页绑定并确认能用它登录", strings.Join(unbound, "、"))
	}
	return nil
}

func (h *Handler) githubLoginBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	clientID, _, ok := h.githubOAuthConfig()
	if !ok {
		response.WriteJSON(w, response.ErrDefault("GitHub 登录未开启"))
		return
	}
	var req githubBeginRequest
	if !passkeyBody(r, &req) || !validGithubRedirectURI(req.RedirectURI) {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	authURL, ok := h.githubAuthorizeURL(clientID, githubState{kind: "login", redirectURI: req.RedirectURI})
	if !ok {
		response.WriteJSON(w, response.ErrDefault("请稍后再试"))
		return
	}
	response.WriteJSON(w, response.OK(map[string]string{"url": authURL}))
}

func (h *Handler) githubLoginFinish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	clientID, secret, ok := h.githubOAuthConfig()
	if !ok {
		response.WriteJSON(w, response.ErrDefault("GitHub 登录未开启"))
		return
	}
	var req githubFinishRequest
	if !passkeyBody(r, &req) || req.Code == "" {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	state, ok := h.takeGithubState(req.State, "login", 0)
	if !ok {
		response.WriteJSON(w, response.ErrDefault("登录已过期，请重新登录"))
		return
	}
	account, err := githubExchange(clientID, secret, req.Code, state.redirectURI)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	link, err := h.repo.GetUserGithubByGithubID(account.ID)
	if err != nil || link == nil {
		response.WriteJSON(w, response.ErrDefault("该 GitHub 账号未绑定面板用户，请先用密码登录后在个人中心绑定"))
		return
	}
	user, err := h.repo.GetUserByID(link.UserID)
	if err != nil || user == nil {
		response.WriteJSON(w, response.ErrDefault("账号不可用"))
		return
	}
	if user.Status != 1 {
		response.WriteJSON(w, response.ErrDefault("账号被停用"))
		return
	}
	if user.RoleID != 0 {
		response.WriteJSON(w, response.ErrDefault("只有管理员可以登录"))
		return
	}
	token, err := auth.GenerateTokenAt(user.ID, user.User, user.RoleID, h.jwtSecret, time.Now())
	if err != nil {
		response.WriteJSON(w, response.ErrDefault("登录失败"))
		return
	}
	response.WriteJSON(w, response.OK(map[string]interface{}{"token": token, "name": user.User, "role_id": user.RoleID, "requirePasswordChange": user.User == "admin_user"}))
}

func (h *Handler) githubInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	userID, err := userIDFromRequest(r)
	if err != nil {
		response.WriteJSON(w, response.Err(401, "未登录或token已过期"))
		return
	}
	_, _, enabled := h.githubOAuthConfig()
	out := map[string]interface{}{"enabled": enabled, "bound": false}
	if link, err := h.repo.GetUserGithub(userID); err == nil && link != nil {
		out["bound"] = true
		out["login"] = link.Login
		out["createdAt"] = link.CreatedAt
	}
	response.WriteJSON(w, response.OK(out))
}

// checkGithubPassword confirms the current password before (un)binding,
// like adding or removing a passkey.
func (h *Handler) checkGithubPassword(w http.ResponseWriter, userID int64, password string) bool {
	user, err := h.repo.GetUserByID(userID)
	if err != nil || user == nil || user.Status != 1 {
		response.WriteJSON(w, response.ErrDefault("账号不可用"))
		return false
	}
	if ok, _ := security.VerifyPassword(user.Pwd, password); !ok {
		response.WriteJSON(w, response.ErrDefault("当前密码错误"))
		return false
	}
	return true
}

func (h *Handler) githubBindBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	clientID, _, ok := h.githubOAuthConfig()
	if !ok {
		response.WriteJSON(w, response.ErrDefault("GitHub 登录未开启"))
		return
	}
	userID, err := userIDFromRequest(r)
	if err != nil {
		response.WriteJSON(w, response.Err(401, "未登录或token已过期"))
		return
	}
	var req githubBeginRequest
	if !passkeyBody(r, &req) || !validGithubRedirectURI(req.RedirectURI) {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	if !h.checkGithubPassword(w, userID, req.Password) {
		return
	}
	if link, err := h.repo.GetUserGithub(userID); err == nil && link != nil {
		response.WriteJSON(w, response.ErrDefault("已绑定 GitHub 账号，请先解绑"))
		return
	}
	authURL, ok := h.githubAuthorizeURL(clientID, githubState{kind: "bind", userID: userID, redirectURI: req.RedirectURI})
	if !ok {
		response.WriteJSON(w, response.ErrDefault("请稍后再试"))
		return
	}
	response.WriteJSON(w, response.OK(map[string]string{"url": authURL}))
}

func (h *Handler) githubBindFinish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	clientID, secret, ok := h.githubOAuthConfig()
	if !ok {
		response.WriteJSON(w, response.ErrDefault("GitHub 登录未开启"))
		return
	}
	userID, err := userIDFromRequest(r)
	if err != nil {
		response.WriteJSON(w, response.Err(401, "未登录或token已过期"))
		return
	}
	var req githubFinishRequest
	if !passkeyBody(r, &req) || req.Code == "" {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	state, ok := h.takeGithubState(req.State, "bind", userID)
	if !ok {
		response.WriteJSON(w, response.ErrDefault("绑定已过期，请重新绑定"))
		return
	}
	account, err := githubExchange(clientID, secret, req.Code, state.redirectURI)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	if other, err := h.repo.GetUserGithubByGithubID(account.ID); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	} else if other != nil {
		response.WriteJSON(w, response.ErrDefault("该 GitHub 账号已绑定其他用户"))
		return
	}
	login := account.Login
	if len(login) > 100 {
		login = login[:100]
	}
	if err := h.repo.CreateUserGithub(&model.UserGithub{UserID: userID, GithubID: account.ID, Login: login, CreatedAt: time.Now().UnixMilli()}); err != nil {
		response.WriteJSON(w, response.ErrDefault("绑定失败，可能已绑定其他 GitHub 账号"))
		return
	}
	response.WriteJSON(w, response.OK(map[string]string{"login": login}))
}

func (h *Handler) githubUnbind(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	userID, err := userIDFromRequest(r)
	if err != nil {
		response.WriteJSON(w, response.Err(401, "未登录或token已过期"))
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if !passkeyBody(r, &req) {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	if !h.checkGithubPassword(w, userID, req.Password) {
		return
	}
	if user, err := h.repo.GetUserByID(userID); err == nil && user != nil && user.RoleID == 0 && h.adminPasswordLoginDisabled() {
		response.WriteJSON(w, response.ErrDefault("已禁止密码登录，解绑后将无法登录；请先在设置中关闭该开关"))
		return
	}
	deleted, err := h.repo.DeleteUserGithub(userID)
	if err != nil || !deleted {
		response.WriteJSON(w, response.ErrDefault("未绑定 GitHub 账号"))
		return
	}
	response.WriteJSON(w, response.OK(nil))
}
