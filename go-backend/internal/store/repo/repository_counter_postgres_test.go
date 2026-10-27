package repo

import (
	"go-backend/internal/store/model"
	"sync"
	"testing"
	"time"
)

func TestPostgresOrderedTrafficSnapshots(t *testing.T) {
	r := openTrafficPostgresTest(t)
	now := time.Now()
	var id int64 = 2
	var err error
	var wg sync.WaitGroup
	sub := model.Subscription{UserID: id, Name: "counter"}
	if err := r.DB().Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	sequences := make([]int64, 12)
	for i := range sequences {
		sequences[i], err = r.NextCounterSnapshot(1, "kernel")
		if err != nil {
			t.Fatal(err)
		}
	}
	errs := make(chan error, len(sequences))
	for _, sequence := range sequences {
		wg.Add(1)
		go func(sequence int64) {
			defer wg.Done()
			_, err := r.ApplyCounterSnapshot(1, "kernel", sequence, "epoch", func(tx *Repository) error {
				return tx.ApplySubscriptionCounterSamples(1, "epoch", []SubscriptionCounterSample{{SubscriptionID: sub.ID, Direction: "up", Bytes: sequence * 100}}, now.UnixMilli())
			})
			errs <- err
		}(sequence)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := r.DB().First(&sub, sub.ID).Error; err != nil {
		t.Fatal(err)
	}
	if sub.UploadBytes != 1200 {
		t.Fatalf("concurrent snapshots counted %d, want 1200", sub.UploadBytes)
	}
	rows, err := r.ListSubscriptionTraffic(sub.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].UploadBytes != 1200 {
		t.Fatalf("history=%+v", rows)
	}
}
