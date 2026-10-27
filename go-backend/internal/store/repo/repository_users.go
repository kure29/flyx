package repo

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"go-backend/internal/store/model"
)

// ─── User Queries ────────────────────────────────────────────────────

func (r *Repository) GetUserByUsername(username string) (*model.User, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var user model.User
	err := r.db.Where(`"user" = ?`, username).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *Repository) GetUserByID(id int64) (*model.User, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var user model.User
	err := r.db.Where("id = ?", id).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *Repository) UsernameExists(username string) (bool, error) {
	var count int64
	err := r.db.Model(&model.User{}).Where(`"user" = ?`, username).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *Repository) UsernameExistsExceptID(username string, exceptID int64) (bool, error) {
	if r == nil || r.db == nil {
		return false, errors.New("repository not initialized")
	}
	var count int64
	err := r.db.Model(&model.User{}).Where(`"user" = ? AND id != ?`, username, exceptID).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *Repository) UpdateUserNameAndPassword(userID int64, username, passwordMD5 string, now int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Model(&model.User{}).Where("id = ?", userID).Updates(map[string]interface{}{
		"user":                username,
		"pwd":                 passwordMD5,
		"password_changed_at": now,
		"updated_time":        now,
	}).Error
}

// ─── User Package Queries ────────────────────────────────────────────

func (r *Repository) GetUserPackageTunnels(userID int64) ([]model.UserTunnelDetail, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var items []model.UserTunnelDetail
	err := r.db.Model(&model.UserTunnel{}).
		Select("user_tunnel.id, user_tunnel.user_id, user_tunnel.tunnel_id, tunnel.name AS tunnel_name, user_tunnel.status, tunnel.flow AS tunnel_flow, user_tunnel.flow, user_tunnel.flow_mib, user_tunnel.in_flow, user_tunnel.out_flow, user_tunnel.num, user_tunnel.flow_reset_time, user_tunnel.exp_time, user_tunnel.speed_id, speed_limit.name AS speed_limit, speed_limit.speed").
		Joins("LEFT JOIN tunnel ON tunnel.id = user_tunnel.tunnel_id").
		Joins("LEFT JOIN speed_limit ON speed_limit.id = user_tunnel.speed_id").
		Where("user_tunnel.user_id = ?", userID).
		Order("user_tunnel.id ASC").
		Find(&items).Error
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = make([]model.UserTunnelDetail, 0)
	}
	return items, nil
}

func (r *Repository) GetUserPackageForwards(userID int64) ([]model.UserForwardDetail, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}

	type fwdRow struct {
		ID         int64
		Name       string
		TunnelID   int64
		TunnelName string
		RemoteAddr string
		InFlow     int64
		OutFlow    int64
		Status     int
		CreatedAt  int64
	}

	var rows []fwdRow
	err := r.db.Model(&model.Forward{}).
		Select("forward.id, forward.name, forward.tunnel_id, COALESCE(tunnel.name, '') AS tunnel_name, forward.remote_addr, forward.in_flow, forward.out_flow, forward.status, forward.created_time AS created_at").
		Joins("LEFT JOIN tunnel ON tunnel.id = forward.tunnel_id").
		Where("forward.user_id = ?", userID).
		Order("forward.id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}

	items := make([]model.UserForwardDetail, 0, len(rows))
	for _, row := range rows {
		inIP, inPort, err := resolveForwardIngress(r.db, row.ID, row.TunnelID)
		if err != nil {
			return nil, err
		}
		items = append(items, model.UserForwardDetail{
			ID: row.ID, Name: row.Name, TunnelID: row.TunnelID,
			TunnelName: row.TunnelName, InIP: inIP, InPort: inPort,
			RemoteAddr: row.RemoteAddr, InFlow: row.InFlow, OutFlow: row.OutFlow,
			Status: row.Status, CreatedAt: row.CreatedAt,
		})
	}
	return items, nil
}

func (r *Repository) ListUsers() ([]map[string]interface{}, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var users []model.User
	if err := r.db.Where("role_id != ?", 0).Order("id DESC").Find(&users).Error; err != nil {
		return nil, err
	}
	userIDs := make([]int64, 0, len(users))
	for _, u := range users {
		userIDs = append(userIDs, u.ID)
	}
	quotaMap, err := r.ListUserQuotaViewsByUserIDs(userIDs, time.Now())
	if err != nil {
		return nil, err
	}
	items := make([]map[string]interface{}, 0, len(users))
	for _, u := range users {
		item := map[string]interface{}{
			"id": u.ID, "user": u.User, "name": u.User,
			"roleId": u.RoleID, "status": u.Status,
			"flow": u.Flow, "flowMiB": u.FlowMiB, "num": u.Num, "expTime": u.ExpTime,
			"flowResetTime": u.FlowResetTime, "createdTime": u.CreatedTime,
			"updatedTime": nullableInt64(u.UpdatedTime),
			"inFlow":      u.InFlow, "outFlow": u.OutFlow,
			"maxConn": u.MaxConn,
		}
		if quota := quotaMap[u.ID]; quota != nil {
			item["dailyQuotaGB"] = quota.DailyLimitGB
			item["monthlyQuotaGB"] = quota.MonthlyLimitGB
			item["dailyUsedBytes"] = quota.DailyUsedBytes
			item["monthlyUsedBytes"] = quota.MonthlyUsedBytes
			item["disabledByQuota"] = quota.DisabledByQuota
			item["quotaDisabledAt"] = quota.DisabledAt
		}
		items = append(items, item)
	}
	return items, nil
}

func (r *Repository) ListExpiredActiveUserIDs(nowMs int64) ([]int64, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var ids []int64
	err := r.db.Model(&model.User{}).
		Where("role_id != 0 AND status = 1 AND exp_time > 0 AND exp_time < ?", nowMs).
		Pluck("id", &ids).Error
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *Repository) DisableUser(userID int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Model(&model.User{}).Where("id = ?", userID).Update("status", 0).Error
}

func (r *Repository) ListExpiredActiveUserTunnels(nowMs int64) ([]model.ExpiredUserTunnel, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var uts []model.UserTunnel
	err := r.db.Where("status = 1 AND exp_time > 0 AND exp_time < ?", nowMs).Find(&uts).Error
	if err != nil {
		return nil, err
	}
	out := make([]model.ExpiredUserTunnel, len(uts))
	for i, ut := range uts {
		out[i] = model.ExpiredUserTunnel{ID: ut.ID, UserID: ut.UserID, TunnelID: ut.TunnelID}
	}
	return out, nil
}

func (r *Repository) DisableUserTunnel(id int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Model(&model.UserTunnel{}).Where("id = ?", id).Update("status", 0).Error
}

func (r *Repository) GetUserTunnelByID(id int64) (*model.UserTunnel, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var ut model.UserTunnel
	err := r.db.Where("id = ?", id).First(&ut).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ut, nil
}
