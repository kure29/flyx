package handler

import (
	"testing"
	"time"

	"go-backend/internal/store/model"
)

// Hours land in the viewer's local days and months; older ones are left
// out.
func TestSubTrafficBuckets(t *testing.T) {
	cst := time.FixedZone("", 8*3600)
	now := time.Date(2026, 3, 2, 10, 30, 0, 0, cst)
	at := func(y int, m time.Month, d, h int) int64 { return time.Date(y, m, d, h, 0, 0, 0, cst).UnixMilli() }
	hours := []model.SubscriptionTraffic{
		{Hour: at(2025, 1, 31, 23), DownloadBytes: 1}, // before the 12 months
		{Hour: at(2026, 2, 28, 23), DownloadBytes: 10, UploadBytes: 1},
		{Hour: at(2026, 3, 1, 0), DownloadBytes: 20},
		{Hour: at(2026, 3, 2, 9), DownloadBytes: 30},
		{Hour: at(2026, 3, 2, 10), DownloadBytes: 40, UploadBytes: 4},
	}
	hour := subTraffic(hours, "hour", now)
	if len(hour.Points) != 24 || hour.Points[23].Down != 40 || hour.Points[22].Down != 30 || hour.Down != 70 ||
		hour.Points[0].Time != at(2026, 3, 1, 11) {
		t.Fatalf("hour: %+v", hour)
	}
	day := subTraffic(hours, "day", now)
	if len(day.Points) != 30 || day.Points[29].Down != 70 || day.Points[28].Down != 20 || day.Points[27].Down != 10 || day.Up != 5 {
		t.Fatalf("day: %+v", day)
	}
	month := subTraffic(hours, "month", now)
	if len(month.Points) != 12 || month.Points[11].Down != 90 || month.Points[10].Down != 10 || month.Down != 100 ||
		month.Points[0].Time != at(2025, 4, 1, 0) {
		t.Fatalf("month: %+v", month)
	}
}

func TestSubTrafficFractionalTimezones(t *testing.T) {
	for _, offset := range []int{330, 345, -210} {
		loc := time.FixedZone("", offset*60)
		now := time.Date(2026, 4, 1, 0, 20, 0, 0, loc)
		midnight := time.Date(2026, 4, 1, 0, 0, 0, 0, loc)
		rows := []model.SubscriptionTraffic{
			{Hour: midnight.Add(-15 * time.Minute).UnixMilli(), BucketMinutes: 15, UploadBytes: 10},
			{Hour: midnight.UnixMilli(), BucketMinutes: 15, UploadBytes: 20},
		}
		for _, key := range []string{"hour", "day", "month"} {
			view := subTraffic(rows, key, now)
			n := len(view.Points)
			if view.Points[n-1].Up != 20 || view.Points[n-2].Up != 10 || view.Up != 30 || view.Estimated {
				t.Fatalf("offset=%d range=%s: %+v", offset, key, view)
			}
			if view.Points[n-1].Time != midnight.UnixMilli() {
				t.Fatal("bucket not aligned to local boundary")
			}
		}
	}
}

func TestSubTrafficLegacyOverlapConservesBytes(t *testing.T) {
	loc := time.FixedZone("", 345*60)
	now := time.Date(2026, 4, 1, 1, 0, 0, 0, loc)
	midnight := time.Date(2026, 4, 1, 0, 0, 0, 0, loc)
	row := model.SubscriptionTraffic{Hour: midnight.Truncate(time.Hour).UnixMilli(), BucketMinutes: 60, UploadBytes: 101, DownloadBytes: 9223372036854775807}
	for _, key := range []string{"day", "month"} {
		view := subTraffic([]model.SubscriptionTraffic{row}, key, now)
		n := len(view.Points)
		if !view.Estimated || view.Up != 101 || view.Down != row.DownloadBytes || view.Points[n-2].Up != 25 || view.Points[n-1].Up != 76 {
			t.Fatalf("%s: %+v", key, view)
		}
	}
	// Include only the overlap when a legacy hour starts before the range.
	start := subTrafficBuckets("day", now)[0]
	row.Hour = start.Truncate(time.Hour).UnixMilli()
	view := subTraffic([]model.SubscriptionTraffic{row}, "day", now)
	if view.Up != 76 || !view.Estimated {
		t.Fatalf("partial first hour: %+v", view)
	}
}
