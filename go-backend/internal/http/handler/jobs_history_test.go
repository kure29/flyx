package handler

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"go-backend/internal/store/repo"
	"gorm.io/gorm"
)

func TestStatisticsFlowRetainsBaselineAcrossDowntimeAndDatabaseFailures(t *testing.T) {
	for _, failure := range []string{"none", "read", "write"} {
		t.Run(failure, func(t *testing.T) {
			r, err := repo.Open(filepath.Join(t.TempDir(), "history.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = r.Close() })
			h := New(r, "secret")
			now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
			if err := r.DB().Exec("UPDATE user SET in_flow=4100, out_flow=0 WHERE id=1").Error; err != nil {
				t.Fatal(err)
			}
			if err := r.CreateStatisticsFlow(1, 4000, 4000, "11:00", now.Add(-49*time.Hour).UnixMilli()); err != nil {
				t.Fatal(err)
			}
			injectFailure := func(db *gorm.DB) {
				if db.Statement.Table == "statistics_flow" {
					db.AddError(errors.New("injected statistics failure"))
				}
			}
			switch failure {
			case "read":
				err = r.DB().Callback().Query().Before("gorm:query").Register("test:history_failure", injectFailure)
			case "write":
				err = r.DB().Callback().Create().Before("gorm:create").Register("test:history_failure", injectFailure)
			}
			if err != nil {
				t.Fatal(err)
			}
			h.runStatisticsFlowJob(now)
			if failure != "none" {
				if failure == "read" {
					err = r.DB().Callback().Query().Remove("test:history_failure")
				} else {
					err = r.DB().Callback().Create().Remove("test:history_failure")
				}
				if err != nil {
					t.Fatal(err)
				}
				if count := mustQueryInt(t, r, "SELECT COUNT(*) FROM statistics_flow"); count != 1 {
					t.Fatalf("failed job changed history, rows=%d", count)
				}
				if total := mustHandlerCount(t, h, "SELECT total_flow FROM statistics_flow"); total != 4000 {
					t.Fatalf("failed job lost baseline: %d", total)
				}
				h.runStatisticsFlowJob(now)
			}
			if count := mustQueryInt(t, r, "SELECT COUNT(*) FROM statistics_flow"); count != 1 {
				t.Fatalf("successful job did not prune old history: %d", count)
			}
			if got := mustHandlerCount(t, h, "SELECT flow FROM statistics_flow"); got != 100 {
				t.Fatalf("restart counted historical usage: got %d, want 100", got)
			}
			h.runStatisticsFlowJob(now.Add(time.Hour))
			if got := mustHandlerCount(t, h, "SELECT flow FROM statistics_flow ORDER BY id DESC LIMIT 1"); got != 0 {
				t.Fatalf("unchanged total billed twice: %d", got)
			}
		})
	}
}
