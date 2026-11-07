package repo

import (
	"go-backend/internal/store/model"
	"testing"
	"time"
)

func TestPostgresSubscriptionTrafficBucketUpgrade(t *testing.T) {
	assertSubscriptionTrafficBucketUpgrade(t, openTrafficPostgresTest(t))
}

func TestPostgresNodeTrafficFractionalMonthBoundary(t *testing.T) {
	r := openTrafficPostgresTest(t)
	for _, offset := range []int{330, 345, -210} {
		start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.FixedZone("", offset*60))
		if err := r.DB().Exec("DELETE FROM node_traffic").Error; err != nil {
			t.Fatal(err)
		}
		rows := []model.NodeTraffic{
			{NodeID: 1, Hour: start.Add(-15 * time.Minute).UnixMilli(), BucketMinutes: 15, InBytes: 10},
			{NodeID: 1, Hour: start.UnixMilli(), BucketMinutes: 15, InBytes: 20},
			{NodeID: 2, Hour: start.Truncate(time.Hour).UnixMilli(), BucketMinutes: 60, InBytes: 120, OutBytes: 240},
		}
		if err := r.AddNodeTraffic(rows); err != nil {
			t.Fatal(err)
		}
		totals, err := r.NodeTrafficSince(start.UnixMilli())
		if err != nil {
			t.Fatal(err)
		}
		elapsed := start.Sub(start.Truncate(time.Hour)) / time.Minute
		if totals[1][0] != 20 || totals[2][0] != 120-int64(elapsed)*2 || totals[2][1] != 240-int64(elapsed)*4 {
			t.Fatalf("offset=%d totals=%v", offset, totals)
		}
	}
}
