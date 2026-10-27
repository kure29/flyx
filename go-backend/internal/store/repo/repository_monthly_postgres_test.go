package repo

import (
	"go-backend/internal/store/model"
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
