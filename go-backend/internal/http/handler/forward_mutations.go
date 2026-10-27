package handler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go-backend/internal/http/response"
)

func (h *Handler) forwardCreate(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	userID, roleID, err := userRoleFromRequest(r)
	if err != nil {
		response.WriteJSON(w, response.Err(401, "无效的token或token已过期"))
		return
	}
	tunnelID := asInt64(req["tunnelId"], 0)
	if tunnelID <= 0 {
		response.WriteJSON(w, response.ErrDefault("隧道ID不能为空"))
		return
	}
	if err := h.ensureTunnelPermission(userID, roleID, tunnelID); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	tunnel, err := h.getTunnelRecord(tunnelID)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault("隧道不存在"))
		return
	}
	if tunnel.Status != 1 {
		response.WriteJSON(w, response.ErrDefault("隧道已禁用，无法创建转发"))
		return
	}
	if err := h.ensureUserTunnelForwardAllowed(userID, tunnelID, time.Now().UnixMilli()); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	name := asString(req["name"])
	remoteAddr := asString(req["remoteAddr"])
	if name == "" || remoteAddr == "" {
		response.WriteJSON(w, response.ErrDefault("转发名称和目标地址不能为空"))
		return
	}
	if roleID != 0 && !h.allowLocalRemoteAddr() {
		if err := IsSafeRemoteAddr(remoteAddr); err != nil {
			response.WriteJSON(w, response.Err(403, err.Error()))
			return
		}
	}
	if roleID != 0 {
		if speedIDVal, ok := req["speedId"]; ok && speedIDVal != nil {
			response.WriteJSON(w, response.Err(-1, "普通用户无法设置限速规则"))
			return
		}
	}
	speedID := asAnyToInt64Ptr(req["speedId"])
	speedID, err = h.normalizeSpeedLimitReference(speedID)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if roleID != 0 {
		if ipSpeedIDVal, ok := req["ipSpeedId"]; ok && ipSpeedIDVal != nil {
			response.WriteJSON(w, response.Err(-1, "普通用户无法设置每 IP 限速规则"))
			return
		}
	}
	ipSpeedID := asAnyToInt64Ptr(req["ipSpeedId"])
	ipSpeedID, err = h.normalizeSpeedLimitReference(ipSpeedID)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	port := asInt(req["inPort"], 0)
	if port <= 0 {
		port = h.pickTunnelPort(tunnelID)
	}
	if port <= 0 {
		port = 10000
	}
	entryNodes, _ := h.tunnelEntryNodeIDs(tunnelID)
	isNftTunnel, _, err := h.tunnelUsesNftables(tunnelID)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if isNftTunnel {
		if err := h.validateNftablesForwardRequest(tunnel, remoteAddr, entryNodes); err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
	}
	inIp := strings.TrimSpace(asString(req["inIp"]))
	if inIp != "" && len(entryNodes) > 1 {
		response.WriteJSON(w, response.ErrDefault("多入口隧道的转发不支持自定义监听IP"))
		return
	}
	for _, nodeID := range entryNodes {
		node, nodeErr := h.getNodeRecord(nodeID)
		if nodeErr != nil {
			continue
		}
		if err := validateRemoteNodePort(node, port); err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
		if err := validateLocalNodePort(node, port); err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
		if err := h.validateForwardPortAvailability(node, port, 0); err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
	}
	now := time.Now().UnixMilli()
	inx := h.repo.NextIndex("forward")
	userName := h.repo.GetUsernameByID(userID)
	if userName == "" {
		userName = "user"
	}
	maxConn := asInt(req["maxConn"], 0)
	ipMaxConn := asInt(req["ipMaxConn"], 0)
	if ipMaxConn < 0 {
		ipMaxConn = 0
	}
	proxyProtocol := asInt(req["proxyProtocol"], 0)
	proxyProtocolReceive := asInt(req["proxyProtocolReceive"], 0)
	proxyProtocolSend := asInt(req["proxyProtocolSend"], proxyProtocol)

	forwardID, err := h.repo.CreateForwardTx(userID, userName, name, tunnelID, remoteAddr, defaultString(asString(req["strategy"]), "fifo"), now, inx, entryNodes, port, inIp, nullableInt(speedID), maxConn, ipMaxConn, nullableInt(ipSpeedID), proxyProtocol, proxyProtocolReceive, proxyProtocolSend)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	createdForward, err := h.getForwardRecord(forwardID)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if err := h.syncForwardServices(createdForward, "UpdateService", true); err != nil {
		_ = h.deleteForwardByID(forwardID)
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) forwardUpdate(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	id := asInt64(req["id"], 0)
	if id <= 0 {
		response.WriteJSON(w, response.ErrDefault("转发ID不能为空"))
		return
	}
	forward, actorUserID, actorRole, err := h.resolveForwardAccess(r, id)
	if err != nil {
		if errors.Is(err, errForwardNotFound) {
			response.WriteJSON(w, response.ErrDefault("转发不存在"))
			return
		}
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	oldPorts, err := h.listForwardPorts(id)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}

	tunnelID := asInt64(req["tunnelId"], forward.TunnelID)
	if tunnelID <= 0 {
		response.WriteJSON(w, response.ErrDefault("隧道ID不能为空"))
		return
	}
	if err := h.ensureTunnelPermission(actorUserID, actorRole, tunnelID); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	tunnel, err := h.getTunnelRecord(tunnelID)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault("隧道不存在"))
		return
	}
	if tunnel.Status != 1 {
		response.WriteJSON(w, response.ErrDefault("隧道已禁用，无法更新转发"))
		return
	}

	name := strings.TrimSpace(asString(req["name"]))
	if name == "" {
		name = forward.Name
	}
	remoteAddr := strings.TrimSpace(asString(req["remoteAddr"]))
	if remoteAddr == "" {
		remoteAddr = forward.RemoteAddr
	}
	isNftTunnel, entryNodes, err := h.tunnelUsesNftables(tunnelID)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if isNftTunnel {
		if err := h.validateNftablesForwardRequest(tunnel, remoteAddr, entryNodes); err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
	}
	if actorRole != 0 && !h.allowLocalRemoteAddr() {
		if err := IsSafeRemoteAddr(remoteAddr); err != nil {
			response.WriteJSON(w, response.Err(403, err.Error()))
			return
		}
	}
	strategy := strings.TrimSpace(asString(req["strategy"]))
	if strategy == "" {
		strategy = forward.Strategy
	}
	rawSpeedID, hasSpeedID := req["speedId"]
	requestedSpeedID := asAnyToInt64Ptr(rawSpeedID)
	if actorRole != 0 && hasSpeedID && requestedSpeedID != nil && !sameSpeedLimitSelection(forward.SpeedID, requestedSpeedID) {
		response.WriteJSON(w, response.Err(-1, "普通用户无法修改限速规则"))
		return
	}
	speedID := requestedSpeedID
	speedID, err = h.normalizeSpeedLimitReference(speedID)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	newSpeedID := forward.SpeedID
	if speedID != nil {
		newSpeedID = sql.NullInt64{Int64: *speedID, Valid: true}
	} else if _, ok := req["speedId"]; ok {
		newSpeedID = sql.NullInt64{Valid: false}
	}
	rawIPSpeedID, hasIPSpeedID := req["ipSpeedId"]
	requestedIPSpeedID := asAnyToInt64Ptr(rawIPSpeedID)
	newIPSpeedID := forward.IPSpeedID
	if actorRole != 0 {
		if hasIPSpeedID && !sameSpeedLimitSelection(forward.IPSpeedID, requestedIPSpeedID) {
			response.WriteJSON(w, response.Err(-1, "普通用户无法修改每 IP 限速规则"))
			return
		}
	} else {
		ipSpeedID := requestedIPSpeedID
		ipSpeedID, err = h.normalizeSpeedLimitReference(ipSpeedID)
		if err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
		if ipSpeedID != nil {
			newIPSpeedID = sql.NullInt64{Int64: *ipSpeedID, Valid: true}
		} else if hasIPSpeedID {
			newIPSpeedID = sql.NullInt64{Valid: false}
		}
	}

	port := asInt(req["inPort"], 0)
	if port <= 0 {
		minPort := h.repo.GetMinForwardPort(id)
		if minPort.Valid {
			port = int(minPort.Int64)
		}
		if port <= 0 {
			port = h.pickTunnelPort(tunnelID)
		}
	}
	hasInIP := false
	inIp := ""
	if rawInIP, ok := req["inIp"]; ok {
		hasInIP = true
		inIp = asString(rawInIP)
	}
	fwdEntryNodes, _ := h.tunnelEntryNodeIDs(tunnelID)
	if hasInIP && strings.TrimSpace(inIp) != "" && len(fwdEntryNodes) > 1 {
		response.WriteJSON(w, response.ErrDefault("多入口隧道的转发不支持自定义监听IP"))
		return
	}
	// When switching tunnels, entry nodes / service base may change. We must clean up old
	// listeners on nodes that will be removed, otherwise the old ports keep listening.
	tunnelChanged := tunnelID != forward.TunnelID
	oldNodeIDs := forwardPortNodeIDs(oldPorts)
	newNodeIDs := uniqueInt64s(fwdEntryNodes)
	var removedNodeIDs []int64
	var keptNodeIDs []int64
	if tunnelChanged {
		removedNodeIDs = diffInt64s(oldNodeIDs, newNodeIDs)
		keptNodeIDs = diffInt64s(oldNodeIDs, removedNodeIDs)
	}
	for _, nodeID := range fwdEntryNodes {
		node, nodeErr := h.getNodeRecord(nodeID)
		if nodeErr != nil {
			continue
		}
		if err := validateRemoteNodePort(node, port); err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
		if err := validateLocalNodePort(node, port); err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
		if err := h.validateForwardPortAvailability(node, port, id); err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
	}
	now := time.Now().UnixMilli()
	maxConn := asInt(req["maxConn"], forward.MaxConn)
	ipMaxConn := asInt(req["ipMaxConn"], forward.IPMaxConn)
	if ipMaxConn < 0 {
		ipMaxConn = 0
	}
	proxyProtocol := asInt(req["proxyProtocol"], forward.ProxyProtocol)
	proxyProtocolReceive := asInt(req["proxyProtocolReceive"], forward.ProxyProtocolReceive)
	proxyProtocolSend := asInt(req["proxyProtocolSend"], forward.ProxyProtocolSend)

	if err := h.repo.UpdateForward(id, name, tunnelID, remoteAddr, strategy, now, newSpeedID, maxConn, ipMaxConn, newIPSpeedID, proxyProtocol, proxyProtocolReceive, proxyProtocolSend); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if hasInIP {
		err = h.replaceForwardPorts(id, tunnelID, port, inIp)
	} else if tunnelID != forward.TunnelID {
		err = h.replaceForwardPorts(id, tunnelID, port, "")
	} else {
		err = h.replaceForwardPortsPreservingInIP(id, tunnelID, port, oldPorts)
	}
	if err != nil {
		h.rollbackForwardMutation(forward, oldPorts)
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	updatedForward, err := h.getForwardRecord(id)
	if err != nil {
		h.rollbackForwardMutation(forward, oldPorts)
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	warnings := make([]string, 0)
	if tunnelChanged && len(keptNodeIDs) > 0 {
		for _, nodeID := range keptNodeIDs {
			if delErr := h.deleteForwardServicesOnNodeBatch(forward, nodeID); delErr != nil {
				nodeLabel := fmt.Sprintf("%d", nodeID)
				if n, nErr := h.getNodeRecord(nodeID); nErr == nil && n != nil && strings.TrimSpace(n.Name) != "" {
					nodeLabel = strings.TrimSpace(n.Name)
				}
				warnings = append(warnings, fmt.Sprintf("服务器 %s 清理旧转发监听失败: %v", nodeLabel, delErr))
			}
		}
		time.Sleep(tunnelServiceBindRetryDelay)
	}

	syncWarnings, err := h.syncForwardServicesWithWarnings(updatedForward, "UpdateService", true)
	if err != nil {
		h.rollbackForwardMutation(forward, oldPorts)
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	warnings = append(warnings, syncWarnings...)

	// Best-effort cleanup for old entry nodes after a successful tunnel switch.
	// Avoid cleaning nodes that are still used by the updated forward.
	if tunnelChanged && len(removedNodeIDs) > 0 {
		for _, nodeID := range removedNodeIDs {
			if delErr := h.deleteForwardServicesOnNodeBatch(forward, nodeID); delErr != nil {
				nodeLabel := fmt.Sprintf("%d", nodeID)
				if n, nErr := h.getNodeRecord(nodeID); nErr == nil && n != nil && strings.TrimSpace(n.Name) != "" {
					nodeLabel = strings.TrimSpace(n.Name)
				}
				warnings = append(warnings, fmt.Sprintf("服务器 %s 清理旧隧道残留服务失败: %v", nodeLabel, delErr))
			}
		}
	}
	if len(warnings) > 0 {
		response.WriteJSON(w, response.OK(map[string]interface{}{"warnings": warnings}))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) forwardDelete(w http.ResponseWriter, r *http.Request) {
	id := idFromBody(r, w)
	if id <= 0 {
		return
	}
	forward, _, _, err := h.resolveForwardAccess(r, id)
	if err != nil {
		if errors.Is(err, errForwardNotFound) {
			response.WriteJSON(w, response.ErrDefault("转发不存在"))
			return
		}
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	var nftNodeID int64
	if nftMode, entryNodeIDs, modeErr := h.tunnelUsesNftables(forward.TunnelID); modeErr != nil {
		response.WriteJSON(w, response.Err(-2, modeErr.Error()))
		return
	} else if nftMode {
		if len(entryNodeIDs) == 0 {
			response.WriteJSON(w, response.ErrDefault("nftables 转发缺少入口服务器"))
			return
		}
		nftNodeID = entryNodeIDs[0]
	} else if err := h.controlForwardServices(forward, "DeleteService", true); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	if err := h.deleteForwardByID(id); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if nftNodeID > 0 {
		if err := h.reconcileNftablesNodeByRequest(nftNodeID); err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) forwardForceDelete(w http.ResponseWriter, r *http.Request) {
	id := idFromBody(r, w)
	if id <= 0 {
		return
	}
	forward, _, _, err := h.resolveForwardAccess(r, id)
	if err != nil {
		if errors.Is(err, errForwardNotFound) {
			response.WriteJSON(w, response.ErrDefault("转发不存在"))
			return
		}
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}

	// Force delete: remove DB record without touching node services.
	// This is used when nodes are offline or service deletion fails.
	oldPorts, _ := h.listForwardPorts(id)
	if err := h.deleteForwardByID(id); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if tunnel, err := h.getTunnelRecord(forward.TunnelID); err == nil && tunnelIsRealm(tunnel) {
		// realm renders from the database: the forward drops out on the
		// next apply, now or when the node reconnects.
		_, _ = h.syncRealmNodes(forwardPortNodeIDs(oldPorts), nil)
	}
	_ = h.repo.DeleteNftRuleBindingsByForward(id)
	if nftMode, entryNodeIDs, modeErr := h.tunnelUsesNftables(forward.TunnelID); modeErr == nil && nftMode && len(entryNodeIDs) > 0 {
		if err := h.reconcileNftablesNodeByRequest(entryNodeIDs[0]); err != nil {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) forwardPause(w http.ResponseWriter, r *http.Request) {
	h.quotaControlMu.Lock()
	defer h.quotaControlMu.Unlock()
	id := idFromBody(r, w)
	if id <= 0 {
		return
	}
	forward, _, _, err := h.resolveForwardAccess(r, id)
	if err != nil {
		if errors.Is(err, errForwardNotFound) {
			response.WriteJSON(w, response.ErrDefault("转发不存在"))
			return
		}
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if err := h.controlForwardServices(forward, "PauseService", false); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	if err := h.repo.SetManualForwardStatus(forward.UserID, id, 0, time.Now().UnixMilli()); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) forwardResetFlow(w http.ResponseWriter, r *http.Request) {
	id := idFromBody(r, w)
	if id <= 0 {
		return
	}
	if _, _, _, err := h.resolveForwardAccess(r, id); err != nil {
		if errors.Is(err, errForwardNotFound) {
			response.WriteJSON(w, response.ErrDefault("转发不存在"))
			return
		}
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if err := h.repo.ResetForwardFlow(id, time.Now().UnixMilli()); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) forwardResume(w http.ResponseWriter, r *http.Request) {
	h.quotaControlMu.Lock()
	defer h.quotaControlMu.Unlock()
	id := idFromBody(r, w)
	if id <= 0 {
		return
	}
	forward, _, _, err := h.resolveForwardAccess(r, id)
	if err != nil {
		if errors.Is(err, errForwardNotFound) {
			response.WriteJSON(w, response.ErrDefault("转发不存在"))
			return
		}
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	now := time.Now().UnixMilli()
	if err := h.ensureUserTunnelForwardAllowed(forward.UserID, forward.TunnelID, now); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	if err := h.controlForwardServices(forward, "ResumeService", false); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	if err := h.repo.SetManualForwardStatus(forward.UserID, id, 1, now); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) forwardDiagnose(w http.ResponseWriter, r *http.Request) {
	id := asInt64FromBodyKey(r, w, "forwardId")
	if id <= 0 {
		return
	}
	forward, _, _, err := h.resolveForwardAccess(r, id)
	if err != nil {
		if errors.Is(err, errForwardNotFound) {
			response.WriteJSON(w, response.ErrDefault("转发不存在"))
			return
		}
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), diagnosisRequestTimeout)
	defer cancel()
	payload, err := h.diagnoseForwardRuntime(ctx, forward)
	if err != nil {
		if strings.Contains(err.Error(), "不存在") || strings.Contains(err.Error(), "不能为空") || strings.Contains(err.Error(), "错误") {
			response.WriteJSON(w, response.ErrDefault(err.Error()))
			return
		}
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(payload))
}

func (h *Handler) forwardUpdateOrder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Forwards []struct {
			ID  int64 `json:"id"`
			Inx int   `json:"inx"`
		} `json:"forwards"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	for _, f := range req.Forwards {
		h.repo.UpdateForwardOrder(f.ID, f.Inx, time.Now().UnixMilli())
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) forwardBatchDelete(w http.ResponseWriter, r *http.Request) {
	ids := idsFromBody(r, w)
	if ids == nil {
		return
	}
	actorUserID, actorRole, err := userRoleFromRequest(r)
	if err != nil {
		response.WriteJSON(w, response.Err(401, "无效的token或token已过期"))
		return
	}
	s := 0
	f := 0
	failures := make([]batchFailureDetail, 0)
	for _, id := range ids {
		forward, accessErr := h.ensureForwardAccessByActor(actorUserID, actorRole, id)
		if accessErr != nil {
			f++
			failures = appendBatchFailure(failures, id, "", accessErr)
			continue
		}
		var nftNodeID int64
		if nftMode, entryNodeIDs, modeErr := h.tunnelUsesNftables(forward.TunnelID); modeErr != nil {
			f++
			failures = appendBatchFailure(failures, id, forward.Name, modeErr)
			continue
		} else if nftMode {
			if len(entryNodeIDs) == 0 {
				f++
				failures = appendBatchFailureReason(failures, id, forward.Name, "nftables 转发缺少入口服务器")
				continue
			}
			nftNodeID = entryNodeIDs[0]
		} else if err := h.controlForwardServices(forward, "DeleteService", true); err != nil {
			f++
			failures = appendBatchFailure(failures, id, forward.Name, err)
			continue
		}
		if err := h.deleteForwardByID(id); err != nil {
			f++
			failures = appendBatchFailure(failures, id, forward.Name, err)
			continue
		}
		if nftNodeID > 0 {
			if err := h.reconcileNftablesNodeByRequest(nftNodeID); err != nil {
				f++
				failures = appendBatchFailure(failures, id, forward.Name, err)
				continue
			}
		}
		s++
	}
	response.WriteJSON(w, response.OK(batchOperationResult{SuccessCount: s, FailCount: f, Failures: failures}))
}

func (h *Handler) forwardBatchPause(w http.ResponseWriter, r *http.Request) {
	h.quotaControlMu.Lock()
	defer h.quotaControlMu.Unlock()
	ids := idsFromBody(r, w)
	if ids == nil {
		return
	}
	actorUserID, actorRole, err := userRoleFromRequest(r)
	if err != nil {
		response.WriteJSON(w, response.Err(401, "无效的token或token已过期"))
		return
	}
	s := 0
	f := 0
	failures := make([]batchFailureDetail, 0)
	for _, id := range ids {
		forward, accessErr := h.ensureForwardAccessByActor(actorUserID, actorRole, id)
		if accessErr != nil {
			f++
			failures = appendBatchFailure(failures, id, "", accessErr)
			continue
		}
		if err := h.controlForwardServices(forward, "PauseService", false); err != nil {
			f++
			failures = appendBatchFailure(failures, id, forward.Name, err)
			continue
		}
		if err := h.repo.SetManualForwardStatus(forward.UserID, id, 0, time.Now().UnixMilli()); err != nil {
			f++
			failures = appendBatchFailure(failures, id, forward.Name, err)
		} else {
			s++
		}
	}
	response.WriteJSON(w, response.OK(batchOperationResult{SuccessCount: s, FailCount: f, Failures: failures}))
}

func (h *Handler) forwardBatchResume(w http.ResponseWriter, r *http.Request) {
	h.quotaControlMu.Lock()
	defer h.quotaControlMu.Unlock()
	ids := idsFromBody(r, w)
	if ids == nil {
		return
	}
	actorUserID, actorRole, err := userRoleFromRequest(r)
	if err != nil {
		response.WriteJSON(w, response.Err(401, "无效的token或token已过期"))
		return
	}
	s := 0
	f := 0
	now := time.Now().UnixMilli()
	failures := make([]batchFailureDetail, 0)
	for _, id := range ids {
		forward, accessErr := h.ensureForwardAccessByActor(actorUserID, actorRole, id)
		if accessErr != nil {
			f++
			failures = appendBatchFailure(failures, id, "", accessErr)
			continue
		}
		if err := h.ensureUserTunnelForwardAllowed(forward.UserID, forward.TunnelID, now); err != nil {
			f++
			failures = appendBatchFailure(failures, id, forward.Name, err)
			continue
		}
		if err := h.controlForwardServices(forward, "ResumeService", false); err != nil {
			f++
			failures = appendBatchFailure(failures, id, forward.Name, err)
			continue
		}
		if err := h.repo.SetManualForwardStatus(forward.UserID, id, 1, now); err != nil {
			f++
			failures = appendBatchFailure(failures, id, forward.Name, err)
		} else {
			s++
		}
	}
	response.WriteJSON(w, response.OK(batchOperationResult{SuccessCount: s, FailCount: f, Failures: failures}))
}

func (h *Handler) forwardBatchRedeploy(w http.ResponseWriter, r *http.Request) {
	ids := idsFromBody(r, w)
	if ids == nil {
		return
	}
	actorUserID, actorRole, err := userRoleFromRequest(r)
	if err != nil {
		response.WriteJSON(w, response.Err(401, "无效的token或token已过期"))
		return
	}
	s := 0
	f := 0
	failures := make([]batchFailureDetail, 0)
	for _, id := range ids {
		forward, accessErr := h.ensureForwardAccessByActor(actorUserID, actorRole, id)
		if accessErr != nil {
			f++
			failures = appendBatchFailure(failures, id, "", accessErr)
			continue
		}
		if nftMode, entryNodeIDs, modeErr := h.tunnelUsesNftables(forward.TunnelID); modeErr != nil {
			f++
			failures = appendBatchFailure(failures, id, forward.Name, modeErr)
			continue
		} else if nftMode {
			if len(entryNodeIDs) == 0 {
				f++
				failures = appendBatchFailureReason(failures, id, forward.Name, "nftables 转发缺少入口服务器")
				continue
			}
			if err := h.reconcileNftablesNodeByRequest(entryNodeIDs[0]); err != nil {
				f++
				failures = appendBatchFailure(failures, id, forward.Name, err)
			} else {
				s++
			}
			continue
		}
		if err := h.syncForwardServices(forward, "UpdateService", true); err != nil {
			f++
			failures = appendBatchFailure(failures, id, forward.Name, err)
		} else {
			s++
		}
	}
	response.WriteJSON(w, response.OK(batchOperationResult{SuccessCount: s, FailCount: f, Failures: failures}))
}

func (h *Handler) forwardBatchChangeTunnel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ForwardIDs     []int64 `json:"forwardIds"`
		TargetTunnelID int64   `json:"targetTunnelId"`
	}
	if err := decodeJSON(r.Body, &req); err != nil || req.TargetTunnelID <= 0 {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	actorUserID, actorRole, err := userRoleFromRequest(r)
	if err != nil {
		response.WriteJSON(w, response.Err(401, "无效的token或token已过期"))
		return
	}
	if err := h.ensureTunnelPermission(actorUserID, actorRole, req.TargetTunnelID); err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	targetTunnel, err := h.getTunnelRecord(req.TargetTunnelID)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault("目标隧道不存在"))
		return
	}
	if targetTunnel.Status != 1 {
		response.WriteJSON(w, response.ErrDefault("目标隧道已禁用"))
		return
	}
	success := 0
	fail := 0
	failures := make([]batchFailureDetail, 0)
	for _, id := range req.ForwardIDs {
		if id <= 0 {
			continue
		}
		forward, accessErr := h.ensureForwardAccessByActor(actorUserID, actorRole, id)
		if accessErr != nil {
			fail++
			failures = appendBatchFailure(failures, id, "", accessErr)
			continue
		}
		if forward.TunnelID == req.TargetTunnelID {
			fail++
			failures = appendBatchFailureReason(failures, id, forward.Name, "规则已在目标隧道中")
			continue
		}
		oldPorts, listPortsErr := h.listForwardPorts(id)
		if listPortsErr != nil {
			fail++
			failures = appendBatchFailure(failures, id, forward.Name, listPortsErr)
			continue
		}
		if len(oldPorts) == 0 {
			fail++
			failures = appendBatchFailureReason(failures, id, forward.Name, "转发入口端口不存在")
			continue
		}
		oldNodeIDs := forwardPortNodeIDs(oldPorts)
		port := h.repo.GetMinForwardPort(id)
		if err := h.repo.UpdateForwardTunnel(id, req.TargetTunnelID, time.Now().UnixMilli()); err != nil {
			fail++
			failures = appendBatchFailure(failures, id, forward.Name, err)
			continue
		}
		p := 0
		if port.Valid {
			p = int(port.Int64)
		}
		if p <= 0 {
			p = h.pickTunnelPort(req.TargetTunnelID)
		}
		bctEntryNodes, _ := h.tunnelEntryNodeIDs(req.TargetTunnelID)
		newNodeIDs := uniqueInt64s(bctEntryNodes)
		removedNodeIDs := diffInt64s(oldNodeIDs, newNodeIDs)
		keptNodeIDs := diffInt64s(oldNodeIDs, removedNodeIDs)
		portRangeOk := true
		var portRangeErr error
		for _, nid := range bctEntryNodes {
			nd, ndErr := h.getNodeRecord(nid)
			if ndErr != nil {
				portRangeErr = ndErr
				portRangeOk = false
				break
			}
			if validateErr := validateRemoteNodePort(nd, p); validateErr != nil {
				portRangeOk = false
				portRangeErr = validateErr
				break
			}
			if validateErr := validateLocalNodePort(nd, p); validateErr != nil {
				portRangeOk = false
				portRangeErr = validateErr
				break
			}
			if validateErr := h.validateForwardPortAvailability(nd, p, id); validateErr != nil {
				portRangeOk = false
				portRangeErr = validateErr
				break
			}
		}
		if !portRangeOk {
			fail++
			failures = appendBatchFailure(failures, id, forward.Name, portRangeErr)
			h.rollbackForwardMutation(forward, oldPorts)
			continue
		}
		if err := h.replaceForwardPorts(id, req.TargetTunnelID, p, ""); err != nil {
			h.rollbackForwardMutation(forward, oldPorts)
			fail++
			failures = appendBatchFailure(failures, id, forward.Name, err)
			continue
		}
		updatedForward, fetchErr := h.getForwardRecord(id)
		if fetchErr != nil {
			h.rollbackForwardMutation(forward, oldPorts)
			fail++
			failures = appendBatchFailure(failures, id, forward.Name, fetchErr)
			continue
		}
		if len(keptNodeIDs) > 0 {
			for _, nodeID := range keptNodeIDs {
				_ = h.deleteForwardServicesOnNodeBatch(forward, nodeID)
			}
			time.Sleep(tunnelServiceBindRetryDelay)
		}
		if err := h.syncForwardServices(updatedForward, "UpdateService", true); err != nil {
			h.rollbackForwardMutation(forward, oldPorts)
			fail++
			failures = appendBatchFailure(failures, id, forward.Name, err)
			continue
		}
		if len(removedNodeIDs) > 0 {
			for _, nodeID := range removedNodeIDs {
				_ = h.deleteForwardServicesOnNodeBatch(forward, nodeID)
			}
		}
		success++
	}
	response.WriteJSON(w, response.OK(batchOperationResult{SuccessCount: success, FailCount: fail, Failures: failures}))
}

func (h *Handler) deleteForwardByID(id int64) error {
	return h.repo.DeleteForwardCascade(id)
}

func (h *Handler) batchForwardDelete(ids []int64) (int, int) {
	s := 0
	f := 0
	for _, id := range ids {
		if err := h.deleteForwardByID(id); err != nil {
			f++
		} else {
			s++
		}
	}
	return s, f
}

func (h *Handler) batchForwardStatus(ids []int64, status int) (int, int) {
	return h.repo.BatchUpdateForwardStatus(ids, status)
}

type forwardPortReplaceEntry = struct {
	NodeID int64
	Port   int
	InIP   string
}

func buildForwardPortEntriesWithPreservedInIP(entryNodeIDs []int64, oldPorts []forwardPortRecord, port int) []forwardPortReplaceEntry {
	preservedByNode := make(map[int64]string)
	for _, fp := range oldPorts {
		current, exists := preservedByNode[fp.NodeID]
		if !exists {
			preservedByNode[fp.NodeID] = fp.InIP
			continue
		}
		if strings.TrimSpace(current) == "" && strings.TrimSpace(fp.InIP) != "" {
			preservedByNode[fp.NodeID] = fp.InIP
		}
	}

	entries := make([]forwardPortReplaceEntry, 0, len(entryNodeIDs))
	for _, nid := range entryNodeIDs {
		entries = append(entries, forwardPortReplaceEntry{
			NodeID: nid,
			Port:   port,
			InIP:   preservedByNode[nid],
		})
	}

	return entries
}

func (h *Handler) replaceForwardPorts(forwardID, tunnelID int64, port int, inIp string) error {
	entryNodes, err := h.tunnelEntryNodeIDs(tunnelID)
	if err != nil {
		return err
	}
	entries := make([]forwardPortReplaceEntry, len(entryNodes))
	for i, nid := range entryNodes {
		entries[i] = forwardPortReplaceEntry{NodeID: nid, Port: port, InIP: inIp}
	}
	return h.repo.ReplaceForwardPorts(forwardID, entries)
}

func (h *Handler) replaceForwardPortsPreservingInIP(forwardID, tunnelID int64, port int, oldPorts []forwardPortRecord) error {
	entryNodes, err := h.tunnelEntryNodeIDs(tunnelID)
	if err != nil {
		return err
	}
	entries := buildForwardPortEntriesWithPreservedInIP(entryNodes, oldPorts, port)
	return h.repo.ReplaceForwardPorts(forwardID, entries)
}

func (h *Handler) replaceForwardPortsWithRecords(forwardID int64, ports []forwardPortRecord) error {
	entries := make([]forwardPortReplaceEntry, len(ports))
	for i, fp := range ports {
		entries[i] = forwardPortReplaceEntry{NodeID: fp.NodeID, Port: fp.Port, InIP: fp.InIP}
	}
	return h.repo.ReplaceForwardPorts(forwardID, entries)
}

func (h *Handler) rollbackForwardMutation(oldForward *forwardRecord, oldPorts []forwardPortRecord) {
	if h == nil || oldForward == nil || h.repo == nil {
		return
	}

	h.repo.RollbackForwardFields(
		oldForward.ID, oldForward.UserID, oldForward.UserName, oldForward.Name,
		oldForward.TunnelID, oldForward.RemoteAddr, oldForward.Strategy, oldForward.Status,
		oldForward.SpeedID, oldForward.MaxConn, oldForward.IPMaxConn, oldForward.IPSpeedID, oldForward.ProxyProtocol, oldForward.ProxyProtocolReceive, oldForward.ProxyProtocolSend,
		time.Now().UnixMilli(),
	)

	if err := h.replaceForwardPortsWithRecords(oldForward.ID, oldPorts); err != nil {
		return
	}

	_ = h.syncForwardServices(oldForward, "UpdateService", true)
}

func validateLocalNodePort(node *nodeRecord, port int) error {
	if node == nil || node.IsRemote == 1 || port <= 0 {
		return nil
	}
	portRange := strings.TrimSpace(node.PortRange)
	if portRange == "" {
		return nil
	}
	minPort, maxPort := parsePortRangeMinMax(portRange)
	if minPort <= 0 || maxPort <= 0 {
		return nil
	}
	if port < minPort || port > maxPort {
		return fmt.Errorf("端口 %d 超出服务器 %s 允许范围 %d-%d", port, node.Name, minPort, maxPort)
	}
	return nil
}

func (h *Handler) validateForwardPortAvailability(node *nodeRecord, port int, currentForwardID int64) error {
	if h == nil || h.repo == nil || node == nil || port <= 0 {
		return nil
	}
	occupied, err := h.repo.HasOtherForwardOnNodePort(node.ID, port, currentForwardID)
	if err != nil {
		return err
	}
	if occupied {
		return fmt.Errorf("服务器 %s 端口 %d 已被其他转发占用", node.Name, port)
	}
	return nil
}
