package repo

import (
	"go-backend/internal/store/model"
	"path/filepath"
	"testing"
	"time"
)

func TestMonthlyFlowCatchUpIsIdempotentAndPreservesLegacyUsage(t *testing.T) {
	r, err := Open(filepath.Join(t.TempDir(), "monthly.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	joined := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	id, err := r.CreateUser("cycle-user", "x", 1, 0, 100, 31, 100, 1, 0, joined.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.DB().Model(&model.User{}).Where("id = ?", id).Update("in_flow", 1000).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC)
	if err := r.RollMonthlyFlows(now); err != nil {
		t.Fatal(err)
	}
	var user model.User
	if err := r.DB().First(&user, id).Error; err != nil {
		t.Fatal(err)
	}
	if user.InFlow != 0 || user.FlowResetAt != time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("missed short-month reset: %+v", user)
	}
	if err := r.DB().Model(&model.User{}).Where("id = ?", id).Update("in_flow", 200).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := r.RollMonthlyFlows(now); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.DB().First(&user, id).Error; err != nil {
		t.Fatal(err)
	}
	if user.InFlow != 200 {
		t.Fatal("retry reset new usage")
	}
	if err := r.DB().Model(&model.User{}).Where("id = ?", id).Update("flow_reset_at", 0).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.RollMonthlyFlows(now); err != nil {
		t.Fatal(err)
	}
	if err := r.DB().First(&user, id).Error; err != nil {
		t.Fatal(err)
	}
	if user.InFlow != 200 || user.FlowResetAt != now.UnixMilli() {
		t.Fatal("legacy initialization cleared usage")
	}
}

func TestMonthlyFlowRollsBeforeNewUsage(t *testing.T) {
	r, err := Open(filepath.Join(t.TempDir(), "roll.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	now := time.Now()
	day := int64(now.Day())
	id, err := r.CreateUser("missed-job", "x", 1, 0, 100, day, 10, 1, 0, now.AddDate(0, -2, 0).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.DB().Model(&model.User{}).Where("id = ?", id).Update("in_flow", 1000).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.ApplyFlowUploadDeltasBatch([]FlowUploadCounterDelta{{UserID: id, InFlow: 100}}); err != nil {
		t.Fatal(err)
	}
	var user model.User
	if err := r.DB().First(&user, id).Error; err != nil {
		t.Fatal(err)
	}
	if user.InFlow != 100 {
		t.Fatalf("old cycle survived first upload: %d", user.InFlow)
	}
	if err := r.RollMonthlyFlows(now); err != nil {
		t.Fatal(err)
	}
	if err := r.DB().First(&user, id).Error; err != nil {
		t.Fatal(err)
	}
	if user.InFlow != 100 {
		t.Fatal("maintenance erased new-cycle upload")
	}
}
