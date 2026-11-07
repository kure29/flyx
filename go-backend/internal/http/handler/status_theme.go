package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/statustheme"
)

// Status page themes: the page at /status/ is whichever theme the admin
// picked, the built-in one unless another is installed and chosen. Themes
// are installed from a theme.tar.gz, uploaded or taken from the latest
// release of a GitHub repository (see internal/statustheme).

const statusPageThemeKey = "status_page_theme"

// The choice is read on every request for the page and its assets; a
// switch made on this panel takes effect at once, one restored from a
// backup within this long.
const statusThemeChoiceTTL = 5 * time.Second

type statusThemeChoiceCache struct {
	mu    sync.Mutex
	short string
	at    time.Time
}

// GitHub endpoints, variables for tests.
var (
	statusThemeGithubAPI = "https://api.github.com"
	statusThemeGithubWeb = "https://github.com"
	statusThemeHTTP      = &http.Client{Timeout: 90 * time.Second}
)

const statusThemeAsset = "theme.tar.gz"

// SetStatusThemesDir sets where installed themes are kept.
func (h *Handler) SetStatusThemesDir(dir string) {
	h.statusThemes = &statustheme.Store{Dir: dir}
}

func (h *Handler) selectedStatusTheme() string {
	c := &h.statusThemeChoice
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.at.IsZero() && time.Since(c.at) < statusThemeChoiceTTL {
		return c.short
	}
	cfg, err := h.repo.GetConfigsByNames([]string{statusPageThemeKey})
	if err != nil {
		// Serve the last known choice rather than fail the page.
		return c.short
	}
	c.short, c.at = strings.TrimSpace(cfg[statusPageThemeKey]), time.Now()
	return c.short
}

func (h *Handler) setSelectedStatusTheme(short string) error {
	if err := h.repo.UpsertConfig(statusPageThemeKey, short, time.Now().UnixMilli()); err != nil {
		return err
	}
	c := &h.statusThemeChoice
	c.mu.Lock()
	c.short, c.at = short, time.Now()
	c.mu.Unlock()
	return nil
}

// serveStatusTheme serves the chosen theme at /status/. The page is served
// whether or not the status page is on: the theme asks the public API,
// which answers that it is off.
func (h *Handler) serveStatusTheme(w http.ResponseWriter, r *http.Request) {
	fsys, _, done := h.statusThemes.Open(h.selectedStatusTheme())
	defer done()
	statustheme.Serve(w, r, fsys)
}

// statusThemeFail answers an error: a refusal in its own words, anything
// else logged and reported in general terms.
func statusThemeFail(w http.ResponseWriter, what string, err error) {
	var refusal statustheme.Error
	if errors.As(err, &refusal) {
		response.WriteJSON(w, response.ErrDefault(refusal.Error()))
		return
	}
	log.Printf("status theme: %s: %v", what, err)
	response.WriteJSON(w, response.ErrDefault(what+"失败："+err.Error()))
}

func requirePost(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return false
	}
	return true
}

type statusThemeListResult struct {
	Themes     []statustheme.Theme `json:"themes"`
	APIVersion int                 `json:"apiVersion"`
}

func (h *Handler) statusThemeList(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	response.WriteJSON(w, response.OK(statusThemeListResult{
		Themes:     h.statusThemes.List(h.selectedStatusTheme()),
		APIVersion: statustheme.APIVersion,
	}))
}

type statusThemeShortRequest struct {
	Short string `json:"short"`
}

func decodeThemeShort(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req statusThemeShortRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return "", false
	}
	return strings.TrimSpace(req.Short), true
}

func (h *Handler) statusThemeSelect(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	short, ok := decodeThemeShort(w, r)
	if !ok {
		return
	}
	if _, found := h.statusThemes.Find(short); !found {
		response.WriteJSON(w, response.ErrDefault("没有这个主题，或者它不能在当前面板上使用"))
		return
	}
	if short == statustheme.DefaultShort {
		short = ""
	}
	if err := h.setSelectedStatusTheme(short); err != nil {
		statusThemeFail(w, "切换主题", err)
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) statusThemeDelete(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	short, ok := decodeThemeShort(w, r)
	if !ok {
		return
	}
	if err := h.statusThemes.Remove(short); err != nil {
		statusThemeFail(w, "删除主题", err)
		return
	}
	// Its settings go with it: a later theme of the same name starts clean.
	if err := h.repo.DeleteConfigByName(statusThemeConfigKey(short)); err != nil {
		log.Printf("status theme: delete settings of %s: %v", short, err)
	}
	// The page falls back to the built-in theme either way; clearing the
	// choice keeps a later install of the same name from switching to it.
	if h.selectedStatusTheme() == short {
		if err := h.setSelectedStatusTheme(""); err != nil {
			statusThemeFail(w, "删除主题", err)
			return
		}
	}
	response.WriteJSON(w, response.OKEmpty())
}

// readThemePackage reads a whole package, refusing one over the size limit.
func readThemePackage(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, statustheme.MaxPackage+1))
	if err != nil {
		return nil, err
	}
	if len(data) > statustheme.MaxPackage {
		return nil, statustheme.Error(fmt.Sprintf("主题包超过 %d MiB", statustheme.MaxPackage>>20))
	}
	return data, nil
}

func (h *Handler) statusThemeUpload(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, statustheme.MaxPackage+(1<<20))
	mr, err := r.MultipartReader()
	if err != nil {
		response.WriteJSON(w, response.ErrDefault("请选择 theme.tar.gz 上传"))
		return
	}
	for {
		part, err := mr.NextPart()
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				response.WriteJSON(w, response.ErrDefault(fmt.Sprintf("主题包超过 %d MiB", statustheme.MaxPackage>>20)))
				return
			}
			response.WriteJSON(w, response.ErrDefault("请选择 theme.tar.gz 上传"))
			return
		}
		if part.FormName() != "file" {
			_ = part.Close()
			continue
		}
		data, err := readThemePackage(part)
		_ = part.Close()
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				err = statustheme.Error(fmt.Sprintf("主题包超过 %d MiB", statustheme.MaxPackage>>20))
			}
			statusThemeFail(w, "上传主题", err)
			return
		}
		theme, err := h.statusThemes.Install(bytes.NewReader(data), "")
		if err != nil {
			statusThemeFail(w, "安装主题", err)
			return
		}
		response.WriteJSON(w, response.OK(theme))
		return
	}
}

type statusThemeInstallRequest struct {
	URL string `json:"url"`
}

func (h *Handler) statusThemeInstall(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var req statusThemeInstallRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	owner, repoName, ok := parseThemeRepo(req.URL)
	if !ok {
		response.WriteJSON(w, response.ErrDefault("填主题的 GitHub 仓库地址，形如 https://github.com/作者/仓库"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	release, err := latestThemeRelease(ctx, owner, repoName)
	if err != nil {
		statusThemeFail(w, "读取主题仓库", err)
		return
	}
	theme, err := h.installThemeRelease(ctx, release, "")
	if err != nil {
		statusThemeFail(w, "安装主题", err)
		return
	}
	response.WriteJSON(w, response.OK(theme))
}

type statusThemeUpdateResult struct {
	Updated bool              `json:"updated"`
	Version string            `json:"version"`
	Theme   statustheme.Theme `json:"theme"`
}

func (h *Handler) statusThemeUpdate(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	short, ok := decodeThemeShort(w, r)
	if !ok {
		return
	}
	if short == statustheme.DefaultShort {
		response.WriteJSON(w, response.ErrDefault("内置主题随面板一起升级"))
		return
	}
	installed, found := h.statusThemes.Find(short)
	if !found {
		response.WriteJSON(w, response.ErrDefault("没有这个主题"))
		return
	}
	owner, repoName, ok := parseThemeRepo(installed.URL)
	if !ok || !strings.Contains(installed.URL, "github.com/") {
		response.WriteJSON(w, response.ErrDefault("这个主题的 url 不是 https://github.com/作者/仓库，只能手动上传新包"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	release, err := latestThemeRelease(ctx, owner, repoName)
	if err != nil {
		statusThemeFail(w, "检查更新", err)
		return
	}
	if strings.TrimPrefix(release.TagName, "v") == installed.Version {
		response.WriteJSON(w, response.OK(statusThemeUpdateResult{Version: installed.Version, Theme: installed}))
		return
	}
	theme, err := h.installThemeRelease(ctx, release, short)
	if err != nil {
		statusThemeFail(w, "更新主题", err)
		return
	}
	response.WriteJSON(w, response.OK(statusThemeUpdateResult{Updated: true, Version: theme.Version, Theme: theme}))
}

func (h *Handler) statusThemePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	img, ok := h.statusThemes.Preview(strings.TrimSpace(r.URL.Query().Get("short")))
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-cache")
	_, _ = w.Write(img)
}

var themeRepoName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)

// parseThemeRepo takes owner and repository from https://github.com/o/r
// (with or without the scheme, a trailing .git or further path) or o/r.
func parseThemeRepo(raw string) (owner, repoName string, ok bool) {
	s := strings.TrimSpace(raw)
	for _, prefix := range []string{"https://", "http://"} {
		s = strings.TrimPrefix(s, prefix)
	}
	if rest, found := strings.CutPrefix(s, "github.com/"); found {
		s = rest
	} else if strings.Contains(s, ".") && strings.Index(s, ".") < strings.Index(s+"/", "/") {
		// Another host.
		return "", "", false
	}
	parts := strings.Split(strings.Trim(s, "/"), "/")
	if len(parts) < 2 {
		return "", "", false
	}
	owner, repoName = parts[0], strings.TrimSuffix(parts[1], ".git")
	if !themeRepoName.MatchString(owner) || !themeRepoName.MatchString(repoName) || repoName == "." || repoName == ".." {
		return "", "", false
	}
	return owner, repoName, true
}

type themeRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func themeGithubRequest(ctx context.Context, url, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "flyx-panel")
	return statusThemeHTTP.Do(req)
}

func latestThemeRelease(ctx context.Context, owner, repoName string) (*themeRelease, error) {
	resp, err := themeGithubRequest(ctx, fmt.Sprintf("%s/repos/%s/%s/releases/latest", statusThemeGithubAPI, owner, repoName), "application/vnd.github+json")
	if err != nil {
		return nil, statustheme.Error("连不上 GitHub，稍后再试，或者下载 theme.tar.gz 后手动上传")
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, statustheme.Error("仓库不存在，或者还没有正式 release")
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests:
		return nil, statustheme.Error("GitHub 暂时拒绝了请求（可能是访问次数超限），稍后再试")
	case resp.StatusCode != http.StatusOK:
		return nil, statustheme.Error(fmt.Sprintf("GitHub 返回 %d，稍后再试", resp.StatusCode))
	}
	var rel themeRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel); err != nil {
		return nil, statustheme.Error("GitHub 的回复无法识别，稍后再试")
	}
	return &rel, nil
}

func (h *Handler) installThemeRelease(ctx context.Context, rel *themeRelease, expect string) (statustheme.Theme, error) {
	var url string
	for _, a := range rel.Assets {
		if a.Name == statusThemeAsset {
			if a.Size > statustheme.MaxPackage {
				return statustheme.Theme{}, statustheme.Error(fmt.Sprintf("主题包超过 %d MiB", statustheme.MaxPackage>>20))
			}
			url = a.URL
		}
	}
	if url == "" {
		return statustheme.Theme{}, statustheme.Error(fmt.Sprintf("最新的 release（%s）里没有 %s", rel.TagName, statusThemeAsset))
	}
	// Only from GitHub itself, whatever the API answered.
	if !strings.HasPrefix(url, statusThemeGithubWeb+"/") {
		return statustheme.Theme{}, statustheme.Error("release 里的下载地址不在 GitHub 上")
	}
	resp, err := themeGithubRequest(ctx, url, "application/octet-stream")
	if err != nil {
		return statustheme.Theme{}, statustheme.Error("下载主题包失败，稍后再试，或者下载后手动上传")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return statustheme.Theme{}, statustheme.Error(fmt.Sprintf("下载主题包失败（%d）", resp.StatusCode))
	}
	data, err := readThemePackage(resp.Body)
	if err != nil {
		return statustheme.Theme{}, err
	}
	return h.statusThemes.Install(bytes.NewReader(data), expect)
}

// Theme settings: a theme declares its form in theme.json ("config", the same
// shape as monitor's); the admin's values are kept per theme and served to
// every visitor. Values are not checked against the form, as in monitor: a
// value saved under one version of a theme meets the next, so the theme must
// check what it reads anyway.

const (
	statusThemeConfigPrefix = "status_theme_config_"
	maxStatusThemeConfig    = 64 << 10
)

func statusThemeConfigKey(short string) string { return statusThemeConfigPrefix + short }

// statusThemeValues returns a theme's saved settings, {} when none.
func (h *Handler) statusThemeValues(short string) (json.RawMessage, error) {
	cfg, err := h.repo.GetConfigsByNames([]string{statusThemeConfigKey(short)})
	if err != nil {
		return nil, err
	}
	if v := strings.TrimSpace(cfg[statusThemeConfigKey(short)]); v != "" && json.Valid([]byte(v)) {
		return json.RawMessage(v), nil
	}
	return json.RawMessage("{}"), nil
}

type statusThemeConfigAnswer struct {
	Theme  string          `json:"theme"`
	Values json.RawMessage `json:"values"`
}

// publicStatusThemeConfig serves the settings of the theme in use, without a
// login: the theme reads them when it loads.
func (h *Handler) publicStatusThemeConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	gate, err := h.statusHistoryGate(time.Now())
	if err != nil {
		response.WriteJSON(w, response.Err(-2, "状态数据暂时不可用"))
		return
	}
	if !gate.enabled {
		response.WriteJSON(w, response.Err(404, "状态页未开启"))
		return
	}
	short := h.statusThemes.Effective(h.selectedStatusTheme())
	values, err := h.statusThemeValues(short)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, "状态数据暂时不可用"))
		return
	}
	hdr := w.Header()
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Cache-Control", "public, max-age=5")
	if r.Method == http.MethodHead {
		return
	}
	response.WriteJSON(w, response.OK(statusThemeConfigAnswer{Theme: short, Values: values}))
}

func (h *Handler) statusThemeConfigGet(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	short, ok := decodeThemeShort(w, r)
	if !ok {
		return
	}
	if _, found := h.statusThemes.Find(short); !found {
		response.WriteJSON(w, response.ErrDefault("没有这个主题"))
		return
	}
	values, err := h.statusThemeValues(short)
	if err != nil {
		statusThemeFail(w, "读取主题设置", err)
		return
	}
	response.WriteJSON(w, response.OK(statusThemeConfigAnswer{Theme: short, Values: values}))
}

type statusThemeConfigSaveRequest struct {
	Short  string          `json:"short"`
	Values json.RawMessage `json:"values"`
}

func (h *Handler) statusThemeConfigSave(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxStatusThemeConfig+4096)
	var req statusThemeConfigSaveRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	short := strings.TrimSpace(req.Short)
	if _, found := h.statusThemes.Find(short); !found {
		response.WriteJSON(w, response.ErrDefault("没有这个主题"))
		return
	}
	var values map[string]json.RawMessage
	if len(req.Values) == 0 || json.Unmarshal(req.Values, &values) != nil || values == nil {
		response.WriteJSON(w, response.ErrDefault("主题设置必须是一个 JSON 对象"))
		return
	}
	compact, err := json.Marshal(values)
	if err != nil {
		statusThemeFail(w, "保存主题设置", err)
		return
	}
	if len(compact) > maxStatusThemeConfig {
		response.WriteJSON(w, response.ErrDefault(fmt.Sprintf("主题设置超过 %d KiB", maxStatusThemeConfig>>10)))
		return
	}
	if err := h.repo.UpsertConfig(statusThemeConfigKey(short), string(compact), time.Now().UnixMilli()); err != nil {
		statusThemeFail(w, "保存主题设置", err)
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}
