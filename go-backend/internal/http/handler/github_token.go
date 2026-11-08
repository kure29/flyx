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
)

// A GitHub token lets the panel read a private release repository (a
// private fork): release lookups, the panel's own upgrade files and,
// through the panel's GitHub mirror (/api/v1/open_api/gh), the agent
// binaries and install script the nodes download. It also lifts GitHub's
// API limit for unauthenticated requests (60 an hour per IP). Nodes never
// see the token; with a private repository they must download through the
// mirror (加速地址 = 面板中转).
//
// It comes from the GITHUB_TOKEN environment variable. Up to 3.9.2 it could
// also be set in 设置 (stored as github_token); the official repository is
// public, so that setting is gone, and a value left stored is not used but
// stays hidden (handler.go, config_policy.go).
const githubTokenConfig = "github_token"

var (
	githubTokenMu     sync.RWMutex
	githubTokenSource = func() string { return os.Getenv("GITHUB_TOKEN") }
)

// githubToken returns the token to authenticate GitHub requests with, or "".
func githubToken() string {
	githubTokenMu.RLock()
	source := githubTokenSource
	githubTokenMu.RUnlock()
	return strings.TrimSpace(source())
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
