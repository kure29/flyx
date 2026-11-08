package repo

import (
	"errors"

	"gorm.io/gorm"

	"go-backend/internal/store/model"
)

// ─── Node Queries ────────────────────────────────────────────────────

func (r *Repository) NodeExistsBySecret(secret string) (bool, error) {
	if r == nil || r.db == nil {
		return false, errors.New("repository not initialized")
	}
	var count int64
	err := r.db.Model(&model.Node{}).Where("secret = ?", secret).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *Repository) GetNodeBySecret(secret string) (*model.Node, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var n model.Node
	err := r.db.Where("secret = ?", secret).First(&n).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (r *Repository) GetNodeByID(id int64) (*model.Node, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var n model.Node
	err := r.db.Where("id = ?", id).First(&n).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (r *Repository) UpdateNodeOnline(nodeID int64, status int, version string, httpVal, tlsVal, socksVal int) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Model(&model.Node{}).Where("id = ?", nodeID).Updates(map[string]interface{}{
		"status": status, "version": version, "http": httpVal, "tls": tlsVal,
		"socks": socksVal, "updated_time": unixMilliNow(),
	}).Error
}

func (r *Repository) UpdateNodeStatus(nodeID int64, status int) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Model(&model.Node{}).Where("id = ?", nodeID).Updates(map[string]interface{}{
		"status": status, "updated_time": unixMilliNow(),
	}).Error
}

// ─── List Methods (return map[string]interface{}) ────────────────────

func (r *Repository) ListNodes() ([]map[string]interface{}, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var nodes []model.Node
	if err := r.db.Order("inx ASC, id ASC").Find(&nodes).Error; err != nil {
		return nil, err
	}
	nodeIDs := make([]int64, 0, len(nodes))
	for _, n := range nodes {
		if defaultNodeForwardMode(n.ForwardMode) == "nftables" {
			nodeIDs = append(nodeIDs, n.ID)
		}
	}
	sshConfigByNodeID := make(map[int64]map[string]interface{}, len(nodeIDs))
	if len(nodeIDs) > 0 {
		var configs []model.NodeSSHConfig
		if err := r.db.Where("node_id IN ?", nodeIDs).Find(&configs).Error; err != nil {
			return nil, err
		}
		for _, cfg := range configs {
			sshConfigByNodeID[cfg.NodeID] = map[string]interface{}{
				"host":     cfg.Host,
				"port":     cfg.Port,
				"username": cfg.Username,
				"authType": cfg.AuthType,
				"sudoMode": cfg.SudoMode,
			}
		}
	}
	items := make([]map[string]interface{}, 0, len(nodes))
	for _, n := range nodes {
		items = append(items, map[string]interface{}{
			"id": n.ID, "inx": n.Inx, "name": n.Name,
			"remark":       nullableString(n.Remark),
			"expiryTime":   nullableInt64(n.ExpiryTime),
			"renewalCycle": nullableString(n.RenewalCycle),
			"ip":           n.ServerIP, "serverIp": n.ServerIP,
			"serverIpV4":    nullableString(n.ServerIPV4),
			"serverIpV6":    nullableString(n.ServerIPV6),
			"extraIPs":      nullableString(n.ExtraIPs),
			"port":          n.Port,
			"tcpListenAddr": n.TCPListenAddr,
			"udpListenAddr": n.UDPListenAddr,
			"version":       nullableString(n.Version),
			"http":          n.HTTP, "tls": n.TLS, "socks": n.Socks,
			"status": n.Status, "isRemote": n.IsRemote,
			"forwardMode":             defaultNodeForwardMode(n.ForwardMode),
			"remoteUrl":               nullableString(n.RemoteURL),
			"remoteToken":             nullableString(n.RemoteToken),
			"remoteConfig":            nullableString(n.RemoteConfig),
			"expiryReminderDismissed": n.ExpiryReminderDismissed,
			"interfaceName":           nullableString(n.InterfaceName),
			"sshConfig":               sshConfigByNodeID[n.ID],
			"latencyTargets":          n.LatencyTargets,
			"notifyEvents":            n.NotifyEvents,
			"nftError":                n.NftError,
			"hostOs":                  n.HostOS,
			"hostKernel":              n.HostKernel,
			"hostArch":                n.HostArch,
			"hostCpuName":             n.HostCPUName,
			"hostVirtualization":      n.HostVirtualization,
			"region":                  n.Region,
			"regionDetected":          n.RegionDetected,
			"group":                   n.GroupName,
			"publicNote":              n.PublicNote,
			"price":                   n.Price,
			"currency":                n.Currency,
			"trafficQuotaGb":          n.TrafficQuotaGB,
			"trafficCalc":             n.TrafficCalc,
			"trafficResetDay":         n.TrafficResetDay,
		})
	}
	return items, nil
}
