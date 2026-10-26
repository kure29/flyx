package handler

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/store/repo"
)

func (h *Handler) nodeCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req map[string]interface{}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	notifyEvents, err := nodeNotifyEventsFromRequest(req)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	name := asString(req["name"])
	serverIP := asString(req["serverIp"])
	if name == "" || serverIP == "" {
		response.WriteJSON(w, response.ErrDefault("服务器名称和地址不能为空"))
		return
	}
	if err := IsValidNodeAddress(serverIP); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}

	now := time.Now().UnixMilli()
	inx := h.repo.NextIndex("node")
	forwardMode := defaultNodeForwardMode(asString(req["forwardMode"]))
	status := 0
	if forwardMode == "nftables" {
		status = 1
	}
	if err := h.repo.CreateNode(
		name,
		randomToken(16),
		serverIP,
		nullableText(asString(req["serverIpV4"])),
		nullableText(asString(req["serverIpV6"])),
		defaultString(asString(req["port"]), "1000-65535"),
		nullableText(asString(req["interfaceName"])),
		nullableText(""),
		nullableText(strings.TrimSpace(asString(req["remark"]))),
		nullableUnixMilli(asInt64(req["expiryTime"], 0)),
		nullableText(normalizeNodeRenewalCycle(asString(req["renewalCycle"]))),
		asInt(req["http"], 0),
		asInt(req["tls"], 0),
		asInt(req["socks"], 0),
		now,
		status,
		defaultString(asString(req["tcpListenAddr"]), "[::]"),
		defaultString(asString(req["udpListenAddr"]), "[::]"),
		inx,
		asInt(req["isRemote"], 0),
		nullableText(asString(req["remoteUrl"])),
		nullableText(asString(req["remoteToken"])),
		nullableText(asString(req["remoteConfig"])),
		nullableText(asString(req["extraIPs"])),
		forwardMode,
		notifyEvents,
	); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	nodeID, err := h.findCreatedNodeID(name, serverIP)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if err := h.persistNodeSSHConfig(nodeID, req, forwardMode, now, false); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) nodeUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req map[string]interface{}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	notifyEvents, err := nodeNotifyEventsFromRequest(req)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	id := asInt64(req["id"], 0)
	if id <= 0 {
		response.WriteJSON(w, response.ErrDefault("服务器ID不能为空"))
		return
	}

	currentStatus, currentHTTP, currentTLS, currentSocks, err := h.repo.GetNodeStatusFields(id)
	if err != nil {
		if err == sql.ErrNoRows {
			response.WriteJSON(w, response.ErrDefault("服务器不存在"))
			return
		}
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}

	newHTTP := asInt(req["http"], currentHTTP)
	newTLS := asInt(req["tls"], currentTLS)
	newSocks := asInt(req["socks"], currentSocks)
	forwardMode := defaultNodeForwardMode(strings.TrimSpace(asString(req["forwardMode"])))
	currentForwardMode, err := h.repo.GetNodeForwardMode(id)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	serverIP := asString(req["serverIp"])
	if serverIP != "" {
		if err := IsValidNodeAddress(serverIP); err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
	}
	usesNftablesRuntime := forwardMode == "nftables" || defaultNodeForwardMode(currentForwardMode) == "nftables"
	if currentStatus == 1 && !usesNftablesRuntime && (newHTTP != currentHTTP || newTLS != currentTLS || newSocks != currentSocks) {
		if err := h.applyNodeProtocolChange(id, newHTTP, newTLS, newSocks); err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
	}

	now := time.Now().UnixMilli()
	if err := h.repo.UpdateNode(id,
		asString(req["name"]),
		serverIP,
		nullableText(asString(req["serverIpV4"])),
		nullableText(asString(req["serverIpV6"])),
		defaultString(asString(req["port"]), "1000-65535"),
		nullableText(asString(req["interfaceName"])),
		nullableText(asString(req["extraIPs"])),
		nullableText(strings.TrimSpace(asString(req["remark"]))),
		nullableUnixMilli(asInt64(req["expiryTime"], 0)),
		nullableText(normalizeNodeRenewalCycle(asString(req["renewalCycle"]))),
		forwardMode,
		newHTTP,
		newTLS,
		newSocks,
		defaultString(asString(req["tcpListenAddr"]), "[::]"),
		defaultString(asString(req["udpListenAddr"]), "[::]"),
		now,
		notifyEvents,
	); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if err := h.persistNodeSSHConfig(id, req, forwardMode, now, true); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if forwardMode == "nftables" && currentStatus != 1 {
		if err := h.repo.UpdateNodeStatus(id, 1); err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) findCreatedNodeID(name, serverIP string) (int64, error) {
	if h == nil || h.repo == nil {
		return 0, errors.New("handler not initialized")
	}
	nodes, err := h.repo.ListNodes()
	if err != nil {
		return 0, err
	}
	for i := len(nodes) - 1; i >= 0; i-- {
		item := nodes[i]
		if asString(item["name"]) != name {
			continue
		}
		if asString(item["serverIp"]) != serverIP {
			continue
		}
		if nodeID := asInt64(item["id"], 0); nodeID > 0 {
			return nodeID, nil
		}
	}
	return 0, errors.New("服务器创建成功，但未能查询到服务器记录")
}

func (h *Handler) persistNodeSSHConfig(nodeID int64, req map[string]interface{}, forwardMode string, now int64, preserveSecrets bool) error {
	if h == nil || h.repo == nil {
		return errors.New("handler not initialized")
	}
	if nodeID <= 0 {
		return errors.New("服务器ID不能为空")
	}
	if forwardMode != "nftables" {
		return h.repo.DeleteNodeSSHConfig(nodeID)
	}
	cfgMap := asMap(req["sshConfig"])
	host := strings.TrimSpace(asString(cfgMap["host"]))
	if host == "" {
		host = strings.TrimSpace(asString(req["serverIp"]))
	}
	port := asInt(cfgMap["port"], 22)
	username := strings.TrimSpace(asString(cfgMap["username"]))
	authType := strings.TrimSpace(asString(cfgMap["authType"]))
	password := asString(cfgMap["password"])
	privateKey := asString(cfgMap["privateKey"])
	passphrase := asString(cfgMap["passphrase"])
	sudoMode := strings.TrimSpace(asString(cfgMap["sudoMode"]))

	if preserveSecrets {
		existing, err := h.repo.GetNodeSSHConfig(nodeID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if existing != nil {
			if host == "" {
				host = strings.TrimSpace(existing.Host)
			}
			if port <= 0 {
				port = existing.Port
			}
			if username == "" {
				username = strings.TrimSpace(existing.Username)
			}
			if authType == "" {
				authType = strings.TrimSpace(existing.AuthType)
			}
			if strings.TrimSpace(password) == "" && existing.Password.Valid {
				password = existing.Password.String
			}
			if strings.TrimSpace(privateKey) == "" && existing.PrivateKey.Valid {
				privateKey = existing.PrivateKey.String
			}
			if strings.TrimSpace(passphrase) == "" && existing.Passphrase.Valid {
				passphrase = existing.Passphrase.String
			}
			if sudoMode == "" {
				sudoMode = strings.TrimSpace(existing.SudoMode)
			}
		}
	}

	if host == "" || username == "" {
		return errors.New("nftables 服务器 SSH 配置不完整")
	}
	if port <= 0 || port > 65535 {
		return errors.New("nftables 服务器 SSH 端口无效")
	}

	authType = strings.ToLower(authType)
	switch authType {
	case "password":
		if strings.TrimSpace(password) == "" {
			return errors.New("nftables 服务器 SSH 密码不能为空")
		}
		privateKey = ""
	case "private_key", "":
		authType = "private_key"
		if strings.TrimSpace(privateKey) == "" {
			return errors.New("nftables 服务器 SSH 私钥不能为空")
		}
		password = ""
	default:
		return errors.New("nftables 服务器 SSH 认证方式无效")
	}

	switch strings.ToLower(sudoMode) {
	case "", "none":
		sudoMode = "none"
	case "sudo", "sudo_su":
	default:
		return errors.New("nftables 服务器 sudo 模式无效")
	}

	return h.repo.UpsertNodeSSHConfig(nodeID, repo.NftSSHConfigInput{
		Host:       host,
		Port:       port,
		Username:   username,
		AuthType:   authType,
		Password:   password,
		PrivateKey: privateKey,
		Passphrase: passphrase,
		SudoMode:   sudoMode,
	}, now)
}

func (h *Handler) nodeDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	id := idFromBody(r, w)
	if id <= 0 {
		return
	}
	if err := h.deleteNodeByID(id); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) nodeInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}

	var req struct {
		ID      int64  `json:"id"`
		Channel string `json:"channel"`
		// Mode "docker" returns a docker run command instead of install.sh.
		Mode string `json:"mode"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	if req.ID <= 0 {
		response.WriteJSON(w, response.ErrDefault("参数错误"))
		return
	}

	channel := normalizeReleaseChannel(req.Channel)
	version, err := resolveLatestReleaseByChannel(channel)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, fmt.Sprintf("获取最新%s失败: %v", releaseChannelLabel(channel), err)))
		return
	}

	secret, err := h.repo.GetNodeSecret(req.ID)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault("服务器不存在"))
		return
	}
	panelAddr, err := h.repo.GetViteConfigValue("ip")
	if err != nil {
		if err == sql.ErrNoRows {
			response.WriteJSON(w, response.ErrDefault("请先前往网站配置中设置ip"))
			return
		}
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if req.Mode == "docker" {
		response.WriteJSON(w, response.OK(dockerInstallCommand(version, processServerAddress(panelAddr), secret)))
		return
	}
	enabled, proxyURL := h.getGithubProxyConfig()

	var cmd string
	if enabled {
		cmd = fmt.Sprintf("curl -L %s/https://github.com/%s/releases/download/%s/install.sh -o ./install.sh && chmod +x ./install.sh && PROXY_ENABLED=true PROXY_URL=%s VERSION=%s ./install.sh -a %s -s %s",
			proxyURL, githubRepo, version, proxyURL, version, processServerAddress(panelAddr), secret)
	} else {
		cmd = fmt.Sprintf("curl -L https://github.com/%s/releases/download/%s/install.sh -o ./install.sh && chmod +x ./install.sh && PROXY_ENABLED=false VERSION=%s ./install.sh -a %s -s %s",
			githubRepo, version, version, processServerAddress(panelAddr), secret)
	}
	response.WriteJSON(w, response.OK(cmd))
}

// dockerInstallCommand runs the agent image published with the release.
// Host networking lets forwards listen on any port, NET_ADMIN is for the
// nftables counters and whitelists, and the restart policy brings the
// container back after a one-click upgrade. An existing container of the
// same name is replaced, so the command also reinstalls.
func dockerInstallCommand(version, panelAddr, secret string) string {
	owner := strings.ToLower(strings.SplitN(githubRepo, "/", 2)[0])
	return fmt.Sprintf("docker rm -f flux_agent >/dev/null 2>&1; docker run -d --name flux_agent --restart unless-stopped --network host --cap-add NET_ADMIN -v /etc/flux_agent:/etc/flux_agent -e FLUX_AGENT_ADDR=%s -e FLUX_AGENT_SECRET=%s ghcr.io/%s/flux-agent:%s",
		panelAddr, secret, owner, version)
}

func (h *Handler) nodeUpdateOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		Nodes []struct {
			ID  int64 `json:"id"`
			Inx int   `json:"inx"`
		} `json:"nodes"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	for _, n := range req.Nodes {
		h.repo.UpdateNodeOrder(n.ID, n.Inx, time.Now().UnixMilli())
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) nodeDismissExpiryReminder(w http.ResponseWriter, r *http.Request) {
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
		response.WriteJSON(w, response.ErrDefault("服务器ID不能为空"))
		return
	}
	if err := h.repo.UpdateNodeExpiryReminderDismissed(req.ID, 1); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) nodeBatchDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	ids := idsFromBody(r, w)
	if ids == nil {
		return
	}
	for _, id := range ids {
		_ = h.deleteNodeByID(id)
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) nodeCheckStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	items, err := h.repo.ListNodes()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	for _, item := range items {
		item["notifyEvents"] = parseNodeNotifyEvents(asString(item["notifyEvents"]))
	}
	response.WriteJSON(w, response.OK(items))
}

func (h *Handler) deleteNodeByID(id int64) error {
	return h.repo.DeleteNodeCascade(id)
}

func normalizeNodeRenewalCycle(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "month", "quarter", "year":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return ""
	}
}

func defaultNodeForwardMode(mode string) string {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case "nftables":
		return "nftables"
	default:
		return "agent"
	}
}
