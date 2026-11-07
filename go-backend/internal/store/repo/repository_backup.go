package repo

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go-backend/internal/security"
	"go-backend/internal/store/model"
)

// ─── Export Methods ──────────────────────────────────────────────────

// Keep balances and receipts in the same database snapshot. In PostgreSQL a
// plain READ COMMITTED transaction would still mix different snapshots.
func (r *Repository) exportBackupSnapshot(read func(*Repository) (*model.BackupData, error)) (*model.BackupData, error) {
	var backup *model.BackupData
	opts := &sql.TxOptions{}
	if r.db.Dialector.Name() == "postgres" {
		opts.Isolation = sql.LevelRepeatableRead
	}
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var err error
		backup, err = read(&Repository{db: tx, dbPath: r.dbPath})
		return err
	}, opts)
	return backup, err
}

func (r *Repository) ExportAll() (*model.BackupData, error) {
	return r.exportBackupSnapshot(func(tx *Repository) (*model.BackupData, error) { return tx.exportAll() })
}

func (r *Repository) exportAll() (*model.BackupData, error) {
	backup := &model.BackupData{Version: "1.0", ExportedAt: unixMilliNow()}

	users, err := r.exportUsers()
	if err != nil {
		return nil, fmt.Errorf("export users failed: %w", err)
	}
	backup.Users = users

	nodes, err := r.exportNodes()
	if err != nil {
		return nil, fmt.Errorf("export nodes failed: %w", err)
	}
	backup.Nodes = nodes

	tunnels, err := r.exportTunnels()
	if err != nil {
		return nil, fmt.Errorf("export tunnels failed: %w", err)
	}
	backup.Tunnels = tunnels

	forwards, err := r.exportForwards()
	if err != nil {
		return nil, fmt.Errorf("export forwards failed: %w", err)
	}
	backup.Forwards = forwards

	userTunnels, err := r.exportUserTunnels()
	if err != nil {
		return nil, fmt.Errorf("export user tunnels failed: %w", err)
	}
	backup.UserTunnels = userTunnels

	speedLimits, err := r.exportSpeedLimits()
	if err != nil {
		return nil, fmt.Errorf("export speed limits failed: %w", err)
	}
	backup.SpeedLimits = speedLimits

	tunnelGroups, err := r.exportTunnelGroups()
	if err != nil {
		return nil, fmt.Errorf("export tunnel groups failed: %w", err)
	}
	backup.TunnelGroups = tunnelGroups

	userGroups, err := r.exportUserGroups()
	if err != nil {
		return nil, fmt.Errorf("export user groups failed: %w", err)
	}
	backup.UserGroups = userGroups

	permissions, err := r.exportPermissions()
	if err != nil {
		return nil, fmt.Errorf("export permissions failed: %w", err)
	}
	backup.Permissions = permissions

	configs, err := r.ListConfigs()
	if err != nil {
		return nil, fmt.Errorf("export configs failed: %w", err)
	}
	backup.Configs = FilterBackupConfigs(configs)

	inbounds, err := r.exportInbounds()
	if err != nil {
		return nil, fmt.Errorf("export inbounds failed: %w", err)
	}
	backup.Inbounds = inbounds

	if err := r.db.Where("scope LIKE ?", "flow:%").Find(&backup.FlowCursors).Error; err != nil {
		return nil, err
	}
	return backup, nil
}

func (r *Repository) ExportPartial(types []string) (*model.BackupData, error) {
	return r.exportBackupSnapshot(func(tx *Repository) (*model.BackupData, error) { return tx.exportPartial(types) })
}

func (r *Repository) exportPartial(types []string) (*model.BackupData, error) {
	backup := &model.BackupData{Version: "1.0", ExportedAt: unixMilliNow()}
	typeSet := make(map[string]bool)
	for _, t := range types {
		typeSet[t] = true
	}

	if typeSet["users"] {
		v, err := r.exportUsers()
		if err != nil {
			return nil, fmt.Errorf("export users failed: %w", err)
		}
		backup.Users = v
	}
	if typeSet["nodes"] {
		v, err := r.exportNodes()
		if err != nil {
			return nil, fmt.Errorf("export nodes failed: %w", err)
		}
		backup.Nodes = v
	}
	if typeSet["tunnels"] {
		v, err := r.exportTunnels()
		if err != nil {
			return nil, fmt.Errorf("export tunnels failed: %w", err)
		}
		backup.Tunnels = v
	}
	if typeSet["forwards"] {
		v, err := r.exportForwards()
		if err != nil {
			return nil, fmt.Errorf("export forwards failed: %w", err)
		}
		backup.Forwards = v
	}
	if typeSet["userTunnels"] {
		v, err := r.exportUserTunnels()
		if err != nil {
			return nil, fmt.Errorf("export user tunnels failed: %w", err)
		}
		backup.UserTunnels = v
	}
	if typeSet["speedLimits"] {
		v, err := r.exportSpeedLimits()
		if err != nil {
			return nil, fmt.Errorf("export speed limits failed: %w", err)
		}
		backup.SpeedLimits = v
	}
	if typeSet["tunnelGroups"] {
		v, err := r.exportTunnelGroups()
		if err != nil {
			return nil, fmt.Errorf("export tunnel groups failed: %w", err)
		}
		backup.TunnelGroups = v
	}
	if typeSet["userGroups"] {
		v, err := r.exportUserGroups()
		if err != nil {
			return nil, fmt.Errorf("export user groups failed: %w", err)
		}
		backup.UserGroups = v
	}
	if typeSet["permissions"] {
		v, err := r.exportPermissions()
		if err != nil {
			return nil, fmt.Errorf("export permissions failed: %w", err)
		}
		backup.Permissions = v
	}
	if typeSet["configs"] {
		v, err := r.ListConfigs()
		if err != nil {
			return nil, fmt.Errorf("export configs failed: %w", err)
		}
		backup.Configs = FilterBackupConfigs(v)
	}
	if typeSet["inbounds"] {
		v, err := r.exportInbounds()
		if err != nil {
			return nil, fmt.Errorf("export inbounds failed: %w", err)
		}
		backup.Inbounds = v
	}
	if typeSet["users"] && typeSet["nodes"] && typeSet["forwards"] && typeSet["userTunnels"] {
		if err := r.db.Where("scope LIKE ?", "flow:%").Find(&backup.FlowCursors).Error; err != nil {
			return nil, err
		}
	}
	return backup, nil
}

func (r *Repository) exportUsers() ([]model.UserBackup, error) {
	var users []model.User
	if err := r.db.Order("id ASC").Find(&users).Error; err != nil {
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
	out := make([]model.UserBackup, 0, len(users))
	for _, u := range users {
		b := model.UserBackup{
			ID: u.ID, User: u.User, Pwd: u.Pwd, RoleID: u.RoleID,
			ExpTime: u.ExpTime, Flow: u.Flow, FlowMiB: u.FlowMiB, InFlow: u.InFlow, OutFlow: u.OutFlow,
			FlowResetTime: u.FlowResetTime, FlowResetAt: u.FlowResetAt, Num: u.Num,
			CreatedTime: u.CreatedTime, Status: u.Status,
		}
		if quota := quotaMap[u.ID]; quota != nil {
			b.DailyQuotaGB = quota.DailyLimitGB
			b.MonthlyQuotaGB = quota.MonthlyLimitGB
			b.DisabledByQuota = quota.DisabledByQuota
			b.QuotaDisabledAt = quota.DisabledAt
		}
		if u.UpdatedTime.Valid {
			b.UpdatedTime = u.UpdatedTime.Int64
		}
		out = append(out, b)
	}
	return out, nil
}

func (r *Repository) exportNodes() ([]model.NodeBackup, error) {
	var nodes []model.Node
	if err := r.db.Order("inx ASC, id ASC").Find(&nodes).Error; err != nil {
		return nil, err
	}
	out := make([]model.NodeBackup, 0, len(nodes))
	for _, n := range nodes {
		b := model.NodeBackup{
			ID: n.ID, Name: n.Name, Secret: n.Secret, ServerIP: n.ServerIP,
			Remark: n.Remark.String, RenewalCycle: n.RenewalCycle.String,
			Port: n.Port, HTTP: n.HTTP, TLS: n.TLS, Socks: n.Socks,
			CreatedTime: n.CreatedTime, Status: n.Status,
			TCPListenAddr: n.TCPListenAddr, UDPListenAddr: n.UDPListenAddr,
			Inx: n.Inx, IsRemote: n.IsRemote,
			LatencyTargets: n.LatencyTargets,
			NotifyEvents:   &n.NotifyEvents,
		}
		if n.ExpiryTime.Valid {
			b.ExpiryTime = n.ExpiryTime.Int64
		}
		if n.UpdatedTime.Valid {
			b.UpdatedTime = n.UpdatedTime.Int64
		}
		if n.ServerIPV4.Valid {
			b.ServerIPv4 = n.ServerIPV4.String
		}
		if n.ServerIPV6.Valid {
			b.ServerIPv6 = n.ServerIPV6.String
		}
		if n.InterfaceName.Valid {
			b.InterfaceName = n.InterfaceName.String
		}
		if n.Version.Valid {
			b.Version = n.Version.String
		}
		if n.RemoteURL.Valid {
			b.RemoteURL = n.RemoteURL.String
		}
		if n.RemoteToken.Valid {
			b.RemoteToken = n.RemoteToken.String
		}
		if n.RemoteConfig.Valid {
			b.RemoteConfig = n.RemoteConfig.String
		}
		out = append(out, b)
	}
	return out, nil
}

func (r *Repository) exportTunnels() ([]model.TunnelBackup, error) {
	var tunnels []model.Tunnel
	if err := r.db.Order("inx ASC, id ASC").Find(&tunnels).Error; err != nil {
		return nil, err
	}
	out := make([]model.TunnelBackup, 0, len(tunnels))
	for _, t := range tunnels {
		b := model.TunnelBackup{
			ID: t.ID, Name: t.Name, TrafficRatio: t.TrafficRatio,
			Type: t.Type, Protocol: t.Protocol, Flow: t.Flow,
			CreatedTime: t.CreatedTime, UpdatedTime: t.UpdatedTime,
			Status: t.Status, Inx: t.Inx, IPPreference: t.IPPreference,
			ProbeTargetHost: t.ProbeTargetHost, ProbeTargetPort: t.ProbeTargetPort,
			Engine: t.Engine,
		}
		if t.InIP.Valid {
			b.InIP = t.InIP.String
		}
		chains, err := r.exportChainTunnels(t.ID)
		if err != nil {
			return nil, err
		}
		b.ChainTunnels = chains
		out = append(out, b)
	}
	return out, nil
}

func (r *Repository) exportChainTunnels(tunnelID int64) ([]model.ChainTunnelBackup, error) {
	var chains []model.ChainTunnel
	if err := r.db.Where("tunnel_id = ?", tunnelID).Order("inx ASC, id ASC").Find(&chains).Error; err != nil {
		return nil, err
	}
	out := make([]model.ChainTunnelBackup, 0, len(chains))
	for _, c := range chains {
		b := model.ChainTunnelBackup{
			ID: c.ID, TunnelID: c.TunnelID, ChainType: c.ChainType, NodeID: c.NodeID,
		}
		if c.Port.Valid {
			b.Port = int(c.Port.Int64)
		}
		if c.Strategy.Valid {
			b.Strategy = c.Strategy.String
		}
		if c.Inx.Valid {
			b.Inx = int(c.Inx.Int64)
		}
		if c.Protocol.Valid {
			b.Protocol = c.Protocol.String
		}
		out = append(out, b)
	}
	return out, nil
}

func (r *Repository) exportForwards() ([]model.ForwardBackup, error) {
	var forwards []model.Forward
	if err := r.db.Order("id ASC").Find(&forwards).Error; err != nil {
		return nil, err
	}
	out := make([]model.ForwardBackup, 0, len(forwards))
	for _, f := range forwards {
		b := model.ForwardBackup{
			ID: f.ID, UserID: f.UserID, UserName: f.UserName, Name: f.Name,
			TunnelID: f.TunnelID, RemoteAddr: f.RemoteAddr, Strategy: f.Strategy,
			InFlow: f.InFlow, OutFlow: f.OutFlow, CreatedTime: f.CreatedTime,
			UpdatedTime: f.UpdatedTime, Status: f.Status, Inx: f.Inx,
			IPMaxConn:            f.IPMaxConn,
			ProxyProtocol:        f.ProxyProtocol,
			ProxyProtocolReceive: f.ProxyProtocolReceive,
			ProxyProtocolSend:    f.ProxyProtocolSend,
		}
		if f.SpeedID.Valid {
			v := f.SpeedID.Int64
			b.SpeedID = &v
		}
		if f.IPSpeedID.Valid {
			v := f.IPSpeedID.Int64
			b.IPSpeedID = &v
		}
		ports, err := r.exportForwardPorts(f.ID)
		if err != nil {
			return nil, err
		}
		portsCopy := append([]model.ForwardPortBackup(nil), ports...)
		b.ForwardPorts = &portsCopy
		out = append(out, b)
	}
	return out, nil
}

func (r *Repository) exportForwardPorts(forwardID int64) ([]model.ForwardPortBackup, error) {
	var fps []model.ForwardPort
	if err := r.db.Where("forward_id = ?", forwardID).Order("id ASC").Find(&fps).Error; err != nil {
		return nil, err
	}
	out := make([]model.ForwardPortBackup, 0, len(fps))
	for _, fp := range fps {
		out = append(out, model.ForwardPortBackup{NodeID: fp.NodeID, Port: fp.Port})
	}
	return out, nil
}

func (r *Repository) exportUserTunnels() ([]model.UserTunnelBackup, error) {
	var uts []model.UserTunnel
	if err := r.db.Order("id ASC").Find(&uts).Error; err != nil {
		return nil, err
	}
	out := make([]model.UserTunnelBackup, 0, len(uts))
	for _, ut := range uts {
		b := model.UserTunnelBackup{
			ID: ut.ID, UserID: ut.UserID, TunnelID: ut.TunnelID,
			Num: ut.Num, Flow: ut.Flow, FlowMiB: ut.FlowMiB, InFlow: ut.InFlow, OutFlow: ut.OutFlow,
			FlowResetTime: ut.FlowResetTime, FlowResetAt: ut.FlowResetAt, ExpTime: ut.ExpTime, Status: ut.Status,
		}
		if ut.SpeedID.Valid {
			b.SpeedID = ut.SpeedID.Int64
		}
		out = append(out, b)
	}
	return out, nil
}

func (r *Repository) exportSpeedLimits() ([]model.SpeedLimitBackup, error) {
	var sls []model.SpeedLimit
	if err := r.db.Order("id ASC").Find(&sls).Error; err != nil {
		return nil, err
	}
	out := make([]model.SpeedLimitBackup, 0, len(sls))
	for _, sl := range sls {
		b := model.SpeedLimitBackup{
			ID: sl.ID, Name: sl.Name, Speed: int64(sl.Speed),
			CreatedTime: sl.CreatedTime, Status: sl.Status,
		}
		if sl.UpdatedTime.Valid {
			b.UpdatedTime = sl.UpdatedTime.Int64
		}
		out = append(out, b)
	}
	return out, nil
}

func (r *Repository) exportTunnelGroups() ([]model.TunnelGroupBackup, error) {
	var groups []model.TunnelGroup
	if err := r.db.Order("id ASC").Find(&groups).Error; err != nil {
		return nil, err
	}
	out := make([]model.TunnelGroupBackup, 0, len(groups))
	for _, tg := range groups {
		b := model.TunnelGroupBackup{
			ID: tg.ID, Name: tg.Name, CreatedTime: tg.CreatedTime,
			UpdatedTime: tg.UpdatedTime, Status: tg.Status,
		}
		var tunnelIDs []int64
		r.db.Model(&model.TunnelGroupTunnel{}).Where("tunnel_group_id = ?", tg.ID).Pluck("tunnel_id", &tunnelIDs)
		b.Tunnels = tunnelIDs
		out = append(out, b)
	}
	return out, nil
}

func (r *Repository) exportUserGroups() ([]model.UserGroupBackup, error) {
	var groups []model.UserGroup
	if err := r.db.Order("id ASC").Find(&groups).Error; err != nil {
		return nil, err
	}
	out := make([]model.UserGroupBackup, 0, len(groups))
	for _, ug := range groups {
		b := model.UserGroupBackup{
			ID: ug.ID, Name: ug.Name, CreatedTime: ug.CreatedTime,
			UpdatedTime: ug.UpdatedTime, Status: ug.Status,
		}
		var userIDs []int64
		r.db.Model(&model.UserGroupUser{}).Where("user_group_id = ?", ug.ID).Pluck("user_id", &userIDs)
		b.Users = userIDs
		out = append(out, b)
	}
	return out, nil
}

func (r *Repository) exportPermissions() ([]model.PermissionBackup, error) {
	var perms []model.GroupPermission
	if err := r.db.Order("id ASC").Find(&perms).Error; err != nil {
		return nil, err
	}
	out := make([]model.PermissionBackup, 0, len(perms))
	for _, p := range perms {
		b := model.PermissionBackup{
			ID: p.ID, UserGroupID: p.UserGroupID, TunnelGroupID: p.TunnelGroupID,
			CreatedTime: p.CreatedTime,
		}
		var grants []model.GroupPermissionGrant
		r.db.Where("user_group_id = ? AND tunnel_group_id = ?", p.UserGroupID, p.TunnelGroupID).Find(&grants)
		for _, g := range grants {
			b.Grants = append(b.Grants, model.PermissionGrantBackup{
				ID: g.ID, UserGroupID: g.UserGroupID, TunnelGroupID: g.TunnelGroupID,
				UserTunnelID: g.UserTunnelID, CreatedTime: g.CreatedTime,
				CreatedByGroup: g.CreatedByGroup,
			})
		}
		out = append(out, b)
	}
	return out, nil
}

// ─── Import Methods ──────────────────────────────────────────────────

func (r *Repository) Import(backup *model.BackupData, types []string) (*model.ImportResult, error) {
	result := &model.ImportResult{}
	typeSet := make(map[string]bool)
	for _, t := range types {
		typeSet[t] = true
	}

	err := r.db.Transaction(func(tx *gorm.DB) error {
		now := unixMilliNow()

		if typeSet["users"] && len(backup.Users) > 0 {
			count, err := importUsers(tx, backup.Users, now)
			if err != nil {
				return fmt.Errorf("import users failed: %w", err)
			}
			result.UsersImported = count
		}
		if typeSet["nodes"] && len(backup.Nodes) > 0 {
			count, err := importNodes(tx, backup.Nodes, now)
			if err != nil {
				return fmt.Errorf("import nodes failed: %w", err)
			}
			result.NodesImported = count
		}
		if typeSet["tunnels"] && len(backup.Tunnels) > 0 {
			count, err := importTunnels(tx, backup.Tunnels, now)
			if err != nil {
				return fmt.Errorf("import tunnels failed: %w", err)
			}
			result.TunnelsImported = count
		}
		if typeSet["forwards"] && len(backup.Forwards) > 0 {
			count, err := importForwards(tx, backup.Forwards, now)
			if err != nil {
				return fmt.Errorf("import forwards failed: %w", err)
			}
			result.ForwardsImported = count
		}
		if typeSet["userTunnels"] && len(backup.UserTunnels) > 0 {
			count, err := importUserTunnels(tx, backup.UserTunnels, now)
			if err != nil {
				return fmt.Errorf("import user tunnels failed: %w", err)
			}
			result.UserTunnelsImported = count
		}
		if typeSet["speedLimits"] && len(backup.SpeedLimits) > 0 {
			count, err := importSpeedLimits(tx, backup.SpeedLimits, now)
			if err != nil {
				return fmt.Errorf("import speed limits failed: %w", err)
			}
			result.SpeedLimitsImported = count
		}
		if typeSet["tunnelGroups"] && len(backup.TunnelGroups) > 0 {
			count, err := importTunnelGroups(tx, backup.TunnelGroups, now)
			if err != nil {
				return fmt.Errorf("import tunnel groups failed: %w", err)
			}
			result.TunnelGroupsImported = count
		}
		if typeSet["userGroups"] && len(backup.UserGroups) > 0 {
			count, err := importUserGroups(tx, backup.UserGroups, now)
			if err != nil {
				return fmt.Errorf("import user groups failed: %w", err)
			}
			result.UserGroupsImported = count
		}
		if typeSet["permissions"] && len(backup.Permissions) > 0 {
			count, err := importPermissions(tx, backup.Permissions, now)
			if err != nil {
				return fmt.Errorf("import permissions failed: %w", err)
			}
			result.PermissionsImported = count
		}
		if typeSet["configs"] && len(backup.Configs) > 0 {
			count, err := importConfigs(tx, backup.Configs, now)
			if err != nil {
				return fmt.Errorf("import configs failed: %w", err)
			}
			result.ConfigsImported = count
		}
		if typeSet["inbounds"] && backup.Inbounds != nil {
			count, err := importInbounds(tx, backup.Inbounds, now)
			if err != nil {
				return fmt.Errorf("import inbounds failed: %w", err)
			}
			result.InboundsImported = count
		}
		if typeSet["users"] && typeSet["nodes"] && typeSet["forwards"] && typeSet["userTunnels"] {
			imported := make(map[int64]bool)
			for _, node := range backup.Nodes {
				imported[node.ID] = true
				if err := tx.Where("scope LIKE ?", fmt.Sprintf("flow:%d:%%", node.ID)).Delete(&model.TrafficCursor{}).Error; err != nil {
					return err
				}
			}
			for _, cursor := range backup.FlowCursors {
				var nodeID int64
				var stream string
				if _, err := fmt.Sscanf(cursor.Scope, "flow:%d:%s", &nodeID, &stream); err != nil || !imported[nodeID] {
					continue
				}
				id, err := hex.DecodeString(stream)
				digest, digestErr := hex.DecodeString(cursor.Digest)
				if err != nil || len(id) != 16 || cursor.Sequence <= 0 || digestErr != nil || len(digest) != 32 {
					return fmt.Errorf("invalid flow cursor in backup")
				}
				if err := tx.Create(&cursor).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Rows restored with their ids leave PostgreSQL sequences behind; move
	// them past the restored ids so new rows don't collide.
	if err := ensurePostgresIDDefaultsFn(r.db); err != nil {
		return nil, err
	}
	return result, nil
}

func importUsers(tx *gorm.DB, users []model.UserBackup, now int64) (int, error) {
	count := 0
	for _, u := range users {
		pwdHash, status, err := normalizeImportedUserPassword(u.Pwd, u.Status)
		if err != nil {
			return count, err
		}
		item := model.User{
			ID:                u.ID,
			User:              u.User,
			Pwd:               pwdHash,
			RoleID:            u.RoleID,
			ExpTime:           u.ExpTime,
			Flow:              u.Flow,
			FlowMiB:           u.FlowMiB,
			InFlow:            u.InFlow,
			OutFlow:           u.OutFlow,
			FlowResetTime:     u.FlowResetTime,
			FlowResetAt:       u.FlowResetAt,
			Num:               u.Num,
			CreatedTime:       u.CreatedTime,
			UpdatedTime:       sql.NullInt64{Int64: now, Valid: true},
			Status:            status,
			PasswordChangedAt: now,
		}
		err = tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"user", "pwd", "role_id", "exp_time", "flow", "flow_mib", "in_flow", "out_flow",
				"flow_reset_time", "flow_reset_at", "num", "updated_time", "status", "password_changed_at",
			}),
		}).Create(&item).Error
		if err != nil {
			return count, err
		}
		if u.DailyQuotaGB > 0 || u.MonthlyQuotaGB > 0 || u.DisabledByQuota != 0 || u.QuotaDisabledAt > 0 {
			current := time.UnixMilli(now)
			dayKey := int64(current.Year()*10000 + int(current.Month())*100 + current.Day())
			monthKey := int64(current.Year()*100 + int(current.Month()))
			quotaItem := model.UserQuota{
				UserID:           u.ID,
				DailyLimitGB:     u.DailyQuotaGB,
				MonthlyLimitGB:   u.MonthlyQuotaGB,
				DailyUsedBytes:   0,
				MonthlyUsedBytes: 0,
				DayKey:           dayKey,
				MonthKey:         monthKey,
				DisabledByQuota:  u.DisabledByQuota,
				DisabledAt:       u.QuotaDisabledAt,
				PausedForwardIDs: "",
				CreatedTime:      now,
				UpdatedTime:      now,
			}
			err = tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "user_id"}},
				DoUpdates: clause.AssignmentColumns([]string{
					"daily_limit_gb", "monthly_limit_gb", "daily_used_bytes", "monthly_used_bytes",
					"day_key", "month_key", "disabled_by_quota", "disabled_at", "paused_forward_ids", "updated_time",
				}),
			}).Create(&quotaItem).Error
			if err != nil {
				return count, err
			}
		} else {
			if err := tx.Where("user_id = ?", u.ID).Delete(&model.UserQuota{}).Error; err != nil {
				return count, err
			}
		}
		count++
	}
	return count, nil
}

func normalizeImportedUserPassword(password string, status int) (string, int, error) {
	password = strings.TrimSpace(password)
	if strings.HasPrefix(password, "$2") {
		return password, status, nil
	}
	if security.IsLegacyPasswordHash(password) || password == "" {
		replacement, err := randomPasswordHash()
		if err != nil {
			return "", status, err
		}
		return replacement, 0, nil
	}
	hash, err := security.HashPassword(password)
	if err != nil {
		return "", status, err
	}
	return hash, status, nil
}

func randomPasswordHash() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return security.HashPassword(hex.EncodeToString(buf))
}

func importNodes(tx *gorm.DB, nodes []model.NodeBackup, now int64) (int, error) {
	count := 0
	for _, n := range nodes {
		item := model.Node{
			ID:             n.ID,
			Name:           n.Name,
			Remark:         sql.NullString{String: n.Remark, Valid: n.Remark != ""},
			ExpiryTime:     sql.NullInt64{Int64: n.ExpiryTime, Valid: n.ExpiryTime > 0},
			RenewalCycle:   sql.NullString{String: n.RenewalCycle, Valid: n.RenewalCycle != ""},
			Secret:         n.Secret,
			ServerIP:       n.ServerIP,
			ServerIPV4:     sql.NullString{String: n.ServerIPv4, Valid: true},
			ServerIPV6:     sql.NullString{String: n.ServerIPv6, Valid: true},
			Port:           n.Port,
			InterfaceName:  sql.NullString{String: n.InterfaceName, Valid: true},
			Version:        sql.NullString{String: n.Version, Valid: true},
			HTTP:           n.HTTP,
			TLS:            n.TLS,
			Socks:          n.Socks,
			CreatedTime:    n.CreatedTime,
			UpdatedTime:    sql.NullInt64{Int64: now, Valid: true},
			Status:         n.Status,
			TCPListenAddr:  n.TCPListenAddr,
			UDPListenAddr:  n.UDPListenAddr,
			Inx:            n.Inx,
			IsRemote:       n.IsRemote,
			RemoteURL:      sql.NullString{String: n.RemoteURL, Valid: true},
			RemoteToken:    sql.NullString{String: n.RemoteToken, Valid: true},
			RemoteConfig:   sql.NullString{String: n.RemoteConfig, Valid: true},
			LatencyTargets: n.LatencyTargets,
		}
		columns := []string{
			"name", "remark", "expiry_time", "renewal_cycle", "secret", "server_ip", "server_ip_v4", "server_ip_v6", "port", "interface_name", "version",
			"http", "tls", "socks", "updated_time", "status", "tcp_listen_addr", "udp_listen_addr",
			"inx", "is_remote", "remote_url", "remote_token", "remote_config",
		}
		if n.NotifyEvents != nil {
			item.NotifyEvents = *n.NotifyEvents
			columns = append(columns, "notify_events")
		}
		err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns(columns),
		}).Create(&item).Error
		if err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func importTunnels(tx *gorm.DB, tunnels []model.TunnelBackup, now int64) (int, error) {
	count := 0
	for _, t := range tunnels {
		item := model.Tunnel{
			ID:              t.ID,
			Name:            t.Name,
			TrafficRatio:    t.TrafficRatio,
			Type:            t.Type,
			Protocol:        t.Protocol,
			Flow:            t.Flow,
			CreatedTime:     t.CreatedTime,
			UpdatedTime:     now,
			Status:          t.Status,
			InIP:            sql.NullString{String: t.InIP, Valid: true},
			Inx:             t.Inx,
			IPPreference:    t.IPPreference,
			ProbeTargetHost: t.ProbeTargetHost,
			ProbeTargetPort: t.ProbeTargetPort,
			Engine:          t.Engine,
		}
		err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"name", "traffic_ratio", "type", "protocol", "flow", "updated_time", "status", "in_ip", "inx", "ip_preference", "probe_target_host", "probe_target_port", "engine",
			}),
		}).Create(&item).Error
		if err != nil {
			return count, err
		}
		for _, ct := range t.ChainTunnels {
			chainItem := model.ChainTunnel{
				ID:        ct.ID,
				TunnelID:  ct.TunnelID,
				ChainType: ct.ChainType,
				NodeID:    ct.NodeID,
				Port:      sql.NullInt64{Int64: int64(ct.Port), Valid: true},
				Strategy:  sql.NullString{String: ct.Strategy, Valid: true},
				Inx:       sql.NullInt64{Int64: int64(ct.Inx), Valid: true},
				Protocol:  sql.NullString{String: ct.Protocol, Valid: true},
			}
			err = tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "id"}},
				DoUpdates: clause.AssignmentColumns([]string{
					"chain_type", "node_id", "port", "strategy", "inx", "protocol",
				}),
			}).Create(&chainItem).Error
			if err != nil {
				return count, err
			}
		}
		count++
	}
	return count, nil
}

func nullableBackupInt64(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

func importForwards(tx *gorm.DB, forwards []model.ForwardBackup, now int64) (int, error) {
	count := 0
	for _, f := range forwards {
		item := model.Forward{
			ID:                   f.ID,
			UserID:               f.UserID,
			UserName:             f.UserName,
			Name:                 f.Name,
			TunnelID:             f.TunnelID,
			RemoteAddr:           f.RemoteAddr,
			Strategy:             f.Strategy,
			InFlow:               f.InFlow,
			OutFlow:              f.OutFlow,
			CreatedTime:          f.CreatedTime,
			UpdatedTime:          now,
			Status:               f.Status,
			Inx:                  f.Inx,
			SpeedID:              sql.NullInt64{Int64: nullableBackupInt64(f.SpeedID), Valid: f.SpeedID != nil && *f.SpeedID > 0},
			IPMaxConn:            f.IPMaxConn,
			IPSpeedID:            sql.NullInt64{Int64: nullableBackupInt64(f.IPSpeedID), Valid: f.IPSpeedID != nil && *f.IPSpeedID > 0},
			ProxyProtocol:        f.ProxyProtocol,
			ProxyProtocolReceive: f.ProxyProtocolReceive,
			ProxyProtocolSend:    f.ProxyProtocolSend,
		}
		err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"user_id", "user_name", "name", "tunnel_id", "remote_addr", "strategy",
				"in_flow", "out_flow", "updated_time", "status", "inx", "speed_id", "ip_max_conn", "ip_speed_id", "proxy_protocol", "proxy_protocol_receive", "proxy_protocol_send",
			}),
		}).Create(&item).Error
		if err != nil {
			return count, err
		}
		if f.ForwardPorts != nil {
			if err := tx.Where("forward_id = ?", f.ID).Delete(&model.ForwardPort{}).Error; err != nil {
				return count, err
			}
			for _, fp := range *f.ForwardPorts {
				if err := tx.Create(&model.ForwardPort{ForwardID: f.ID, NodeID: fp.NodeID, Port: fp.Port}).Error; err != nil {
					return count, err
				}
			}
		}
		count++
	}
	return count, nil
}

func importUserTunnels(tx *gorm.DB, userTunnels []model.UserTunnelBackup, _ int64) (int, error) {
	count := 0
	for _, ut := range userTunnels {
		item := model.UserTunnel{
			ID:            ut.ID,
			UserID:        ut.UserID,
			TunnelID:      ut.TunnelID,
			SpeedID:       sql.NullInt64{Int64: ut.SpeedID, Valid: ut.SpeedID > 0},
			Num:           ut.Num,
			Flow:          ut.Flow,
			FlowMiB:       ut.FlowMiB,
			InFlow:        ut.InFlow,
			OutFlow:       ut.OutFlow,
			FlowResetTime: ut.FlowResetTime, FlowResetAt: ut.FlowResetAt,
			ExpTime: ut.ExpTime,
			Status:  ut.Status,
		}
		err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"user_id", "tunnel_id", "speed_id", "num", "flow", "flow_mib", "in_flow", "out_flow",
				"flow_reset_time", "flow_reset_at", "exp_time", "status",
			}),
		}).Create(&item).Error
		if err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func importSpeedLimits(tx *gorm.DB, speedLimits []model.SpeedLimitBackup, now int64) (int, error) {
	count := 0
	for _, sl := range speedLimits {
		item := model.SpeedLimit{
			ID:          sl.ID,
			Name:        sl.Name,
			Speed:       int(sl.Speed),
			TunnelID:    sql.NullInt64{Int64: 0, Valid: false},
			TunnelName:  sql.NullString{String: "", Valid: false},
			CreatedTime: sl.CreatedTime,
			UpdatedTime: sql.NullInt64{Int64: now, Valid: true},
			Status:      sl.Status,
		}
		err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"name", "speed", "tunnel_id", "tunnel_name", "updated_time", "status",
			}),
		}).Create(&item).Error
		if err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func importTunnelGroups(tx *gorm.DB, tunnelGroups []model.TunnelGroupBackup, now int64) (int, error) {
	count := 0
	for _, tg := range tunnelGroups {
		item := model.TunnelGroup{
			ID:          tg.ID,
			Name:        tg.Name,
			CreatedTime: tg.CreatedTime,
			UpdatedTime: now,
			Status:      tg.Status,
		}
		err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{"name", "updated_time", "status"}),
		}).Create(&item).Error
		if err != nil {
			return count, err
		}
		if err := tx.Where("tunnel_group_id = ?", tg.ID).Delete(&model.TunnelGroupTunnel{}).Error; err != nil {
			return count, err
		}
		for _, tunnelID := range tg.Tunnels {
			if err := tx.Create(&model.TunnelGroupTunnel{TunnelGroupID: tg.ID, TunnelID: tunnelID, CreatedTime: now}).Error; err != nil {
				return count, err
			}
		}
		count++
	}
	return count, nil
}

func importUserGroups(tx *gorm.DB, userGroups []model.UserGroupBackup, now int64) (int, error) {
	count := 0
	for _, ug := range userGroups {
		item := model.UserGroup{
			ID:          ug.ID,
			Name:        ug.Name,
			CreatedTime: ug.CreatedTime,
			UpdatedTime: now,
			Status:      ug.Status,
		}
		err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{"name", "updated_time", "status"}),
		}).Create(&item).Error
		if err != nil {
			return count, err
		}
		if err := tx.Where("user_group_id = ?", ug.ID).Delete(&model.UserGroupUser{}).Error; err != nil {
			return count, err
		}
		for _, userID := range ug.Users {
			if err := tx.Create(&model.UserGroupUser{UserGroupID: ug.ID, UserID: userID, CreatedTime: now}).Error; err != nil {
				return count, err
			}
		}
		count++
	}
	return count, nil
}

func importPermissions(tx *gorm.DB, permissions []model.PermissionBackup, _ int64) (int, error) {
	count := 0
	for _, p := range permissions {
		item := model.GroupPermission{
			ID:            p.ID,
			UserGroupID:   p.UserGroupID,
			TunnelGroupID: p.TunnelGroupID,
			CreatedTime:   p.CreatedTime,
		}
		err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{"user_group_id", "tunnel_group_id"}),
		}).Create(&item).Error
		if err != nil {
			return count, err
		}
		for _, g := range p.Grants {
			grantItem := model.GroupPermissionGrant{
				ID:             g.ID,
				UserGroupID:    g.UserGroupID,
				TunnelGroupID:  g.TunnelGroupID,
				UserTunnelID:   g.UserTunnelID,
				CreatedTime:    g.CreatedTime,
				CreatedByGroup: g.CreatedByGroup,
			}
			err = tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "id"}},
				DoUpdates: clause.AssignmentColumns([]string{"user_tunnel_id", "created_by_group"}),
			}).Create(&grantItem).Error
			if err != nil {
				return count, err
			}
		}
		count++
	}
	return count, nil
}

func importConfigs(tx *gorm.DB, configs map[string]string, now int64) (int, error) {
	configs = FilterBackupConfigs(configs)
	count := 0
	for name, value := range configs {
		err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "name"}},
			DoUpdates: clause.AssignmentColumns([]string{"value", "time"}),
		}).Create(&model.ViteConfig{Name: name, Value: value, Time: now}).Error
		if err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}
