package repo

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"go-backend/internal/store/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Legacy settings (3.0.10) that became the default subscription.
const (
	legacySubTokenKey = "node_sub_token"
	legacySubRename   = "sub_rename_rules"
	legacySubPrefix   = "sub_name_prefix"
	legacySubSort     = "sub_sort"
	legacySubRegions  = "sub_region_groups"
)

func randomToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// subscriptionCredentials returns a fresh UUID and password for a
// subscription's own credentials.
func subscriptionCredentials() (string, string) {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32], randomToken()
}

// newDefaultSubscription is "我的订阅", with its own credentials like every
// subscription.
func newDefaultSubscription(token, options string, now int64) model.Subscription {
	id, pw := subscriptionCredentials()
	return model.Subscription{
		Name: "我的订阅", Kind: model.SubscriptionShare, Token: token, UUID: id, Password: pw, SourceAll: 1,
		Options: options, Enabled: 1, CreatedTime: now, UpdatedTime: now, TrafficSince: now,
	}
}

// shareOwnSubscriptions gives subscriptions that used the owner's
// credentials ("own", before every subscription had its own) their own
// credentials. AppliedActive stays 0, so the sync loop pushes them to the
// kernels; the link (token) is kept.
func shareOwnSubscriptions(db *gorm.DB) error {
	var own []model.Subscription
	if err := db.Where("kind <> ?", model.SubscriptionShare).Find(&own).Error; err != nil {
		return err
	}
	for _, s := range own {
		fields := map[string]interface{}{"kind": model.SubscriptionShare, "applied_active": 0}
		if s.UUID == "" || s.Password == "" {
			fields["uuid"], fields["password"] = subscriptionCredentials()
		}
		if err := db.Model(&model.Subscription{}).Where("id = ?", s.ID).Updates(fields).Error; err != nil {
			return err
		}
	}
	return nil
}

// migrateSubscriptions makes sure there is at least one subscription and
// that every subscription has its own credentials. On upgrade the old
// single subscription (token and naming settings) becomes the default one
// and shared users become subscriptions, keeping their links and
// credentials.
func migrateSubscriptions(db *gorm.DB, now int64) error {
	if err := shareOwnSubscriptions(db); err != nil {
		return err
	}
	if err := createFirstSubscriptions(db, now); err != nil {
		return err
	}
	return groupLegacySubscriptions(db, now)
}

// createFirstSubscriptions turns the pre-3.1 single subscription and its
// shared users into subscriptions when there are none yet.
func createFirstSubscriptions(db *gorm.DB, now int64) error {
	var count int64
	if err := db.Model(&model.Subscription{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		cfg := map[string]string{}
		var rows []model.ViteConfig
		if err := tx.Where("name IN ?", []string{legacySubTokenKey, legacySubRename, legacySubPrefix, legacySubSort, legacySubRegions}).
			Find(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			cfg[r.Name] = r.Value
		}
		opts, _ := json.Marshal(map[string]interface{}{
			"rename":       cfg[legacySubRename],
			"prefix":       cfg[legacySubPrefix],
			"sort":         cfg[legacySubSort],
			"regionGroups": cfg[legacySubRegions] == "true",
		})
		token := cfg[legacySubTokenKey]
		if len(token) < 32 {
			token = randomToken()
		}
		def := newDefaultSubscription(token, string(opts), now)
		if err := tx.Create(&def).Error; err != nil {
			return err
		}

		var users []model.SubUser
		if err := tx.Order("id ASC").Find(&users).Error; err != nil {
			return err
		}
		for i, u := range users {
			sub := model.Subscription{
				Name: u.Name, Kind: model.SubscriptionShare, Token: u.Token, UUID: u.UUID, Password: u.Password,
				SourceAll: 0, InboundIDs: u.InboundIDs, Options: string(opts), ExpireTime: u.ExpireTime,
				Enabled: u.Enabled, AppliedActive: u.AppliedActive, Remark: u.Remark, Inx: i + 1,
				CreatedTime: u.CreatedTime, UpdatedTime: now,
			}
			if err := tx.Create(&sub).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("1 = 1").Delete(&model.SubUser{}).Error; err != nil {
			return err
		}
		return tx.Where("name IN ?", []string{legacySubTokenKey, legacySubRename, legacySubPrefix, legacySubSort, legacySubRegions}).
			Delete(&model.ViteConfig{}).Error
	})
}

func (r *Repository) ListSubscriptions() ([]model.Subscription, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	// Rows from older versions or backups get their node group first.
	if err := r.GroupLegacySubscriptions(); err != nil {
		return nil, err
	}
	var out []model.Subscription
	err := r.db.Order("inx ASC, id ASC").Find(&out).Error
	return out, err
}

// ListUserSubscriptions lists the subscriptions assigned to a panel user.
func (r *Repository) ListUserSubscriptions(userID int64) ([]model.Subscription, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	// Rows from older versions or backups get their node group first.
	if err := r.GroupLegacySubscriptions(); err != nil {
		return nil, err
	}
	var out []model.Subscription
	err := r.db.Where("user_id = ?", userID).Order("inx ASC, id ASC").Find(&out).Error
	return out, err
}

// AssignUserSubscriptions makes ids exactly the subscriptions assigned to
// userID: they move to it (from whoever had them), its others are unassigned.
func (r *Repository) AssignUserSubscriptions(userID int64, ids []int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		clear := tx.Model(&model.Subscription{}).Where("user_id = ?", userID)
		if len(ids) > 0 {
			clear = clear.Where("id NOT IN ?", ids)
		}
		if err := clear.Update("user_id", 0).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		return tx.Model(&model.Subscription{}).Where("id IN ?", ids).Update("user_id", userID).Error
	})
}

// UserNames maps panel user ids to their names.
func (r *Repository) UserNames() (map[int64]string, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	// Separate column names get quoted: user is reserved in PostgreSQL.
	var users []model.User
	if err := r.db.Select("id", "user").Find(&users).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]string, len(users))
	for _, u := range users {
		out[u.ID] = u.User
	}
	return out, nil
}

func (r *Repository) GetSubscription(id int64) (*model.Subscription, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	// Rows from older versions or backups get their node group first.
	if err := r.GroupLegacySubscriptions(); err != nil {
		return nil, err
	}
	var s model.Subscription
	res := r.db.Where("id = ?", id).Limit(1).Find(&s)
	if res.Error != nil || res.RowsAffected == 0 {
		return nil, res.Error
	}
	return &s, nil
}

func (r *Repository) GetSubscriptionByToken(token string) (*model.Subscription, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	// Rows from older versions or backups get their node group first.
	if err := r.GroupLegacySubscriptions(); err != nil {
		return nil, err
	}
	var s model.Subscription
	res := r.db.Where("token = ?", token).Limit(1).Find(&s)
	if res.Error != nil || res.RowsAffected == 0 {
		return nil, res.Error
	}
	return &s, nil
}

// DefaultSubscription is the first subscription (the one the old
// single-subscription API refers to), created if none exists.
func (r *Repository) DefaultSubscription(now int64) (*model.Subscription, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var s model.Subscription
	res := r.db.Order("inx ASC, id ASC").Limit(1).Find(&s)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected > 0 {
		return &s, nil
	}
	s = newDefaultSubscription(randomToken(), "", now)
	if err := r.db.Create(&s).Error; err != nil {
		return nil, err
	}
	// It takes every node: one node group of everything.
	if err := groupLegacySubscriptions(r.db, now); err != nil {
		return nil, err
	}
	return r.GetSubscription(s.ID)
}

func (r *Repository) SaveSubscription(s *model.Subscription) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	// Traffic columns are only changed by the counter updates, so a save
	// based on a stale read cannot drop traffic counted meanwhile.
	if s.ID == 0 {
		return r.db.Create(s).Error
	}
	return r.db.Omit("upload_bytes", "download_bytes", "traffic_reset_time", "traffic_since").Save(s).Error
}

func (r *Repository) DeleteSubscription(id int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("subscription_id = ?", id).Delete(&model.SubscriptionCounter{}).Error; err != nil {
			return err
		}
		if err := tx.Where("subscription_id = ?", id).Delete(&model.SubscriptionTraffic{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&model.Subscription{}).Error
	})
}

func (r *Repository) UpdateSubscriptionApplied(id int64, active int) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Model(&model.Subscription{}).Where("id = ?", id).Update("applied_active", active).Error
}

// SubscriptionCounterSample is one cumulative per-user counter of a node.
type SubscriptionCounterSample struct {
	SubscriptionID int64
	Direction      string // "up" or "down"
	Bytes          int64
}

// ApplySubscriptionCounterSamples adds what each counter grew by since the
// last sample of the same epoch (the whole value after a counter reset) to
// the subscriptions' traffic.
func (r *Repository) ApplySubscriptionCounterSamples(nodeID int64, epoch string, samples []SubscriptionCounterSample, now int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	if len(samples) == 0 {
		return nil
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		var states []model.SubscriptionCounter
		if err := tx.Where("node_id = ?", nodeID).Find(&states).Error; err != nil {
			return err
		}
		type key struct {
			id  int64
			dir string
		}
		prev := make(map[key]model.SubscriptionCounter, len(states))
		for _, st := range states {
			prev[key{st.SubscriptionID, st.Direction}] = st
		}
		up := map[int64]int64{}
		down := map[int64]int64{}
		for _, s := range samples {
			if s.Bytes < 0 || (s.Direction != "up" && s.Direction != "down") {
				continue
			}
			delta := s.Bytes
			old, seen := prev[key{s.SubscriptionID, s.Direction}]
			if seen && old.Epoch == epoch {
				if s.Bytes < old.Bytes {
					continue
				}
				delta = s.Bytes - old.Bytes
			}
			if delta > 0 {
				if s.Direction == "up" {
					up[s.SubscriptionID] += delta
				} else {
					down[s.SubscriptionID] += delta
				}
			}
			row := model.SubscriptionCounter{NodeID: nodeID, SubscriptionID: s.SubscriptionID, Direction: s.Direction,
				Epoch: epoch, Bytes: s.Bytes, UpdatedTime: now}
			prev[key{s.SubscriptionID, s.Direction}] = row
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "node_id"}, {Name: "subscription_id"}, {Name: "direction"}},
				DoUpdates: clause.AssignmentColumns([]string{"epoch", "bytes", "updated_time"}),
			}).Create(&row).Error; err != nil {
				return err
			}
		}
		ids := map[int64]bool{}
		for id := range up {
			ids[id] = true
		}
		for id := range down {
			ids[id] = true
		}
		hour := now - now%int64(15*time.Minute/time.Millisecond)
		var hourly []model.SubscriptionTraffic
		for id := range ids {
			if err := tx.Model(&model.Subscription{}).Where("id = ?", id).Updates(map[string]interface{}{
				"upload_bytes":   gorm.Expr("upload_bytes + ?", up[id]),
				"download_bytes": gorm.Expr("download_bytes + ?", down[id]),
			}).Error; err != nil {
				return err
			}
			hourly = append(hourly, model.SubscriptionTraffic{SubscriptionID: id, Hour: hour, BucketMinutes: 15, UploadBytes: up[id], DownloadBytes: down[id]})
		}
		if len(hourly) == 0 {
			return nil
		}
		// Quarter-hour history aligns with half-hour and quarter-hour time zones.
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "subscription_id"}, {Name: "hour"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				// The first new bucket can overlap an existing legacy hour.
				"bucket_minutes": gorm.Expr("CASE WHEN subscription_traffic.bucket_minutes > excluded.bucket_minutes THEN subscription_traffic.bucket_minutes ELSE excluded.bucket_minutes END"),
				"upload_bytes":   gorm.Expr("subscription_traffic.upload_bytes + excluded.upload_bytes"),
				"download_bytes": gorm.Expr("subscription_traffic.download_bytes + excluded.download_bytes"),
			}),
		}).Create(&hourly).Error
	})
}

// ListSubscriptionTraffic returns a subscription's aggregates from fromMs on,
// oldest first.
func (r *Repository) ListSubscriptionTraffic(id, fromMs int64) ([]model.SubscriptionTraffic, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("repository not initialized")
	}
	var out []model.SubscriptionTraffic
	err := r.db.Where("subscription_id = ? AND hour >= ?", id, fromMs).Order("hour ASC").Find(&out).Error
	return out, err
}

// PurgeSubscriptionTraffic drops the hours before beforeMs.
func (r *Repository) PurgeSubscriptionTraffic(beforeMs int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Where("hour < ?", beforeMs).Delete(&model.SubscriptionTraffic{}).Error
}

// ResetSubscriptionTraffic zeroes a subscription's traffic.
func (r *Repository) ResetSubscriptionTraffic(id int64, now int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Model(&model.Subscription{}).Where("id = ?", id).Updates(map[string]interface{}{
		"upload_bytes": 0, "download_bytes": 0, "traffic_reset_time": now, "traffic_since": now,
	}).Error
}

// MarkSubscriptionResetTime records when the traffic was last reset.
func (r *Repository) MarkSubscriptionResetTime(id int64, at int64) error {
	if r == nil || r.db == nil {
		return errors.New("repository not initialized")
	}
	return r.db.Model(&model.Subscription{}).Where("id = ?", id).Update("traffic_reset_time", at).Error
}
