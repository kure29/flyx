package handler

import (
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"go-backend/internal/http/client"
	"go-backend/internal/store/repo"
)

const tunnelServiceBindRetryDelay = 150 * time.Millisecond

func (h *Handler) applyTunnelRuntime(state *tunnelCreateState) ([]int64, []int64, error) {
	return h.applyTunnelRuntimeWithMode(state, false)
}

func (h *Handler) applyTunnelRuntimeUpsert(state *tunnelCreateState) ([]int64, []int64, error) {
	return h.applyTunnelRuntimeWithMode(state, true)
}

func (h *Handler) applyTunnelRuntimeWithMode(state *tunnelCreateState, upsert bool) ([]int64, []int64, error) {
	if h == nil || state == nil {
		return nil, nil, errors.New("invalid tunnel runtime state")
	}
	createdChains := make([]int64, 0)
	createdServices := make([]int64, 0)
	if state.Type != 2 {
		return createdChains, createdServices, nil
	}

	for _, inNode := range state.InNodes {
		targets := state.OutNodes
		if len(state.ChainHops) > 0 {
			targets = state.ChainHops[0]
		} else {
			targets = h.orderBestExitTargets(state.TunnelID, inNode.NodeID, targets)
		}
		chainData, err := buildTunnelChainConfig(state.TunnelID, inNode.NodeID, targets, state.Nodes, state.IPPreference)
		if err != nil {
			return createdChains, createdServices, err
		}
		if err := h.applyTunnelChainOnNode(inNode.NodeID, chainData, upsert); err != nil {
			if shouldDeferTunnelRuntimeApplyError(err) {
				continue
			}
			return createdChains, createdServices, fmt.Errorf("入口服务器 %s 下发转发链失败: %w", nodeDisplayName(state.Nodes[inNode.NodeID]), err)
		}
		createdChains = append(createdChains, inNode.NodeID)
	}

	for i, hop := range state.ChainHops {
		for _, chainNode := range hop {
			nextTargets := state.OutNodes
			if i+1 < len(state.ChainHops) {
				nextTargets = state.ChainHops[i+1]
			} else {
				nextTargets = h.orderBestExitTargets(state.TunnelID, chainNode.NodeID, nextTargets)
			}
			node := state.Nodes[chainNode.NodeID]
			if node != nil && (node.IsRemote == 1 || node.Status != 1) {
				continue
			}
			chainData, err := buildTunnelChainConfig(state.TunnelID, chainNode.NodeID, nextTargets, state.Nodes, state.IPPreference)
			if err != nil {
				return createdChains, createdServices, err
			}
			if err := h.applyTunnelChainOnNode(chainNode.NodeID, chainData, upsert); err != nil {
				if shouldDeferTunnelRuntimeApplyError(err) {
					continue
				}
				return createdChains, createdServices, fmt.Errorf("转发链服务器 %s 下发转发链失败: %w", nodeDisplayName(state.Nodes[chainNode.NodeID]), err)
			}
			createdChains = append(createdChains, chainNode.NodeID)

			serviceData := buildTunnelChainServiceConfig(state.TunnelID, chainNode, state.Nodes[chainNode.NodeID], len(nextTargets))
			if err := h.addTunnelServiceOnNodeWithMode(chainNode.NodeID, state.TunnelID, serviceData, upsert); err != nil {
				if shouldDeferTunnelRuntimeApplyError(err) {
					continue
				}
				return createdChains, createdServices, fmt.Errorf("转发链服务器 %s 下发服务失败: %w", nodeDisplayName(state.Nodes[chainNode.NodeID]), err)
			}
			createdServices = append(createdServices, chainNode.NodeID)
		}
	}

	for _, outNode := range state.OutNodes {
		node := state.Nodes[outNode.NodeID]
		if node != nil && (node.IsRemote == 1 || node.Status != 1) {
			continue
		}
		serviceData := buildTunnelChainServiceConfig(state.TunnelID, outNode, state.Nodes[outNode.NodeID], 1)
		if err := h.addTunnelServiceOnNodeWithMode(outNode.NodeID, state.TunnelID, serviceData, upsert); err != nil {
			if shouldDeferTunnelRuntimeApplyError(err) {
				continue
			}
			return createdChains, createdServices, fmt.Errorf("出口服务器 %s 下发服务失败: %w", nodeDisplayName(state.Nodes[outNode.NodeID]), err)
		}
		createdServices = append(createdServices, outNode.NodeID)
	}

	return createdChains, createdServices, nil
}

func (h *Handler) applyTunnelChainOnNode(nodeID int64, chainData map[string]interface{}, upsert bool) error {
	if upsert {
		return h.upsertTunnelChainOnNode(nodeID, chainData)
	}
	_, err := h.sendNodeCommand(nodeID, "AddChains", chainData, true, false)
	return err
}

func (h *Handler) applyBestExitChainOrder(tunnelID, ownerNodeID int64, outNodes []chainNodeRecord, scores []bestExitCandidateScore, ipPreference string) error {
	if h == nil {
		log.Printf("best_exit: invalid chain update context tunnel=%d owner=%d", tunnelID, ownerNodeID)
		return errors.New("invalid best exit chain update context")
	}
	if tunnelID <= 0 || ownerNodeID <= 0 || len(outNodes) == 0 {
		log.Printf("best_exit: invalid chain update input tunnel=%d owner=%d exits=%d", tunnelID, ownerNodeID, len(outNodes))
		return fmt.Errorf("invalid best exit chain update input tunnel=%d owner=%d exits=%d", tunnelID, ownerNodeID, len(outNodes))
	}
	targets := chainRecordsToRuntimeTargets(outNodes)
	orderedIDs := make([]int64, 0, len(scores))
	for _, score := range scores {
		if score.ExitNodeID > 0 {
			orderedIDs = append(orderedIDs, score.ExitNodeID)
		}
	}
	targets = orderRuntimeTargetsByNodeID(targets, orderedIDs)
	nodes := make(map[int64]*nodeRecord, len(targets)+1)
	if owner, err := h.getNodeRecord(ownerNodeID); err == nil && owner != nil {
		nodes[ownerNodeID] = owner
	}
	for _, target := range targets {
		if node, err := h.getNodeRecord(target.NodeID); err == nil && node != nil {
			nodes[target.NodeID] = node
		}
	}
	owner := nodes[ownerNodeID]
	if owner != nil && owner.IsRemote == 1 {
		if err := h.applyRemoteBestExitChainOrder(tunnelID, ownerNodeID, owner, targets, nodes, ipPreference); err != nil {
			log.Printf("best_exit: update remote federation chain failed tunnel=%d owner=%d err=%v", tunnelID, ownerNodeID, err)
			return err
		}
		log.Printf("best_exit: updated remote federation chain tunnel=%d owner=%d best_exit=%d", tunnelID, ownerNodeID, targets[0].NodeID)
		return nil
	}
	chainData, err := buildTunnelChainConfig(tunnelID, ownerNodeID, targets, nodes, ipPreference)
	if err != nil {
		log.Printf("best_exit: build chain failed tunnel=%d owner=%d err=%v", tunnelID, ownerNodeID, err)
		return err
	}
	if err := h.applyTunnelChainOnNode(ownerNodeID, chainData, true); err != nil {
		log.Printf("best_exit: update chain failed tunnel=%d owner=%d err=%v", tunnelID, ownerNodeID, err)
		return err
	}
	log.Printf("best_exit: updated chain tunnel=%d owner=%d best_exit=%d", tunnelID, ownerNodeID, targets[0].NodeID)
	return nil
}

func (h *Handler) applyRemoteBestExitChainOrder(tunnelID, ownerNodeID int64, owner *nodeRecord, targets []tunnelRuntimeNode, nodes map[int64]*nodeRecord, ipPreference string) error {
	if h == nil || h.repo == nil || owner == nil {
		return errors.New("invalid remote best exit update context")
	}
	bindings, err := h.repo.ListActiveFederationTunnelBindingsByTunnel(tunnelID)
	if err != nil {
		return err
	}
	var binding *repo.FederationTunnelBinding
	for i := range bindings {
		if bindings[i].NodeID == ownerNodeID && bindings[i].ChainType == 2 && bindings[i].Status == 1 {
			binding = &bindings[i]
			break
		}
	}
	if binding == nil {
		return fmt.Errorf("active federation middle binding not found for tunnel=%d owner=%d", tunnelID, ownerNodeID)
	}

	remoteURL := strings.TrimSpace(owner.RemoteURL)
	if remoteURL == "" {
		remoteURL = strings.TrimSpace(binding.RemoteURL)
	}
	remoteToken := strings.TrimSpace(owner.RemoteToken)
	if remoteURL == "" || remoteToken == "" {
		return errors.New("远程服务器缺少共享配置")
	}

	applyTargets := make([]client.RuntimeTarget, 0, len(targets))
	for _, target := range targets {
		targetNode := nodes[target.NodeID]
		if targetNode == nil {
			return errors.New("服务器不存在")
		}
		host, hostErr := selectTunnelDialHost(owner, targetNode, ipPreference, target.ConnectIP)
		if hostErr != nil {
			return hostErr
		}
		if target.Port <= 0 {
			return errors.New("服务器端口不能为空")
		}
		applyTargets = append(applyTargets, client.RuntimeTarget{
			Host:     host,
			Port:     target.Port,
			Protocol: defaultString(target.Protocol, "tls"),
		})
	}

	ownerRuntimeNode := tunnelRuntimeNode{NodeID: ownerNodeID, Protocol: "tls", Strategy: "round", ChainType: 2}
	if chainRows, listErr := h.repo.ListChainNodesForTunnel(tunnelID); listErr == nil {
		for _, row := range chainRows {
			if row.NodeID == ownerNodeID && row.ChainType == 2 {
				ownerRuntimeNode = tunnelRuntimeNode{
					NodeID:    row.NodeID,
					Protocol:  row.Protocol,
					Strategy:  row.Strategy,
					Inx:       int(row.Inx),
					ChainType: row.ChainType,
					Port:      row.Port,
					ConnectIP: row.ConnectIP,
				}
				break
			}
		}
	}

	_, err = client.NewFederationClient().ApplyRole(remoteURL, remoteToken, h.federationLocalDomain(), client.RuntimeApplyRoleRequest{
		ResourceKey: strings.TrimSpace(binding.ResourceKey),
		Role:        "middle",
		Protocol:    defaultString(ownerRuntimeNode.Protocol, "tls"),
		Strategy:    runtimeStrategyForTargets(ownerRuntimeNode, targets),
		Targets:     applyTargets,
	})
	return err
}

func (h *Handler) upsertTunnelChainOnNode(nodeID int64, chainData map[string]interface{}) error {
	if h == nil {
		return errors.New("invalid tunnel chain context")
	}
	chainName := asString(chainData["name"])
	if strings.TrimSpace(chainName) == "" {
		return errors.New("转发链名称不能为空")
	}
	payload := map[string]interface{}{"chain": chainName, "data": chainData}
	_, err := h.sendNodeCommand(nodeID, "UpdateChains", payload, true, false)
	return err
}

func retryTunnelServiceAddWithCleanup(add func() error, cleanup func() error, wait time.Duration) error {
	if add == nil {
		return errors.New("invalid tunnel service add callback")
	}
	err := add()
	if err == nil || !isAddressAlreadyInUseError(err) {
		return err
	}
	if cleanup == nil {
		return err
	}
	if cleanupErr := cleanup(); cleanupErr != nil {
		return cleanupErr
	}
	if wait > 0 {
		time.Sleep(wait)
	}
	return add()
}

func (h *Handler) addTunnelServiceOnNode(nodeID, tunnelID int64, serviceData []map[string]interface{}) error {
	return h.addTunnelServiceOnNodeWithMode(nodeID, tunnelID, serviceData, false)
}

func (h *Handler) addTunnelServiceOnNodeWithMode(nodeID, tunnelID int64, serviceData []map[string]interface{}, upsert bool) error {
	if h == nil {
		return errors.New("invalid tunnel service context")
	}
	serviceName := fmt.Sprintf("%d_tls", tunnelID)
	if len(serviceData) > 0 {
		if name, ok := serviceData[0]["name"].(string); ok && strings.TrimSpace(name) != "" {
			serviceName = strings.TrimSpace(name)
		}
	}
	command := "AddService"
	if upsert {
		command = "UpdateService"
	}
	return retryTunnelServiceAddWithCleanup(
		func() error {
			_, err := h.sendNodeCommand(nodeID, command, serviceData, true, false)
			return err
		},
		func() error {
			_, err := h.sendNodeCommand(nodeID, "DeleteService", map[string]interface{}{"services": []string{serviceName}}, false, true)
			return err
		},
		tunnelServiceBindRetryDelay,
	)
}

func (h *Handler) rollbackTunnelRuntime(chainNodeIDs, serviceNodeIDs []int64, tunnelID int64, protocol string) {
	if h == nil || tunnelID <= 0 {
		return
	}
	if protocol == "" {
		protocol = "tls"
	}
	seenServices := make(map[int64]struct{})
	serviceNames := tunnelRuntimeServiceNames(tunnelID)
	for i := len(serviceNodeIDs) - 1; i >= 0; i-- {
		nodeID := serviceNodeIDs[i]
		if _, ok := seenServices[nodeID]; ok {
			continue
		}
		seenServices[nodeID] = struct{}{}
		_, _ = h.sendNodeCommand(nodeID, "DeleteService", map[string]interface{}{"services": serviceNames}, false, true)
	}

	seenChains := make(map[int64]struct{})
	chainName := fmt.Sprintf("chains_%d", tunnelID)
	for i := len(chainNodeIDs) - 1; i >= 0; i-- {
		nodeID := chainNodeIDs[i]
		if _, ok := seenChains[nodeID]; ok {
			continue
		}
		seenChains[nodeID] = struct{}{}
		_, _ = h.sendNodeCommand(nodeID, "DeleteChains", map[string]interface{}{"chain": chainName}, false, true)
	}
}

func shouldDeferTunnelRuntimeApplyError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	if msg == "" {
		return false
	}
	if strings.Contains(msg, "服务器不在线") || strings.Contains(msg, "节点不在线") {
		return true
	}
	if strings.Contains(msg, "等待服务器响应超时") || strings.Contains(msg, "timeout") || strings.Contains(msg, "超时") {
		return true
	}
	return false
}

// isNodeOfflineOrTimeoutError returns true when the error indicates a node
// is unreachable (offline or timed out), matching the same patterns used by
// shouldDeferTunnelRuntimeApplyError.
func isNodeOfflineOrTimeoutError(err error) bool {
	return shouldDeferTunnelRuntimeApplyError(err)
}

func buildTunnelChainConfig(tunnelID int64, fromNodeID int64, targets []tunnelRuntimeNode, nodes map[int64]*nodeRecord, ipPreference string) (map[string]interface{}, error) {
	fromNode := nodes[fromNodeID]
	if fromNode == nil {
		return nil, errors.New("服务器不存在")
	}
	if len(targets) == 0 {
		return nil, errors.New("转发链目标不能为空")
	}
	nodeItems := make([]map[string]interface{}, 0, len(targets))
	for idx, target := range targets {
		targetNode := nodes[target.NodeID]
		if targetNode == nil {
			return nil, errors.New("服务器不存在")
		}
		host, err := selectTunnelDialHost(fromNode, targetNode, ipPreference, target.ConnectIP)
		if err != nil {
			return nil, err
		}
		port := target.Port
		if port <= 0 {
			return nil, errors.New("服务器端口不能为空")
		}
		protocol := defaultString(target.Protocol, "tls")
		connector := map[string]interface{}{
			"type": "relay",
		}
		connectorMetadata := map[string]interface{}{}
		if isTCPTunnelProtocol(protocol) {
			connectorMetadata["nodelay"] = true
			connectorMetadata["mux.keepaliveInterval"] = "15s"
			connectorMetadata["mux.keepaliveTimeout"] = "45s"
			connectorMetadata["mux.maxFrameSize"] = 32768
			connectorMetadata["mux.maxStreamBuffer"] = 2097152
		}
		if isKCPTunnelProtocol(protocol) {
			connectorMetadata["connectTimeout"] = "30s"
			connectorMetadata["mux.keepaliveInterval"] = "15s"
			connectorMetadata["mux.keepaliveTimeout"] = "45s"
			connectorMetadata["mux.maxFrameSize"] = 32768
			connectorMetadata["mux.maxStreamBuffer"] = 2097152
		}
		if len(connectorMetadata) > 0 {
			connector["metadata"] = connectorMetadata
		}
		nodeItems = append(nodeItems, map[string]interface{}{
			"name":      fmt.Sprintf("node_%d", idx+1),
			"addr":      processServerAddress(fmt.Sprintf("%s:%d", host, port)),
			"connector": connector,
			"dialer":    buildTunnelDialerConfig(protocol),
		})
	}

	strategy := runtimeTunnelStrategy(defaultString(strings.TrimSpace(targets[0].Strategy), "round"))
	hop := map[string]interface{}{
		"name": fmt.Sprintf("hop_%d", tunnelID),
		"selector": map[string]interface{}{
			"strategy":    strategy,
			"maxFails":    1,
			"failTimeout": int64(600000000000),
		},
		"nodes": nodeItems,
	}
	if strings.TrimSpace(fromNode.InterfaceName) != "" {
		hop["interface"] = fromNode.InterfaceName
	}

	return map[string]interface{}{
		"name": fmt.Sprintf("chains_%d", tunnelID),
		"hops": []map[string]interface{}{hop},
	}, nil
}

func (h *Handler) orderBestExitTargets(tunnelID, ownerNodeID int64, targets []tunnelRuntimeNode) []tunnelRuntimeNode {
	if len(targets) <= 1 || !isBestTunnelStrategy(targets[0].Strategy) {
		return append([]tunnelRuntimeNode(nil), targets...)
	}
	if h == nil || h.bestExit == nil {
		return append([]tunnelRuntimeNode(nil), targets...)
	}
	return h.bestExit.orderTargets(bestExitOwnerKey{TunnelID: tunnelID, OwnerNodeID: ownerNodeID}, targets)
}

func runtimeStrategyForTargets(owner tunnelRuntimeNode, targets []tunnelRuntimeNode) string {
	strategy := defaultString(owner.Strategy, "round")
	if len(targets) > 0 {
		strategy = defaultString(targets[0].Strategy, strategy)
	}
	return runtimeTunnelStrategy(strategy)
}

func buildTunnelChainServiceConfig(tunnelID int64, chainNode tunnelRuntimeNode, node *nodeRecord, nextHopCandidateCount int) []map[string]interface{} {
	if node == nil {
		return nil
	}
	protocol := defaultString(chainNode.Protocol, "tls")
	handlerCfg := map[string]interface{}{
		"type": "relay",
	}
	handlerMetadata := map[string]interface{}{}
	if isTCPTunnelProtocol(protocol) {
		handlerMetadata["nodelay"] = true
		handlerMetadata["mux.keepaliveInterval"] = "15s"
		handlerMetadata["mux.keepaliveTimeout"] = "45s"
		handlerMetadata["mux.maxFrameSize"] = 32768
		handlerMetadata["mux.maxStreamBuffer"] = 2097152
	}
	if isKCPTunnelProtocol(protocol) {
		handlerMetadata["connectTimeout"] = "30s"
		handlerMetadata["mux.keepaliveInterval"] = "15s"
		handlerMetadata["mux.keepaliveTimeout"] = "45s"
		handlerMetadata["mux.maxFrameSize"] = 32768
		handlerMetadata["mux.maxStreamBuffer"] = 2097152
	}
	if len(handlerMetadata) > 0 {
		handlerCfg["metadata"] = handlerMetadata
	}
	if nextHopCandidateCount > 1 {
		handlerCfg["retries"] = nextHopCandidateCount - 1
	}
	service := map[string]interface{}{
		"name":     fmt.Sprintf("tunnel_%d", tunnelID),
		"addr":     processServerAddress(fmt.Sprintf("%s:%d", defaultString(strings.TrimSpace(chainNode.ConnectIP), node.TCPListenAddr), chainNode.Port)),
		"handler":  handlerCfg,
		"listener": buildTunnelListenerConfig(protocol),
	}
	if chainNode.ChainType == 2 {
		service["handler"].(map[string]interface{})["chain"] = fmt.Sprintf("chains_%d", tunnelID)
	}
	if chainNode.ChainType == 3 && strings.TrimSpace(node.InterfaceName) != "" {
		service["metadata"] = map[string]interface{}{"interface": node.InterfaceName}
	}
	return []map[string]interface{}{service}
}

func selectTunnelDialHost(fromNode, toNode *nodeRecord, ipPreference string, connectIp string) (string, error) {
	if fromNode == nil || toNode == nil {
		return "", errors.New("服务器不存在")
	}
	if strings.TrimSpace(connectIp) != "" {
		return strings.TrimSpace(connectIp), nil
	}
	fromV4 := nodeSupportsV4(fromNode)
	fromV6 := nodeSupportsV6(fromNode)
	toV4 := nodeSupportsV4(toNode)
	toV6 := nodeSupportsV6(toNode)

	switch strings.TrimSpace(ipPreference) {
	case "v6":
		if fromV6 && toV6 {
			if host := pickNodeAddressV6(toNode); host != "" {
				return host, nil
			}
		}
		if fromV4 && toV4 {
			if host := pickNodeAddressV4(toNode); host != "" {
				return host, nil
			}
		}
	case "v4":
		if fromV4 && toV4 {
			if host := pickNodeAddressV4(toNode); host != "" {
				return host, nil
			}
		}
		if fromV6 && toV6 {
			if host := pickNodeAddressV6(toNode); host != "" {
				return host, nil
			}
		}
	default:
		// 同版本优先
		if fromV4 && toV4 {
			if host := pickNodeAddressV4(toNode); host != "" {
				return host, nil
			}
		}
		if fromV6 && toV6 {
			if host := pickNodeAddressV6(toNode); host != "" {
				return host, nil
			}
		}
		// 跨版本支持：v6入v4出 / v4入v6出
		if fromV6 && toV4 {
			if host := pickNodeAddressV4(toNode); host != "" {
				return host, nil
			}
		}
		if fromV4 && toV6 {
			if host := pickNodeAddressV6(toNode); host != "" {
				return host, nil
			}
		}
	}
	return "", fmt.Errorf("服务器链路不兼容：%s(v4=%t,v6=%t) -> %s(v4=%t,v6=%t)", nodeDisplayName(fromNode), fromV4, fromV6, nodeDisplayName(toNode), toV4, toV6)
}

func nodeDisplayName(node *nodeRecord) string {
	if node == nil {
		return "node"
	}
	if strings.TrimSpace(node.Name) != "" {
		return strings.TrimSpace(node.Name)
	}
	return fmt.Sprintf("node_%d", node.ID)
}

func isTCPTunnelProtocol(protocol string) bool {
	p := strings.ToLower(strings.TrimSpace(defaultString(protocol, "tls")))
	return p == "tls" || p == "mtls" || p == "mtcp"
}

func isKCPTunnelProtocol(protocol string) bool {
	return strings.EqualFold(strings.TrimSpace(protocol), "kcp")
}

func isTLSTunnelProtocol(protocol string) bool {
	return strings.EqualFold(strings.TrimSpace(defaultString(protocol, "tls")), "tls")
}

func buildTunnelDialerConfig(protocol string) map[string]interface{} {
	dialer := map[string]interface{}{
		"type": protocol,
	}
	if isKCPTunnelProtocol(protocol) {
		dialer["metadata"] = map[string]interface{}{
			"kcp.keepalive":   10,
			"kcp.tcp":         false,
			"kcp.mode":        "fast3",
			"kcp.sndwnd":      4096,
			"kcp.rcvwnd":      4096,
			"kcp.mtu":         1350,
			"kcp.sockbuf":     4194304,
			"kcp.smuxbuf":     4194304,
			"kcp.streambuf":   2097152,
			"kcp.datashard":   10,
			"kcp.parityshard": 3,
			"kcp.nocomp":      true,
			"kcp.nc":          1,
		}
	}
	return dialer
}

func buildTunnelListenerConfig(protocol string) map[string]interface{} {
	listener := map[string]interface{}{
		"type": protocol,
	}
	if isKCPTunnelProtocol(protocol) {
		listener["metadata"] = map[string]interface{}{
			"kcp.keepalive":   10,
			"kcp.tcp":         false,
			"kcp.mode":        "fast3",
			"kcp.sndwnd":      4096,
			"kcp.rcvwnd":      4096,
			"kcp.mtu":         1350,
			"kcp.sockbuf":     4194304,
			"kcp.smuxbuf":     4194304,
			"kcp.streambuf":   2097152,
			"kcp.datashard":   10,
			"kcp.parityshard": 3,
			"kcp.nocomp":      true,
			"kcp.nc":          1,
		}
	}
	return listener
}

func nodeSupportsV4(node *nodeRecord) bool {
	if node == nil {
		return false
	}
	if strings.TrimSpace(node.ServerIPv4) != "" {
		return true
	}
	if strings.TrimSpace(node.ServerIPv6) != "" {
		return false
	}
	legacy := strings.Trim(strings.TrimSpace(node.ServerIP), "[]")
	if legacy == "" {
		return false
	}
	if ip := net.ParseIP(legacy); ip != nil {
		return ip.To4() != nil
	}
	return true
}

func nodeSupportsV6(node *nodeRecord) bool {
	if node == nil {
		return false
	}
	if strings.TrimSpace(node.ServerIPv6) != "" {
		return true
	}
	if strings.TrimSpace(node.ServerIPv4) != "" {
		return false
	}
	legacy := strings.Trim(strings.TrimSpace(node.ServerIP), "[]")
	if legacy == "" {
		return false
	}
	if ip := net.ParseIP(legacy); ip != nil {
		return ip.To4() == nil
	}
	return true
}

func pickNodeAddressV4(node *nodeRecord) string {
	if node == nil {
		return ""
	}
	if v := strings.TrimSpace(node.ServerIPv4); v != "" {
		return v
	}
	return strings.TrimSpace(node.ServerIP)
}

func pickNodeAddressV6(node *nodeRecord) string {
	if node == nil {
		return ""
	}
	if v := strings.TrimSpace(node.ServerIPv6); v != "" {
		return v
	}
	return strings.TrimSpace(node.ServerIP)
}
