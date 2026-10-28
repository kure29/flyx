package repo

import (
	"fmt"
	"go-backend/internal/store/model"
	"gorm.io/gorm"
	"sync"
	"testing"
	"time"
)

func TestPostgresMonthlyRollPreservesConcurrentUploads(t *testing.T) {
	r := openTrafficPostgresTest(t)
	now := time.Date(2026, 3, 1, 0, 0, 1, 0, time.UTC)
	id, err := r.CreateUser("monthly", "hash", 1, 0, 100, 1, 100, 1, 0, now.AddDate(0, -2, 0).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.DB().Model(&model.User{}).Where("id = ?", id).Update("in_flow", 9999).Error; err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 24)
	for i := 0; i < 12; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); errs <- r.RollMonthlyFlows(now) }()
		go func() {
			defer wg.Done()
			errs <- r.ApplyFlowUploadDeltasBatchAt([]FlowUploadCounterDelta{{UserID: id, InFlow: 100}}, now)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var user model.User
	if err := r.DB().First(&user, id).Error; err != nil {
		t.Fatal(err)
	}
	if user.InFlow != 1200 || user.FlowResetAt != now.Truncate(24*time.Hour).UnixMilli() {
		t.Fatalf("lost or reset usage: %+v", user)
	}
}

func TestPostgresMonthlyMaintenanceDoesNotLockCurrentCycles(t *testing.T) {
	for _, table := range []string{"user", "user_tunnel"} {
		t.Run(table, func(t *testing.T) {
			r := openTrafficPostgresTest(t)
			now := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
			seedMonthlyMaintenanceRows(t, r, now)
			tx := r.DB().Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			defer tx.Rollback()
			if err := tx.Table(table).Where("id=?", 101).Update("in_flow", 300).Error; err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- r.RollMonthlyFlows(now) }()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				tx.Rollback()
				<-done
				t.Fatal("maintenance waited for a current-cycle account")
			}
			var usage int64
			if err := r.DB().Table(table).Select("in_flow").Where("id=?", 102).Scan(&usage).Error; err != nil {
				t.Fatal(err)
			}
			if usage != 0 {
				t.Fatalf("due cycle not reset: %d", usage)
			}
		})
	}
}

func TestPostgresMonthlyMaintenanceRechecksCandidateAfterUpload(t *testing.T) {
	for _, table := range []string{"user", "user_tunnel"} {
		t.Run(table, func(t *testing.T) {
			r := openTrafficPostgresTest(t)
			now := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
			seedMonthlyMaintenanceRows(t, r, now)
			// Only this table is due, so the maintenance transaction holds no other
			// accounting row lock while its candidate scan is paused.
			other := "user_tunnel"
			if table == other {
				other = "user"
			}
			if err := r.DB().Table(other).Where("id=?", 102).Update("flow_reset_at", now.UnixMilli()).Error; err != nil {
				t.Fatal(err)
			}
			read, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			defer releaseOnce.Do(func() { close(release) })
			if err := r.DB().Callback().Query().After("gorm:query").Register("test:monthly_candidate", func(tx *gorm.DB) {
				if tx.Statement.Table == table && tx.Statement.Clauses["FOR"].Expression == nil {
					close(read)
					<-release
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer r.DB().Callback().Query().Remove("test:monthly_candidate")
			done := make(chan error, 1)
			go func() { done <- r.RollMonthlyFlows(now) }()
			select {
			case <-read:
			case <-time.After(5 * time.Second):
				t.Fatal("candidate scan not reached")
			}
			err := r.ApplyFlowUploadDeltasBatchAt([]FlowUploadCounterDelta{{UserID: 102, UserTunnelID: 102, InFlow: 300}}, now)
			releaseOnce.Do(func() { close(release) })
			if err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("maintenance did not finish")
			}
			var usage int64
			if err := r.DB().Table(table).Select("in_flow").Where("id=?", 102).Scan(&usage).Error; err != nil {
				t.Fatal(err)
			}
			if usage != 300 {
				t.Fatalf("new-cycle upload erased: %d", usage)
			}
		})
	}
}

func seedMonthlyMaintenanceRows(t *testing.T, r *Repository, now time.Time) {
	t.Helper()
	for _, id := range []int64{101, 102} {
		at := now.UnixMilli()
		if id == 102 {
			at = now.AddDate(0, -1, 0).UnixMilli()
		}
		if err := r.DB().Create(&model.User{ID: id, User: fmt.Sprintf("monthly-%d", id), RoleID: 1, FlowResetTime: 1, FlowResetAt: at, InFlow: 100, Status: 1}).Error; err != nil {
			t.Fatal(err)
		}
		if err := r.DB().Create(&model.UserTunnel{ID: id, UserID: id, TunnelID: id, FlowResetTime: 1, FlowResetAt: at, InFlow: 100, Status: 1}).Error; err != nil {
			t.Fatal(err)
		}
	}
}
