package repo

import (
	"go-backend/internal/store/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *Repository) exportInbounds() (*model.InboundsBackup, error) {
	out := &model.InboundsBackup{
		Kernels:  []model.KernelBackup{},
		Inbounds: []model.InboundBackup{},
		Relays:   []model.InboundRelayBackup{},
	}

	var kernels []model.NodeKernel
	if err := r.db.Order("id ASC").Find(&kernels).Error; err != nil {
		return nil, err
	}
	for _, k := range kernels {
		out.Kernels = append(out.Kernels, model.KernelBackup{
			ID: k.ID, NodeID: k.NodeID, KernelType: k.KernelType, Version: k.Version,
			Enabled: k.Enabled, CreatedTime: k.CreatedTime,
		})
	}

	var inbounds []model.NodeInbound
	if err := r.db.Order("id ASC").Find(&inbounds).Error; err != nil {
		return nil, err
	}
	for _, i := range inbounds {
		out.Inbounds = append(out.Inbounds, model.InboundBackup{
			ID: i.ID, KernelID: i.KernelID, NodeID: i.NodeID, Name: i.Name, Protocol: i.Protocol,
			Listen: i.Listen, Port: i.Port, Transport: i.Transport, Security: i.Security,
			Settings: i.Settings, Extra: i.Extra, ShareHost: i.ShareHost, ShareAddressMode: i.ShareAddressMode, Remark: i.Remark,
			RelayOnly: i.RelayOnly, AcceptProxyProtocol: i.AcceptProxyProtocol,
			RelayWhitelist: i.RelayWhitelist, WhitelistExtra: i.WhitelistExtra, Enabled: i.Enabled,
			UploadBytes: i.UploadBytes, DownloadBytes: i.DownloadBytes, Inx: i.Inx,
			CreatedTime: i.CreatedTime, SourceID: i.SourceID, ImportedName: i.ImportedName, OutboundID: i.OutboundID, SplitRules: i.SplitRules, OutboundIPStrategy: i.OutboundIPStrategy,
		})
	}

	var relays []model.NodeInboundRelay
	if err := r.db.Order("id ASC").Find(&relays).Error; err != nil {
		return nil, err
	}
	for _, rel := range relays {
		out.Relays = append(out.Relays, model.InboundRelayBackup{
			ID: rel.ID, InboundID: rel.InboundID, ForwardID: rel.ForwardID, Name: rel.Name,
			ShareHost: rel.ShareHost, Managed: rel.Managed, CreatedTime: rel.CreatedTime,
		})
	}

	var counters []model.NodeInboundCounter
	if err := r.db.Order("id ASC").Find(&counters).Error; err != nil {
		return nil, err
	}
	for _, c := range counters {
		out.Counters = append(out.Counters, model.InboundCounterBackup{
			InboundID: c.InboundID, Protocol: c.Protocol, Direction: c.Direction,
			Epoch: c.Epoch, Bytes: c.Bytes,
		})
	}
	var sources []model.RemoteSource
	if err := r.db.Order("id ASC").Find(&sources).Error; err != nil {
		return nil, err
	}
	for _, src := range sources {
		out.Sources = append(out.Sources, model.RemoteSourceBackup{
			ID: src.ID, Name: src.Name, URL: src.URL, IntervalHours: src.IntervalHours,
			RelayOnly: src.RelayOnly, Kind: src.Kind, Role: src.Role, UserAgent: src.UserAgent, Enabled: src.Enabled, CreatedTime: src.CreatedTime,
		})
	}
	var templates []model.SubscriptionTemplate
	if err := r.db.Order("id ASC").Find(&templates).Error; err != nil {
		return nil, err
	}
	for _, t := range templates {
		out.Templates = append(out.Templates, model.TemplateBackup{
			ID: t.ID, Name: t.Name, Kind: t.Kind, Content: t.Content, SourceURL: t.SourceURL, BuilderConfig: t.BuilderConfig, CreatedTime: t.CreatedTime,
		})
	}
	var groups []model.NodeGroup
	if err := r.db.Order("id ASC").Find(&groups).Error; err != nil {
		return nil, err
	}
	for _, g := range groups {
		out.NodeGroups = append(out.NodeGroups, model.NodeGroupBackup{
			ID: g.ID, Name: g.Name, SourceAll: g.SourceAll, InboundIDs: g.InboundIDs, SourceIDs: g.SourceIDs,
			Options: g.Options, Inx: g.Inx, CreatedTime: g.CreatedTime,
		})
	}
	var subs []model.Subscription
	if err := r.db.Order("id ASC").Find(&subs).Error; err != nil {
		return nil, err
	}
	for _, sub := range subs {
		out.Subscriptions = append(out.Subscriptions, model.SubscriptionBackup{
			ID: sub.ID, Name: sub.Name, Kind: sub.Kind, Token: sub.Token, UUID: sub.UUID, Password: sub.Password,
			SourceAll: sub.SourceAll, InboundIDs: sub.InboundIDs, SourceIDs: sub.SourceIDs, GroupIDs: sub.GroupIDs, Options: sub.Options, ExpireTime: sub.ExpireTime,
			Enabled: sub.Enabled, Remark: sub.Remark, Inx: sub.Inx, CreatedTime: sub.CreatedTime,
			UploadBytes: sub.UploadBytes, DownloadBytes: sub.DownloadBytes, TrafficLimit: sub.TrafficLimit,
			ResetDay: sub.ResetDay, TrafficResetTime: sub.TrafficResetTime, TrafficSince: sub.TrafficSince, UserID: sub.UserID,
		})
	}
	return out, nil
}

// importInbounds upserts kernels, inbounds and relay links by id. Kernels
// come back as pending with no applied config, so the panel pushes them to
// the nodes again (and installs the kernel where it is missing).
func importInbounds(tx *gorm.DB, b *model.InboundsBackup, now int64) (int, error) {
	for _, k := range b.Kernels {
		// One kernel per node and type: drop a different row in the way.
		if err := tx.Where("node_id = ? AND kernel_type = ? AND id <> ?", k.NodeID, k.KernelType, k.ID).
			Delete(&model.NodeKernel{}).Error; err != nil {
			return 0, err
		}
		version := k.Version
		if version == "" {
			version = "latest"
		}
		item := model.NodeKernel{
			ID: k.ID, NodeID: k.NodeID, KernelType: k.KernelType, Version: version,
			Status: "pending", Enabled: k.Enabled, CreatedTime: k.CreatedTime, UpdatedTime: now,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"node_id", "kernel_type", "version", "installed_version", "status", "last_error",
				"config_hash", "applied_hash", "enabled", "updated_time",
			}),
		}).Create(&item).Error; err != nil {
			return 0, err
		}
	}

	// Sources first: imported nodes point at them. A refresh fills in the
	// status fields.
	for _, src := range b.Sources {
		kind := src.Kind
		if kind != model.RemoteSourceFile {
			kind = model.RemoteSourceURL // backups from before file sources
		}
		item := model.RemoteSource{
			ID: src.ID, Name: src.Name, URL: src.URL, IntervalHours: src.IntervalHours,
			RelayOnly: src.RelayOnly, Kind: kind, Role: src.Role, UserAgent: src.UserAgent, Enabled: src.Enabled, CreatedTime: src.CreatedTime, UpdatedTime: now,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{"name", "url", "interval_hours", "relay_only", "kind", "role", "user_agent", "enabled", "updated_time"}),
		}).Create(&item).Error; err != nil {
			return 0, err
		}
	}

	// Templates keep their ids: subscription options refer to them.
	for _, t := range b.Templates {
		item := model.SubscriptionTemplate{
			ID: t.ID, Name: t.Name, Kind: t.Kind, Content: t.Content, SourceURL: t.SourceURL, BuilderConfig: t.BuilderConfig,
			CreatedTime: t.CreatedTime, UpdatedTime: now,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{"name", "kind", "content", "source_url", "builder_config", "updated_time"}),
		}).Create(&item).Error; err != nil {
			return 0, err
		}
	}

	for _, i := range b.Inbounds {
		listen := i.Listen
		if listen == "" {
			listen = "::"
		}
		// Backups made before the per-inbound policy have none; an unknown
		// value would fail rendering the whole kernel, so it becomes auto.
		strategy := i.OutboundIPStrategy
		switch strategy {
		case "auto", "ipv4_only", "ipv6_only", "prefer_ipv4", "prefer_ipv6":
		default:
			strategy = "auto"
		}
		item := model.NodeInbound{
			ID: i.ID, KernelID: i.KernelID, NodeID: i.NodeID, Name: i.Name, Protocol: i.Protocol,
			Listen: listen, Port: i.Port, Transport: i.Transport, Security: i.Security,
			Settings: i.Settings, Extra: i.Extra, ShareHost: i.ShareHost, ShareAddressMode: i.ShareAddressMode, Remark: i.Remark,
			RelayOnly: i.RelayOnly, AcceptProxyProtocol: i.AcceptProxyProtocol,
			RelayWhitelist: i.RelayWhitelist, WhitelistExtra: i.WhitelistExtra, Enabled: i.Enabled,
			UploadBytes: i.UploadBytes, DownloadBytes: i.DownloadBytes, Inx: i.Inx,
			CreatedTime: i.CreatedTime, UpdatedTime: now, SourceID: i.SourceID, ImportedName: i.ImportedName, OutboundID: i.OutboundID,
			SplitRules: i.SplitRules, OutboundIPStrategy: strategy,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"kernel_id", "node_id", "name", "protocol", "listen", "port", "transport", "security",
				"settings", "extra", "share_host", "share_address_mode", "remark", "relay_only", "accept_proxy_protocol",
				"relay_whitelist", "whitelist_extra", "enabled", "upload_bytes", "download_bytes",
				"inx", "updated_time", "source_id", "imported_name", "outbound_id", "split_rules", "outbound_ip_strategy",
			}),
		}).Create(&item).Error; err != nil {
			return 0, err
		}
	}

	for _, rel := range b.Relays {
		if err := tx.Where("forward_id = ? AND id <> ?", rel.ForwardID, rel.ID).
			Delete(&model.NodeInboundRelay{}).Error; err != nil {
			return 0, err
		}
		item := model.NodeInboundRelay{
			ID: rel.ID, InboundID: rel.InboundID, ForwardID: rel.ForwardID, Name: rel.Name,
			ShareHost: rel.ShareHost, Managed: rel.Managed, CreatedTime: rel.CreatedTime,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{"inbound_id", "forward_id", "name", "share_host", "managed"}),
		}).Create(&item).Error; err != nil {
			return 0, err
		}
	}

	// Counter baselines keep the next traffic sample from being counted
	// twice when the restored panel talks to the same nodes.
	for _, c := range b.Counters {
		item := model.NodeInboundCounter{
			InboundID: c.InboundID, Protocol: c.Protocol, Direction: c.Direction,
			Epoch: c.Epoch, Bytes: c.Bytes, UpdatedTime: now,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "inbound_id"}, {Name: "protocol"}, {Name: "direction"}},
			DoUpdates: clause.AssignmentColumns([]string{"epoch", "bytes", "updated_time"}),
		}).Create(&item).Error; err != nil {
			return 0, err
		}
	}
	// Node groups keep their ids, like inbounds and templates: subscriptions
	// refer to them.
	for _, g := range b.NodeGroups {
		item := model.NodeGroup{
			ID: g.ID, Name: g.Name, SourceAll: g.SourceAll, InboundIDs: g.InboundIDs, SourceIDs: g.SourceIDs,
			Options: g.Options, Inx: g.Inx, CreatedTime: g.CreatedTime, UpdatedTime: now,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{"name", "source_all", "inbound_ids", "source_ids", "options", "inx", "updated_time"}),
		}).Create(&item).Error; err != nil {
			return 0, err
		}
	}
	// Subscriptions keep their links and credentials, matched by token.
	// AppliedActive starts at 0 so the expiry loop pushes the active share
	// subscriptions to the kernels.
	subs := append([]model.SubscriptionBackup(nil), b.Subscriptions...)
	for _, u := range b.Users {
		subs = append(subs, model.SubscriptionBackup{
			Name: u.Name, Kind: model.SubscriptionShare, Token: u.Token, UUID: u.UUID, Password: u.Password,
			InboundIDs: u.InboundIDs, ExpireTime: u.ExpireTime, Enabled: u.Enabled, Remark: u.Remark,
			CreatedTime: u.CreatedTime,
		})
	}
	for _, sb := range subs {
		if sb.Token == "" {
			continue
		}
		var existing model.Subscription
		res := tx.Where("token = ?", sb.Token).Limit(1).Find(&existing)
		if res.Error != nil {
			return 0, res.Error
		}
		item := model.Subscription{
			Name: sb.Name, Kind: sb.Kind, Token: sb.Token, UUID: sb.UUID, Password: sb.Password,
			SourceAll: sb.SourceAll, InboundIDs: sb.InboundIDs, SourceIDs: sb.SourceIDs, GroupIDs: sb.GroupIDs, Options: sb.Options, ExpireTime: sb.ExpireTime,
			Enabled: sb.Enabled, Remark: sb.Remark, Inx: sb.Inx, CreatedTime: sb.CreatedTime, UpdatedTime: now,
			UploadBytes: sb.UploadBytes, DownloadBytes: sb.DownloadBytes, TrafficLimit: sb.TrafficLimit,
			ResetDay: sb.ResetDay, TrafficResetTime: sb.TrafficResetTime, TrafficSince: sb.TrafficSince, UserID: sb.UserID,
		}
		// Every subscription has its own credentials (older backups may
		// carry owner-credential ones).
		item.Kind = model.SubscriptionShare
		if item.UUID == "" || item.Password == "" {
			item.UUID, item.Password = subscriptionCredentials()
		}
		if res.RowsAffected > 0 {
			item.ID = existing.ID
		}
		if err := tx.Save(&item).Error; err != nil {
			return 0, err
		}
	}
	return len(b.Inbounds), nil
}
