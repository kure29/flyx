package repo

import (
	"errors"

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

func (r *Repository) ListUsers() ([]model.User, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var users []model.User
	if err := r.db.Order("role_id ASC, id ASC").Find(&users).Error; err != nil {
		return nil, err
	}
	if users == nil {
		users = make([]model.User, 0)
	}
	return users, nil
}

func (r *Repository) HasActiveNonAdminUsers() (bool, error) {
	if r == nil || r.db == nil {
		return false, errors.New("repository not initialized")
	}
	var count int64
	if err := r.db.Model(&model.User{}).Where("role_id != 0 AND status = 1").Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *Repository) UpdateUser(id int64, username, pwdHash string, expTime, flow, flowMiB, flowResetTime int64, num, status int, now int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	updates := map[string]interface{}{
		"user":            username,
		"exp_time":        expTime,
		"flow":            flow,
		"flow_mib":        flowMiB,
		"flow_reset_time": flowResetTime,
		"num":             num,
		"status":          status,
		"updated_time":    now,
	}
	if pwdHash != "" {
		updates["pwd"] = pwdHash
		updates["password_changed_at"] = now
	}
	return r.db.Model(&model.User{}).Where("id = ?", id).Updates(updates).Error
}

func (r *Repository) UpdateUserStatus(id int64, status int, now int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Model(&model.User{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":       status,
		"updated_time": now,
	}).Error
}

func (r *Repository) ResetUserFlow(id int64, now int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Model(&model.User{}).Where("id = ?", id).Updates(map[string]interface{}{
		"in_flow":       0,
		"out_flow":      0,
		"flow_reset_at": now,
		"updated_time":  now,
	}).Error
}

func (r *Repository) DeleteUserCascade(id int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", id).Delete(&model.UserTunnel{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", id).Delete(&model.UserGroupUser{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", id).Delete(&model.MonitorPermission{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", id).Delete(&model.UserGithub{}).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", id).Delete(&model.Passkey{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.Subscription{}).Where("user_id = ?", id).Update("user_id", 0).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&model.User{}).Error
	})
}

func (r *Repository) CountForwardsByUser(userID int64) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("repository not initialized")
	}
	var count int64
	if err := r.db.Model(&model.Forward{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *Repository) CountForwardsByUserMap() (map[int64]int, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	type row struct {
		UserID int64
		Cnt    int
	}
	var rows []row
	if err := r.db.Model(&model.Forward{}).Select("user_id, COUNT(*) AS cnt").Group("user_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]int, len(rows))
	for _, rw := range rows {
		out[rw.UserID] = rw.Cnt
	}
	return out, nil
}

func (r *Repository) ListUserTunnelIDsMap() (map[int64][]int64, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var rows []struct {
		UserID   int64
		TunnelID int64
	}
	if err := r.db.Model(&model.UserTunnel{}).Select("user_id, tunnel_id").Where("status = 1").Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64][]int64)
	for _, rw := range rows {
		out[rw.UserID] = append(out[rw.UserID], rw.TunnelID)
	}
	return out, nil
}

func (r *Repository) ListUserSubscriptionIDsMap() (map[int64][]int64, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var rows []struct {
		ID     int64
		UserID int64
	}
	if err := r.db.Model(&model.Subscription{}).Select("id, user_id").Where("user_id > 0").Order("inx ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64][]int64)
	for _, rw := range rows {
		out[rw.UserID] = append(out[rw.UserID], rw.ID)
	}
	return out, nil
}

func (r *Repository) ReplaceUserTunnels(userID int64, tunnelIDs []int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	want := make(map[int64]bool, len(tunnelIDs))
	dedup := make([]int64, 0, len(tunnelIDs))
	for _, tid := range tunnelIDs {
		if tid > 0 && !want[tid] {
			want[tid] = true
			dedup = append(dedup, tid)
		}
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		var existing []model.UserTunnel
		if err := tx.Where("user_id = ?", userID).Find(&existing).Error; err != nil {
			return err
		}
		have := make(map[int64]model.UserTunnel, len(existing))
		for _, ut := range existing {
			have[ut.TunnelID] = ut
			if !want[ut.TunnelID] {
				if err := tx.Where("id = ?", ut.ID).Delete(&model.UserTunnel{}).Error; err != nil {
					return err
				}
			} else if ut.Status != 1 {
				if err := tx.Model(&model.UserTunnel{}).Where("id = ?", ut.ID).Update("status", 1).Error; err != nil {
					return err
				}
			}
		}
		for _, tid := range dedup {
			if _, ok := have[tid]; ok {
				continue
			}
			row := model.UserTunnel{
				UserID:        userID,
				TunnelID:      tid,
				Num:           0,
				Flow:          0,
				FlowMiB:       0,
				InFlow:        0,
				OutFlow:       0,
				FlowResetTime: 0,
				ExpTime:       0,
				Status:        1,
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func mapForwardToRecord(f model.Forward) model.ForwardRecord {
	proxyProtocolReceive, proxyProtocolSend := normalizeForwardProxyProtocol(f.ProxyProtocol, f.ProxyProtocolReceive, f.ProxyProtocolSend)
	return model.ForwardRecord{
		ID:                   f.ID,
		UserID:               f.UserID,
		UserName:             f.UserName,
		Name:                 f.Name,
		TunnelID:             f.TunnelID,
		RemoteAddr:           f.RemoteAddr,
		Strategy:             f.Strategy,
		Status:               f.Status,
		SpeedID:              f.SpeedID,
		MaxConn:              f.MaxConn,
		IPMaxConn:            f.IPMaxConn,
		ProxyProtocol:        proxyProtocolSend,
		ProxyProtocolReceive: proxyProtocolReceive,
		ProxyProtocolSend:    proxyProtocolSend,
	}
}

func (r *Repository) ListForwardsByUser(userID int64) ([]model.ForwardRecord, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var rows []model.Forward
	if err := r.db.Where("user_id = ?", userID).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]model.ForwardRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapForwardToRecord(row))
	}
	return out, nil
}

func (r *Repository) ListForwardsByUserAndStatus(userID int64, status int) ([]model.ForwardRecord, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var rows []model.Forward
	if err := r.db.Where("user_id = ? AND status = ?", userID, status).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]model.ForwardRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapForwardToRecord(row))
	}
	return out, nil
}

func (r *Repository) ListExpiredUsersWithActiveForwards(nowMs int64) ([]int64, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var userIDs []int64
	err := r.db.Model(&model.User{}).
		Select("DISTINCT \"user\".id").
		Joins("INNER JOIN forward ON forward.user_id = \"user\".id AND forward.status = 1").
		Where("\"user\".role_id != 0 AND \"user\".exp_time > 0 AND \"user\".exp_time <= ?", nowMs).
		Pluck("id", &userIDs).Error
	if err != nil {
		return nil, err
	}
	return userIDs, nil
}
