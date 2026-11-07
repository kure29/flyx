package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"go-backend/internal/http/response"
)

// A GitHub token lets the panel read a private release repository: release
// lookups, the panel's own upgrade files and, through the panel's GitHub
// mirror (/api/v1/open_api/gh), the agent binaries and install script the
// nodes download. Nodes never see the token; with a private repository they
// must download through the mirror (加速地址 = 面板中转).
//
// It is set in 设置 (stored as github_token, never sent back) and falls back
// to the GITHUB_TOKEN environment variable.
const githubTokenConfig = "github_token"

var (
	githubTokenMu     sync.RWMutex
	githubTokenSource = func() string { return "" }
)

// githubToken returns the token to authenticate GitHub requests with, or "".
func githubToken() string {
	githubTokenMu.RLock()
	source := githubTokenSource
	githubTokenMu.RUnlock()
	if t := strings.TrimSpace(source()); t != "" {
		return t
	}
	return strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
}

func (h *Handler) installGithubTokenSource() {
	githubTokenMu.Lock()
	defer githubTokenMu.Unlock()
	githubTokenSource = func() string {
		if h == nil || h.repo == nil {
			return ""
		}
		cfg, err := h.repo.GetConfigByName(githubTokenConfig)
		if err != nil || cfg == nil {
			return ""
		}
		return cfg.Value
	}
}

// githubTokenStatus tells the settings page whether a token is set (never
// the token itself) and which repository releases come from.
func (h *Handler) githubTokenStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	if !isAdminRequest(r) {
		response.WriteJSON(w, response.Err(403, "权限不足"))
		return
	}
	stored := ""
	if cfg, err := h.repo.GetConfigByName(githubTokenConfig); err == nil && cfg != nil {
		stored = strings.TrimSpace(cfg.Value)
	}
	response.WriteJSON(w, response.OK(map[string]interface{}{
		"set":     stored != "",
		"fromEnv": stored == "" && strings.TrimSpace(os.Getenv("GITHUB_TOKEN")) != "",
		"repo":    githubRepo,
	}))
}

// githubTokenSave stores (or, with clear, removes) the token after checking
// it can read the release repository.
func (h *Handler) githubTokenSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	if !isAdminRequest(r) {
		response.WriteJSON(w, response.Err(403, "权限不足"))
		return
	}
	var req struct {
		Token string `json:"token"`
		Clear bool   `json:"clear"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	token := strings.TrimSpace(req.Token)
	if !req.Clear {
		if token == "" || len(token) > 255 || strings.ContainsAny(token, " \r\n\t") {
			response.WriteJSON(w, response.ErrDefault("令牌格式不正确"))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		if err := checkGithubToken(ctx, token); err != nil {
			response.WriteJSON(w, response.ErrDefault("令牌不可用："+err.Error()))
			return
		}
	} else {
		token = ""
	}
	if err := h.repo.UpsertConfig(githubTokenConfig, token, time.Now().UnixMilli()); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	resetGithubCaches()
	response.WriteJSON(w, response.OKEmpty())
}

// checkGithubToken verifies the token can read the release repository.
func checkGithubToken(ctx context.Context, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/repos/%s", releaseAPIBase, githubRepo), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("连接 GitHub 失败: %v", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized:
		return fmt.Errorf("令牌无效或已过期")
	case http.StatusNotFound, http.StatusForbidden:
		return fmt.Errorf("令牌没有仓库 %s 的读取权限", githubRepo)
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("GitHub 返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
}

// resetGithubCaches drops what was looked up without (or with another)
// token, so a new token takes effect at once.
func resetGithubCaches() {
	releaseCacheMu.Lock()
	releaseCache, releaseCacheAt = nil, time.Time{}
	releaseCacheMu.Unlock()
	ghMirrorTagMu.Lock()
	ghMirrorTags = map[string]ghMirrorTag{}
	ghMirrorTagMu.Unlock()
}

// githubReleaseAssetURL finds the API URL of a release file of the panel's
// own repository, which with the token can be downloaded from a private
// repository (github.com/…/releases/download/… cannot).
func githubReleaseAssetURL(ctx context.Context, repo, tag, file, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/repos/%s/releases/tags/%s", ghMirrorBases.api, repo, tag), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("查询发布 %s 失败: %v", tag, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("查询发布 %s: GitHub 返回 %d", tag, resp.StatusCode)
	}
	var rel struct {
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel); err != nil {
		return "", fmt.Errorf("解析发布 %s 失败: %v", tag, err)
	}
	for _, a := range rel.Assets {
		if a.Name == file && strings.HasPrefix(a.URL, ghMirrorBases.api+"/") {
			return a.URL, nil
		}
	}
	return "", fmt.Errorf("发布 %s 中没有文件 %s", tag, file)
}

// githubLatestTagAPI reads the latest release's tag through the API, for a
// private repository whose github.com pages need a login.
func githubLatestTagAPI(ctx context.Context, repo, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/repos/%s/releases/latest", ghMirrorBases.api, repo), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub 返回 %d", resp.StatusCode)
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel); err != nil {
		return "", err
	}
	if !ghMirrorSegment.MatchString(rel.TagName) {
		return "", fmt.Errorf("无效的版本号 %q", rel.TagName)
	}
	return rel.TagName, nil
}

// isOwnRepo reports whether owner/repo is the panel's release repository.
func isOwnRepo(repo string) bool {
	return strings.EqualFold(strings.TrimSpace(repo), githubRepo)
}
