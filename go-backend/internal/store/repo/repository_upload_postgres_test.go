package repo

import (
	"errors"
	"fmt"
	"go-backend/internal/store/model"
	"sync"
	"testing"
	"time"
)

func TestPostgresReliableTrafficTransactions(t *testing.T) {
	r := openTrafficPostgresTest(t)
	now := time.Now()
	id, err := r.CreateUser("traffic", "hash", 1, 0, 100, int64(now.Day()), 100, 1, 0, now.AddDate(0, -2, 0).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	apply := func(sequence, amount int64, fail bool) error {
		_, err := r.ApplyTrafficBatch("gost-stream", sequence, fmt.Sprintf("%064x", amount), func(tx *Repository) error {
			if err := tx.ApplyFlowUploadDeltasBatchAt([]FlowUploadCounterDelta{{UserID: id, InFlow: amount}}, now); err != nil {
				return err
			}
			if _, err := tx.AddUserQuotaUsage(id, amount, now); err != nil {
				return err
			}
			if fail {
				return errors.New("injected transaction failure")
			}
			return nil
		})
		return err
	}
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- apply(1, 100, false) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	check := func(want int64) {
		t.Helper()
		var user model.User
		if err := r.DB().First(&user, id).Error; err != nil {
			t.Fatal(err)
		}
		quota, err := r.GetUserQuotaView(id, now)
		if err != nil {
			t.Fatal(err)
		}
		if user.InFlow != want || quota.DailyUsedBytes != want {
			t.Fatalf("flow=%d quota=%d want=%d", user.InFlow, quota.DailyUsedBytes, want)
		}
	}
	check(100)
	if err := apply(2, 200, true); err == nil {
		t.Fatal("expected rollback")
	}
	check(100)
	if err := apply(2, 200, false); err != nil {
		t.Fatal(err)
	}
	check(300)
}
