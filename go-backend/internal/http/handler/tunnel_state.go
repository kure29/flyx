package handler

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

func (h *Handler) reconstructTunnelState(tunnelID int64) (*tunnelCreateState, error) {
	tunnel, err := h.getTunnelRecord(tunnelID)
	if err != nil {
		return nil, err
	}

	chainRows, err := h.listChainNodesForTunnel(tunnelID)
	if err != nil {
		return nil, err
	}

	ipPreference := h.repo.GetTunnelIPPreference(tunnelID)

	state := &tunnelCreateState{
		TunnelID:     tunnelID,
		Type:         tunnel.Type,
		IPPreference: ipPreference,
		InNodes:      make([]tunnelRuntimeNode, 0),
		ChainHops:    make([][]tunnelRuntimeNode, 0),
		OutNodes:     make([]tunnelRuntimeNode, 0),
		Nodes:        make(map[int64]*nodeRecord),
		NodeIDList:   make([]int64, 0),
	}

	inNodes, chainHops, outNodes := splitChainNodeGroups(chainRows)

	for _, r := range inNodes {
		state.InNodes = append(state.InNodes, tunnelRuntimeNode{
			NodeID:    r.NodeID,
			Protocol:  r.Protocol,
			Strategy:  r.Strategy,
			ChainType: 1,
		})
		state.NodeIDList = append(state.NodeIDList, r.NodeID)
	}

	for _, r := range outNodes {
		state.OutNodes = append(state.OutNodes, tunnelRuntimeNode{
			NodeID:    r.NodeID,
			Protocol:  r.Protocol,
			Strategy:  r.Strategy,
			ChainType: 3,
			Port:      r.Port,
			ConnectIP: r.ConnectIP,
		})
		state.NodeIDList = append(state.NodeIDList, r.NodeID)
	}

	for _, hop := range chainHops {
		stateHop := make([]tunnelRuntimeNode, 0)
		for _, r := range hop {
			stateHop = append(stateHop, tunnelRuntimeNode{
				NodeID:    r.NodeID,
				Protocol:  r.Protocol,
				Strategy:  r.Strategy,
				ChainType: 2,
				Inx:       int(r.Inx),
				Port:      r.Port,
				ConnectIP: r.ConnectIP,
			})
			state.NodeIDList = append(state.NodeIDList, r.NodeID)
		}
		state.ChainHops = append(state.ChainHops, stateHop)
	}

	seen := make(map[int64]struct{})
	for _, id := range state.NodeIDList {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		node, err := h.getNodeRecord(id)
		if err != nil {
			return nil, err
		}
		state.Nodes[id] = node
	}

	return state, nil
}

type tunnelRuntimeNode struct {
	NodeID    int64
	Protocol  string
	Strategy  string
	Inx       int
	ChainType int
	Port      int
	ConnectIP string
}

type tunnelCreateState struct {
	TunnelID     int64
	Type         int
	IPPreference string // "" = auto, "v4" = prefer IPv4, "v6" = prefer IPv6
	InNodes      []tunnelRuntimeNode
	ChainHops    [][]tunnelRuntimeNode
	OutNodes     []tunnelRuntimeNode
	Nodes        map[int64]*nodeRecord
	NodeIDList   []int64
}

func (h *Handler) prepareTunnelCreateState(tx *gorm.DB, req map[string]interface{}, tunnelType int, excludeTunnelID int64) (*tunnelCreateState, error) {
	state := &tunnelCreateState{
		Type:      tunnelType,
		InNodes:   make([]tunnelRuntimeNode, 0),
		ChainHops: make([][]tunnelRuntimeNode, 0),
		OutNodes:  make([]tunnelRuntimeNode, 0),
		Nodes:     make(map[int64]*nodeRecord),
	}
	nodeIDs := make([]int64, 0)

	for _, item := range asMapSlice(req["inNodeId"]) {
		nodeID := asInt64(item["nodeId"], 0)
		if nodeID <= 0 {
			continue
		}
		nodeIDs = append(nodeIDs, nodeID)
		state.InNodes = append(state.InNodes, tunnelRuntimeNode{
			NodeID:    nodeID,
			Protocol:  defaultString(asString(item["protocol"]), "tls"),
			Strategy:  defaultString(asString(item["strategy"]), "round"),
			ChainType: 1,
		})
	}
	if len(state.InNodes) == 0 {
		return nil, errors.New("入口不能为空")
	}

	if tunnelType == 2 {
		outNodesRaw := asMapSlice(req["outNodeId"])
		if len(outNodesRaw) == 0 {
			return nil, errors.New("出口不能为空")
		}

		allocated := map[int64]int{}
		for _, item := range outNodesRaw {
			nodeID := asInt64(item["nodeId"], 0)
			if nodeID <= 0 {
				continue
			}
			nodeIDs = append(nodeIDs, nodeID)
			port := asInt(item["port"], 0)
			if port <= 0 {
				isRemote, remoteErr := h.repo.IsRemoteNodeTx(tx, nodeID)
				if remoteErr != nil {
					return nil, remoteErr
				}
				if !isRemote {
					var err error
					if excludeTunnelID > 0 {
						port, err = h.repo.PickNodePortTx(tx, nodeID, allocated, excludeTunnelID)
					} else {
						port, err = h.repo.PickRandomNodePortTx(tx, nodeID, allocated, excludeTunnelID)
					}
					if err != nil {
						return nil, err
					}
				}
			}
			state.OutNodes = append(state.OutNodes, tunnelRuntimeNode{
				NodeID:    nodeID,
				Protocol:  defaultString(asString(item["protocol"]), "tls"),
				Strategy:  defaultString(asString(item["strategy"]), "round"),
				ChainType: 3,
				Port:      port,
				ConnectIP: asString(item["connectIp"]),
			})
		}
		if len(state.OutNodes) == 0 {
			return nil, errors.New("出口不能为空")
		}

		for hopIdx, hopRaw := range asAnySlice(req["chainNodes"]) {
			hop := make([]tunnelRuntimeNode, 0)
			for _, item := range asMapSlice(hopRaw) {
				nodeID := asInt64(item["nodeId"], 0)
				if nodeID <= 0 {
					continue
				}
				nodeIDs = append(nodeIDs, nodeID)
				port := asInt(item["port"], 0)
				if port <= 0 {
					isRemote, remoteErr := h.repo.IsRemoteNodeTx(tx, nodeID)
					if remoteErr != nil {
						return nil, remoteErr
					}
					if !isRemote {
						var err error
						if excludeTunnelID > 0 {
							port, err = h.repo.PickNodePortTx(tx, nodeID, allocated, excludeTunnelID)
						} else {
							port, err = h.repo.PickRandomNodePortTx(tx, nodeID, allocated, excludeTunnelID)
						}
						if err != nil {
							return nil, err
						}
					}
				}
				hop = append(hop, tunnelRuntimeNode{
					NodeID:    nodeID,
					Protocol:  defaultString(asString(item["protocol"]), "tls"),
					Strategy:  defaultString(asString(item["strategy"]), "round"),
					Inx:       hopIdx + 1,
					ChainType: 2,
					Port:      port,
					ConnectIP: asString(item["connectIp"]),
				})
			}
			if len(hop) > 0 {
				state.ChainHops = append(state.ChainHops, hop)
			}
		}
	}

	// When updating an existing tunnel (excludeTunnelID > 0), build a set of
	// node IDs that already belong to the tunnel so we can tolerate offline
	// nodes that the user is keeping or removing, while still rejecting newly
	// added offline nodes.
	existingNodeIDs := make(map[int64]struct{})
	if excludeTunnelID > 0 {
		var existIDs []int64
		var err error
		existIDs, err = h.repo.ListTunnelChainNodeIDsTx(tx, excludeTunnelID)
		if err != nil {
			return nil, err
		}
		for _, eid := range existIDs {
			existingNodeIDs[eid] = struct{}{}
		}
	}

	seen := make(map[int64]struct{}, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		if _, ok := seen[nodeID]; ok {
			return nil, errors.New("服务器重复")
		}
		seen[nodeID] = struct{}{}
		state.NodeIDList = append(state.NodeIDList, nodeID)
		node, err := h.repo.GetNodeRecordTx(tx, nodeID)
		if err != nil {
			if strings.Contains(err.Error(), "不存在") {
				return nil, errors.New("服务器不存在")
			}
			return nil, err
		}
		if node == nil {
			return nil, errors.New("服务器不存在")
		}
		if node.IsRemote != 1 && node.Status != 1 {
			// For tunnel updates, allow offline nodes that already belong to the
			// tunnel (user may be removing them). Only reject genuinely new offline nodes.
			_, isExisting := existingNodeIDs[nodeID]
			if excludeTunnelID <= 0 || !isExisting {
				return nil, errors.New("部分服务器不在线")
			}
		}
		state.Nodes[nodeID] = node
	}

	for _, outNode := range state.OutNodes {
		if err := validateRemoteNodePort(state.Nodes[outNode.NodeID], outNode.Port); err != nil {
			return nil, err
		}
	}
	for _, hop := range state.ChainHops {
		for _, chainNode := range hop {
			if err := validateRemoteNodePort(state.Nodes[chainNode.NodeID], chainNode.Port); err != nil {
				return nil, err
			}
		}
	}

	return state, nil
}

func validateTunnelRuntimeBeforeApply(state *tunnelCreateState) error {
	if state.Type != 2 {
		return nil
	}
	for _, node := range state.Nodes {
		if node.IsRemote == 1 && (strings.TrimSpace(node.RemoteURL) == "" || strings.TrimSpace(node.RemoteToken) == "") {
			return fmt.Errorf("远程服务器 %s 缺少共享配置", nodeDisplayName(node))
		}
	}
	groups := append([][]tunnelRuntimeNode{state.InNodes}, state.ChainHops...)
	groups = append(groups, state.OutNodes)
	for i := 0; i+1 < len(groups); i++ {
		for _, source := range groups[i] {
			for _, target := range groups[i+1] {
				node := state.Nodes[target.NodeID]
				if node == nil {
					return errors.New("服务器不存在")
				}
				if _, err := selectTunnelDialHost(state.Nodes[source.NodeID], node, state.IPPreference, target.ConnectIP); err != nil {
					return err
				}
				if node.IsRemote != 1 && target.Port <= 0 {
					return errors.New("服务器端口不能为空")
				}
			}
		}
	}
	return nil
}

// Exercise known chain write constraints inside the validation transaction.
// Remote ports that have not been reserved yet remain NULL until actual apply.
func (h *Handler) validateTunnelChainReplacementTx(tx *gorm.DB, state *tunnelCreateState) error {
	if err := h.repo.DeleteChainTunnelsByTunnelTx(tx, state.TunnelID); err != nil {
		return err
	}
	groups := append([][]tunnelRuntimeNode{state.InNodes, state.OutNodes}, state.ChainHops...)
	for groupIndex, group := range groups {
		for nodeIndex, node := range group {
			inx := nodeIndex + 1
			if node.ChainType == 2 {
				inx = groupIndex - 1
			}
			port := sql.NullInt64{Int64: int64(node.Port), Valid: node.Port > 0}
			if err := h.repo.CreateChainTunnelTx(tx, state.TunnelID, strconv.Itoa(node.ChainType),
				node.NodeID, port, node.Strategy, inx, node.Protocol, node.ConnectIP); err != nil {
				return err
			}
		}
	}
	return nil
}

func buildTunnelInIP(inNodes []tunnelRuntimeNode, nodes map[int64]*nodeRecord, ipPreference string) string {
	set := make(map[string]struct{})
	ordered := make([]string, 0)
	preferV6 := strings.TrimSpace(ipPreference) == "v6"
	for _, inNode := range inNodes {
		node := nodes[inNode.NodeID]
		if node == nil {
			continue
		}
		v4 := strings.TrimSpace(node.ServerIPv4)
		v6 := strings.TrimSpace(node.ServerIPv6)
		var addrs []string
		if preferV6 {
			if v6 != "" {
				addrs = append(addrs, v6)
			}
			if v4 != "" {
				addrs = append(addrs, v4)
			}
		} else {
			if v4 != "" {
				addrs = append(addrs, v4)
			}
			if v6 != "" {
				addrs = append(addrs, v6)
			}
		}
		if len(addrs) == 0 {
			if v := strings.TrimSpace(node.ServerIP); v != "" {
				addrs = append(addrs, v)
			}
		}
		for _, a := range addrs {
			if _, ok := set[a]; !ok {
				set[a] = struct{}{}
				ordered = append(ordered, a)
			}
		}
	}
	return strings.Join(ordered, ",")
}

func validateTunnelConnectIPConstraints(req map[string]interface{}) error {
	outNodes := asMapSlice(req["outNodeId"])
	if len(outNodes) > 1 {
		for _, item := range outNodes {
			if strings.TrimSpace(asString(item["connectIp"])) != "" {
				return fmt.Errorf("多出口隧道不支持设置自定义连接IP")
			}
		}
	}

	for hopIdx, hopRaw := range asAnySlice(req["chainNodes"]) {
		hopNodes := asMapSlice(hopRaw)
		if len(hopNodes) <= 1 {
			continue
		}

		for _, item := range hopNodes {
			if strings.TrimSpace(asString(item["connectIp"])) != "" {
				return fmt.Errorf("转发链第%d跳有多个服务器时不支持设置自定义连接IP", hopIdx+1)
			}
		}
	}

	return nil
}

func applyTunnelPortsToRequest(req map[string]interface{}, state *tunnelCreateState) {
	if req == nil || state == nil {
		return
	}
	outPorts := make(map[int64]int)
	for _, n := range state.OutNodes {
		outPorts[n.NodeID] = n.Port
	}
	for _, item := range asMapSlice(req["outNodeId"]) {
		nodeID := asInt64(item["nodeId"], 0)
		if port, ok := outPorts[nodeID]; ok && port > 0 {
			item["port"] = port
		}
	}

	chainPorts := make(map[int64]int)
	for _, hop := range state.ChainHops {
		for _, n := range hop {
			chainPorts[n.NodeID] = n.Port
		}
	}
	for _, hopRaw := range asAnySlice(req["chainNodes"]) {
		for _, item := range asMapSlice(hopRaw) {
			nodeID := asInt64(item["nodeId"], 0)
			if port, ok := chainPorts[nodeID]; ok && port > 0 {
				item["port"] = port
			}
		}
	}
}

func (h *Handler) replaceTunnelChainsTx(tx *gorm.DB, tunnelID int64, req map[string]interface{}) error {
	allocated := map[int64]int{}
	inNodes := asMapSlice(req["inNodeId"])
	for i, n := range inNodes {
		nodeID := asInt64(n["nodeId"], 0)
		if nodeID <= 0 {
			continue
		}
		if err := h.repo.CreateChainTunnelTx(
			tx,
			tunnelID,
			"1",
			nodeID,
			sql.NullInt64{},
			defaultString(asString(n["strategy"]), "round"),
			i+1,
			defaultString(asString(n["protocol"]), "tls"),
			"",
		); err != nil {
			return err
		}
	}
	for i, n := range asMapSlice(req["outNodeId"]) {
		nodeID := asInt64(n["nodeId"], 0)
		if nodeID <= 0 {
			continue
		}
		port := asInt(n["port"], 0)
		if port <= 0 {
			var pickErr error
			port, pickErr = h.repo.PickRandomNodePortTx(tx, nodeID, allocated, 0)
			if pickErr != nil {
				return pickErr
			}
		}
		connectIp := asString(n["connectIp"])
		if err := h.repo.CreateChainTunnelTx(
			tx,
			tunnelID,
			"3",
			nodeID,
			sql.NullInt64{Int64: int64(port), Valid: true},
			defaultString(asString(n["strategy"]), "round"),
			i+1,
			defaultString(asString(n["protocol"]), "tls"),
			connectIp,
		); err != nil {
			return err
		}
	}
	chainNodes := asAnySlice(req["chainNodes"])
	for i, grp := range chainNodes {
		for _, n := range asMapSlice(grp) {
			nodeID := asInt64(n["nodeId"], 0)
			if nodeID <= 0 {
				continue
			}
			port := asInt(n["port"], 0)
			if port <= 0 {
				var pickErr error
				port, pickErr = h.repo.PickRandomNodePortTx(tx, nodeID, allocated, 0)
				if pickErr != nil {
					return pickErr
				}
			}
			connectIp := asString(n["connectIp"])
			if err := h.repo.CreateChainTunnelTx(
				tx,
				tunnelID,
				"2",
				nodeID,
				sql.NullInt64{Int64: int64(port), Valid: true},
				defaultString(asString(n["strategy"]), "round"),
				i+1,
				defaultString(asString(n["protocol"]), "tls"),
				connectIp,
			); err != nil {
				return err
			}
		}
	}
	return nil
}
