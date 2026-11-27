package handler

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/kernel"
	"go-backend/internal/security"
	"go-backend/internal/store/model"
)

const (
	mibBytes = int64(1024 * 1024)
	gibBytes = int64(1024 * 1024 * 1024)
)

// userFlowLimitBytes returns the user's forwarding traffic quota in bytes
// (0 = unlimited). Prefers FlowMiB when set, falling back to Flow (GiB).
func userFlowLimitBytes(u *model.User) int64 {
	if u == nil {
		return 0
	}
	if u.FlowMiB > 0 {
		return u.FlowMiB * mibBytes
	}
	if u.Flow > 0 {
		return u.Flow * gibBytes
	}
	return 0
}

func userFlowUsedBytes(u *model.User) int64 {
	if u == nil {
		return 0
	}
	return u.InFlow + u.OutFlow
}

func isUserForwardFlowExceeded(u *model.User) bool {
	if u == nil || u.RoleID == 0 {
		return false
	}
	limit := userFlowLimitBytes(u)
	return limit > 0 && userFlowUsedBytes(u) >= limit
}

func isUserForwardExpired(u *model.User, nowMs int64) bool {
	if u == nil || u.RoleID == 0 {
		return false
	}
	return u.ExpTime > 0 && u.ExpTime <= nowMs
}

func (h *Handler) checkUserForwardQuotaForCreate(userID int64, roleID int, tunnelID int64) error {
	if roleID == 0 {
		return nil
	}
	u, err := h.repo.GetUserByID(userID)
	if err != nil {
		return err
	}
	if u == nil || u.Status != 1 {
		return errors.New("账号已停用，无法创建转发")
	}
	nowMs := time.Now().UnixMilli()
	if isUserForwardExpired(u, nowMs) {
		return errors.New("账号中转已到期，无法创建转发")
	}
	if isUserForwardFlowExceeded(u) {
		return errors.New("中转流量已用完，无法创建转发")
	}
	if err := h.ensureTunnelPermission(userID, roleID, tunnelID); err != nil {
		return err
	}
	if u.Num > 0 {
		cnt, err := h.repo.CountForwardsByUser(userID)
		if err != nil {
			return err
		}
		if cnt >= int64(u.Num) {
			return errors.New("已达到最大转发规则数上限")
		}
	}
	return nil
}

func (h *Handler) checkUserForwardQuotaForResume(userID int64, roleID int, tunnelID int64) error {
	if roleID == 0 {
		return nil
	}
	u, err := h.repo.GetUserByID(userID)
	if err != nil {
		return err
	}
	if u == nil || u.Status != 1 {
		return errors.New("账号已停用，无法恢复转发")
	}
	nowMs := time.Now().UnixMilli()
	if isUserForwardExpired(u, nowMs) {
		return errors.New("账号中转已到期，无法恢复转发")
	}
	if isUserForwardFlowExceeded(u) {
		return errors.New("中转流量已用完，无法恢复转发")
	}
	return h.ensureTunnelPermission(userID, roleID, tunnelID)
}

// pauseUserActiveForwards pauses all active forwards belonging to userID.
func (h *Handler) pauseUserActiveForwards(userID int64) {
	if h == nil || h.repo == nil || userID <= 0 {
		return
	}
	forwards, err := h.repo.ListForwardsByUserAndStatus(userID, 1)
	if err != nil {
		log.Printf("pause user forwards list failed user_id=%d err=%v", userID, err)
		return
	}
	now := time.Now().UnixMilli()
	for i := range forwards {
		f := &forwards[i]
		if err := h.controlForwardServices(f, "PauseService", false); err != nil {
			log.Printf("pause user forward service failed user_id=%d forward_id=%d err=%v", userID, f.ID, err)
		}
		if err := h.repo.UpdateForwardStatus(f.ID, 0, now); err != nil {
			log.Printf("pause user forward db update failed user_id=%d forward_id=%d err=%v", userID, f.ID, err)
		}
	}
}

// pauseUserForwardsOnTunnel pauses active forwards of userID on a specific tunnel (e.g. when tunnel access is revoked).
func (h *Handler) pauseUserForwardsOnTunnel(userID, tunnelID int64) {
	if h == nil || h.repo == nil || userID <= 0 || tunnelID <= 0 {
		return
	}
	forwards, err := h.repo.ListActiveForwardsByUserTunnel(userID, tunnelID)
	if err != nil {
		log.Printf("pause user tunnel forwards list failed user_id=%d tunnel_id=%d err=%v", userID, tunnelID, err)
		return
	}
	now := time.Now().UnixMilli()
	for i := range forwards {
		f := &forwards[i]
		if err := h.controlForwardServices(f, "PauseService", false); err != nil {
			log.Printf("pause user tunnel forward failed user_id=%d forward_id=%d err=%v", userID, f.ID, err)
		}
		if err := h.repo.UpdateForwardStatus(f.ID, 0, now); err != nil {
			log.Printf("pause user tunnel forward db update failed user_id=%d forward_id=%d err=%v", userID, f.ID, err)
		}
	}
}

// resumeUserPausedForwards automatically resumes paused forwards of userID
// when the user is eligible (enabled, not expired, not over quota, tunnel enabled & authorized).
func (h *Handler) resumeUserPausedForwards(userID int64) {
	if h == nil || h.repo == nil || userID <= 0 {
		return
	}
	u, err := h.repo.GetUserByID(userID)
	if err != nil || u == nil || u.RoleID == 0 || u.Status != 1 {
		return
	}
	nowMs := time.Now().UnixMilli()
	if isUserForwardExpired(u, nowMs) || isUserForwardFlowExceeded(u) {
		return
	}
	forwards, err := h.repo.ListForwardsByUserAndStatus(userID, 0)
	if err != nil {
		log.Printf("resume user forwards list failed user_id=%d err=%v", userID, err)
		return
	}
	for i := range forwards {
		f := &forwards[i]
		ok, err := h.repo.UserTunnelExistsByUserAndTunnel(userID, f.TunnelID)
		if err != nil || !ok {
			continue
		}
		tunnel, err := h.getTunnelRecord(f.TunnelID)
		if err != nil || tunnel == nil || tunnel.Status != 1 {
			continue
		}
		if err := h.controlForwardServices(f, "ResumeService", false); err != nil {
			log.Printf("auto-resume user forward failed user_id=%d forward_id=%d err=%v", userID, f.ID, err)
			continue
		}
		if err := h.repo.UpdateForwardStatus(f.ID, 1, nowMs); err != nil {
			log.Printf("auto-resume user forward db update failed user_id=%d forward_id=%d err=%v", userID, f.ID, err)
		}
	}
}

// enforceUserForwardFlowLimit pauses active forwards if userID has exceeded forwarding quota.
func (h *Handler) enforceUserForwardFlowLimit(userID int64) {
	if h == nil || h.repo == nil || userID <= 0 {
		return
	}
	h.quotaControlMu.Lock()
	defer h.quotaControlMu.Unlock()
	u, err := h.repo.GetUserByID(userID)
	if err != nil || u == nil || !isUserForwardFlowExceeded(u) {
		return
	}
	h.pauseUserActiveForwards(userID)
}

func (h *Handler) pauseExpiredUsersForwards(nowMs int64) {
	if h == nil || h.repo == nil {
		return
	}
	userIDs, err := h.repo.ListExpiredUsersWithActiveForwards(nowMs)
	if err != nil {
		log.Printf("list expired users with active forwards failed: %v", err)
		return
	}
	for _, uid := range userIDs {
		h.quotaControlMu.Lock()
		h.pauseUserActiveForwards(uid)
		h.quotaControlMu.Unlock()
	}
}

func (h *Handler) deleteUserForwards(userID int64) {
	if h == nil || h.repo == nil || userID <= 0 {
		return
	}
	forwards, err := h.repo.ListForwardsByUser(userID)
	if err != nil {
		log.Printf("list user forwards for delete failed user_id=%d err=%v", userID, err)
		return
	}
	for i := range forwards {
		f := &forwards[i]
		_ = h.controlForwardServices(f, "DeleteService", true)
		_ = h.deleteForwardByID(f.ID)
	}
}

// ─── Admin User Management APIs ──────────────────────────────────────

type userView struct {
	ID              int64   `json:"id"`
	User            string  `json:"user"`
	RoleID          int     `json:"roleId"`
	Status          int     `json:"status"`
	ExpTime         int64   `json:"expTime"`
	Flow            int64   `json:"flow"`
	FlowMiB         int64   `json:"flowMiB"`
	FlowLimitBytes  int64   `json:"flowLimitBytes"`
	InFlow          int64   `json:"inFlow"`
	OutFlow         int64   `json:"outFlow"`
	FlowResetTime   int64   `json:"flowResetTime"`
	Num             int     `json:"num"`
	ForwardCount    int     `json:"forwardCount"`
	TunnelIDs       []int64 `json:"tunnelIds"`
	SubscriptionIDs []int64 `json:"subscriptionIds"`
	CreatedTime     int64   `json:"createdTime"`
	OverQuota       bool    `json:"overQuota"`
	Expired         bool    `json:"expired"`
}

func buildUserView(u *model.User, forwardCount int, tunnelIDs, subIDs []int64, nowMs int64) userView {
	if tunnelIDs == nil {
		tunnelIDs = []int64{}
	}
	if subIDs == nil {
		subIDs = []int64{}
	}
	return userView{
		ID:              u.ID,
		User:            u.User,
		RoleID:          u.RoleID,
		Status:          u.Status,
		ExpTime:         u.ExpTime,
		Flow:            u.Flow,
		FlowMiB:         u.FlowMiB,
		FlowLimitBytes:  userFlowLimitBytes(u),
		InFlow:          u.InFlow,
		OutFlow:         u.OutFlow,
		FlowResetTime:   u.FlowResetTime,
		Num:             u.Num,
		ForwardCount:    forwardCount,
		TunnelIDs:       tunnelIDs,
		SubscriptionIDs: subIDs,
		CreatedTime:     u.CreatedTime,
		OverQuota:       isUserForwardFlowExceeded(u),
		Expired:         isUserForwardExpired(u, nowMs),
	}
}

func (h *Handler) userList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	users, err := h.repo.ListUsers()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	fwdCounts, err := h.repo.CountForwardsByUserMap()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	tunnelMap, err := h.repo.ListUserTunnelIDsMap()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	subMap, err := h.repo.ListUserSubscriptionIDsMap()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	nowMs := time.Now().UnixMilli()
	items := make([]userView, 0, len(users))
	for i := range users {
		u := &users[i]
		if u.RoleID == 0 {
			continue
		}
		items = append(items, buildUserView(u, fwdCounts[u.ID], tunnelMap[u.ID], subMap[u.ID], nowMs))
	}
	response.WriteJSON(w, response.OK(items))
}

type userMutationRequest struct {
	ID              int64    `json:"id"`
	User            string   `json:"user"`
	Pwd             string   `json:"pwd"`
	Status          *int     `json:"status"`
	ExpTime         int64    `json:"expTime"`
	Flow            int64    `json:"flow"`
	FlowMiB         int64    `json:"flowMiB"`
	FlowResetTime   int64    `json:"flowResetTime"`
	Num             int      `json:"num"`
	TunnelIDs       *[]int64 `json:"tunnelIds"`
	SubscriptionIDs *[]int64 `json:"subscriptionIds"`
}

func normalizeUserFlowQuota(flow, flowMiB int64) (int64, int64) {
	if flowMiB < 0 {
		flowMiB = 0
	}
	if flow < 0 {
		flow = 0
	}
	if flowMiB > 0 {
		return flowMiB / 1024, flowMiB
	}
	if flow > 0 {
		return flow, flow * 1024
	}
	return 0, 0
}

func (h *Handler) userCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req userMutationRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	username := strings.TrimSpace(req.User)
	if username == "" || len([]rune(username)) > 100 {
		response.WriteJSON(w, response.ErrDefault("用户名不能为空且不超过 100 个字符"))
		return
	}
	if len(req.Pwd) < 6 {
		response.WriteJSON(w, response.ErrDefault("密码长度至少为 6 位"))
		return
	}
	if req.FlowResetTime < 0 || req.FlowResetTime > 28 {
		response.WriteJSON(w, response.ErrDefault("每月重置日只能是 0–28（0 表示不重置）"))
		return
	}
	if req.Num < 0 {
		req.Num = 0
	}
	if req.ExpTime < 0 {
		req.ExpTime = 0
	}
	existing, err := h.repo.GetUserByUsername(username)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if existing != nil {
		response.WriteJSON(w, response.ErrDefault("用户名已存在"))
		return
	}
	pwdHash, err := security.HashPassword(req.Pwd)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	status := 1
	if req.Status != nil && *req.Status == 0 {
		status = 0
	}
	flow, flowMiB := normalizeUserFlowQuota(req.Flow, req.FlowMiB)
	now := time.Now().UnixMilli()
	userID, err := h.repo.CreateUser(username, pwdHash, 1, req.ExpTime, flow, req.FlowResetTime, req.Num, status, 0, now, flowMiB)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if req.TunnelIDs != nil {
		if err := h.repo.ReplaceUserTunnels(userID, *req.TunnelIDs); err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
	}
	if req.SubscriptionIDs != nil {
		if err := h.repo.AssignUserSubscriptions(userID, *req.SubscriptionIDs); err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
	}
	response.WriteJSON(w, response.OK(map[string]interface{}{"id": userID}))
}

func (h *Handler) userUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req userMutationRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	if req.ID <= 0 {
		response.WriteJSON(w, response.ErrDefault("用户 ID 不能为空"))
		return
	}
	existing, err := h.repo.GetUserByID(req.ID)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if existing == nil {
		response.WriteJSON(w, response.ErrDefault("用户不存在"))
		return
	}
	if existing.RoleID == 0 {
		response.WriteJSON(w, response.ErrDefault("不能在此修改管理员账号"))
		return
	}
	username := strings.TrimSpace(req.User)
	if username == "" || len([]rune(username)) > 100 {
		response.WriteJSON(w, response.ErrDefault("用户名不能为空且不超过 100 个字符"))
		return
	}
	taken, err := h.repo.UsernameExistsExceptID(username, existing.ID)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if taken {
		response.WriteJSON(w, response.ErrDefault("用户名已存在"))
		return
	}
	var pwdHash string
	if req.Pwd != "" {
		if len(req.Pwd) < 6 {
			response.WriteJSON(w, response.ErrDefault("密码长度至少为 6 位"))
			return
		}
		pwdHash, err = security.HashPassword(req.Pwd)
		if err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
	}
	if req.FlowResetTime < 0 || req.FlowResetTime > 28 {
		response.WriteJSON(w, response.ErrDefault("每月重置日只能是 0–28（0 表示不重置）"))
		return
	}
	if req.Num < 0 {
		req.Num = 0
	}
	if req.ExpTime < 0 {
		req.ExpTime = 0
	}
	status := existing.Status
	if req.Status != nil {
		if *req.Status == 0 {
			status = 0
		} else {
			status = 1
		}
	}
	flow, flowMiB := normalizeUserFlowQuota(req.Flow, req.FlowMiB)
	now := time.Now().UnixMilli()

	// Track old tunnels before replacement so revoked tunnels get their active forwards paused.
	var oldTunnelIDs []int64
	if req.TunnelIDs != nil {
		tMap, _ := h.repo.ListUserTunnelIDsMap()
		oldTunnelIDs = tMap[existing.ID]
	}

	if err := h.repo.UpdateUser(existing.ID, username, pwdHash, req.ExpTime, flow, flowMiB, req.FlowResetTime, req.Num, status, now); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if req.TunnelIDs != nil {
		if err := h.repo.ReplaceUserTunnels(existing.ID, *req.TunnelIDs); err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
		keep := make(map[int64]bool, len(*req.TunnelIDs))
		for _, tid := range *req.TunnelIDs {
			keep[tid] = true
		}
		for _, oldTID := range oldTunnelIDs {
			if !keep[oldTID] {
				h.quotaControlMu.Lock()
				h.pauseUserForwardsOnTunnel(existing.ID, oldTID)
				h.quotaControlMu.Unlock()
			}
		}
	}
	if req.SubscriptionIDs != nil {
		if err := h.repo.AssignUserSubscriptions(existing.ID, *req.SubscriptionIDs); err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
	}

	updated, err := h.repo.GetUserByID(existing.ID)
	if err == nil && updated != nil {
		if updated.Status == 0 || isUserForwardExpired(updated, now) || isUserForwardFlowExceeded(updated) {
			h.quotaControlMu.Lock()
			h.pauseUserActiveForwards(updated.ID)
			h.quotaControlMu.Unlock()
		}
	}
	response.WriteJSON(w, response.OKEmpty())
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
	existing, err := h.repo.GetUserByID(id)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if existing == nil {
		response.WriteJSON(w, response.ErrDefault("用户不存在"))
		return
	}
	if existing.RoleID == 0 {
		response.WriteJSON(w, response.ErrDefault("不能删除管理员账号"))
		return
	}
	h.quotaControlMu.Lock()
	h.deleteUserForwards(id)
	h.quotaControlMu.Unlock()
	if err := h.repo.DeleteUserCascade(id); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) userResetFlow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	id := idFromBody(r, w)
	if id <= 0 {
		return
	}
	existing, err := h.repo.GetUserByID(id)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if existing == nil {
		response.WriteJSON(w, response.ErrDefault("用户不存在"))
		return
	}
	now := time.Now().UnixMilli()
	if err := h.repo.ResetUserFlow(id, now); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if existing.RoleID != 0 {
		h.quotaControlMu.Lock()
		h.resumeUserPausedForwards(id)
		h.quotaControlMu.Unlock()
	}
	response.WriteJSON(w, response.OKEmpty())
}

// ─── User Self-Service APIs ──────────────────────────────────────────

func (h *Handler) userPackage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	userID, roleID, err := userRoleFromRequest(r)
	if err != nil {
		response.WriteJSON(w, response.Err(401, "无效的token或token已过期"))
		return
	}
	u, err := h.repo.GetUserByID(userID)
	if err != nil || u == nil {
		response.WriteJSON(w, response.ErrDefault("用户不存在"))
		return
	}
	fwdCount, _ := h.repo.CountForwardsByUser(userID)
	var tunnels []map[string]interface{}
	if roleID == 0 {
		tunnels, _ = h.repo.ListEnabledTunnelSummaries()
	} else {
		tunnels, _ = h.repo.ListEnabledTunnelSummariesForUser(userID)
	}
	if tunnels == nil {
		tunnels = []map[string]interface{}{}
	}
	nowMs := time.Now().UnixMilli()
	response.WriteJSON(w, response.OK(map[string]interface{}{
		"userInfo": map[string]interface{}{
			"id":             u.ID,
			"name":           u.User,
			"roleId":         u.RoleID,
			"status":         u.Status,
			"expTime":        u.ExpTime,
			"flow":           u.Flow,
			"flowMiB":        u.FlowMiB,
			"flowLimitBytes": userFlowLimitBytes(u),
			"inFlow":         u.InFlow,
			"outFlow":        u.OutFlow,
			"flowResetTime":  u.FlowResetTime,
			"num":            u.Num,
			"forwardCount":   fwdCount,
			"overQuota":      isUserForwardFlowExceeded(u),
			"expired":        isUserForwardExpired(u, nowMs),
		},
		"tunnels": tunnels,
	}))
}

type userSubscriptionView struct {
	ID               int64    `json:"id"`
	Name             string   `json:"name"`
	Path             string   `json:"path"`
	Status           string   `json:"status"`
	IPSuspendedUntil int64    `json:"ipSuspendedUntil,omitempty"`
	ExpireTime       int64    `json:"expireTime"`
	UploadBytes      int64    `json:"uploadBytes"`
	DownloadBytes    int64    `json:"downloadBytes"`
	TrafficLimit     int64    `json:"trafficLimit"`
	ResetDay         int      `json:"resetDay"`
	NextResetTime    int64    `json:"nextResetTime"`
	TrafficSince     int64    `json:"trafficSince"`
	NodeCount        int      `json:"nodeCount"`
	Rules            bool     `json:"rules"`
	Templates        []string `json:"templates,omitempty"`
}

func templateClientKeys(m map[string]int64) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k, id := range m {
		if id > 0 {
			keys = append(keys, k)
		}
	}
	return keys
}

func subscriptionStatusString(s *model.Subscription, now time.Time) string {
	if s.Enabled != 1 {
		return "disabled"
	}
	if subIPSuspended(s, now) {
		return "ip_suspended"
	}
	if s.ExpireTime > 0 && now.UnixMilli() > s.ExpireTime {
		return "expired"
	}
	if subOverQuota(s) {
		return "over_quota"
	}
	return "ok"
}

func nextMonthlyResetUnixMs(day int, now time.Time) int64 {
	if day < 1 || day > 28 {
		return 0
	}
	year, month := now.Year(), now.Month()
	candidate := time.Date(year, month, day, 0, 0, 0, 0, now.Location())
	if !candidate.After(now) {
		candidate = time.Date(year, month+1, day, 0, 0, 0, 0, now.Location())
	}
	return candidate.UnixMilli()
}

func (h *Handler) userSubscriptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	uid, err := userIDFromRequest(r)
	if err != nil {
		response.WriteJSON(w, response.Err(401, "未登录"))
		return
	}
	subs, err := h.repo.ListUserSubscriptions(uid)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	c, err := h.loadSubContext()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	now := time.Now()
	items := make([]userSubscriptionView, 0, len(subs))
	for i := range subs {
		s := &subs[i]
		pipe := parsePipeline(s.Options)
		suspendedUntil := int64(0)
		if subIPSuspended(s, now) {
			suspendedUntil = s.IPSuspendedUntil
		}
		items = append(items, userSubscriptionView{
			ID:               s.ID,
			Name:             s.Name,
			Path:             "/api/v1/open_api/sub/" + s.Token,
			Status:           subscriptionStatusString(s, now),
			IPSuspendedUntil: suspendedUntil,
			ExpireTime:       s.ExpireTime,
			UploadBytes:      s.UploadBytes,
			DownloadBytes:    s.DownloadBytes,
			TrafficLimit:     s.TrafficLimit,
			ResetDay:         s.ResetDay,
			NextResetTime:    nextMonthlyResetUnixMs(s.ResetDay, now),
			TrafficSince:     subTrafficSince(s),
			NodeCount:        len(kernel.ApplySubOptions(h.subNodes(c, s), pipe.options())),
			Rules:            pipe.Rules,
			Templates:        templateClientKeys(pipe.Templates),
		})
	}
	response.WriteJSON(w, response.OK(items))
}

func (h *Handler) userSubscriptionReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	uid, err := userIDFromRequest(r)
	if err != nil {
		response.WriteJSON(w, response.Err(401, "未登录"))
		return
	}
	id := idFromBody(r, w)
	if id <= 0 {
		return
	}
	s, err := h.repo.GetSubscription(id)
	if err != nil || s == nil || s.UserID != uid {
		response.WriteJSON(w, response.ErrDefault("订阅不存在"))
		return
	}
	c, push, err := h.resetSubscriptionLink(s)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	now := time.Now()
	pipe := parsePipeline(s.Options)
	suspendedUntil := int64(0)
	if subIPSuspended(s, now) {
		suspendedUntil = s.IPSuspendedUntil
	}
	view := userSubscriptionView{
		ID:               s.ID,
		Name:             s.Name,
		Path:             "/api/v1/open_api/sub/" + s.Token,
		Status:           subscriptionStatusString(s, now),
		IPSuspendedUntil: suspendedUntil,
		ExpireTime:       s.ExpireTime,
		UploadBytes:      s.UploadBytes,
		DownloadBytes:    s.DownloadBytes,
		TrafficLimit:     s.TrafficLimit,
		ResetDay:         s.ResetDay,
		NextResetTime:    nextMonthlyResetUnixMs(s.ResetDay, now),
		TrafficSince:     subTrafficSince(s),
		NodeCount:        len(kernel.ApplySubOptions(h.subNodes(c, s), pipe.options())),
		Rules:            pipe.Rules,
		Templates:        templateClientKeys(pipe.Templates),
	}
	h.respondAfterSubPush(w, push, view)
}
