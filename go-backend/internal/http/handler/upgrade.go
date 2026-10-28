package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"go-backend/internal/http/response"
)

// failedForward tracks a forward that failed redeployment, for retry.
type failedForward struct {
	id      int64
	forward *forwardRecord
	err     error
}

// githubRepo is where releases (node installer, agent binaries, panel
// compose files) are fetched from. Release builds set it to the repository
// that built them via -ldflags "-X go-backend/internal/http/handler.githubRepo=owner/repo".
var githubRepo = "kure29/flyx"

const (
	githubAPIBase  = "https://api.github.com"
	githubHTMLBase = "https://github.com"
	upgradeTimeout = 5 * time.Minute
	batchWorkers   = 5

	releaseChannelStable = "stable"
	releaseChannelDev    = "dev"

	defaultGithubProxyEnabled = true
	defaultGithubProxyURL     = "https://gcode.hostcentral.cc"
)

var (
	stableVersionPattern = regexp.MustCompile(`^\d+(?:\.\d+)+$`)
	testKeywordPattern   = regexp.MustCompile(`(?i)(alpha|beta|rc)`)
)

const nodeOnlineRedeployCooldown = 30 * time.Second

type githubRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	PublishedAt string `json:"published_at"`
	Prerelease  bool   `json:"prerelease"`
	Draft       bool   `json:"draft"`
}

func normalizeReleaseChannel(channel string) string {
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case releaseChannelDev:
		return releaseChannelDev
	default:
		return releaseChannelStable
	}
}

func releaseChannelFromTag(tag string) string {
	normalized := strings.ToLower(strings.TrimSpace(tag))
	if normalized == "" {
		return releaseChannelDev
	}
	if testKeywordPattern.MatchString(normalized) {
		return releaseChannelDev
	}
	if stableVersionPattern.MatchString(normalized) {
		return releaseChannelStable
	}

	return releaseChannelDev
}

func releaseChannelLabel(channel string) string {
	if normalizeReleaseChannel(channel) == releaseChannelDev {
		return "测试版"
	}

	return "正式版"
}

func (h *Handler) getGithubProxyConfig() (enabled bool, proxyURL string) {
	enabled = defaultGithubProxyEnabled
	proxyURL = defaultGithubProxyURL

	if h == nil || h.repo == nil {
		return
	}

	if enabledCfg, err := h.repo.GetConfigByName("github_proxy_enabled"); err == nil && enabledCfg != nil {
		enabled = enabledCfg.Value != "false"
	}

	if urlCfg, err := h.repo.GetConfigByName("github_proxy_url"); err == nil && urlCfg != nil && urlCfg.Value != "" {
		proxyURL = strings.TrimSpace(urlCfg.Value)
		if !strings.HasPrefix(proxyURL, "http://") && !strings.HasPrefix(proxyURL, "https://") {
			proxyURL = "https://" + proxyURL
		}
		proxyURL = strings.TrimSuffix(proxyURL, "/")
	}

	return
}

func (h *Handler) buildGithubDownloadURL(version, filename string) string {
	enabled, proxyURL := h.getGithubProxyConfig()
	base := fmt.Sprintf("%s/%s/releases/download/%s/%s", githubHTMLBase, githubRepo, version, filename)

	if enabled {
		return fmt.Sprintf("%s/%s", proxyURL, base)
	}
	return base
}

// Unauthenticated GitHub API calls are limited to 60 an hour per IP, which a
// shared VPS address can use up on its own. Releases are cached, a stale copy
// is served when the API refuses, and the latest stable tag can also be read
// from the github.com "latest" redirect, which isn't rate limited. Setting
// GITHUB_TOKEN raises the API limit to 5000 an hour.
const releaseCacheTTL = 10 * time.Minute

var (
	releaseAPIBase  = githubAPIBase
	releaseHTMLBase = githubHTMLBase

	releaseCacheMu sync.Mutex
	releaseCacheAt time.Time
	releaseCache   []githubRelease
)

// fetchGitHubReleases returns the repository's 50 newest releases.
func fetchGitHubReleases() ([]githubRelease, error) {
	return cachedGitHubReleases(requestGitHubReleases)
}

// cachedGitHubReleases serves the shared release cache, refreshing it with
// request when it is stale and falling back to the stale copy on failure.
func cachedGitHubReleases(request func() ([]githubRelease, error)) ([]githubRelease, error) {
	releaseCacheMu.Lock()
	defer releaseCacheMu.Unlock()

	if releaseCache != nil && time.Since(releaseCacheAt) < releaseCacheTTL {
		return releaseCache, nil
	}
	releases, err := request()
	if err != nil {
		if releaseCache != nil {
			return releaseCache, nil
		}
		return nil, err
	}
	releaseCache, releaseCacheAt = releases, time.Now()
	return releases, nil
}

func requestGitHubReleases() ([]githubRelease, error) {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/repos/%s/releases?per_page=50", releaseAPIBase, githubRepo), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求GitHub API失败: %v", err)
	}
	defer resp.Body.Close()

	return decodeGitHubReleases(resp)
}

func decodeGitHubReleases(resp *http.Response) ([]githubRelease, error) {
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		if resp.StatusCode == http.StatusTooManyRequests || strings.Contains(strings.ToLower(string(body)), "rate limit") {
			return nil, fmt.Errorf("GitHub API 请求次数超限（未认证每个 IP 每小时 60 次），请稍后再试，或给面板设置环境变量 GITHUB_TOKEN")
		}
		return nil, fmt.Errorf("GitHub API返回 %d: %s", resp.StatusCode, string(body))
	}

	var releases []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("解析GitHub API响应失败: %v", err)
	}

	return releases, nil
}

// latestStableFromRedirect reads the latest release's tag from where
// github.com/<repo>/releases/latest redirects to (…/releases/tag/<tag>).
func latestStableFromRedirect() (string, error) {
	client := &http.Client{
		Timeout:       15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Head(fmt.Sprintf("%s/%s/releases/latest", releaseHTMLBase, githubRepo))
	if err != nil {
		return "", err
	}
	resp.Body.Close()

	location := resp.Header.Get("Location")
	_, tag, found := strings.Cut(location, "/releases/tag/")
	tag = strings.TrimSpace(tag)
	if !found || tag == "" || releaseChannelFromTag(tag) != releaseChannelStable {
		return "", fmt.Errorf("无法从 %q 读取版本号", location)
	}
	return tag, nil
}

func resolveLatestReleaseByChannel(channel string) (string, error) {
	normalizedChannel := normalizeReleaseChannel(channel)
	releases, err := fetchGitHubReleases()
	if err != nil {
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

func (h *Handler) nodeUpgrade(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}

	var req struct {
		ID      int64  `json:"id"`
		Version string `json:"version"`
		Channel string `json:"channel"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	if req.ID <= 0 {
		response.WriteJSON(w, response.ErrDefault("服务器ID无效"))
		return
	}

	channel := normalizeReleaseChannel(req.Channel)
	version := strings.TrimSpace(req.Version)
	if version == "" {
		var err error
		version, err = resolveLatestReleaseByChannel(channel)
		if err != nil {
			response.WriteJSON(w, response.Err(-2, fmt.Sprintf("获取最新%s失败: %v", releaseChannelLabel(channel), err)))
			return
		}
	}

	downloadURL := h.buildGithubDownloadURL(version, "gost-{ARCH}")
	checksumURL := h.buildGithubDownloadURL(version, "gost-{ARCH}.sha256")

	result, err := h.wsServer.SendCommand(req.ID, "UpgradeAgent", map[string]interface{}{
		"downloadUrl": downloadURL,
		"checksumUrl": checksumURL,
	}, upgradeTimeout)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, fmt.Sprintf("升级失败: %v", err)))
		return
	}
	h.markNodePendingUpgradeRedeploy(req.ID)

	response.WriteJSON(w, response.OK(map[string]interface{}{
		"version": version,
		"message": result.Message,
	}))
}

func resolveLatestRelease() (string, error) {
	return resolveLatestReleaseByChannel(releaseChannelStable)
}

func resolveLatestReleaseAPI() (string, error) {
	return resolveLatestReleaseByChannel(releaseChannelStable)
}

func (h *Handler) nodeBatchUpgrade(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}

	var req struct {
		IDs     []int64 `json:"ids"`
		Version string  `json:"version"`
		Channel string  `json:"channel"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	if len(req.IDs) == 0 {
		response.WriteJSON(w, response.ErrDefault("ids不能为空"))
		return
	}

	channel := normalizeReleaseChannel(req.Channel)
	version := strings.TrimSpace(req.Version)
	if version == "" {
		var err error
		version, err = resolveLatestReleaseByChannel(channel)
		if err != nil {
			response.WriteJSON(w, response.Err(-2, fmt.Sprintf("获取最新%s失败: %v", releaseChannelLabel(channel), err)))
			return
		}
	}

	downloadURL := h.buildGithubDownloadURL(version, "gost-{ARCH}")
	checksumURL := h.buildGithubDownloadURL(version, "gost-{ARCH}.sha256")

	type upgradeResult struct {
		ID      int64  `json:"id"`
		Success bool   `json:"success"`
		Message string `json:"message"`
	}

	results := make([]upgradeResult, len(req.IDs))
	sem := make(chan struct{}, batchWorkers)
	var wg sync.WaitGroup

	for i, id := range req.IDs {
		wg.Add(1)
		go func(index int, nodeID int64) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			result, err := h.wsServer.SendCommand(nodeID, "UpgradeAgent", map[string]interface{}{
				"downloadUrl": downloadURL,
				"checksumUrl": checksumURL,
			}, upgradeTimeout)
			if err != nil {
				results[index] = upgradeResult{ID: nodeID, Success: false, Message: err.Error()}
				return
			}
			h.markNodePendingUpgradeRedeploy(nodeID)
			results[index] = upgradeResult{ID: nodeID, Success: true, Message: result.Message}
		}(i, id)
	}
	wg.Wait()

	response.WriteJSON(w, response.OK(map[string]interface{}{
		"version": version,
		"results": results,
	}))
}

func (h *Handler) listReleases(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}

	var req struct {
		Channel string `json:"channel"`
	}
	if err := decodeJSON(r.Body, &req); err != nil && err != io.EOF {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}

	channel := normalizeReleaseChannel(req.Channel)

	releases, err := fetchGitHubReleases()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, fmt.Sprintf("获取版本列表失败: %v", err)))
		return
	}

	type releaseItem struct {
		Version     string `json:"version"`
		Name        string `json:"name"`
		PublishedAt string `json:"publishedAt"`
		Prerelease  bool   `json:"prerelease"`
		Channel     string `json:"channel"`
	}

	items := make([]releaseItem, 0, len(releases))
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
		items = append(items, releaseItem{
			Version:     tag,
			Name:        r.Name,
			PublishedAt: r.PublishedAt,
			Prerelease:  itemChannel == releaseChannelDev,
			Channel:     itemChannel,
		})
	}

	response.WriteJSON(w, response.OK(items))
}

func (h *Handler) nodeRollback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}

	var req struct {
		ID int64 `json:"id"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	if req.ID <= 0 {
		response.WriteJSON(w, response.ErrDefault("服务器ID无效"))
		return
	}

	result, err := h.wsServer.SendCommand(req.ID, "RollbackAgent", map[string]interface{}{}, 30*time.Second)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, fmt.Sprintf("回退失败: %v", err)))
		return
	}

	response.WriteJSON(w, response.OK(map[string]interface{}{
		"message": result.Message,
	}))
}

func (h *Handler) markNodePendingUpgradeRedeploy(nodeID int64) {
	if h == nil || nodeID <= 0 {
		return
	}
	h.upgradeMu.Lock()
	h.pendingUpgradeRedeploy[nodeID] = struct{}{}
	h.upgradeMu.Unlock()
}

func (h *Handler) consumeNodePendingUpgradeRedeploy(nodeID int64) bool {
	if h == nil || nodeID <= 0 {
		return false
	}
	h.upgradeMu.Lock()
	_, ok := h.pendingUpgradeRedeploy[nodeID]
	if ok {
		delete(h.pendingUpgradeRedeploy, nodeID)
	}
	h.upgradeMu.Unlock()
	return ok
}

func (h *Handler) onNodeOnline(nodeID int64) {
	if h == nil || h.repo == nil || h.wsServer == nil {
		return
	}
	h.notifyNodeOnline(nodeID)
	if node, err := h.getNodeRecord(nodeID); err == nil && (node == nil || node.Status != 1) {
		return // The next connection will resume any pending reconciliation.
	}
	h.invalidateNodeQuotaAcknowledgements(nodeID)
	go func() {
		h.reconcileNodeKernels(nodeID)
		// Probe once the kernels are restored: earlier, the node's ports
		// would still look closed.
		h.scheduleNodeReconnectProbe(nodeID)
	}()
	if !h.startNodeOnlineRedeploy(nodeID, time.Now()) {
		return
	}
	defer h.finishNodeOnlineRedeploy(nodeID)

	// A fresh agent needs a full restore. Shared failures remain persisted and
	// are retried by maintenance without restarting acknowledged services.
	h.reconcileSharedNodeRuntime(nodeID)
	if !h.redeployLocalNodeRuntime(nodeID) {
		h.scheduleNodeLocalRuntimeRetry(nodeID)
	} else {
		h.clearNodeLocalRuntimeRetry(nodeID)
	}
}

// Local retries are separate from reconnect reconciliation: a failed local
// forward must not cause every shared listener to be replayed every 30 seconds.
func (h *Handler) scheduleNodeLocalRuntimeRetry(nodeID int64) {
	h.upgradeMu.Lock()
	defer h.upgradeMu.Unlock()
	if h.nodeLocalRuntimeRetryQueued == nil {
		h.nodeLocalRuntimeRetryQueued = make(map[int64]struct{})
	}
	if _, queued := h.nodeLocalRuntimeRetryQueued[nodeID]; queued {
		return
	}
	h.nodeLocalRuntimeRetryQueued[nodeID] = struct{}{}
	time.AfterFunc(nodeOnlineRedeployCooldown, func() {
		h.upgradeMu.Lock()
		_, queued := h.nodeLocalRuntimeRetryQueued[nodeID]
		delete(h.nodeLocalRuntimeRetryQueued, nodeID)
		h.upgradeMu.Unlock()
		if !queued {
			return
		}
		h.retryNodeLocalRuntime(nodeID)
	})
}

func (h *Handler) clearNodeLocalRuntimeRetry(nodeID int64) {
	h.upgradeMu.Lock()
	delete(h.nodeLocalRuntimeRetryQueued, nodeID)
	h.upgradeMu.Unlock()
}

func (h *Handler) retryNodeLocalRuntime(nodeID int64) {
	if h == nil || h.repo == nil || h.wsServer == nil {
		return
	}
	if node, err := h.getNodeRecord(nodeID); err == nil && (node == nil || node.Status != 1) {
		return
	}
	h.upgradeMu.Lock()
	if _, inFlight := h.nodeOnlineRedeploying[nodeID]; inFlight {
		h.upgradeMu.Unlock()
		h.scheduleNodeLocalRuntimeRetry(nodeID)
		return
	}
	if h.nodeOnlineRedeploying == nil {
		h.nodeOnlineRedeploying = make(map[int64]struct{})
	}
	h.nodeOnlineRedeploying[nodeID] = struct{}{}
	h.upgradeMu.Unlock()
	defer h.finishNodeOnlineRedeploy(nodeID)
	if !h.redeployLocalNodeRuntime(nodeID) {
		h.scheduleNodeLocalRuntimeRetry(nodeID)
	} else {
		h.clearNodeLocalRuntimeRetry(nodeID)
	}
}

func (h *Handler) startNodeOnlineRedeploy(nodeID int64, now time.Time) bool {
	if h == nil || nodeID <= 0 {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}

	h.upgradeMu.Lock()
	defer h.upgradeMu.Unlock()
	if h.pendingUpgradeRedeploy == nil {
		h.pendingUpgradeRedeploy = make(map[int64]struct{})
	}
	if h.nodeOnlineRedeployAt == nil {
		h.nodeOnlineRedeployAt = make(map[int64]time.Time)
	}
	if h.nodeOnlineRedeployQueued == nil {
		h.nodeOnlineRedeployQueued = make(map[int64]struct{})
	}
	if h.nodeOnlineRedeploying == nil {
		h.nodeOnlineRedeploying = make(map[int64]struct{})
	}

	_, pendingUpgrade := h.pendingUpgradeRedeploy[nodeID]
	lastRedeployAt := h.nodeOnlineRedeployAt[nodeID]
	_, inFlight := h.nodeOnlineRedeploying[nodeID]
	if fireAt, start := nextNodeOnlineRedeployFireAt(lastRedeployAt, now, pendingUpgrade, inFlight); !start {
		h.queueNodeOnlineRedeployLocked(nodeID, fireAt)
		return false
	}

	delete(h.pendingUpgradeRedeploy, nodeID)
	h.nodeOnlineRedeployAt[nodeID] = now
	h.nodeOnlineRedeploying[nodeID] = struct{}{}
	return true
}

func nextNodeOnlineRedeployFireAt(lastRedeployAt, now time.Time, pendingUpgrade bool, inFlight bool) (time.Time, bool) {
	if now.IsZero() {
		now = time.Now()
	}
	if inFlight {
		fireAt := now.Add(nodeOnlineRedeployCooldown)
		if !lastRedeployAt.IsZero() {
			cooldownAt := lastRedeployAt.Add(nodeOnlineRedeployCooldown)
			if cooldownAt.After(now) {
				fireAt = cooldownAt
			}
		}
		return fireAt, false
	}
	if !pendingUpgrade && !lastRedeployAt.IsZero() && now.Sub(lastRedeployAt) < nodeOnlineRedeployCooldown {
		return lastRedeployAt.Add(nodeOnlineRedeployCooldown), false
	}
	return time.Time{}, true
}

func (h *Handler) queueNodeOnlineRedeployLocked(nodeID int64, fireAt time.Time) {
	if h == nil || nodeID <= 0 {
		return
	}
	if h.nodeOnlineRedeployQueued == nil {
		h.nodeOnlineRedeployQueued = make(map[int64]struct{})
	}
	if _, queued := h.nodeOnlineRedeployQueued[nodeID]; queued {
		return
	}
	if fireAt.IsZero() {
		fireAt = time.Now().Add(nodeOnlineRedeployCooldown)
	}
	delay := time.Until(fireAt)
	if delay < 0 {
		delay = 0
	}
	h.nodeOnlineRedeployQueued[nodeID] = struct{}{}
	time.AfterFunc(delay, func() {
		h.upgradeMu.Lock()
		delete(h.nodeOnlineRedeployQueued, nodeID)
		h.upgradeMu.Unlock()
		h.onNodeOnline(nodeID)
	})
}

func (h *Handler) finishNodeOnlineRedeploy(nodeID int64) {
	if h == nil || nodeID <= 0 {
		return
	}
	h.upgradeMu.Lock()
	delete(h.nodeOnlineRedeploying, nodeID)
	h.upgradeMu.Unlock()
}

func (h *Handler) redeployNodeRuntimeAfterUpgrade(nodeID int64) bool {
	sharedOK := h.reconcileSharedNodeRuntime(nodeID)
	localOK := h.redeployLocalNodeRuntime(nodeID)
	return sharedOK && localOK
}

func (h *Handler) reconcileSharedNodeRuntime(nodeID int64) bool {
	succeeded := true
	if err := h.reconcilePeerShareResourcesOnNode(nodeID); err != nil {
		fmt.Printf("reconnect shared resource reconciliation failed on node %d: %v\n", nodeID, err)
		succeeded = false
	}
	if err := h.reconcilePeerShareRoleRuntimesOnNode(nodeID); err != nil {
		fmt.Printf("reconnect shared role reconciliation failed on node %d: %v\n", nodeID, err)
		succeeded = false
	}
	return succeeded
}

func (h *Handler) redeployLocalNodeRuntime(nodeID int64) bool {
	tunnelIDs, err := h.repo.ListActiveTunnelIDsByNode(nodeID)
	if err != nil {
		fmt.Printf("post-upgrade redeploy: list tunnels for node %d failed: %v\n", nodeID, err)
		return false
	}
	forwardIDs, err := h.repo.ListForwardIDsByNode(nodeID)
	if err != nil {
		fmt.Printf("post-upgrade redeploy: list forwards for node %d failed: %v\n", nodeID, err)
		return false
	}

	// First pass: deploy everything
	tunnelFailed := make(map[int64]struct{})
	for _, tunnelID := range tunnelIDs {
		if err := h.redeployTunnelAndForwards(tunnelID); err != nil {
			tunnelFailed[tunnelID] = struct{}{}
			fmt.Printf("post-upgrade redeploy: tunnel %d failed on node %d: %v\n", tunnelID, nodeID, err)
		}
	}

	// Collect forwards that failed independently (not skipped due to tunnel failure)
	var failedForwards []failedForward

	for _, forwardID := range forwardIDs {
		forward, getErr := h.getForwardRecord(forwardID)
		if getErr != nil || forward == nil {
			continue
		}
		if _, skipped := tunnelFailed[forward.TunnelID]; skipped {
			continue
		}
		if err := h.syncForwardServices(forward, "UpdateService", true); err != nil {
			failedForwards = append(failedForwards, failedForward{id: forwardID, forward: forward, err: err})
			fmt.Printf("post-upgrade redeploy: forward %d failed on node %d: %v\n", forwardID, nodeID, err)
		}
	}

	// Retry failed items with exponential backoff (max 3 attempts)
	return h.retryFailedRedeploys(nodeID, tunnelFailed, failedForwards)
}

// isRetryableError returns true if the error looks transient and worth retrying.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	// Skip non-retryable errors: not-found, already-exists, validation errors
	if strings.Contains(msg, "not found") || strings.Contains(msg, "不存在") {
		return false
	}
	if strings.Contains(msg, "already exists") || strings.Contains(msg, "已存在") {
		return false
	}
	// Everything else (timeout, connection lost, port in use, etc.) is retryable
	return true
}

// retryFailedRedeploys retries failed tunnels and forwards with exponential backoff.
func (h *Handler) retryFailedRedeploys(nodeID int64, tunnelFailed map[int64]struct{}, failedForwards []failedForward) bool {
	if len(tunnelFailed) == 0 && len(failedForwards) == 0 {
		return true
	}

	permanentFailure := false
	const maxRetries = 3
	baseDelay := time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		delay := baseDelay * time.Duration(1<<uint(attempt-1)) // 1s, 2s, 4s
		time.Sleep(delay)

		// Retry failed tunnels
		for tunnelID := range tunnelFailed {
			if err := h.redeployTunnelAndForwards(tunnelID); err == nil {
				delete(tunnelFailed, tunnelID)
				fmt.Printf("post-upgrade redeploy retry: tunnel %d succeeded on node %d (attempt %d)\n", tunnelID, nodeID, attempt)
			} else if !isRetryableError(err) {
				permanentFailure = true
				delete(tunnelFailed, tunnelID) // Preserve failure while avoiding immediate retries.
			} else {
				fmt.Printf("post-upgrade redeploy retry: tunnel %d still failing on node %d (attempt %d): %v\n", tunnelID, nodeID, attempt, err)
			}
		}

		// Retry failed forwards
		var stillFailed []failedForward
		for _, ff := range failedForwards {
			if _, skipped := tunnelFailed[ff.forward.TunnelID]; skipped {
				stillFailed = append(stillFailed, ff) // Tunnel still failed, skip forward
				continue
			}
			if err := h.syncForwardServices(ff.forward, "UpdateService", true); err == nil {
				fmt.Printf("post-upgrade redeploy retry: forward %d succeeded on node %d (attempt %d)\n", ff.id, nodeID, attempt)
			} else if !isRetryableError(err) {
				permanentFailure = true // Keep reconciliation pending for a later reconnect.
			} else {
				stillFailed = append(stillFailed, ff)
				fmt.Printf("post-upgrade redeploy retry: forward %d still failing on node %d (attempt %d): %v\n", ff.id, nodeID, attempt, err)
			}
		}
		failedForwards = stillFailed

		if len(tunnelFailed) == 0 && len(failedForwards) == 0 {
			fmt.Printf("post-upgrade redeploy retry: all items recovered on node %d\n", nodeID)
			return !permanentFailure
		}
	}

	// Final summary
	for tunnelID := range tunnelFailed {
		fmt.Printf("post-upgrade redeploy: tunnel %d permanently failed on node %d after retries\n", tunnelID, nodeID)
	}
	for _, ff := range failedForwards {
		fmt.Printf("post-upgrade redeploy: forward %d permanently failed on node %d after retries\n", ff.id, nodeID)
	}
	return false
}
