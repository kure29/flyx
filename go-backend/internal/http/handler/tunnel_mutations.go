package handler

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go-backend/internal/http/client"
	"go-backend/internal/http/response"
	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"

	"gorm.io/gorm"
)

func (h *Handler) tunnelCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req map[string]interface{}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	flow, trafficRatio, err := parseTunnelBilling(req)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	if err := validateTunnelConnectIPConstraints(req); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	name := asString(req["name"])
	if name == "" {
		response.WriteJSON(w, response.ErrDefault("隧道名称不能为空"))
		return
	}
	nameDup, err := h.repo.TunnelNameExists(name)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if nameDup {
		response.WriteJSON(w, response.ErrDefault("隧道名称重复"))
		return
	}

	typeVal := asInt(req["type"], 1)
	status := asInt(req["status"], 1)
	inIP := asString(req["inIp"])
	ipPreference := asString(req["ipPreference"])
	probeTarget, probeTargetConfigured, err := parseTunnelProbeTargetFromRequest(req)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	probeTargetHost := ""
	probeTargetPort := 0
	if probeTargetConfigured {
		probeTargetHost = probeTarget.Host
		probeTargetPort = probeTarget.Port
	}
	now := time.Now().UnixMilli()
	inx := h.repo.NextIndex("tunnel")
	localDomain := h.federationLocalDomain()

	tx := h.repo.BeginTx()
	if tx.Error != nil {
		response.WriteJSON(w, response.Err(-2, tx.Error.Error()))
		return
	}
	defer func() { tx.Rollback() }()

	runtimeState, err := h.prepareTunnelCreateState(tx, req, typeVal, 0)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	runtimeState.IPPreference = ipPreference
	if strings.TrimSpace(inIP) == "" {
		inIP = buildTunnelInIP(runtimeState.InNodes, runtimeState.Nodes, ipPreference)
	}
	entryNodeIDs := make([]int64, 0, len(runtimeState.InNodes))
	for _, inNode := range runtimeState.InNodes {
		if inNode.NodeID > 0 {
			entryNodeIDs = append(entryNodeIDs, inNode.NodeID)
		}
	}
	if err := h.validateNftablesTunnelStateTx(tx, entryNodeIDs); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	engine, err := normalizeTunnelEngine(asString(req["engine"]))
	if err == nil && engine == tunnelEngineRealm {
		err = h.validateRealmTunnelTx(tx, typeVal, entryNodeIDs)
	}
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	if err := validateTunnelRuntimeBeforeApply(runtimeState); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}

	if len(runtimeState.InNodes) > 0 {
		firstNodeID := runtimeState.InNodes[0].NodeID
		isRemote, rUrl, rToken, _ := h.repo.GetNodeRemoteFieldsTx(tx, firstNodeID)
		if isRemote == 1 {
			fc := client.NewFederationClient()

			targetProto := "tcp"
			targetPort := 0
			targetAddr := ""

			if typeVal == 1 {
				if len(runtimeState.OutNodes) > 0 {
					outNode := runtimeState.OutNodes[0]
					outNodeRec := runtimeState.Nodes[outNode.NodeID]
					targetAddr = processServerAddress(outNodeRec.ServerIP)
					if outNode.Port > 0 {
						targetAddr = fmt.Sprintf("%s:%d", targetAddr, outNode.Port)
					}
				}
				if len(runtimeState.InNodes) > 0 {
					inNodesRaw := asMapSlice(req["inNodeId"])
					if len(inNodesRaw) > 0 {
						targetPort = asInt(inNodesRaw[0]["port"], 0)
						targetProto = defaultString(asString(inNodesRaw[0]["protocol"]), "tcp")
					}
				}

				if targetPort > 0 && targetAddr != "" {
					inNodeRec := runtimeState.Nodes[firstNodeID]
					if err := validateRemoteNodePort(inNodeRec, targetPort); err != nil {
						response.WriteJSON(w, response.ErrDefault(err.Error()))
						return
					}
					domainCfg, _ := h.repo.GetConfigByName("panel_domain")
					localDomain := ""
					if domainCfg != nil {
						localDomain = domainCfg.Value
					}
					_, err := fc.CreateTunnel(rUrl.String, rToken.String, localDomain, targetProto, targetPort, targetAddr)
					if err != nil {
						response.WriteJSON(w, response.ErrDefault("Remote tunnel creation failed: "+err.Error()))
						return
					}
				}
			}
		}
	}

	var tunnelInIP sql.NullString
	if trimmed := strings.TrimSpace(inIP); trimmed != "" {
		tunnelInIP = sql.NullString{String: trimmed, Valid: true}
	}
	tunnelProtocol := "tls"
	if len(runtimeState.InNodes) > 0 && strings.TrimSpace(runtimeState.InNodes[0].Protocol) != "" {
		tunnelProtocol = strings.TrimSpace(runtimeState.InNodes[0].Protocol)
	}
	tunnel := model.Tunnel{
		Name:            name,
		TrafficRatio:    trafficRatio,
		Type:            typeVal,
		Protocol:        tunnelProtocol,
		Flow:            flow,
		CreatedTime:     now,
		UpdatedTime:     now,
		Status:          status,
		InIP:            tunnelInIP,
		Inx:             inx,
		IPPreference:    ipPreference,
		ProbeTargetHost: probeTargetHost,
		ProbeTargetPort: probeTargetPort,
		Engine:          engine,
	}
	if err := tx.Create(&tunnel).Error; err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	tunnelID := tunnel.ID
	runtimeState.TunnelID = tunnelID
	var federationBindings []repo.FederationTunnelBinding
	var federationReleaseRefs []federationRuntimeReleaseRef
	federationBindings, federationReleaseRefs, err = h.applyFederationRuntime(runtimeState, localDomain)
	if err != nil {
		tx.Rollback()
		err = errors.Join(err, h.releaseFederationRuntimeRefs(federationReleaseRefs))
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	applyTunnelPortsToRequest(req, runtimeState)
	if err := h.replaceTunnelChainsTx(tx, tunnelID, req); err != nil {
		tx.Rollback()
		err = errors.Join(err, h.releaseFederationRuntimeRefs(federationReleaseRefs))
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if err := h.repo.ReplaceFederationTunnelBindingsTx(tx, tunnelID, federationBindings); err != nil {
		tx.Rollback()
		err = errors.Join(err, h.releaseFederationRuntimeRefs(federationReleaseRefs))
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		err = errors.Join(err, h.releaseFederationRuntimeRefs(federationReleaseRefs))
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if typeVal == 2 {
		createdChains, createdServices, applyErr := h.applyTunnelRuntime(runtimeState)
		if applyErr != nil {
			h.rollbackTunnelRuntime(createdChains, createdServices, tunnelID, tunnelProtocol)
			if cleanupErr := h.cleanupFederationRuntime(tunnelID); cleanupErr != nil {
				applyErr = errors.Join(applyErr, cleanupErr)
			} else {
				_ = h.deleteTunnelByID(tunnelID)
			}
			response.WriteJSON(w, response.ErrDefault(applyErr.Error()))
			return
		}
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) cleanupTunnelRuntime(tunnelID int64) {
	tunnel, err := h.getTunnelRecord(tunnelID)
	if err != nil || tunnel.Type != 2 {
		return
	}
	chainRows, err := h.listChainNodesForTunnel(tunnelID)
	if err != nil {
		return
	}

	chainName := fmt.Sprintf("chains_%d", tunnelID)
	serviceNames := tunnelRuntimeServiceNames(tunnelID)

	for _, row := range chainRows {
		if row.ChainType == 1 {
			_, _ = h.sendNodeCommand(row.NodeID, "DeleteChains", map[string]interface{}{"chain": chainName}, false, true)
		} else if row.ChainType == 2 {
			_, _ = h.sendNodeCommand(row.NodeID, "DeleteChains", map[string]interface{}{"chain": chainName}, false, true)
			_, _ = h.sendNodeCommand(row.NodeID, "DeleteService", map[string]interface{}{"services": serviceNames}, false, true)
		} else if row.ChainType == 3 {
			_, _ = h.sendNodeCommand(row.NodeID, "DeleteService", map[string]interface{}{"services": serviceNames}, false, true)
		}
	}
}

func tunnelRuntimeServiceNames(tunnelID int64) []string {
	return []string{
		fmt.Sprintf("tunnel_%d", tunnelID),
		fmt.Sprintf("%d_tls", tunnelID),
		fmt.Sprintf("%d_kcp", tunnelID),
		fmt.Sprintf("%d_wss", tunnelID),
		fmt.Sprintf("%d_mtls", tunnelID),
		fmt.Sprintf("%d_mwss", tunnelID),
		fmt.Sprintf("%d_tcp", tunnelID),
		fmt.Sprintf("%d_mtcp", tunnelID),
	}
}

func tunnelRuntimeNeedsChain(row chainNodeRecord) bool {
	return row.ChainType == 1 || row.ChainType == 2
}

func tunnelRuntimeNeedsService(row chainNodeRecord) bool {
	return row.ChainType == 2 || row.ChainType == 3
}

func removedTunnelRuntimeNodeIDs(oldRows, newRows []chainNodeRecord, needsRuntime func(chainNodeRecord) bool) []int64 {
	if len(oldRows) == 0 || needsRuntime == nil {
		return nil
	}
	newRuntimeNodes := make(map[int64]struct{}, len(newRows))
	for _, row := range newRows {
		if row.NodeID <= 0 || !needsRuntime(row) {
			continue
		}
		newRuntimeNodes[row.NodeID] = struct{}{}
	}
	seen := make(map[int64]struct{}, len(oldRows))
	removed := make([]int64, 0)
	for _, row := range oldRows {
		if row.NodeID <= 0 || !needsRuntime(row) {
			continue
		}
		if _, ok := seen[row.NodeID]; ok {
			continue
		}
		seen[row.NodeID] = struct{}{}
		if _, stillNeeded := newRuntimeNodes[row.NodeID]; stillNeeded {
			continue
		}
		removed = append(removed, row.NodeID)
	}
	return removed
}

func (h *Handler) cleanupObsoleteTunnelRuntime(tunnelID int64, oldRows, newRows []chainNodeRecord) {
	if h == nil || tunnelID <= 0 || len(oldRows) == 0 {
		return
	}
	chainName := fmt.Sprintf("chains_%d", tunnelID)
	for _, nodeID := range removedTunnelRuntimeNodeIDs(oldRows, newRows, tunnelRuntimeNeedsChain) {
		_, _ = h.sendNodeCommand(nodeID, "DeleteChains", map[string]interface{}{"chain": chainName}, false, true)
	}
	serviceNames := tunnelRuntimeServiceNames(tunnelID)
	for _, nodeID := range removedTunnelRuntimeNodeIDs(oldRows, newRows, tunnelRuntimeNeedsService) {
		_, _ = h.sendNodeCommand(nodeID, "DeleteService", map[string]interface{}{"services": serviceNames}, false, true)
	}
}

func (h *Handler) tunnelGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	id := idFromBody(r, w)
	if id <= 0 {
		return
	}
	items, err := h.repo.ListTunnels()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	h.attachBestExitStates(items)
	for _, it := range items {
		if asInt64(it["id"], 0) == id {
			response.WriteJSON(w, response.OK(it))
			return
		}
	}
	response.WriteJSON(w, response.ErrDefault("隧道不存在"))
}

func (h *Handler) tunnelUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req map[string]interface{}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	flow, trafficRatio, err := parseTunnelBilling(req)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	if err := validateTunnelConnectIPConstraints(req); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	id := asInt64(req["id"], 0)
	if id <= 0 {
		response.WriteJSON(w, response.ErrDefault("隧道ID不能为空"))
		return
	}
	typeVal := asInt(req["type"], 1)
	ipPreference := asString(req["ipPreference"])
	_, hasProbeTargetHost := req["probeTargetHost"]
	_, hasProbeTargetPort := req["probeTargetPort"]
	probeTargetFieldsPresent := hasProbeTargetHost || hasProbeTargetPort
	probeTargetHost := ""
	probeTargetPort := 0
	if probeTargetFieldsPresent {
		probeTarget, probeTargetConfigured, err := parseTunnelProbeTargetFromRequest(req)
		if err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
		if probeTargetConfigured {
			probeTargetHost = probeTarget.Host
			probeTargetPort = probeTarget.Port
		}
	}
	oldEntryNodeIDs, err := h.tunnelEntryNodeIDs(id)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	oldTunnel, err := h.getTunnelRecord(id)
	if err != nil || oldTunnel == nil {
		response.WriteJSON(w, response.ErrDefault("隧道不存在"))
		return
	}
	if !probeTargetFieldsPresent && oldTunnel != nil {
		probeTargetHost = oldTunnel.ProbeTargetHost
		probeTargetPort = oldTunnel.ProbeTargetPort
	}
	oldChainRows, err := h.listChainNodesForTunnel(id)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}

	now := time.Now().UnixMilli()
	localDomain := h.federationLocalDomain()

	// Check the complete request and known database constraints before changing
	// any existing listener or remote binding.
	validationTx := h.repo.BeginTx()
	if validationTx.Error != nil {
		response.WriteJSON(w, response.Err(-2, validationTx.Error.Error()))
		return
	}
	defer validationTx.Rollback()
	runtimeState, err := h.prepareTunnelCreateState(validationTx, req, typeVal, id)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	runtimeState.TunnelID = id
	runtimeState.IPPreference = ipPreference
	entryNodeIDs := make([]int64, 0, len(runtimeState.InNodes))
	for _, inNode := range runtimeState.InNodes {
		if inNode.NodeID > 0 {
			entryNodeIDs = append(entryNodeIDs, inNode.NodeID)
		}
	}
	if err := h.validateNftablesTunnelStateTx(validationTx, entryNodeIDs); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	engine := oldTunnel.Engine
	if raw, ok := req["engine"]; ok {
		if engine, err = normalizeTunnelEngine(asString(raw)); err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
	}
	if engine == tunnelEngineRealm {
		if err := h.validateRealmTunnelTx(validationTx, typeVal, entryNodeIDs); err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
	}

	if err := h.validateTunnelEntryPortConflictsForNewEntriesTx(validationTx, id, oldEntryNodeIDs, entryNodeIDs); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	if err := validateTunnelRuntimeBeforeApply(runtimeState); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}

	inIp := buildTunnelInIP(runtimeState.InNodes, runtimeState.Nodes, ipPreference)

	updateProtocol := "tls"
	if len(runtimeState.OutNodes) > 0 && strings.TrimSpace(runtimeState.OutNodes[0].Protocol) != "" {
		updateProtocol = strings.TrimSpace(runtimeState.OutNodes[0].Protocol)
	} else if len(runtimeState.InNodes) > 0 && strings.TrimSpace(runtimeState.InNodes[0].Protocol) != "" {
		updateProtocol = strings.TrimSpace(runtimeState.InNodes[0].Protocol)
	}
	if err := h.repo.UpdateTunnelTx(validationTx, id, asString(req["name"]), typeVal,
		flow, trafficRatio, asInt(req["status"], 1),
		inIp, ipPreference, updateProtocol, probeTargetHost, probeTargetPort, now); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if err := h.validateTunnelChainReplacementTx(validationTx, runtimeState); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if err := validationTx.Rollback().Error; err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if err := h.cleanupFederationRuntime(id); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	if oldTunnel.Type == 2 && typeVal != 2 {
		h.cleanupTunnelRuntime(id)
	}

	var federationBindings []repo.FederationTunnelBinding
	var federationReleaseRefs []federationRuntimeReleaseRef
	federationBindings, federationReleaseRefs, err = h.applyFederationRuntime(runtimeState, localDomain)
	if err != nil {
		err = errors.Join(err, h.releaseFederationRuntimeRefs(federationReleaseRefs))
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	applyTunnelPortsToRequest(req, runtimeState)

	tx := h.repo.BeginTx()
	if tx.Error != nil {
		tx.Rollback()
		err := errors.Join(tx.Error, h.releaseFederationRuntimeRefs(federationReleaseRefs))
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	defer func() { tx.Rollback() }()

	if err := h.repo.UpdateTunnelTx(
		tx,
		id,
		asString(req["name"]),
		typeVal,
		flow,
		trafficRatio,
		asInt(req["status"], 1),
		inIp,
		ipPreference,
		updateProtocol,
		probeTargetHost,
		probeTargetPort,
		now,
	); err != nil {
		tx.Rollback()
		err = errors.Join(err, h.releaseFederationRuntimeRefs(federationReleaseRefs))
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}

	if err := h.repo.SetTunnelEngineTx(tx, id, engine); err != nil {
		tx.Rollback()
		err = errors.Join(err, h.releaseFederationRuntimeRefs(federationReleaseRefs))
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if err := h.repo.DeleteChainTunnelsByTunnelTx(tx, id); err != nil {
		tx.Rollback()
		err = errors.Join(err, h.releaseFederationRuntimeRefs(federationReleaseRefs))
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if err := h.replaceTunnelChainsTx(tx, id, req); err != nil {
		tx.Rollback()
		err = errors.Join(err, h.releaseFederationRuntimeRefs(federationReleaseRefs))
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if err := h.repo.ReplaceFederationTunnelBindingsTx(tx, id, federationBindings); err != nil {
		tx.Rollback()
		err = errors.Join(err, h.releaseFederationRuntimeRefs(federationReleaseRefs))
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}

	newEntryNodeIDs := make([]int64, 0, len(runtimeState.InNodes))
	for _, in := range runtimeState.InNodes {
		if in.NodeID > 0 {
			newEntryNodeIDs = append(newEntryNodeIDs, in.NodeID)
		}
	}
	if err := h.validateTunnelEntryPortConflictsForNewEntriesTx(tx, id, oldEntryNodeIDs, newEntryNodeIDs); err != nil {
		tx.Rollback()
		err = errors.Join(err, h.releaseFederationRuntimeRefs(federationReleaseRefs))
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}

	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		err = errors.Join(err, h.releaseFederationRuntimeRefs(federationReleaseRefs))
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}

	newEntryNodeIDs, _ = h.tunnelEntryNodeIDs(id)
	if !sameInt64Set(oldEntryNodeIDs, newEntryNodeIDs) {
		h.cleanupTunnelForwardRuntimesOnRemovedEntryNodes(id, oldEntryNodeIDs, newEntryNodeIDs)
		h.syncTunnelForwardsEntryPorts(id, newEntryNodeIDs)
	}

	if typeVal == 2 {
		applyRuntime := h.applyTunnelRuntime
		if oldTunnel != nil && oldTunnel.Type == 2 {
			applyRuntime = h.applyTunnelRuntimeUpsert
		}
		createdChains, createdServices, applyErr := applyRuntime(runtimeState)
		if applyErr != nil {
			updateProtocol := "tls"
			if len(runtimeState.InNodes) > 0 && strings.TrimSpace(runtimeState.InNodes[0].Protocol) != "" {
				updateProtocol = strings.TrimSpace(runtimeState.InNodes[0].Protocol)
			}
			if oldTunnel == nil || oldTunnel.Type != 2 {
				h.rollbackTunnelRuntime(createdChains, createdServices, id, updateProtocol)
			}
			applyErr = errors.Join(applyErr, h.cleanupFederationRuntime(id))
			if len(federationReleaseRefs) == 0 && shouldDeferTunnelRuntimeApplyError(applyErr) {
				response.WriteJSON(w, response.OKEmpty())
				return
			}
			response.WriteJSON(w, response.ErrDefault(applyErr.Error()))
			return
		}
		newChainRows, _ := h.listChainNodesForTunnel(id)
		h.cleanupObsoleteTunnelRuntime(id, oldChainRows, newChainRows)
	}

	if engine != oldTunnel.Engine {
		h.switchTunnelEngine(id, tunnelIsRealm(oldTunnel), oldEntryNodeIDs, newEntryNodeIDs)
		response.WriteJSON(w, response.OKEmpty())
		return
	}
	if engine == tunnelEngineRealm {
		// realm renders whole nodes: re-render the old and new entries
		// (forward ports, tunnel status).
		if _, err := h.syncRealmNodes(append(append([]int64{}, oldEntryNodeIDs...), newEntryNodeIDs...), nil); err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
		response.WriteJSON(w, response.OKEmpty())
		return
	}

	oldType := 0
	if oldTunnel != nil {
		oldType = oldTunnel.Type
	}
	if tunnelForwardRuntimeNeedsSync(oldType, typeVal, oldEntryNodeIDs, newEntryNodeIDs) {
		forwards, fwdErr := h.listForwardsByTunnel(id)
		if fwdErr != nil {
			response.WriteJSON(w, response.OKEmpty())
			return
		}
		for i := range forwards {
			_ = h.syncForwardServices(&forwards[i], "UpdateService", true)
		}
	}

	response.WriteJSON(w, response.OKEmpty())
}

func tunnelForwardRuntimeNeedsSync(oldType, newType int, oldEntryNodeIDs, newEntryNodeIDs []int64) bool {
	if oldType != newType {
		return true
	}
	return !sameInt64Set(oldEntryNodeIDs, newEntryNodeIDs)
}

func sameInt64Set(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	m := make(map[int64]int, len(a))
	for _, v := range a {
		m[v]++
	}
	for _, v := range b {
		c, ok := m[v]
		if !ok || c <= 0 {
			return false
		}
		if c == 1 {
			delete(m, v)
			continue
		}
		m[v] = c - 1
	}
	return len(m) == 0
}

func pickForwardPortFromRecords(ports []forwardPortRecord) int {
	min := 0
	for _, fp := range ports {
		if fp.Port <= 0 {
			continue
		}
		if min == 0 || fp.Port < min {
			min = fp.Port
		}
	}
	return min
}

func forwardPortNodeIDs(ports []forwardPortRecord) []int64 {
	ids := make([]int64, 0, len(ports))
	for _, fp := range ports {
		if fp.NodeID <= 0 {
			continue
		}
		ids = append(ids, fp.NodeID)
	}
	return uniqueInt64s(ids)
}

func (h *Handler) deleteForwardServicesOnNodeBatch(forward *forwardRecord, nodeID int64) error {
	if h == nil || forward == nil || nodeID <= 0 {
		return errors.New("invalid forward service cleanup context")
	}
	if tunnel, err := h.getTunnelRecord(forward.TunnelID); err == nil && tunnelIsRealm(tunnel) {
		// The forward left a realm tunnel: re-rendering drops it there.
		_, err := h.syncRealmNodes([]int64{nodeID}, nil)
		return err
	}
	bases, err := h.forwardServiceBaseCandidates(forward)
	if err != nil {
		return err
	}
	if len(bases) == 0 {
		return nil
	}

	names := make([]string, 0, len(bases)*3)
	seen := make(map[string]struct{}, len(bases)*3)
	appendName := func(name string) {
		if strings.TrimSpace(name) == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	for _, base := range bases {
		appendName(base + "_tcp")
		appendName(base + "_udp")
		appendName(base)
	}
	if len(names) == 0 {
		return nil
	}

	payload := map[string]interface{}{"services": names}
	_, err = h.sendNodeCommand(nodeID, "DeleteService", payload, false, true)
	return err
}

func uniqueInt64s(input []int64) []int64 {
	if len(input) <= 1 {
		return input
	}
	seen := make(map[int64]struct{}, len(input))
	out := make([]int64, 0, len(input))
	for _, v := range input {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func diffInt64s(base, subtract []int64) []int64 {
	if len(base) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(subtract))
	for _, v := range subtract {
		seen[v] = struct{}{}
	}
	out := make([]int64, 0, len(base))
	for _, v := range base {
		if _, ok := seen[v]; ok {
			continue
		}
		out = append(out, v)
	}
	return uniqueInt64s(out)
}

func (h *Handler) cleanupTunnelForwardRuntimesOnRemovedEntryNodes(tunnelID int64, oldEntryNodeIDs, newEntryNodeIDs []int64) {
	if h == nil || h.repo == nil || tunnelID <= 0 {
		return
	}

	removedNodeIDs := diffInt64s(oldEntryNodeIDs, newEntryNodeIDs)
	if len(removedNodeIDs) == 0 {
		return
	}

	forwards, err := h.listForwardsByTunnel(tunnelID)
	if err != nil || len(forwards) == 0 {
		return
	}

	for i := range forwards {
		f := &forwards[i]
		if f == nil {
			continue
		}
		for _, nodeID := range removedNodeIDs {
			_ = h.deleteForwardServicesOnNode(f, nodeID)
		}
	}
}

func (h *Handler) validateForwardPortAvailabilityTx(tx *gorm.DB, node *nodeRecord, port int, currentForwardID int64) error {
	if h == nil || h.repo == nil || tx == nil || node == nil || port <= 0 {
		return nil
	}
	occupied, err := h.repo.HasOtherForwardOnNodePortTx(tx, node.ID, port, currentForwardID)
	if err != nil {
		return err
	}
	if occupied {
		return fmt.Errorf("服务器 %s 端口 %d 已被其他转发占用", node.Name, port)
	}
	return nil
}

func (h *Handler) validateTunnelEntryPortConflictsForNewEntriesTx(tx *gorm.DB, tunnelID int64, oldEntryNodeIDs, newEntryNodeIDs []int64) error {
	if h == nil || h.repo == nil || tx == nil || tunnelID <= 0 {
		return nil
	}

	addedNodeIDs := diffInt64s(newEntryNodeIDs, oldEntryNodeIDs)
	if len(addedNodeIDs) == 0 {
		return nil
	}

	forwards, err := h.repo.ListForwardsByTunnelTx(tx, tunnelID)
	if err != nil {
		return err
	}
	if len(forwards) == 0 {
		return nil
	}

	for i := range forwards {
		f := &forwards[i]
		if f == nil {
			continue
		}
		oldPorts, portsErr := h.repo.ListForwardPortsTx(tx, f.ID)
		if portsErr != nil {
			return portsErr
		}
		port := pickForwardPortFromRecords(oldPorts)
		if port <= 0 {
			continue
		}

		for _, nodeID := range addedNodeIDs {
			node, nodeErr := h.repo.GetNodeRecordTx(tx, nodeID)
			if nodeErr != nil {
				return nodeErr
			}

			if err := h.validateForwardPortAvailabilityTx(tx, node, port, f.ID); err != nil {
				return fmt.Errorf("转发 %s 入口端口冲突: %w", f.Name, err)
			}
		}
	}

	return nil
}

func (h *Handler) syncTunnelForwardsEntryPorts(tunnelID int64, entryNodeIDs []int64) {
	if h == nil || h.repo == nil || tunnelID <= 0 {
		return
	}
	entryNodeIDs = uniqueInt64s(entryNodeIDs)
	if len(entryNodeIDs) == 0 {
		return
	}

	forwards, err := h.listForwardsByTunnel(tunnelID)
	if err != nil || len(forwards) == 0 {
		return
	}

	allowInIP := len(entryNodeIDs) == 1
	for i := range forwards {
		f := &forwards[i]
		if f == nil {
			continue
		}
		oldPorts, err := h.listForwardPorts(f.ID)
		if err != nil {
			continue
		}
		referencePort := pickForwardPortFromRecords(oldPorts)
		if referencePort <= 0 {
			continue
		}

		// Build a map of existing node → port/inIP from old records.
		oldPortByNode := make(map[int64]forwardPortRecord)
		for _, fp := range oldPorts {
			if fp.NodeID > 0 {
				oldPortByNode[fp.NodeID] = fp
			}
		}

		entries := make([]forwardPortReplaceEntry, 0, len(entryNodeIDs))
		for _, nid := range entryNodeIDs {
			if existing, ok := oldPortByNode[nid]; ok && existing.Port > 0 {
				// Existing entry node: keep its current port.
				inIP := existing.InIP
				if !allowInIP {
					inIP = ""
				}
				entries = append(entries, forwardPortReplaceEntry{NodeID: nid, Port: existing.Port, InIP: inIP})
				continue
			}

			// New entry node: try to follow the reference port.
			port := h.resolvePortForNewEntryNode(nid, referencePort, f.ID)
			inIP := ""
			if allowInIP {
				// For single-entry tunnels, try to preserve inIP from old records.
				for _, fp := range oldPorts {
					if strings.TrimSpace(fp.InIP) != "" {
						inIP = fp.InIP
						break
					}
				}
			}
			entries = append(entries, forwardPortReplaceEntry{NodeID: nid, Port: port, InIP: inIP})
		}
		_ = h.repo.ReplaceForwardPorts(f.ID, entries)
	}
}

// resolvePortForNewEntryNode determines the port for a forward on a newly added
// entry node. It tries to reuse referencePort (from existing entries); if that
// port is out of range or already occupied, it picks a random available port
// for this specific node.
func (h *Handler) resolvePortForNewEntryNode(nodeID int64, referencePort int, forwardID int64) int {
	node, err := h.getNodeRecord(nodeID)
	if err != nil {
		return referencePort
	}

	// Check if referencePort is within the node's allowed range.
	if validateLocalNodePort(node, referencePort) == nil &&
		validateRemoteNodePort(node, referencePort) == nil {
		// In range — check availability.
		occupied, occErr := h.repo.HasOtherForwardOnNodePort(nodeID, referencePort, forwardID)
		if occErr == nil && !occupied {
			return referencePort
		}
	}

	// referencePort doesn't work for this node; pick a random one.
	newPort := h.pickRandomPortForNode(nodeID)
	if newPort > 0 {
		return newPort
	}
	return referencePort // last resort fallback
}

// pickRandomPortForNode picks a random available port from a single node's
// port range, excluding ports already occupied by other forwards or chains.
func (h *Handler) pickRandomPortForNode(nodeID int64) int {
	portRange, err := h.repo.GetNodePortRange(nodeID)
	if err != nil {
		return 0
	}
	if portRange == "" {
		portRange = "1000-65535"
	}

	nodePorts, err := parsePorts(portRange)
	if err != nil || len(nodePorts) == 0 {
		return 0
	}

	used, err := h.getUsedPorts(nodeID)
	if err != nil {
		return 0
	}

	var available []int
	for _, p := range nodePorts {
		if !used[p] {
			available = append(available, p)
		}
	}

	if len(available) == 0 {
		return 0
	}

	idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(available))))
	return available[idx.Int64()]
}

func (h *Handler) tunnelDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	id := idFromBody(r, w)
	if id <= 0 {
		return
	}
	if err := h.cleanupFederationRuntime(id); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	h.cleanupTunnelRuntime(id)
	if err := h.deleteTunnelByID(id); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) tunnelDiagnose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	id := asInt64FromBodyKey(r, w, "tunnelId")
	if id <= 0 {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), diagnosisRequestTimeout)
	defer cancel()
	result, err := h.diagnoseTunnelRuntime(ctx, id)
	if err != nil {
		if strings.Contains(err.Error(), "不存在") || strings.Contains(err.Error(), "不完整") {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(result))
}

func (h *Handler) tunnelUpdateOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		Tunnels []struct {
			ID  int64 `json:"id"`
			Inx int   `json:"inx"`
		} `json:"tunnels"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	for _, t := range req.Tunnels {
		h.repo.UpdateTunnelOrder(t.ID, t.Inx, time.Now().UnixMilli())
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) tunnelBatchDelete(w http.ResponseWriter, r *http.Request) {
	ids := idsFromBody(r, w)
	if ids == nil {
		return
	}
	success := 0
	fail := 0
	failures := make([]batchFailureDetail, 0)
	for _, id := range ids {
		tunnelName, _ := h.repo.GetTunnelName(id)
		if _, err := h.getTunnelRecord(id); err != nil {
			fail++
			failures = appendBatchFailure(failures, id, tunnelName, err)
			continue
		}
		if err := h.cleanupFederationRuntime(id); err != nil {
			fail++
			failures = appendBatchFailure(failures, id, tunnelName, err)
			continue
		}
		h.cleanupTunnelRuntime(id)
		if err := h.deleteTunnelByID(id); err != nil {
			fail++
			failures = appendBatchFailure(failures, id, tunnelName, err)
		} else {
			success++
		}
	}
	response.WriteJSON(w, response.OK(batchOperationResult{SuccessCount: success, FailCount: fail, Failures: failures}))
}

func (h *Handler) redeployTunnelAndForwards(tunnelID int64) error {
	tunnel, err := h.getTunnelRecord(tunnelID)
	if err != nil {
		return err
	}

	if tunnel.Type == 2 {
		state, err := h.reconstructTunnelState(tunnelID)
		if err != nil {
			return err
		}
		if err := validateTunnelRuntimeBeforeApply(state); err != nil {
			return err
		}
		if err := h.cleanupFederationRuntime(tunnelID); err != nil {
			return err
		}
		h.cleanupTunnelRuntime(tunnelID)
		federationBindings, federationReleaseRefs, fedErr := h.applyFederationRuntime(state, h.federationLocalDomain())
		if fedErr != nil {
			return errors.Join(fedErr, h.releaseFederationRuntimeRefs(federationReleaseRefs))
		}
		tx := h.repo.BeginTx()
		if tx.Error != nil {
			return errors.Join(tx.Error, h.releaseFederationRuntimeRefs(federationReleaseRefs))
		}
		if replaceErr := h.repo.ReplaceFederationTunnelBindingsTx(tx, tunnelID, federationBindings); replaceErr != nil {
			tx.Rollback()
			return errors.Join(replaceErr, h.releaseFederationRuntimeRefs(federationReleaseRefs))
		}
		if commitErr := tx.Commit().Error; commitErr != nil {
			return errors.Join(commitErr, h.releaseFederationRuntimeRefs(federationReleaseRefs))
		}
		_, _, applyErr := h.applyTunnelRuntime(state)
		if applyErr != nil {
			applyErr = errors.Join(applyErr, h.cleanupFederationRuntime(tunnelID))
			return applyErr
		}
	}

	forwards, err := h.listForwardsByTunnel(tunnelID)
	if err != nil {
		return err
	}
	for i := range forwards {
		if err := h.syncForwardServices(&forwards[i], "UpdateService", true); err != nil {
			return err
		}
	}

	return nil
}

func (h *Handler) tunnelBatchRedeploy(w http.ResponseWriter, r *http.Request) {
	ids := idsFromBody(r, w)
	if ids == nil {
		return
	}
	success := 0
	fail := 0
	failures := make([]batchFailureDetail, 0)
	for _, tunnelID := range ids {
		tunnel, tunnelErr := h.getTunnelRecord(tunnelID)
		tunnelName := ""
		if tunnelErr == nil && tunnel != nil {
			tunnelName, _ = h.repo.GetTunnelName(tunnelID)
		}
		if tunnelErr != nil {
			fail++
			failures = appendBatchFailure(failures, tunnelID, tunnelName, tunnelErr)
			continue
		}
		if nftMode, entryNodeIDs, modeErr := h.tunnelUsesNftables(tunnelID); modeErr != nil {
			fail++
			failures = appendBatchFailure(failures, tunnelID, tunnelName, modeErr)
			continue
		} else if nftMode {
			if len(entryNodeIDs) == 0 {
				fail++
				failures = appendBatchFailureReason(failures, tunnelID, tunnelName, "nftables 转发缺少入口服务器")
				continue
			}
			if reconcileErr := h.reconcileNftablesNodeByRequest(entryNodeIDs[0]); reconcileErr != nil {
				fail++
				failures = appendBatchFailure(failures, tunnelID, tunnelName, reconcileErr)
				continue
			}
			success++
			continue
		}
		if err := h.redeployTunnelAndForwards(tunnelID); err != nil {
			fail++
			failures = appendBatchFailure(failures, tunnelID, tunnelName, err)
			continue
		}
		success++
	}
	response.WriteJSON(w, response.OK(batchOperationResult{SuccessCount: success, FailCount: fail, Failures: failures}))
}

func (h *Handler) deleteTunnelByID(id int64) error {
	if h == nil || h.repo == nil {
		return errors.New("repository not initialized")
	}

	tunnelName, _ := h.repo.GetTunnelName(id)
	tunnel, _ := h.repo.GetTunnelRecord(id)
	entryNodeIDs, _ := h.tunnelEntryNodeIDs(id)
	if err := h.repo.DeleteTunnelCascade(id); err != nil {
		return err
	}
	if tunnelIsRealm(tunnel) {
		_, _ = h.syncRealmNodes(entryNodeIDs, nil)
	}

	shareID, port, ok := parsePeerShareInfoFromFederationTunnelName(tunnelName)
	if !ok {
		return nil
	}

	return h.repo.MarkPeerShareRuntimeReleasedByPort(shareID, port, time.Now().UnixMilli())
}

func (h *Handler) tunnelEntryNodeIDs(tunnelID int64) ([]int64, error) {
	return h.repo.TunnelEntryNodeIDs(tunnelID)
}

func (h *Handler) pickTunnelPort(tunnelID int64) int {
	entryNodes, err := h.tunnelEntryNodeIDs(tunnelID)
	if err != nil || len(entryNodes) == 0 {
		return 10000
	}

	var commonAvailable []int
	firstNode := true

	for _, nodeID := range entryNodes {
		portRange, err := h.repo.GetNodePortRange(nodeID)
		if err != nil {
			continue
		}
		if portRange == "" {
			portRange = "1000-65535"
		}

		nodePorts, err := parsePorts(portRange)
		if err != nil {
			continue
		}

		used, err := h.getUsedPorts(nodeID)
		if err != nil {
			continue
		}

		var available []int
		for _, p := range nodePorts {
			if !used[p] {
				available = append(available, p)
			}
		}

		if firstNode {
			commonAvailable = available
			firstNode = false
		} else {
			set := make(map[int]bool)
			for _, p := range available {
				set[p] = true
			}
			var newCommon []int
			for _, p := range commonAvailable {
				if set[p] {
					newCommon = append(newCommon, p)
				}
			}
			commonAvailable = newCommon
		}

		if len(commonAvailable) == 0 {
			break
		}
	}

	if len(commonAvailable) > 0 {
		idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(commonAvailable))))
		return commonAvailable[idx.Int64()]
	}

	return 10000
}

func (h *Handler) getUsedPorts(nodeID int64) (map[int]bool, error) {
	return h.repo.GetUsedPortsOnNodeAsMap(nodeID)
}

func parsePorts(portRange string) ([]int, error) {
	var ports []int
	parts := strings.Split(portRange, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			rangeParts := strings.Split(part, "-")
			if len(rangeParts) != 2 {
				return nil, errors.New("invalid port range format")
			}
			start, err1 := strconv.Atoi(strings.TrimSpace(rangeParts[0]))
			end, err2 := strconv.Atoi(strings.TrimSpace(rangeParts[1]))
			if err1 != nil || err2 != nil || start > end {
				return nil, errors.New("invalid port range values")
			}
			for i := start; i <= end; i++ {
				ports = append(ports, i)
			}
		} else {
			p, err := strconv.Atoi(part)
			if err != nil {
				return nil, errors.New("invalid port value")
			}
			ports = append(ports, p)
		}
	}
	return ports, nil
}
