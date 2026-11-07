package repo

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"go-backend/internal/store/model"

	"gorm.io/gorm"
)

// NodeGroupOptionKeys are the keys of a subscription's options that
// process its nodes; on upgrade they move into the subscription's own node
// group. The rest (chain, rules, templates) stays with the subscription.
var NodeGroupOptionKeys = []string{
	"lines", "regions", "protocols", "include", "exclude", "rename",
	"addFlag", "prefix", "dedupe", "sort", "script",
}

func (r *Repository) ListNodeGroups() ([]model.NodeGroup, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var out []model.NodeGroup
	err := r.db.Order("inx ASC, id ASC").Find(&out).Error
	return out, err
}

func (r *Repository) GetNodeGroup(id int64) (*model.NodeGroup, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var g model.NodeGroup
	res := r.db.Where("id = ?", id).Limit(1).Find(&g)
	if res.Error != nil || res.RowsAffected == 0 {
		return nil, res.Error
	}
	return &g, nil
}

func (r *Repository) SaveNodeGroup(g *model.NodeGroup) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Save(g).Error
}

func (r *Repository) DeleteNodeGroup(id int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Where("id = ?", id).Delete(&model.NodeGroup{}).Error
}

// UpdateNodeGroupOrder sets the groups' display order.
func (r *Repository) UpdateNodeGroupOrder(ids []int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		for i, id := range ids {
			if err := tx.Model(&model.NodeGroup{}).Where("id = ?", id).Update("inx", i).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// ParseGroupIDs reads a comma separated id list.
func ParseGroupIDs(s string) []int64 {
	var out []int64
	for _, part := range strings.Split(s, ",") {
		if id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64); err == nil && id > 0 {
			out = append(out, id)
		}
	}
	return out
}

// FormatGroupIDs writes an id list as stored.
func FormatGroupIDs(ids []int64) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.FormatInt(id, 10))
	}
	return strings.Join(parts, ",")
}

// GroupLegacySubscriptions gives subscriptions without node groups (an
// older backup restored, rows written by hand) a group of their own.
func (r *Repository) GroupLegacySubscriptions() error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	var n int64
	if err := r.db.Model(&model.Subscription{}).Where("group_ids = ''").Count(&n).Error; err != nil || n == 0 {
		return err
	}
	return groupLegacySubscriptions(r.db, unixMilliNow())
}

// groupLegacySubscriptions gives every subscription without node groups
// (from before they existed, or restored from an older backup) a group of
// its own: its sources, the remote sources its chain used (their nodes were
// part of it), and its node processing. The subscription's own columns are
// left as they were, for a downgrade.
func groupLegacySubscriptions(db *gorm.DB, now int64) error {
	var subs []model.Subscription
	if err := db.Where("group_ids = ''").Order("inx ASC, id ASC").Find(&subs).Error; err != nil {
		return err
	}
	if len(subs) == 0 {
		return nil
	}
	var inx int64
	if err := db.Model(&model.NodeGroup{}).Select("COALESCE(MAX(inx), 0)").Scan(&inx).Error; err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, s := range subs {
			raw := map[string]json.RawMessage{}
			_ = json.Unmarshal([]byte(s.Options), &raw)
			nodeOpts := map[string]json.RawMessage{}
			for _, k := range NodeGroupOptionKeys {
				if v, ok := raw[k]; ok {
					nodeOpts[k] = v
				}
			}
			opts, _ := json.Marshal(nodeOpts)

			sources := ParseGroupIDs(s.SourceIDs)
			seen := map[int64]bool{}
			for _, id := range sources {
				seen[id] = true
			}
			for _, key := range []string{"chainLandingSources", "chainRelaySources"} {
				var ids []int64
				_ = json.Unmarshal(raw[key], &ids)
				for _, id := range ids {
					if id > 0 && !seen[id] {
						seen[id] = true
						sources = append(sources, id)
					}
				}
			}

			inx++
			g := model.NodeGroup{
				Name: s.Name, SourceAll: s.SourceAll, InboundIDs: s.InboundIDs, SourceIDs: FormatGroupIDs(sources),
				Options: string(opts), Inx: int(inx), CreatedTime: now, UpdatedTime: now,
			}
			if err := tx.Create(&g).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.Subscription{}).Where("id = ?", s.ID).
				Update("group_ids", strconv.FormatInt(g.ID, 10)).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
