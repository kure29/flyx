package handler

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
)

func isUserQuotaExceeded(view *model.UserQuotaView) bool {
	if view == nil {
		return false
	}
	if view.DailyLimitGB > 0 && view.DailyUsedBytes >= view.DailyLimitGB*bytesPerGB {
		return true
	}
	if view.MonthlyLimitGB > 0 && view.MonthlyUsedBytes >= view.MonthlyLimitGB*bytesPerGB {
		return true
	}
	return false
}

func (h *Handler) userQuotaBlockReason(userID int64, now int64) (string, error) {
	if h == nil || h.repo == nil || userID <= 0 {
		return "", nil
	}
	quota, err := h.repo.GetUserQuotaView(userID, time.UnixMilli(now))
	if err != nil || quota == nil {
		return "", err
	}
	if quota.DisabledByQuota == 1 || isUserQuotaExceeded(quota) {
		return "该用户流量配额已超额，禁止开启转发", nil
	}
	return "", nil
}

func (h *Handler) enforceUserQuotaIfNeeded(userID int64, quota *model.UserQuotaView) {
	if h == nil || h.repo == nil || quota == nil {
		return
	}
	if !isUserQuotaExceeded(quota) && quota.DisabledByQuota == 0 && quota.PausedForwardIDs == "" {
		return
	}
	h.reconcileUserQuota(userID, time.Now())
}

func (h *Handler) reconcileUserQuota(userID int64, now time.Time) {
	h.quotaControlMu.Lock()
	defer h.quotaControlMu.Unlock()
	quota, err := h.repo.GetUserQuotaView(userID, now)
	if err != nil || quota == nil {
		return
	}
	if isUserQuotaExceeded(quota) {
		forwards, err := h.listActiveForwardsByUser(userID)
		if err != nil {
			return
		}
		ids := make([]int64, 0, len(forwards))
		for _, f := range forwards {
			if f.Status == 1 {
				ids = append(ids, f.ID)
			}
		}
		// Persist every intended pause before attempting any remote operation.
		if err := h.repo.MarkUserQuotaDisabled(userID, ids, now.UnixMilli()); err != nil {
			return
		}
		quota, err = h.repo.GetUserQuotaView(userID, now)
		if err != nil || quota == nil {
			return
		}
	}
	for _, id := range repo.QuotaForwardIDs(quota) {
		forward, err := h.getForwardRecord(id)
		if errors.Is(err, errForwardNotFound) || errors.Is(err, sql.ErrNoRows) || (err == nil && forward == nil) {
			_ = h.repo.AcknowledgeQuotaResume(userID, id, now.UnixMilli())
			continue
		}
		if err != nil {
			continue
		}
		command, status := "ResumeService", 1
		if quota.DisabledByQuota == 1 {
			command, status = "PauseService", 0
			// Retry even acknowledged pauses: a node restart can discard its
			// runtime block while the panel's durable intent remains valid.
		} else if err := h.ensureUserTunnelForwardAllowed(forward.UserID, forward.TunnelID, now.UnixMilli()); err != nil {
			continue
		}
		if err := h.controlForwardServices(forward, command, false); err != nil {
			log.Printf("quota control user=%d forward=%d command=%s: %v", userID, id, command, err)
			continue
		}
		if err := h.repo.UpdateForwardStatus(id, status, now.UnixMilli()); err != nil {
			continue
		}
		if status == 1 {
			_ = h.repo.AcknowledgeQuotaResume(userID, id, now.UnixMilli())
		}
	}
}

func (h *Handler) applyUserQuotaRelease(release *repo.UserQuotaRelease, now int64) {
	if h == nil || h.repo == nil || release == nil || !release.UnblockUser {
		return
	}
	h.reconcileUserQuota(release.UserID, time.UnixMilli(now))
}

func (h *Handler) resetUserQuotaWindows(now time.Time) {
	if h == nil || h.repo == nil {
		return
	}
	if _, err := h.repo.RollUserQuotaWindows(now); err != nil {
		return
	}
	ids, err := h.repo.ListUserQuotaControlUsers()
	if err != nil {
		return
	}
	for _, id := range ids {
		h.reconcileUserQuota(id, now)
	}
}

func (h *Handler) userQuotaReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		UserID int64  `json:"userId"`
		Scope  string `json:"scope"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	if req.UserID <= 0 {
		response.WriteJSON(w, response.ErrDefault("用户ID不能为空"))
		return
	}
	release, err := h.repo.ResetUserQuotaUsage(req.UserID, req.Scope, time.Now())
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	nowMs := time.Now().UnixMilli()
	h.applyUserQuotaRelease(release, nowMs)
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) ensureUserForwardAllowedByQuota(userID int64, now int64) error {
	reason, err := h.userQuotaBlockReason(userID, now)
	if err != nil {
		return err
	}
	if reason != "" {
		return errors.New(reason)
	}
	return nil
}
