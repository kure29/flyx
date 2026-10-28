package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"go-backend/internal/config"
	"go-backend/internal/http/response"
)

const (
	panelDeployDirEnv                 = "PANEL_DEPLOY_DIR"
	panelBackendContainerEnv          = "PANEL_BACKEND_CONTAINER"
	defaultPanelDeployDir             = "/opt/flyx-panel"
	defaultPanelBackendName           = "flyx-panel-backend"
	dockerSocketPath                  = "/var/run/docker.sock"
	maxSystemUpgradeComposeAssetBytes = 1 << 20
	systemUpgradeMessage              = "升级 helper 已启动，面板服务将短暂重启"
	systemUpgradeConflictError        = "已有面板升级任务执行中"
)

var safeBackendContainerPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
var enableIPv6ComposePattern = regexp.MustCompile(`(?im)^\s*enable_ipv6\s*:\s*['"]?true['"]?\s*(?:#.*)?$`)
var systemUpgradeReleaseBaseURL = githubHTMLBase
var systemUpgradeAPIBaseURL = githubAPIBase
var systemUpgradeHTTPGet = func(client *http.Client, url string) (*http.Response, error) {
	return client.Get(url)
}

type systemUpgradeExecutor struct {
	deployDir        string
	backendContainer string
	// composeProject is the Compose project the running containers belong
	// to. It need not match the deploy directory's name (e.g. installed
	// from /root, deployed in /opt/flvx-panel); without it Compose would
	// try to create a second flux-panel-backend and hit a name conflict.
	composeProject string
	// hostDeployDir is the host directory mounted at deployDir. Compose
	// resolves relative bind mounts (./) against the directory it runs in
	// as a host path, so the helper must run in the host path itself;
	// running in deployDir would remount an empty /opt/flvx-panel.
	hostDeployDir string
}

var hostPathPattern = regexp.MustCompile(`^/[A-Za-z0-9._/@+-]*$`)

// detectHostDeployDir returns the host source of the backend container's
// deploy directory mount, or "" when it cannot be determined.
func (e *systemUpgradeExecutor) detectHostDeployDir(ctx context.Context) string {
	if validateBackendContainerName(e.backendContainer) != nil || !hostPathPattern.MatchString(e.deployDir) {
		return ""
	}
	tmpl := fmt.Sprintf(`{{range .Mounts}}{{if eq .Destination %q}}{{.Source}}{{end}}{{end}}`, e.deployDir)
	out, err := exec.CommandContext(ctx, "docker", "inspect", "-f", tmpl, e.backendContainer).Output()
	if err != nil {
		return ""
	}
	src := strings.TrimSpace(string(out))
	if !hostPathPattern.MatchString(src) {
		return ""
	}
	return src
}

var composeProjectPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// detectComposeProject reads the Compose project label of the backend
// container, the same way panel_install.sh does.
func (e *systemUpgradeExecutor) detectComposeProject(ctx context.Context) string {
	if validateBackendContainerName(e.backendContainer) != nil {
		return ""
	}
	out, err := exec.CommandContext(ctx, "docker", "inspect", "-f",
		`{{index .Config.Labels "com.docker.compose.project"}}`, e.backendContainer).Output()
	if err != nil {
		return ""
	}
	project := strings.TrimSpace(string(out))
	if !composeProjectPattern.MatchString(project) {
		return ""
	}
	return project
}

type systemUpgradeCapabilityData struct {
	Capable          bool     `json:"capable"`
	Reasons          []string `json:"reasons"`
	DeployDir        string   `json:"deployDir"`
	BackendContainer string   `json:"backendContainer"`
}

type systemUpgradeReleaseData struct {
	Version     string `json:"version"`
	Name        string `json:"name"`
	PublishedAt string `json:"publishedAt"`
	Prerelease  bool   `json:"prerelease"`
	Channel     string `json:"channel"`
}

type systemUpgradeVersionData struct {
	CurrentVersion string                      `json:"currentVersion"`
	LatestVersion  string                      `json:"latestVersion"`
	HasUpdate      bool                        `json:"hasUpdate"`
	Channel        string                      `json:"channel"`
	Reason         string                      `json:"reason,omitempty"`
	Capability     systemUpgradeCapabilityData `json:"capability"`
}

type systemUpgradeCheckData struct {
	CurrentVersion string                      `json:"currentVersion"`
	LatestVersion  string                      `json:"latestVersion"`
	HasUpdate      bool                        `json:"hasUpdate"`
	Channel        string                      `json:"channel"`
	Capability     systemUpgradeCapabilityData `json:"capability"`
	Releases       []systemUpgradeReleaseData  `json:"releases"`
}

type systemUpgradeRunData struct {
	Version         string `json:"version"`
	Channel         string `json:"channel"`
	ComposeAsset    string `json:"composeAsset"`
	HelperContainer string `json:"helperContainer"`
	BackendImageID  string `json:"backendImageId"`
	Message         string `json:"message"`
}

type systemUpgradeRequest struct {
	Version string `json:"version"`
	Channel string `json:"channel"`
}

func newSystemUpgradeExecutor() *systemUpgradeExecutor {
	deployDir := strings.TrimSpace(os.Getenv(panelDeployDirEnv))
	if deployDir == "" {
		deployDir = defaultPanelDeployDir
	}
	backendContainer := strings.TrimSpace(os.Getenv(panelBackendContainerEnv))
	if backendContainer == "" {
		backendContainer = defaultPanelBackendName
	}
	return &systemUpgradeExecutor{deployDir: deployDir, backendContainer: backendContainer}
}

func currentPanelVersion() string {
	version := strings.TrimSpace(config.IdentityEnv("FLYX_VERSION", "FLUX_VERSION"))
	if version == "" {
		return "dev"
	}
	return version
}

func validateBackendContainerName(value string) error {
	if value == "" {
		return fmt.Errorf("backend container name is empty")
	}
	if !safeBackendContainerPattern.MatchString(value) {
		return fmt.Errorf("unsafe backend container name: %s", value)
	}
	return nil
}

func validateUpgradeVersion(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("upgrade version is empty")
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("unsafe upgrade version: contains control character")
		}
	}
	return nil
}

func (e *systemUpgradeExecutor) composePath() string {
	return filepath.Join(e.deployDir, "docker-compose.yml")
}
func (e *systemUpgradeExecutor) envPath() string { return filepath.Join(e.deployDir, ".env") }

func (e *systemUpgradeExecutor) capability(ctx context.Context) systemUpgradeCapabilityData {
	reasons := make([]string, 0)
	if !filepath.IsAbs(e.deployDir) {
		reasons = append(reasons, "部署目录必须是绝对路径")
	}
	if err := validateBackendContainerName(e.backendContainer); err != nil {
		reasons = append(reasons, err.Error())
	}
	if out, err := exec.CommandContext(ctx, "docker", "--version").CombinedOutput(); err != nil {
		reasons = append(reasons, fmt.Sprintf("docker CLI不可用: %v: %s", err, strings.TrimSpace(string(out))))
	}
	if info, err := os.Stat(dockerSocketPath); err != nil {
		reasons = append(reasons, "docker socket不可用: "+err.Error())
	} else if info.IsDir() {
		reasons = append(reasons, "docker socket路径不是文件")
	}
	if info, err := os.Stat(e.composePath()); err != nil {
		reasons = append(reasons, "部署docker-compose.yml不可用: "+err.Error())
		hint := "后端容器挂载的部署目录里没有 docker-compose.yml（通常是旧版面板内升级把挂载弄丢了）。请在服务器上进入存放 docker-compose.yml 和 .env 的目录（常见为 /root 或安装脚本所在目录），执行 docker compose up -d --force-recreate backend frontend，或在该目录重新运行安装脚本选择“更新面板”"
		if src := e.detectHostDeployDir(ctx); src != "" {
			hint += "。当前挂载的宿主机目录：" + src
		}
		reasons = append(reasons, hint)
	} else if info.IsDir() {
		reasons = append(reasons, "部署docker-compose.yml不是文件")
	}
	if info, err := os.Stat(e.envPath()); err != nil {
		reasons = append(reasons, "部署.env不可用: "+err.Error())
	} else if info.IsDir() {
		reasons = append(reasons, "部署.env不是文件")
	}
	if out, err := exec.CommandContext(ctx, "docker", "compose", "version").CombinedOutput(); err != nil {
		reasons = append(reasons, fmt.Sprintf("docker compose不可用: %v: %s", err, strings.TrimSpace(string(out))))
	}
	if _, err := e.currentBackendImage(ctx); err != nil {
		reasons = append(reasons, err.Error())
	}

	return systemUpgradeCapabilityData{
		Capable:          len(reasons) == 0,
		Reasons:          reasons,
		DeployDir:        e.deployDir,
		BackendContainer: e.backendContainer,
	}
}

func (e *systemUpgradeExecutor) selectComposeAsset(current []byte) string {
	if enableIPv6ComposePattern.Match(current) {
		return "docker-compose-v6.yml"
	}
	return "docker-compose-v4.yml"
}

func (e *systemUpgradeExecutor) helperScript() string {
	return `set -eu
LOGFILE="$PANEL_DEPLOY_DIR/upgrade.log"
log() { echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*" | tee -a "$LOGFILE"; }

cd "$PANEL_DEPLOY_DIR"
echo "" > "$LOGFILE"
log "开始面板升级"
log "工作目录: $(pwd)"
log "Compose 项目: ${COMPOSE_PROJECT_NAME:-（按目录名）}"

if [ ! -f docker-compose.yml ]; then
  log "错误: docker-compose.yml 不存在"
  exit 1
fi
if [ ! -f .env ]; then
  log "错误: .env 不存在"
  exit 1
fi

log "拉取新镜像..."
if ! docker compose pull backend frontend >> "$LOGFILE" 2>&1; then
  log "错误: 拉取镜像失败"
  exit 1
fi

log "等待旧容器释放资源..."
sleep 3

log "重启服务（force-recreate）..."
UPOUT="$PANEL_DEPLOY_DIR/.upgrade-compose.out"
if ! docker compose up -d --force-recreate --remove-orphans backend frontend > "$UPOUT" 2>&1; then
  cat "$UPOUT" >> "$LOGFILE"
  # 3.4.0 renamed the network's Compose key; the existing network keeps the
  # other key in its label and Compose refuses it. Recreate the network.
  NETWORK=$(sed -n 's/.*network \([^ ]*\) was found but has incorrect label com\.docker\.compose\.network.*/\1/p' "$UPOUT" | head -n 1)
  rm -f "$UPOUT"
  if [ -z "$NETWORK" ]; then
    log "错误: 重启服务失败"
    exit 1
  fi
  log "网络 $NETWORK 的标签与 Compose 配置不一致，停止服务并重建网络（数据卷保留）..."
  docker compose down --remove-orphans >> "$LOGFILE" 2>&1 || true
  if ! docker network rm "$NETWORK" >> "$LOGFILE" 2>&1 || ! docker compose up -d >> "$LOGFILE" 2>&1; then
    log "错误: 重启服务失败"
    exit 1
  fi
  log "升级完成"
  exit 0
fi
cat "$UPOUT" >> "$LOGFILE"
rm -f "$UPOUT"

log "升级完成"
`
}

func (e *systemUpgradeExecutor) buildHelperRunArgs(imageID, helperName string) ([]string, error) {
	if err := validateBackendContainerName(e.backendContainer); err != nil {
		return nil, err
	}
	args := []string{
		"run", "-d", "--rm", "--name", helperName,
		"--volumes-from", e.backendContainer,
		"-v", dockerSocketPath + ":" + dockerSocketPath,
	}
	if hostPathPattern.MatchString(e.hostDeployDir) && e.hostDeployDir != e.deployDir {
		args = append(args,
			"-v", e.hostDeployDir+":"+e.hostDeployDir,
			"-e", panelDeployDirEnv+"="+e.hostDeployDir)
	} else {
		args = append(args, "-e", panelDeployDirEnv+"="+e.deployDir)
	}
	if composeProjectPattern.MatchString(e.composeProject) {
		args = append(args, "-e", "COMPOSE_PROJECT_NAME="+e.composeProject)
	}
	return append(args,
		"--entrypoint", "/bin/sh", imageID,
		"-c", e.helperScript(),
	), nil
}

func (e *systemUpgradeExecutor) updateEnvVersion(envPath, version string) error {
	if err := validateUpgradeVersion(version); err != nil {
		return err
	}
	mode, err := fileModeOrDefault(envPath, 0o600)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(envPath)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	for _, key := range []string{"FLYX_VERSION", "FLUX_VERSION"} {
		found := false
		for i, line := range lines {
			if strings.HasPrefix(line, key+"=") {
				lines[i] = key + "=" + version
				found = true
			}
		}
		if !found {
			lines = append(lines, key+"="+version)
		}
	}
	return writeFileWithMode(envPath, []byte(strings.Join(lines, "\n")+"\n"), mode)
}

func (e *systemUpgradeExecutor) backupFile(path string) (string, error) {
	mode, err := fileModeOrDefault(path, 0o600)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	backupPath := path + ".upgrade.bak"
	if err := writeFileWithMode(backupPath, data, mode); err != nil {
		return "", err
	}
	return backupPath, nil
}

func (e *systemUpgradeExecutor) restoreBackup(path string) error {
	backupPath := path + ".upgrade.bak"
	mode, err := fileModeOrDefault(backupPath, 0o600)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(backupPath)
	if err != nil {
		return err
	}
	return writeFileWithMode(path, data, mode)
}

func (e *systemUpgradeExecutor) restoreUpgradeBackups(paths ...string) error {
	var errs []string
	for _, path := range paths {
		if err := e.restoreBackup(path); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", path, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func (e *systemUpgradeExecutor) replaceCompose(path string, data []byte) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return fmt.Errorf("compose asset is empty")
	}
	mode, err := fileModeOrDefault(path, 0o644)
	if err != nil {
		return err
	}
	return writeFileWithMode(path, data, mode)
}

func (h *Handler) buildSystemUpgradeDownloadURL(version, filename string) string {
	enabled, proxyURL := h.getGithubProxyConfig()
	base := fmt.Sprintf("%s/%s/releases/download/%s/%s", strings.TrimRight(systemUpgradeReleaseBaseURL, "/"), githubRepo, version, filename)
	if enabled && !isPanelGithubMirror(proxyURL) {
		return fmt.Sprintf("%s/%s", proxyURL, base)
	}
	return base
}

// fetchSystemUpgradeReleases lists releases through the panel's GitHub
// proxy when one is set, sharing the cache of node upgrades. A shared proxy
// often has its API quota used up, so a failure retries directly with the
// panel's own quota (and GITHUB_TOKEN).
func (h *Handler) fetchSystemUpgradeReleases() ([]githubRelease, error) {
	enabled, proxyURL := h.getGithubProxyConfig()
	if !enabled || isPanelGithubMirror(proxyURL) {
		return fetchGitHubReleases()
	}
	return cachedGitHubReleases(func() ([]githubRelease, error) {
		client := &http.Client{Timeout: 15 * time.Second}
		url := fmt.Sprintf("%s/%s/repos/%s/releases?per_page=50", proxyURL, strings.TrimRight(systemUpgradeAPIBaseURL, "/"), githubRepo)
		resp, err := systemUpgradeHTTPGet(client, url)
		if err == nil {
			releases, decodeErr := decodeGitHubReleases(resp)
			resp.Body.Close()
			if decodeErr == nil {
				return releases, nil
			}
			err = decodeErr
		}
		releases, directErr := requestGitHubReleases()
		if directErr != nil {
			return nil, fmt.Errorf("通过加速代理：%v；直连：%w", err, directErr)
		}
		return releases, nil
	})
}

func (h *Handler) resolveSystemUpgradeLatestReleaseByChannel(channel string) (string, error) {
	normalizedChannel := normalizeReleaseChannel(channel)
	releases, err := h.fetchSystemUpgradeReleases()
	if err != nil {
		// The github.com redirect is not rate limited like the API.
		if normalizedChannel == releaseChannelStable {
			if tag, redirectErr := latestStableFromRedirect(); redirectErr == nil {
				return tag, nil
			}
		}
		return "", err
	}

	for _, r := range releases {
		if r.Draft {
			continue
		}
		tag := strings.TrimSpace(r.TagName)
		if tag == "" {
			continue
		}
		if releaseChannelFromTag(tag) == normalizedChannel {
			return tag, nil
		}
	}

	return "", fmt.Errorf("未找到%s版本号", releaseChannelLabel(normalizedChannel))
}

func fileModeOrDefault(path string, fallback os.FileMode) (os.FileMode, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fallback, nil
		}
		return 0, err
	}
	return info.Mode().Perm(), nil
}

func writeFileWithMode(path string, data []byte, mode os.FileMode) error {
	if err := os.WriteFile(path, data, mode); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

func (e *systemUpgradeExecutor) currentBackendImage(ctx context.Context) (string, error) {
	if err := validateBackendContainerName(e.backendContainer); err != nil {
		return "", err
	}
	out, err := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.Image}}", e.backendContainer).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("inspect backend image failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	imageID := strings.TrimSpace(string(out))
	if imageID == "" {
		return "", fmt.Errorf("backend image id is empty")
	}
	return imageID, nil
}

func (e *systemUpgradeExecutor) startHelper(ctx context.Context, imageID, helperName string) (string, error) {
	args, err := e.buildHelperRunArgs(imageID, helperName)
	if err != nil {
		return "", err
	}
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("start helper failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	containerID := strings.TrimSpace(string(out))
	if containerID == "" {
		containerID = helperName
	}
	return containerID, nil
}

func (h *Handler) downloadReleaseAsset(version, filename string) ([]byte, error) {
	url := h.buildSystemUpgradeDownloadURL(version, filename)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := systemUpgradeHTTPGet(client, url)
	if err != nil {
		return nil, fmt.Errorf("下载%s失败: %v", filename, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("下载%s返回 %d: %s", filename, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSystemUpgradeComposeAssetBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取%s失败: %v", filename, err)
	}
	if len(body) > maxSystemUpgradeComposeAssetBytes {
		return nil, fmt.Errorf("下载%s过大", filename)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, fmt.Errorf("下载%s内容为空", filename)
	}
	return body, nil
}

func releasesForChannel(releases []githubRelease, channel string) []systemUpgradeReleaseData {
	channel = normalizeReleaseChannel(channel)
	items := make([]systemUpgradeReleaseData, 0, len(releases))
	for _, r := range releases {
		if r.Draft {
			continue
		}
		tag := strings.TrimSpace(r.TagName)
		if tag == "" {
			continue
		}
		itemChannel := releaseChannelFromTag(tag)
		if itemChannel != channel {
			continue
		}
		items = append(items, systemUpgradeReleaseData{
			Version:     tag,
			Name:        r.Name,
			PublishedAt: r.PublishedAt,
			Prerelease:  itemChannel == releaseChannelDev,
			Channel:     itemChannel,
		})
	}
	return items
}

func decodeSystemUpgradeRequest(r *http.Request, req *systemUpgradeRequest) error {
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	return decoder.Decode(req)
}

func systemUpgradeVersionResponse(current, channel, latest string, lookupErr error, capability systemUpgradeCapabilityData) systemUpgradeVersionData {
	data := systemUpgradeVersionData{
		CurrentVersion: current,
		LatestVersion:  latest,
		HasUpdate:      latest != "" && latest != current,
		Channel:        channel,
		Capability:     capability,
	}
	if lookupErr != nil {
		data.LatestVersion = ""
		data.HasUpdate = false
		data.Reason = lookupErr.Error()
	}
	return data
}

func (h *Handler) systemVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}

	channel := releaseChannelStable
	current := currentPanelVersion()
	exec := newSystemUpgradeExecutor()
	capability := exec.capability(r.Context())
	latest, err := h.resolveSystemUpgradeLatestReleaseByChannel(channel)
	data := systemUpgradeVersionResponse(current, channel, latest, err, capability)
	response.WriteJSON(w, response.OK(data))
}

func (h *Handler) systemCheckUpdates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}

	var req systemUpgradeRequest
	if err := decodeSystemUpgradeRequest(r, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	channel := normalizeReleaseChannel(req.Channel)
	current := currentPanelVersion()
	exec := newSystemUpgradeExecutor()
	capability := exec.capability(r.Context())

	githubReleases, err := h.fetchSystemUpgradeReleases()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, fmt.Sprintf("获取版本列表失败: %v", err)))
		return
	}
	releases := releasesForChannel(githubReleases, channel)
	latest := ""
	if len(releases) > 0 {
		latest = releases[0].Version
	}
	response.WriteJSON(w, response.OK(systemUpgradeCheckData{
		CurrentVersion: current,
		LatestVersion:  latest,
		HasUpdate:      latest != "" && latest != current,
		Channel:        channel,
		Capability:     capability,
		Releases:       releases,
	}))
}

func (h *Handler) systemUpgrade(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	if !h.systemUpgradeMu.TryLock() {
		response.WriteJSON(w, response.ErrDefault(systemUpgradeConflictError))
		return
	}
	defer h.systemUpgradeMu.Unlock()

	var req systemUpgradeRequest
	if err := decodeSystemUpgradeRequest(r, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	channel := normalizeReleaseChannel(req.Channel)
	exec := newSystemUpgradeExecutor()
	capability := exec.capability(r.Context())
	if !capability.Capable {
		response.WriteJSON(w, response.ErrDefault("当前环境不支持面板自升级: "+strings.Join(capability.Reasons, "; ")))
		return
	}
	version := strings.TrimSpace(req.Version)
	if version == "" {
		var err error
		version, err = h.resolveSystemUpgradeLatestReleaseByChannel(channel)
		if err != nil {
			response.WriteJSON(w, response.Err(-2, fmt.Sprintf("获取最新%s失败: %v", releaseChannelLabel(channel), err)))
			return
		}
	}
	imageID, err := exec.currentBackendImage(r.Context())
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}

	composePath := exec.composePath()
	envPath := exec.envPath()
	composeData, err := os.ReadFile(composePath)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, "读取compose失败: "+err.Error()))
		return
	}
	composeAsset := exec.selectComposeAsset(composeData)
	newCompose, err := h.downloadReleaseAsset(version, composeAsset)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if _, err := exec.backupFile(composePath); err != nil {
		response.WriteJSON(w, response.Err(-2, "备份compose失败: "+err.Error()))
		return
	}
	if _, err := exec.backupFile(envPath); err != nil {
		response.WriteJSON(w, response.Err(-2, "备份.env失败: "+err.Error()))
		return
	}
	if err := exec.replaceCompose(composePath, newCompose); err != nil {
		if restoreErr := exec.restoreUpgradeBackups(composePath, envPath); restoreErr != nil {
			err = fmt.Errorf("%v; 回滚失败: %v", err, restoreErr)
		}
		response.WriteJSON(w, response.Err(-2, "替换compose失败: "+err.Error()))
		return
	}
	if err := exec.updateEnvVersion(envPath, version); err != nil {
		if restoreErr := exec.restoreUpgradeBackups(composePath, envPath); restoreErr != nil {
			err = fmt.Errorf("%v; 回滚失败: %v", err, restoreErr)
		}
		response.WriteJSON(w, response.Err(-2, "更新版本配置失败: "+err.Error()))
		return
	}
	exec.composeProject = exec.detectComposeProject(r.Context())
	exec.hostDeployDir = exec.detectHostDeployDir(r.Context())
	helperName := fmt.Sprintf("flyx-upgrade-helper-%d", time.Now().Unix())
	helperContainer, err := exec.startHelper(r.Context(), imageID, helperName)
	if err != nil {
		if restoreErr := exec.restoreUpgradeBackups(composePath, envPath); restoreErr != nil {
			err = fmt.Errorf("%v; 回滚失败: %v", err, restoreErr)
		}
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}

	response.WriteJSON(w, response.OK(systemUpgradeRunData{
		Version:         version,
		Channel:         channel,
		ComposeAsset:    composeAsset,
		HelperContainer: helperContainer,
		BackendImageID:  imageID,
		Message:         systemUpgradeMessage,
	}))
}
