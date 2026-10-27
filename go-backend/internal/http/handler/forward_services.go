package handler

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func (h *Handler) syncForwardServices(forward *forwardRecord, method string, allowFallbackAdd bool) error {
	_, err := h.syncForwardServicesWithWarnings(forward, method, allowFallbackAdd)
	return err
}

func (h *Handler) syncForwardServicesWithWarnings(forward *forwardRecord, method string, allowFallbackAdd bool) ([]string, error) {
	if h == nil || forward == nil {
		return nil, errors.New("invalid forward sync context")
	}

	tunnel, err := h.getTunnelRecord(forward.TunnelID)
	if err != nil {
		return nil, err
	}
	nftMode, entryNodeIDs, err := h.tunnelUsesNftables(forward.TunnelID)
	if err != nil {
		return nil, err
	}
	if nftMode {
		if err := h.validateNftablesForwardRequest(tunnel, forward.RemoteAddr, entryNodeIDs); err != nil {
			return nil, err
		}
		if len(entryNodeIDs) == 0 {
			return nil, errors.New("nftables 转发缺少入口服务器")
		}
		return nil, h.syncNftablesNode(entryNodeIDs[0])
	}
	if tunnelIsRealm(tunnel) {
		return h.syncRealmForward(forward, nil)
	}
	ports, err := h.listForwardPorts(forward.ID)
	if err != nil {
		return nil, err
	}
	if len(ports) == 0 {
		return nil, errors.New("转发入口端口不存在")
	}
	warnings := make([]string, 0)

	// Resolve user tunnel first so runtime service name can carry the real user_tunnel id.
	userTunnelID, utLimiterID, utSpeed, err := h.resolveUserTunnelAndLimiter(forward.UserID, forward.TunnelID)
	if err != nil {
		return nil, err
	}

	// Determine limiter from forward's SpeedID first, fallback to UserTunnel's limiter
	var limiterID *int64
	var speed *int

	if forward.SpeedID.Valid && forward.SpeedID.Int64 > 0 {
		// Forward has its own speed limit
		speedVal, err := h.repo.GetSpeedLimitSpeed(forward.SpeedID.Int64)
		if err == nil && speedVal > 0 {
			limiterID = &forward.SpeedID.Int64
			speed = &speedVal
		}
	}

	if limiterID == nil {
		// Fall back to UserTunnel speed limit
		limiterID = utLimiterID
		speed = utSpeed
	}

	var ipSpeed *int
	if forward.IPSpeedID.Valid && forward.IPSpeedID.Int64 > 0 {
		if speedVal, err := h.repo.GetSpeedLimitSpeed(forward.IPSpeedID.Int64); err == nil && speedVal > 0 {
			ipSpeed = &speedVal
		}
	}

	serviceBase := buildForwardServiceBaseWithResolvedUserTunnel(forward.ID, forward.UserID, userTunnelID)

	user, err := h.repo.GetUserByID(forward.UserID)
	if err != nil {
		return nil, err
	}

	userMaxConn := 0
	if user != nil && user.MaxConn > 0 {
		userMaxConn = user.MaxConn
	}
	connLimiterConfigs := buildConnLimiterConfigs(forward, userMaxConn)

	for _, fp := range ports {
		runtimeLimiters := forwardRuntimeLimiters{ConnLimiter: joinLimiterNames(connLimiterConfigs)}
		trafficLimiterNames := make([]string, 0, 2)
		if limiterID != nil && speed != nil {
			totalLimiterName := strconv.FormatInt(*limiterID, 10)
			if err := h.ensureLimiterOnNode(fp.NodeID, *limiterID, *speed); err != nil {
				// If the limiter push fails because the node is offline, skip it with a warning
				if isNodeOfflineOrTimeoutError(err) {
					node, _ := h.getNodeRecord(fp.NodeID)
					nodeName := fmt.Sprintf("%d", fp.NodeID)
					if node != nil && strings.TrimSpace(node.Name) != "" {
						nodeName = strings.TrimSpace(node.Name)
					}
					warnings = append(warnings, fmt.Sprintf("服务器 %s 不在线，已跳过下发", nodeName))
					continue
				}
				return nil, err
			}
			trafficLimiterNames = append(trafficLimiterNames, totalLimiterName)
		}
		if ipSpeed != nil {
			ruleLimiterName := fmt.Sprintf("rule_traffic_limit_%d", forward.ID)
			if err := h.ensureTrafficLimiterOnNode(fp.NodeID, ruleLimiterName, nil, ipSpeed); err != nil {
				// If the limiter push fails because the node is offline, skip it with a warning
				if isNodeOfflineOrTimeoutError(err) {
					node, _ := h.getNodeRecord(fp.NodeID)
					nodeName := fmt.Sprintf("%d", fp.NodeID)
					if node != nil && strings.TrimSpace(node.Name) != "" {
						nodeName = strings.TrimSpace(node.Name)
					}
					warnings = append(warnings, fmt.Sprintf("服务器 %s 不在线，已跳过下发", nodeName))
					continue
				}
				return nil, err
			}
			trafficLimiterNames = append(trafficLimiterNames, ruleLimiterName)
		}
		runtimeLimiters.TrafficLimiter = strings.Join(trafficLimiterNames, ",")

		for _, connLimiterConfig := range connLimiterConfigs {
			if err := h.ensureConnLimiterOnNode(fp.NodeID, connLimiterConfig); err != nil {
				warnings = append(warnings, fmt.Sprintf("服务器 %d 连接限制器下发失败: %v", fp.NodeID, err))
			}
		}

		node, err := h.getNodeRecord(fp.NodeID)
		if err != nil {
			return nil, err
		}
		services := buildForwardServiceConfigs(serviceBase, forward, tunnel, node, fp.Port, strings.TrimSpace(fp.InIP), runtimeLimiters)
		_, err = h.sendNodeCommand(node.ID, method, services, true, false)
		if err != nil && allowFallbackAdd && method == "UpdateService" {
			if isNotFoundError(err) {
				if delErr := h.deleteForwardServicesOnNode(forward, node.ID); delErr != nil && !isNotFoundError(delErr) {
					return warnings, fmt.Errorf("服务器 %s 清理旧服务失败: %w", node.Name, delErr)
				}
			}
			_, err = h.sendNodeCommand(node.ID, "AddService", services, true, false)
		}
		if err != nil && strings.EqualFold(strings.TrimSpace(method), "UpdateService") && isAddressAlreadyInUseError(err) {
			err = h.rebindForwardServiceOnSelfOccupiedPort(forward, node, fp.Port, services)
		}
		if err != nil && strings.EqualFold(strings.TrimSpace(method), "UpdateService") && isCannotAssignRequestedAddressError(err) {
			var warning string
			warning, err = h.fallbackForwardPortToDefaultBind(forward, tunnel, node, fp, serviceBase, runtimeLimiters)
			if err == nil && warning != "" {
				warnings = append(warnings, warning)
			}
		}
		// When a node is offline, skip it with a warning instead of failing.
		// This lets users modify forward rules even when some entry nodes are down.
		if err != nil && isNodeOfflineOrTimeoutError(err) {
			warnings = append(warnings, fmt.Sprintf("服务器 %s 不在线，已跳过下发", node.Name))
			continue
		}
		if err != nil {
			return warnings, fmt.Errorf("服务器 %s 下发失败: %w", node.Name, err)
		}
	}

	// Keep paused forwards paused after UpdateService/AddService, since agent-side UpdateService
	// always restarts services.
	if forward.Status != 1 {
		if err := h.controlForwardServices(forward, "PauseService", false); err != nil {
			return warnings, err
		}
	}
	return warnings, nil
}

func (h *Handler) fallbackForwardPortToDefaultBind(forward *forwardRecord, tunnel *tunnelRecord, node *nodeRecord, fp forwardPortRecord, serviceBase string, runtimeLimiters forwardRuntimeLimiters) (string, error) {
	if h == nil || forward == nil || tunnel == nil || node == nil {
		return "", errors.New("invalid bind fallback context")
	}
	if fp.Port <= 0 {
		return "", errors.New("invalid forward port")
	}
	explicitBindIP := strings.TrimSpace(fp.InIP)
	if explicitBindIP == "" {
		return "", errors.New("default bind address cannot be assigned")
	}

	if err := h.deleteForwardServicesOnNode(forward, node.ID); err != nil {
		return "", err
	}

	time.Sleep(150 * time.Millisecond)
	defaultServices := buildForwardServiceConfigs(serviceBase, forward, tunnel, node, fp.Port, "", runtimeLimiters)
	if _, err := h.sendNodeCommand(node.ID, "AddService", defaultServices, true, false); err != nil {
		return "", err
	}
	if err := h.repo.UpdateForwardPortBindIP(forward.ID, node.ID, fp.Port, ""); err != nil {
		return "", err
	}

	warning := fmt.Sprintf("服务器 %s 监听IP %s 不在主机网卡地址中，已自动回退为默认监听IP", strings.TrimSpace(node.Name), explicitBindIP)
	return warning, nil
}

func (h *Handler) rebindForwardServiceOnSelfOccupiedPort(forward *forwardRecord, node *nodeRecord, port int, services []map[string]interface{}) error {
	if h == nil || forward == nil || node == nil {
		return errors.New("invalid self-occupy rebind context")
	}
	if port <= 0 {
		return errors.New("invalid forward port")
	}

	hasOtherForward, err := h.repo.HasOtherForwardOnNodePort(node.ID, port, forward.ID)
	if err != nil {
		return err
	}
	if hasOtherForward {
		return fmt.Errorf("端口 %d 已被其他转发占用", port)
	}

	bases, err := h.forwardServiceBaseCandidates(forward)
	if err != nil {
		return err
	}

	if err := h.deleteForwardServiceBasesOnNode(node.ID, bases); err != nil {
		return err
	}

	time.Sleep(150 * time.Millisecond)

	_, err = h.sendNodeCommand(node.ID, "AddService", services, true, false)
	if err != nil {
		return err
	}

	return nil
}

func (h *Handler) deleteForwardServicesOnNode(forward *forwardRecord, nodeID int64) error {
	if h == nil || forward == nil {
		return errors.New("invalid forward delete context")
	}
	bases, err := h.forwardServiceBaseCandidates(forward)
	if err != nil {
		return err
	}
	return h.deleteForwardServiceBasesOnNode(nodeID, bases)

}

func (h *Handler) forwardServiceBaseCandidates(forward *forwardRecord) ([]string, error) {
	if h == nil || forward == nil {
		return nil, errors.New("invalid forward service base context")
	}
	userTunnelID, _, _, err := h.resolveUserTunnelAndLimiter(forward.UserID, forward.TunnelID)
	if err != nil {
		return nil, err
	}
	userTunnelIDs, err := h.listUserTunnelIDs(forward.UserID, forward.TunnelID)
	if err != nil {
		return nil, err
	}
	allUserTunnelIDs, err := h.listUserTunnelIDsByUser(forward.UserID)
	if err != nil {
		return nil, err
	}
	candidateTunnelIDs := make([]int64, 0, len(userTunnelIDs)+len(allUserTunnelIDs))
	candidateTunnelIDs = append(candidateTunnelIDs, userTunnelIDs...)
	candidateTunnelIDs = append(candidateTunnelIDs, allUserTunnelIDs...)
	return buildForwardServiceBaseCandidates(forward.ID, forward.UserID, userTunnelID, candidateTunnelIDs), nil

}

func (h *Handler) deleteForwardServiceBasesOnNode(nodeID int64, bases []string) error {
	names := buildForwardServiceDeleteNames(bases)
	if len(names) == 0 {
		return nil
	}
	payload := map[string]interface{}{"services": names}
	_, err := h.sendNodeCommand(nodeID, "DeleteService", payload, false, true)
	return err
}

func buildForwardServiceDeleteNames(bases []string) []string {
	names := make([]string, 0, len(bases)*3)
	seen := make(map[string]struct{}, len(bases)*3)
	appendName := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	for _, base := range bases {
		base = strings.TrimSpace(base)
		if base == "" {
			continue
		}
		appendName(base + "_tcp")
		appendName(base + "_udp")
		appendName(base)
	}
	return names
}

func (h *Handler) controlForwardServices(forward *forwardRecord, commandType string, tolerateNotFound bool) error {
	if h == nil || forward == nil {
		return errors.New("invalid forward control context")
	}
	if tunnel, err := h.getTunnelRecord(forward.TunnelID); err == nil && tunnelIsRealm(tunnel) {
		return h.controlRealmForward(forward, commandType)
	}
	usesNFT, nodes, err := h.tunnelUsesNftables(forward.TunnelID)
	if err != nil {
		return err
	}
	if usesNFT {
		return h.controlNftablesForward(forward, nodes, commandType)
	}
	ports, err := h.listForwardPorts(forward.ID)
	if err != nil {
		return err
	}
	if len(ports) == 0 {
		return nil
	}
	userTunnelID, _, _, err := h.resolveUserTunnelAndLimiter(forward.UserID, forward.TunnelID)
	if err != nil {
		return err
	}
	userTunnelIDs, err := h.listUserTunnelIDs(forward.UserID, forward.TunnelID)
	if err != nil {
		return err
	}
	allUserTunnelIDs, err := h.listUserTunnelIDsByUser(forward.UserID)
	if err != nil {
		return err
	}
	candidateTunnelIDs := make([]int64, 0, len(userTunnelIDs)+len(allUserTunnelIDs))
	candidateTunnelIDs = append(candidateTunnelIDs, userTunnelIDs...)
	candidateTunnelIDs = append(candidateTunnelIDs, allUserTunnelIDs...)
	bases := buildForwardServiceBaseCandidates(forward.ID, forward.UserID, userTunnelID, candidateTunnelIDs)
	if strings.EqualFold(strings.TrimSpace(commandType), "DeleteService") {
		seen := map[int64]struct{}{}
		for _, fp := range ports {
			if _, ok := seen[fp.NodeID]; ok {
				continue
			}
			seen[fp.NodeID] = struct{}{}
			if err := h.deleteForwardServiceBasesOnNode(fp.NodeID, bases); err != nil {
				if isNodeOfflineOrTimeoutError(err) {
					continue
				}
				if tolerateNotFound && isNotFoundError(err) {
					continue
				}
				return err
			}
		}
		return nil
	}
	seen := map[int64]struct{}{}
	healed := false
	for _, fp := range ports {
		if _, ok := seen[fp.NodeID]; ok {
			continue
		}
		seen[fp.NodeID] = struct{}{}

		nodeHandled, lastNotFoundErr, err := h.controlForwardServicesOnNode(fp.NodeID, bases, commandType)
		if err != nil {
			return err
		}

		if !nodeHandled && lastNotFoundErr != nil && !healed && shouldSelfHealForwardServiceControl(commandType) {
			if healErr := h.syncForwardServices(forward, "UpdateService", true); healErr != nil {
				return healErr
			}
			healed = true
			nodeHandled, lastNotFoundErr, err = h.controlForwardServicesOnNode(fp.NodeID, bases, commandType)
			if err != nil {
				return err
			}
		}

		if nodeHandled {
			continue
		}
		if tolerateNotFound {
			continue
		}
		if lastNotFoundErr != nil {
			return lastNotFoundErr
		}
		return errors.New("service control failed")
	}
	return nil
}

func (h *Handler) controlForwardServicesOnNode(nodeID int64, bases []string, commandType string) (bool, error, error) {
	return controlForwardServiceCommand(bases, commandType, func(name string) error {
		payload := map[string]interface{}{
			"services": []string{name},
		}
		_, err := h.sendNodeCommand(nodeID, commandType, payload, false, false)
		return err
	})
}

func controlForwardServiceCommand(bases []string, commandType string, send func(name string) error) (bool, error, error) {
	var lastNotFoundErr error
	for _, base := range bases {
		variants := []string{base + "_tcp", base + "_udp"}
		if shouldTryLegacySingleService(commandType) || strings.EqualFold(strings.TrimSpace(commandType), "DeleteService") {
			variants = append(variants, base)
		}

		candidateHandled := false
		for _, name := range variants {
			err := send(name)
			if err == nil {
				candidateHandled = true
				continue
			}
			if !isNotFoundError(err) {
				return false, lastNotFoundErr, err
			}
			lastNotFoundErr = err
		}

		if candidateHandled {
			return true, nil, nil
		}
	}
	return false, lastNotFoundErr, nil
}

func deleteForwardServiceCandidates(bases []string, send func(name string) error) error {
	for _, base := range bases {
		for _, name := range append([]string{base + "_tcp", base + "_udp", base}, []string{}...) {
			err := send(name)
			if err == nil {
				continue
			}
			if isNotFoundError(err) {
				continue
			}
			return err
		}
	}
	return nil
}

func shouldSelfHealForwardServiceControl(commandType string) bool {
	cmd := strings.ToLower(strings.TrimSpace(commandType))
	return cmd == "pauseservice" || cmd == "resumeservice"
}

func buildForwardServiceBase(forwardID, userID, userTunnelID int64) string {
	return fmt.Sprintf("%d_%d_%d", forwardID, userID, userTunnelID)
}

func buildForwardServiceBaseWithResolvedUserTunnel(forwardID, userID, resolvedUserTunnelID int64) string {
	if resolvedUserTunnelID <= 0 {
		return buildForwardServiceBase(forwardID, userID, 0)
	}
	return buildForwardServiceBase(forwardID, userID, resolvedUserTunnelID)
}

func buildForwardServiceBaseCandidates(forwardID, userID, preferredUserTunnelID int64, userTunnelIDs []int64) []string {
	orderedIDs := make([]int64, 0, len(userTunnelIDs)+2)
	seen := make(map[int64]struct{}, len(userTunnelIDs)+2)

	appendID := func(id int64) {
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		orderedIDs = append(orderedIDs, id)
	}

	appendID(preferredUserTunnelID)
	for _, id := range userTunnelIDs {
		appendID(id)
	}
	appendID(0)

	bases := make([]string, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		bases = append(bases, buildForwardServiceBase(forwardID, userID, id))
	}
	return bases
}

func buildForwardControlServiceNames(base, commandType string) []string {
	names := []string{base + "_tcp", base + "_udp"}
	if strings.EqualFold(strings.TrimSpace(commandType), "DeleteService") {
		return append([]string{base}, names...)
	}
	return names
}

func shouldTryLegacySingleService(commandType string) bool {
	cmd := strings.ToLower(strings.TrimSpace(commandType))
	return cmd == "pauseservice" || cmd == "resumeservice"
}

func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "not found") || strings.Contains(msg, "不存在")
}

func isAlreadyExistsMessage(message string) bool {
	msg := strings.ToLower(strings.TrimSpace(message))
	if msg == "" {
		return false
	}
	if isAddressAlreadyInUseMessage(msg) {
		return false
	}
	compact := compactErrorMessage(msg)
	return strings.Contains(msg, "already exists") || strings.Contains(msg, "已存在") || strings.Contains(compact, "alreadyexists")
}

func isBindAddressInUseError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if msg == "" {
		return false
	}
	return isAddressAlreadyInUseMessage(msg) || strings.Contains(msg, "cannot assign requested address")
}

func isAddressAlreadyInUseError(err error) bool {
	if err == nil {
		return false
	}
	return isAddressAlreadyInUseMessage(strings.ToLower(strings.TrimSpace(err.Error())))
}

func isAddressAlreadyInUseMessage(msg string) bool {
	if msg == "" {
		return false
	}
	if strings.Contains(msg, "address already in use") {
		return true
	}
	return strings.Contains(compactErrorMessage(msg), "addressalreadyinuse")
}

func isCannotAssignRequestedAddressError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if msg == "" {
		return false
	}
	if strings.Contains(msg, "cannot assign requested address") {
		return true
	}
	return strings.Contains(compactErrorMessage(msg), "cannotassignrequestedaddress")
}

func compactErrorMessage(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ""
	}
	return strings.Join(strings.Fields(strings.ToLower(msg)), "")
}

func buildForwardServiceConfigs(baseName string, forward *forwardRecord, tunnel *tunnelRecord, node *nodeRecord, port int, bindIP string, runtimeLimiters forwardRuntimeLimiters) []map[string]interface{} {
	protocols := []string{"tcp", "udp"}
	services := make([]map[string]interface{}, 0, 2)
	targets := splitRemoteTargets(forward.RemoteAddr)
	strategy := strings.TrimSpace(forward.Strategy)
	proxyProtocolReceive, proxyProtocolSend := normalizeForwardProxyProtocol(forward.ProxyProtocol, forward.ProxyProtocolReceive, forward.ProxyProtocolSend)
	if strategy == "" {
		strategy = "fifo"
	}

	for _, protocol := range protocols {
		listenerAddr := node.TCPListenAddr
		if protocol == "udp" {
			listenerAddr = node.UDPListenAddr
		}
		var serviceAddr string
		if bindIP != "" {
			trimmedBindIP := strings.TrimSpace(bindIP)
			if _, _, err := net.SplitHostPort(trimmedBindIP); err == nil {
				serviceAddr = processServerAddress(trimmedBindIP)
			} else {
				serviceAddr = processServerAddress(net.JoinHostPort(strings.Trim(trimmedBindIP, "[]"), strconv.Itoa(port)))
			}
		} else {
			serviceAddr = processServerAddress(fmt.Sprintf("%s:%d", listenerAddr, port))
		}
		service := map[string]interface{}{
			"name": fmt.Sprintf("%s_%s", baseName, protocol),
			"addr": serviceAddr,
			"handler": map[string]interface{}{
				"type": protocol,
			},
			"listener": map[string]interface{}{
				"type": protocol,
			},
			"forwarder": map[string]interface{}{
				"nodes": buildForwarderNodes(targets),
				"selector": map[string]interface{}{
					"strategy":    strategy,
					"maxFails":    1,
					"failTimeout": "600s",
				},
			},
		}
		if runtimeLimiters.ConnLimiter != "" {
			service["climiter"] = runtimeLimiters.ConnLimiter
		}
		if runtimeLimiters.TrafficLimiter != "" {
			service["limiter"] = runtimeLimiters.TrafficLimiter
		}
		if proxyProtocolReceive > 0 {
			serviceMetadata := ensureServiceMetadata(service)
			serviceMetadata["proxyProtocol"] = proxyProtocolReceive
		}
		if proxyProtocolSend > 0 {
			handlerConfig := service["handler"].(map[string]interface{})
			if handlerConfig["metadata"] == nil {
				handlerConfig["metadata"] = map[string]interface{}{}
			}
			handlerConfig["metadata"].(map[string]interface{})["proxyProtocol"] = proxyProtocolSend
		}
		if protocol == "udp" {
			listenerMetadata := map[string]interface{}{
				"keepAlive": true,
				"ttl":       "30s",
			}
			service["listener"].(map[string]interface{})["metadata"] = listenerMetadata
		}
		if tunnel != nil && tunnel.Type == 2 {
			service["handler"].(map[string]interface{})["chain"] = fmt.Sprintf("chains_%d", forward.TunnelID)
		}
		if tunnel != nil && tunnel.Type == 1 && strings.TrimSpace(node.InterfaceName) != "" {
			serviceMetadata := ensureServiceMetadata(service)
			serviceMetadata["interface"] = node.InterfaceName
		}
		services = append(services, service)
	}

	return services
}

func buildForwarderNodes(targets []string) []map[string]interface{} {
	nodes := make([]map[string]interface{}, 0, len(targets))
	for i, addr := range targets {
		nodes = append(nodes, map[string]interface{}{
			"name": fmt.Sprintf("node_%d", i+1),
			"addr": addr,
		})
	}
	return nodes
}

func ensureServiceMetadata(service map[string]interface{}) map[string]interface{} {
	if service["metadata"] == nil {
		service["metadata"] = map[string]interface{}{}
	}
	metadata, ok := service["metadata"].(map[string]interface{})
	if !ok {
		metadata = map[string]interface{}{}
		service["metadata"] = metadata
	}
	return metadata
}

func normalizeForwardProxyProtocol(legacy, receive, send int) (int, int) {
	if send == 0 && legacy > 0 {
		send = legacy
	}
	return receive, send
}

func processServerAddress(serverAddr string) string {
	serverAddr = normalizeServerAddressInput(serverAddr)
	if serverAddr == "" {
		return serverAddr
	}
	if strings.HasPrefix(serverAddr, "[") {
		return serverAddr
	}
	// If the input is a bare IPv6 host (no port), bracket it.
	// IPv6-with-port must be provided in bracket form: [::1]:443.
	if looksLikeIPv6(serverAddr) {
		if ip := net.ParseIP(serverAddr); ip != nil && ip.To4() == nil {
			return "[" + serverAddr + "]"
		}
	}

	idx := strings.LastIndex(serverAddr, ":")
	if idx < 0 {
		if looksLikeIPv6(serverAddr) {
			return "[" + serverAddr + "]"
		}
		return serverAddr
	}
	host := strings.TrimSpace(serverAddr[:idx])
	port := strings.TrimSpace(serverAddr[idx+1:])
	if host == "" || port == "" {
		return serverAddr
	}
	if looksLikeIPv6(host) {
		return "[" + host + "]:" + port
	}
	return serverAddr
}

func normalizeServerAddressInput(serverAddr string) string {
	serverAddr = strings.TrimSpace(serverAddr)
	if serverAddr == "" {
		return serverAddr
	}

	if idx := strings.Index(serverAddr, "://"); idx > 0 {
		if parsed, err := url.Parse(serverAddr); err == nil {
			if host := strings.TrimSpace(parsed.Host); host != "" {
				return host
			}
		}
		serverAddr = serverAddr[idx+3:]
	}

	if idx := strings.IndexAny(serverAddr, "/?#"); idx >= 0 {
		serverAddr = serverAddr[:idx]
	}
	return strings.TrimSpace(serverAddr)
}

func looksLikeIPv6(address string) bool {
	return strings.Count(address, ":") >= 2
}
