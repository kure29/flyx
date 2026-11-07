package repo

import (
	"errors"
	"go-backend/internal/store/model"
)

// ─── Semantic Group Queries (replacing QueryInt64List/QueryPairs passthrough) ─

// ListUserIDsByUserGroup returns all user IDs belonging to a user group.
func (r *Repository) ListUserIDsByUserGroup(userGroupID int64) ([]int64, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var ids []int64
	err := r.db.Model(&model.UserGroupUser{}).
		Where("user_group_id = ?", userGroupID).
		Pluck("user_id", &ids).Error
	return ids, err
}

// ListTunnelIDsByTunnelGroup returns all tunnel IDs belonging to a tunnel group.
func (r *Repository) ListTunnelIDsByTunnelGroup(tunnelGroupID int64) ([]int64, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var ids []int64
	err := r.db.Model(&model.TunnelGroupTunnel{}).
		Where("tunnel_group_id = ?", tunnelGroupID).
		Pluck("tunnel_id", &ids).Error
	return ids, err
}

// ListGroupPermissionPairsByUserGroup returns [userGroupID, tunnelGroupID] pairs
// for all group permissions associated with a user group.
func (r *Repository) ListGroupPermissionPairsByUserGroup(userGroupID int64) ([][2]int64, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var perms []model.GroupPermission
	err := r.db.Where("user_group_id = ?", userGroupID).Find(&perms).Error
	if err != nil {
		return nil, err
	}
	result := make([][2]int64, len(perms))
	for i, p := range perms {
		result[i] = [2]int64{p.UserGroupID, p.TunnelGroupID}
	}
	return result, err
}

// ─── Group Queries ───────────────────────────────────────────────────

func (r *Repository) listTunnelGroupMembers(groupID int64) ([]int64, []string, error) {
	type row struct {
		ID   int64
		Name string
	}
	var rows []row
	err := r.db.Model(&model.TunnelGroupTunnel{}).
		Select("tunnel.id, tunnel.name").
		Joins("JOIN tunnel ON tunnel.id = tunnel_group_tunnel.tunnel_id").
		Where("tunnel_group_tunnel.tunnel_group_id = ?", groupID).
		Order("tunnel.id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, nil, err
	}
	ids := make([]int64, 0, len(rows))
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
		names = append(names, r.Name)
	}
	return ids, names, nil
}

func (r *Repository) listUserGroupMembers(groupID int64) ([]int64, []string, error) {
	type row struct {
		ID   int64
		Name string
	}
	var rows []row
	err := r.db.Model(&model.UserGroupUser{}).
		Select(`"user".id, "user"."user" AS name`).
		Joins(`JOIN "user" ON "user".id = user_group_user.user_id`).
		Where("user_group_user.user_group_id = ?", groupID).
		Order(`"user".id ASC`).
		Find(&rows).Error
	if err != nil {
		return nil, nil, err
	}
	ids := make([]int64, 0, len(rows))
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
		names = append(names, r.Name)
	}
	return ids, names, nil
}
