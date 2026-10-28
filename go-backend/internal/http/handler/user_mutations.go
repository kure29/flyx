package handler

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/security"
)

func (h *Handler) userCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req map[string]interface{}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}

	username := asString(req["user"])
	pwd := asString(req["pwd"])
	if username == "" || pwd == "" {
		response.WriteJSON(w, response.ErrDefault("用户名或密码不能为空"))
		return
	}

	exists, err := h.repo.UserExists(username)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if exists {
		response.WriteJSON(w, response.ErrDefault("用户名已存在"))
		return
	}

	status := asInt(req["status"], 1)
	flow, flowMiB, flowErr := parseTrafficLimit(req, 100)
	if flowErr != nil {
		response.WriteJSON(w, response.ErrDefault(flowErr.Error()))
		return
	}
	num := asInt(req["num"], 10)
	expTime := asInt64(req["expTime"], time.Now().Add(365*24*time.Hour).UnixMilli())
	flowResetTime := asInt64(req["flowResetTime"], 1)
	dailyQuotaGB := asInt64(req["dailyQuotaGB"], 0)
	monthlyQuotaGB := asInt64(req["monthlyQuotaGB"], 0)
	if dailyQuotaGB < 0 || monthlyQuotaGB < 0 {
		response.WriteJSON(w, response.ErrDefault("配额不能小于0"))
		return
	}
	roleID := 1
	now := time.Now().UnixMilli()
	maxConn := asInt(req["maxConn"], 0)

	hashedPassword, err := security.HashPassword(pwd)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	subIDs, assignSubs, err := h.userSubscriptionIDs(req)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	userID, err := h.repo.CreateUser(username, hashedPassword, roleID, expTime, flow, flowResetTime, num, status, maxConn, now, flowMiB)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if dailyQuotaGB > 0 || monthlyQuotaGB > 0 {
		tx := h.repo.BeginTx()
		if tx == nil || tx.Error != nil {
			response.WriteJSON(w, response.Err(-2, "database unavailable"))
			return
		}
		defer func() { tx.Rollback() }()
		if err := h.repo.SaveUserQuotaConfigTx(tx, userID, dailyQuotaGB, monthlyQuotaGB, now); err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
		if err := tx.Commit().Error; err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
	}

	groupIDs := asInt64Slice(req["groupIds"])
	if len(groupIDs) > 0 {
		if addErr := h.repo.AddUserToGroups(userID, groupIDs, now); addErr == nil {
			for _, gid := range groupIDs {
				_ = h.syncPermissionsByUserGroup(gid)
			}
		}
	}
	if assignSubs {
		if err := h.repo.AssignUserSubscriptions(userID, subIDs); err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
	}

	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) userUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req map[string]interface{}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	id := asInt64(req["id"], 0)
	if id <= 0 {
		response.WriteJSON(w, response.ErrDefault("用户ID不能为空"))
		return
	}
	username := asString(req["user"])
	if username == "" {
		response.WriteJSON(w, response.ErrDefault("用户名不能为空"))
		return
	}

	roleID, err := h.repo.GetUserRoleID(id)
	if err != nil {
		if err == sql.ErrNoRows {
			response.WriteJSON(w, response.ErrDefault("用户不存在"))
			return
		}
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if roleID == 0 {
		response.WriteJSON(w, response.ErrDefault("请不要作死"))
		return
	}
	oldUser, err := h.repo.GetUserByID(id)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if oldUser == nil {
		response.WriteJSON(w, response.ErrDefault("用户不存在"))
		return
	}

	dup, err := h.repo.UserExistsExcluding(username, id)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if dup {
		response.WriteJSON(w, response.ErrDefault("用户名已存在"))
		return
	}

	flow, flowMiB, flowErr := parseTrafficLimit(req, 100)
	if flowErr != nil {
		response.WriteJSON(w, response.ErrDefault(flowErr.Error()))
		return
	}
	if _, supplied := req["flowMiB"]; !supplied {
		if current, err := h.repo.GetUserByID(id); err == nil && current != nil && current.Flow == flow {
			flowMiB = current.FlowMiB
		}
	}
	num := asInt(req["num"], 10)
	expTime := asInt64(req["expTime"], time.Now().Add(365*24*time.Hour).UnixMilli())
	flowResetTime := asInt64(req["flowResetTime"], 1)
	status := asInt(req["status"], 1)
	_, hasDailyQuota := req["dailyQuotaGB"]
	_, hasMonthlyQuota := req["monthlyQuotaGB"]
	now := time.Now().UnixMilli()
	maxConn := asInt(req["maxConn"], 0)

	subIDs, assignSubs, err := h.userSubscriptionIDs(req)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}

	pwd := asString(req["pwd"])
	if strings.TrimSpace(pwd) == "" {
		if err := h.repo.UpdateUserWithoutPassword(id, username, flow, num, expTime, flowResetTime, status, maxConn, now, flowMiB); err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
	} else {
		hashedPassword, err := security.HashPassword(pwd)
		if err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
		if err := h.repo.UpdateUserWithPassword(id, username, hashedPassword, flow, num, expTime, flowResetTime, status, maxConn, now, flowMiB); err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
	}

	h.repo.PropagateUserFlowToTunnels(id, flow, num, expTime, flowResetTime, flowMiB)
	if assignSubs {
		if err := h.repo.AssignUserSubscriptions(id, subIDs); err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
	}
	if hasDailyQuota || hasMonthlyQuota {
		dailyQuotaGB := asInt64(req["dailyQuotaGB"], 0)
		monthlyQuotaGB := asInt64(req["monthlyQuotaGB"], 0)
		if !(hasDailyQuota && hasMonthlyQuota) {
			if currentQuota, err := h.repo.GetUserQuotaView(id, time.Now()); err == nil && currentQuota != nil {
				if !hasDailyQuota {
					dailyQuotaGB = currentQuota.DailyLimitGB
				}
				if !hasMonthlyQuota {
					monthlyQuotaGB = currentQuota.MonthlyLimitGB
				}
			}
		}
		tx := h.repo.BeginTx()
		if tx == nil || tx.Error != nil {
			response.WriteJSON(w, response.Err(-2, "database unavailable"))
			return
		}
		defer func() { tx.Rollback() }()
		if err := h.repo.SaveUserQuotaConfigTx(tx, id, dailyQuotaGB, monthlyQuotaGB, now); err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
		if err := tx.Commit().Error; err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
	}

	if groupIDsRaw, ok := req["groupIds"]; ok {
		newGroupIDs := asInt64Slice(groupIDsRaw)
		if affected, replaceErr := h.repo.ReplaceUserGroupsByUserID(id, newGroupIDs, now); replaceErr == nil {
			for _, gid := range affected {
				_ = h.syncPermissionsByUserGroup(gid)
			}
		}
	}
	if oldUser.MaxConn != maxConn {
		warnings, syncErr := h.syncUserMaxConnForwards(id)
		if syncErr != nil {
			response.WriteJSON(w, response.ErrDefault(fmt.Sprintf("最大连接数下发失败: %v", syncErr)))
			return
		}
		if len(warnings) > 0 {
			response.WriteJSON(w, response.OK(map[string]interface{}{"warnings": warnings}))
			return
		}
	}

	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) userGroups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	id := idFromBody(r, w)
	if id <= 0 {
		return
	}
	ids, err := h.repo.GetUserGroupIDsByUserID(id)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(ids))
}

func (h *Handler) userDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	id := idFromBody(r, w)
	if id <= 0 {
		return
	}

	roleID, err := h.repo.GetUserRoleID(id)
	if err != nil {
		if err == sql.ErrNoRows {
			response.WriteJSON(w, response.ErrDefault("用户不存在"))
			return
		}
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if roleID == 0 {
		response.WriteJSON(w, response.ErrDefault("请不要作死"))
		return
	}

	// Remove all of this user's realm endpoints in one apply per node while
	// the port bindings still exist. A rejected apply leaves the user intact
	// so deletion can be retried; offline nodes reconcile on reconnect.
	ports, err := h.repo.ListRealmForwardPortsByUser(id)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	nodeIDs := make([]int64, 0, len(ports))
	override := make(map[int64]bool, len(ports))
	for _, port := range ports {
		nodeIDs = append(nodeIDs, port.NodeID)
		override[port.ForwardID] = false
	}
	warnings, err := h.syncRealmNodes(nodeIDs, override)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	if err := h.repo.DeleteUserCascade(id); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if len(warnings) > 0 {
		response.WriteJSON(w, response.OK(map[string]interface{}{"warnings": warnings}))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) userResetFlow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req map[string]interface{}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	id := asInt64(req["id"], 0)
	typeVal := asInt(req["type"], 0)
	if id <= 0 || (typeVal != 1 && typeVal != 2) {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}

	if typeVal == 1 {
		h.repo.ResetUserFlowByUser(id, time.Now().UnixMilli())
	} else {
		h.repo.ResetUserFlowByUserTunnel(id)
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) userTunnelAssign(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	if err := h.upsertUserTunnel(req); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) userTunnelBatchAssign(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID  int64 `json:"userId"`
		Tunnels []struct {
			TunnelID int64  `json:"tunnelId"`
			SpeedID  *int64 `json:"speedId"`
		} `json:"tunnels"`
	}
	if err := decodeJSON(r.Body, &req); err != nil || req.UserID <= 0 {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	for _, t := range req.Tunnels {
		m := map[string]interface{}{"userId": req.UserID, "tunnelId": t.TunnelID}
		if t.SpeedID != nil {
			m["speedId"] = *t.SpeedID
		}
		if err := h.upsertUserTunnel(m); err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) userTunnelRemove(w http.ResponseWriter, r *http.Request) {
	id := idFromBody(r, w)
	if id <= 0 {
		return
	}
	userID, tunnelID, lookupErr := h.repo.GetUserTunnelUserAndTunnel(id)
	if lookupErr != nil {
		response.WriteJSON(w, response.Err(-2, lookupErr.Error()))
		return
	}
	h.cleanupForwardsForUserTunnel(userID, tunnelID)
	if err := h.repo.DeleteUserTunnel(id); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) userTunnelUpdate(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	id := asInt64(req["id"], 0)
	if id <= 0 {
		response.WriteJSON(w, response.ErrDefault("权限ID不能为空"))
		return
	}

	speedID := asAnyToInt64Ptr(req["speedId"])
	speedID, err := h.normalizeSpeedLimitReference(speedID)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}

	userID, tunnelID, utErr := h.repo.GetUserTunnelUserAndTunnel(id)
	if utErr != nil {
		response.WriteJSON(w, response.Err(-2, utErr.Error()))
		return
	}

	_, oldFlow, oldNum, oldExpTime, oldFlowReset, oldSpeedID, oldStatus, oldErr :=
		h.repo.GetExistingUserTunnel(userID, tunnelID)
	if oldErr != nil {
		response.WriteJSON(w, response.Err(-2, oldErr.Error()))
		return
	}
	oldTunnel, oldTunnelErr := h.repo.GetUserTunnelByID(id)
	if oldTunnelErr != nil {
		response.WriteJSON(w, response.Err(-2, oldTunnelErr.Error()))
		return
	}
	if oldTunnel == nil {
		response.WriteJSON(w, response.ErrDefault("隧道权限不存在"))
		return
	}
	flow, flowMiB, flowErr := parseTrafficLimit(req, 0)
	if flowErr != nil {
		response.WriteJSON(w, response.ErrDefault(flowErr.Error()))
		return
	}
	if _, supplied := req["flowMiB"]; !supplied && oldTunnel.Flow == flow {
		flowMiB = oldTunnel.FlowMiB
	}

	if err := h.repo.UpdateUserTunnel(id,
		flow,
		asInt(req["num"], 0),
		asInt64(req["expTime"], time.Now().Add(365*24*time.Hour).UnixMilli()),
		asInt64(req["flowResetTime"], 1),
		nullableInt(speedID),
		asInt(req["status"], 1),
		flowMiB,
	); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}

	if syncErr := h.syncUserTunnelForwards(userID, tunnelID); syncErr != nil {
		rollbackErr := h.repo.UpdateUserTunnel(
			id,
			oldFlow,
			int(oldNum),
			oldExpTime,
			oldFlowReset,
			oldSpeedID,
			oldStatus,
			oldTunnel.FlowMiB,
		)
		if rollbackErr != nil {
			response.WriteJSON(w, response.Err(-2, fmt.Sprintf("下发失败且回滚失败: %v; 回滚错误: %v", syncErr, rollbackErr)))
			return
		}

		response.WriteJSON(w, response.Err(-2, fmt.Sprintf("下发失败，已回滚: %v", syncErr)))
		return
	}

	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) upsertUserTunnel(req map[string]interface{}) error {
	userID := asInt64(req["userId"], 0)
	tunnelID := asInt64(req["tunnelId"], 0)
	if userID <= 0 || tunnelID <= 0 {
		return fmt.Errorf("userId or tunnelId missing")
	}

	existingID, currentFlow, currentNum, currentExpTime, currentFlowReset, currentSpeedID, currentStatus, lookupErr :=
		h.repo.GetExistingUserTunnel(userID, tunnelID)

	speedID := asAnyToInt64Ptr(req["speedId"])
	var err error
	speedID, err = h.normalizeSpeedLimitReference(speedID)
	if err != nil {
		return err
	}

	reqFlow := asInt64(req["flow"], -1)
	var reqFlowMiB int64
	if _, hasFlowMiB := req["flowMiB"]; hasFlowMiB {
		var flowErr error
		reqFlow, reqFlowMiB, flowErr = parseTrafficLimit(req, 0)
		if flowErr != nil {
			return flowErr
		}
	}
	reqNum := asInt(req["num"], -1)
	reqExpTime := asInt64(req["expTime"], -1)
	reqFlowReset := asInt64(req["flowResetTime"], -1)
	reqStatus := asInt(req["status"], -1)

	if lookupErr == sql.ErrNoRows {
		if reqFlow < 0 || reqNum < 0 || reqExpTime < 0 || reqFlowReset < 0 {
			uFlow, uNum, uExp, uReset, uErr := h.repo.GetUserDefaultsForTunnel(userID)
			if uErr == nil {
				if reqFlow < 0 {
					reqFlow = uFlow
					if user, err := h.repo.GetUserByID(userID); err == nil && user != nil {
						reqFlowMiB = user.FlowMiB
					}
				}
				if reqNum < 0 {
					reqNum = uNum
				}
				if reqExpTime < 0 {
					reqExpTime = uExp
				}
				if reqFlowReset < 0 {
					reqFlowReset = uReset
				}
			}
		}
		if reqFlow < 0 {
			reqFlow = 0
		}
		if reqNum < 0 {
			reqNum = 0
		}
		if reqExpTime < 0 {
			reqExpTime = time.Now().Add(365 * 24 * time.Hour).UnixMilli()
		}
		if reqFlowReset < 0 {
			reqFlowReset = 1
		}
		if reqStatus < 0 {
			reqStatus = 1
		}

		if err := h.repo.InsertUserTunnel(userID, tunnelID, nullableInt(speedID), reqNum, reqFlow, reqFlowReset, reqExpTime, reqStatus, reqFlowMiB); err != nil {
			return err
		}

		if syncErr := h.syncUserTunnelForwards(userID, tunnelID); syncErr != nil {
			insertedID, _, _, _, _, _, _, lookupErr := h.repo.GetExistingUserTunnel(userID, tunnelID)
			if lookupErr != nil {
				return fmt.Errorf("下发失败且回滚失败: %v; 回滚查询错误: %w", syncErr, lookupErr)
			}

			if rollbackErr := h.repo.DeleteUserTunnel(insertedID); rollbackErr != nil {
				return fmt.Errorf("下发失败且回滚失败: %v; 回滚删除错误: %w", syncErr, rollbackErr)
			}

			return fmt.Errorf("下发失败，已回滚: %w", syncErr)
		}

		return nil
	}
	if lookupErr != nil {
		return lookupErr
	}

	newFlow := currentFlow
	oldTunnel, err := h.repo.GetUserTunnelByID(existingID)
	if err != nil {
		return err
	}
	if oldTunnel == nil {
		return fmt.Errorf("隧道权限不存在")
	}
	newFlowMiB := oldTunnel.FlowMiB
	if reqFlow >= 0 {
		newFlow = reqFlow
		newFlowMiB = reqFlowMiB
		if _, supplied := req["flowMiB"]; !supplied && reqFlow == currentFlow {
			newFlowMiB = oldTunnel.FlowMiB
		}
	}

	newNum := int(currentNum)
	if reqNum >= 0 {
		newNum = reqNum
	}

	newExpTime := currentExpTime
	if reqExpTime >= 0 {
		newExpTime = reqExpTime
	}

	newFlowReset := currentFlowReset
	if reqFlowReset >= 0 {
		newFlowReset = reqFlowReset
	}

	newStatus := currentStatus
	if reqStatus >= 0 {
		newStatus = reqStatus
	}

	newSpeedID := currentSpeedID
	if speedID != nil {
		newSpeedID = sql.NullInt64{Int64: *speedID, Valid: true}
	} else if _, ok := req["speedId"]; ok {
		newSpeedID = sql.NullInt64{Valid: false}
	}

	if err := h.repo.UpdateUserTunnelFields(existingID, newSpeedID, newFlow, newNum, newExpTime, newFlowReset, newStatus, newFlowMiB); err != nil {
		return err
	}

	if syncErr := h.syncUserTunnelForwards(userID, tunnelID); syncErr != nil {
		rollbackErr := h.repo.UpdateUserTunnelFields(
			existingID,
			currentSpeedID,
			currentFlow,
			int(currentNum),
			currentExpTime,
			currentFlowReset,
			currentStatus,
			oldTunnel.FlowMiB,
		)
		if rollbackErr != nil {
			return fmt.Errorf("下发失败且回滚失败: %v; 回滚错误: %w", syncErr, rollbackErr)
		}

		return fmt.Errorf("下发失败，已回滚: %w", syncErr)
	}

	return nil
}

func (h *Handler) syncUserTunnelForwards(userID, tunnelID int64) error {
	forwards, err := h.listForwardsByTunnel(tunnelID)
	if err != nil {
		return err
	}
	for i := range forwards {
		f := &forwards[i]
		if f.UserID == userID {
			if err := h.syncForwardServices(f, "UpdateService", true); err != nil {
				return err
			}
		}
	}

	return nil
}

func (h *Handler) syncUserMaxConnForwards(userID int64) ([]string, error) {
	forwards, err := h.listActiveForwardsByUser(userID)
	if err != nil {
		return nil, err
	}
	warnings := make([]string, 0)
	for i := range forwards {
		f := &forwards[i]
		if f.MaxConn > 0 {
			continue
		}
		syncWarnings, syncErr := h.syncForwardServicesWithWarnings(f, "UpdateService", true)
		warnings = append(warnings, syncWarnings...)
		if syncErr != nil {
			return warnings, syncErr
		}
	}
	return warnings, nil
}

// cleanupForwardsForUserTunnel deletes all forwarding rules belonging to a
// specific user+tunnel pair. It notifies nodes to remove the runtime services
// first, then deletes the DB records. This is best-effort: individual failures
// do not abort the overall cleanup so that remaining forwards are still cleaned.
func (h *Handler) cleanupForwardsForUserTunnel(userID, tunnelID int64) {
	if userID <= 0 || tunnelID <= 0 {
		return
	}
	forwards, err := h.repo.ListForwardsByUserAndTunnel(userID, tunnelID)
	if err != nil || len(forwards) == 0 {
		return
	}
	for i := range forwards {
		f := &forwards[i]
		if f.Status == 1 {
			_ = h.controlForwardServices(f, "DeleteService", true)
		}
		_ = h.deleteForwardByID(f.ID)
	}
}
