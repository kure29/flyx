package repo

import (
	"path/filepath"
	"testing"
	"time"

	"go-backend/internal/store/model"
)

func TestSubscriptionTrafficBucketUpgrade(t *testing.T) {
	r, err := Open(filepath.Join(t.TempDir(), "upgrade.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	assertSubscriptionTrafficBucketUpgrade(t, r)
}

func assertSubscriptionTrafficBucketUpgrade(t *testing.T, r *Repository) {
	t.Helper()
	sub := model.Subscription{Name: "timezone", Token: "timezone", UploadBytes: 100, DownloadBytes: 50}
	if err := r.db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	// Reconstruct the old hourly schema and migrate it with existing data.
	if err := r.db.Migrator().DropColumn(&model.SubscriptionTraffic{}, "BucketMinutes"); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 4, 1, 18, 0, 0, 0, time.UTC)
	if err := r.db.Table("subscription_traffic").Create(map[string]interface{}{
		"subscription_id": sub.ID, "hour": start.UnixMilli(), "upload_bytes": 100, "download_bytes": 50,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.db.AutoMigrate(&model.SubscriptionTraffic{}); err != nil {
		t.Fatal(err)
	}
	rows, err := r.ListSubscriptionTraffic(sub.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].BucketMinutes != 60 || rows[0].UploadBytes != 100 || rows[0].DownloadBytes != 50 {
		t.Fatalf("migration changed legacy history: %+v", rows)
	}
	for _, sample := range []struct {
		minute   int
		up, down int64
	}{{5, 20, 10}, {20, 50, 25}, {20, 50, 25}} {
		if err := r.ApplySubscriptionCounterSamples(1, "epoch", []SubscriptionCounterSample{
			{SubscriptionID: sub.ID, Direction: "up", Bytes: sample.up},
			{SubscriptionID: sub.ID, Direction: "down", Bytes: sample.down},
		}, start.Add(time.Duration(sample.minute)*time.Minute).UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	rows, err = r.ListSubscriptionTraffic(sub.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected separate quarter-hour history: %+v", rows)
	}
	if rows[0].Hour != start.UnixMilli() || rows[0].BucketMinutes != 60 || rows[0].UploadBytes != 120 || rows[0].DownloadBytes != 60 {
		t.Fatalf("new write changed legacy bucket duration or totals: %+v", rows[0])
	}
	if rows[1].Hour != start.Add(15*time.Minute).UnixMilli() || rows[1].BucketMinutes != 15 || rows[1].UploadBytes != 30 || rows[1].DownloadBytes != 15 {
		t.Fatalf("new bucket has wrong duration or duplicate bytes: %+v", rows[1])
	}
	if err := r.db.First(&sub, sub.ID).Error; err != nil {
		t.Fatal(err)
	}
	if sub.UploadBytes != 150 || sub.DownloadBytes != 75 {
		t.Fatalf("subscription counters diverged from history: %+v", sub)
	}
}
