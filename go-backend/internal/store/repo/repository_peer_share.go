package repo

import (
	"errors"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go-backend/internal/store/model"
)

// ─── PeerShare CRUD ──────────────────────────────────────────────────

func (r *Repository) CreatePeerShare(share *model.PeerShare) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Create(share).Error
}

func (r *Repository) UpdatePeerShare(share *model.PeerShare) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Model(&model.PeerShare{}).Where("id = ?", share.ID).Updates(map[string]interface{}{
		"name": share.Name, "max_bandwidth": share.MaxBandwidth,
		"expiry_time": share.ExpiryTime, "port_range_start": share.PortRangeStart,
		"port_range_end": share.PortRangeEnd, "is_active": share.IsActive,
		"updated_time": share.UpdatedTime, "allowed_domains": share.AllowedDomains,
		"allowed_ips": share.AllowedIPs,
	}).Error
}

func (r *Repository) DeletePeerShare(id int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("share_id = ?", id).Delete(&model.PeerShareRuntime{}).Error; err != nil {
			return err
		}
		if err := tx.Where("share_id = ?", id).Delete(&model.PeerShareResource{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&model.PeerShare{}).Error
	})
}

func (r *Repository) GetPeerShare(id int64) (*model.PeerShare, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var s model.PeerShare
	err := r.db.Where("id = ?", id).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *Repository) GetPeerShareByToken(token string) (*model.PeerShare, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var s model.PeerShare
	err := r.db.Where("token = ?", token).First(&s).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *Repository) ListPeerShares() ([]model.PeerShare, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var shares []model.PeerShare
	err := r.db.Order("id DESC").Find(&shares).Error
	return shares, err
}

func (r *Repository) AddPeerShareCurrentFlow(shareID int64, delta int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	if shareID <= 0 || delta <= 0 {
		return nil
	}
	return r.db.Model(&model.PeerShare{}).Where("id = ?", shareID).
		UpdateColumns(map[string]interface{}{
			"current_flow": gorm.Expr("current_flow + ?", delta),
			"updated_time": unixMilliNow(),
		}).Error
}

func (r *Repository) ResetPeerShareCurrentFlow(shareID int64, updatedTime int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	if shareID <= 0 {
		return nil
	}
	if updatedTime <= 0 {
		updatedTime = unixMilliNow()
	}
	return r.db.Model(&model.PeerShare{}).Where("id = ?", shareID).Updates(map[string]interface{}{
		"current_flow": 0, "updated_time": updatedTime,
	}).Error
}

// ─── PeerShareRuntime CRUD ───────────────────────────────────────────

func (r *Repository) CreatePeerShareRuntime(item *model.PeerShareRuntime) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	if item == nil {
		return errors.New("runtime item is nil")
	}
	return r.db.Create(item).Error
}

func (r *Repository) UpdatePeerShareRuntime(item *model.PeerShareRuntime) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	if item == nil {
		return errors.New("runtime item is nil")
	}
	return r.db.Model(&model.PeerShareRuntime{}).Where("id = ?", item.ID).Updates(map[string]interface{}{
		"reservation_id": item.ReservationID,
		"binding_id":     item.BindingID, "role": item.Role,
		"chain_name": item.ChainName, "service_name": item.ServiceName,
		"protocol": item.Protocol, "strategy": item.Strategy,
		"port": item.Port, "target": item.Target,
		"applied": item.Applied, "status": item.Status,
		"updated_time": item.UpdatedTime,
	}).Error
}

func (r *Repository) MarkPeerShareRuntimeReleased(id int64, updatedTime int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Model(&model.PeerShareRuntime{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status": 0, "updated_time": updatedTime,
	}).Error
}

func (r *Repository) GetPeerShareRuntimeByResourceKey(shareID int64, resourceKey string) (*model.PeerShareRuntime, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var item model.PeerShareRuntime
	err := r.db.Where("share_id = ? AND resource_key = ?", shareID, resourceKey).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *Repository) GetPeerShareRuntimeByReservationID(shareID int64, reservationID string) (*model.PeerShareRuntime, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var item model.PeerShareRuntime
	err := r.db.Where("share_id = ? AND reservation_id = ?", shareID, reservationID).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *Repository) GetPeerShareRuntimeByBindingID(shareID int64, bindingID string) (*model.PeerShareRuntime, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var item model.PeerShareRuntime
	err := r.db.Where("share_id = ? AND binding_id = ?", shareID, bindingID).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *Repository) GetPeerShareRuntimeByID(id int64) (*model.PeerShareRuntime, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var item model.PeerShareRuntime
	err := r.db.Where("id = ?", id).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *Repository) ListActivePeerShareRuntimesByShareID(shareID int64) ([]model.PeerShareRuntime, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var out []model.PeerShareRuntime
	err := r.db.Where("share_id = ? AND status = 1", shareID).Order("port ASC, id ASC").Find(&out).Error
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = make([]model.PeerShareRuntime, 0)
	}
	return out, nil
}

func (r *Repository) ListActivePeerShareRuntimePorts(shareID int64, nodeID int64) ([]int, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var ports []int
	err := r.db.Model(&model.PeerShareRuntime{}).
		Where("share_id = ? AND node_id = ? AND status = 1 AND port > 0", shareID, nodeID).
		Pluck("port", &ports).Error
	if err != nil {
		return nil, err
	}
	if ports == nil {
		ports = make([]int, 0)
	}
	return ports, nil
}

func (r *Repository) ListActiveForwardPeerShareRuntimesByServiceName(serviceName string) ([]model.PeerShareRuntime, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var items []model.PeerShareRuntime
	err := r.db.Where("service_name = ? AND status = 1 AND role = ?", serviceName, "forward").
		Order("id ASC").
		Find(&items).Error
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = make([]model.PeerShareRuntime, 0)
	}
	return items, nil
}

func (r *Repository) ListActiveForwardPeerShareRuntimesByNodeAndServiceName(nodeID int64, serviceName string) ([]model.PeerShareRuntime, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return []model.PeerShareRuntime{}, nil
	}
	var items []model.PeerShareRuntime
	err := r.db.Where("node_id = ? AND service_name = ? AND status = 1 AND role = ?", nodeID, serviceName, "forward").
		Order("id ASC").
		Find(&items).Error
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = make([]model.PeerShareRuntime, 0)
	}
	return items, nil
}

func (r *Repository) ListActiveForwardPeerShareRuntimesByNode(nodeID int64) ([]model.PeerShareRuntime, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var items []model.PeerShareRuntime
	err := r.db.Where("node_id = ? AND status = 1 AND role = ?", nodeID, "forward").Find(&items).Error
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = make([]model.PeerShareRuntime, 0)
	}
	return items, nil
}

func (r *Repository) GetActiveForwardPeerShareRuntimeByPort(shareID int64, port int) (*model.PeerShareRuntime, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var item model.PeerShareRuntime
	err := r.db.Where("share_id = ? AND port = ? AND status = 1 AND role = ?", shareID, port, "forward").First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *Repository) GetActiveForwardPeerShareRuntimeByServiceName(shareID int64, serviceName string) (*model.PeerShareRuntime, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	serviceName = strings.TrimSpace(serviceName)
	if shareID <= 0 || serviceName == "" {
		return nil, nil
	}
	var item model.PeerShareRuntime
	err := r.db.Where("share_id = ? AND service_name = ? AND status = 1 AND role = ?", shareID, serviceName, "forward").
		Order("id ASC").
		First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *Repository) ExistsActivePeerShareRuntimeOnNodePort(nodeID int64, port int) (bool, error) {
	if r == nil || r.db == nil {
		return false, errors.New("repository not initialized")
	}
	var count int64
	err := r.db.Model(&model.PeerShareRuntime{}).
		Where("node_id = ? AND port = ? AND status = 1", nodeID, port).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *Repository) UpdatePeerShareRuntimeServiceName(id int64, serviceName string, updatedTime int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Model(&model.PeerShareRuntime{}).Where("id = ?", id).Updates(map[string]interface{}{
		"service_name": serviceName,
		"applied":      1,
		"updated_time": updatedTime,
	}).Error
}

func (r *Repository) MarkPeerShareRuntimeReleasedByPort(shareID int64, port int, updatedTime int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	if shareID <= 0 || port <= 0 {
		return nil
	}
	if updatedTime <= 0 {
		updatedTime = unixMilliNow()
	}
	return r.db.Model(&model.PeerShareRuntime{}).Where("share_id = ? AND port = ? AND status = 1", shareID, port).Updates(map[string]interface{}{
		"status":       0,
		"applied":      0,
		"service_name": "",
		"updated_time": updatedTime,
	}).Error
}

func (r *Repository) MarkForwardPeerShareRuntimeReleasedByServiceName(shareID int64, serviceName string, updatedTime int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	serviceName = strings.TrimSpace(serviceName)
	if shareID <= 0 || serviceName == "" {
		return nil
	}
	if updatedTime <= 0 {
		updatedTime = unixMilliNow()
	}
	return r.db.Model(&model.PeerShareRuntime{}).
		Where("share_id = ? AND status = 1 AND role = ? AND service_name = ?", shareID, "forward", serviceName).
		Updates(map[string]interface{}{
			"status":       0,
			"applied":      0,
			"service_name": "",
			"updated_time": updatedTime,
		}).Error
}

// ─── FederationTunnelBinding ─────────────────────────────────────────

func (r *Repository) UpsertFederationTunnelBinding(item *model.FederationTunnelBinding) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	if item == nil {
		return errors.New("binding item is nil")
	}
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "tunnel_id"}, {Name: "node_id"}, {Name: "chain_type"}, {Name: "hop_inx"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"remote_url", "resource_key", "remote_binding_id",
			"allocated_port", "status", "updated_time",
		}),
	}).Create(item).Error
}

func (r *Repository) ListActiveFederationTunnelBindingsByTunnel(tunnelID int64) ([]model.FederationTunnelBinding, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var out []model.FederationTunnelBinding
	err := r.db.Where("tunnel_id = ? AND status = 1", tunnelID).
		Order("chain_type ASC, hop_inx ASC, id ASC").Find(&out).Error
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = make([]model.FederationTunnelBinding, 0)
	}
	return out, nil
}

func (r *Repository) DeleteFederationTunnelBindingsByTunnel(tunnelID int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Where("tunnel_id = ?", tunnelID).Delete(&model.FederationTunnelBinding{}).Error
}
