package repo

import (
	"database/sql"
	"go-backend/internal/store/model"
	"gorm.io/gorm"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestPostgresDailyRollPreservesConcurrentUsage(t *testing.T) {
	dsn := os.Getenv("FLVX_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires temporary PostgreSQL")
	}
	schema := "quota_window_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(`DROP SCHEMA IF EXISTS "` + schema + `" CASCADE`); _ = admin.Close() })
	if _, err := admin.Exec(`CREATE SCHEMA "` + schema + `"`); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	params := parsed.Query()
	params.Set("search_path", schema)
	parsed.RawQuery = params.Encode()
	r, err := OpenPostgres(parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	yesterday := time.Date(2026, 10, 26, 23, 59, 0, 0, time.UTC)
	today := yesterday.Add(time.Minute)
	if _, err := r.AddUserQuotaUsage(123, 100, yesterday); err != nil {
		t.Fatal(err)
	}
	read := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan error, 1)
	if err := r.DB().Callback().Query().After("gorm:query").Register("deep-review:pause-window-roll", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*[]model.UserQuota); ok {
			close(read)
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	go func() { _, err := r.RollUserQuotaWindows(today); finished <- err }()
	select {
	case <-read:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("maintenance did not read quotas")
	}
	added := make(chan error, 1)
	go func() { _, err := r.AddUserQuotaUsage(123, 300, today); added <- err }()
	// Maintenance holds the snapshot while a new-day upload attempts to write.
	// Without the row lock that upload commits first and is overwritten.
	var addErr error
	addFinished := false
	select {
	case addErr = <-added:
		addFinished = true
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	if !addFinished {
		select {
		case addErr = <-added:
		case <-time.After(5 * time.Second):
			t.Fatal("usage writer did not finish")
		}
	}
	if addErr != nil {
		t.Fatal(addErr)
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("maintenance did not complete")
	}
	q, err := r.GetUserQuotaView(123, today)
	if err != nil {
		t.Fatal(err)
	}
	if q.DailyUsedBytes != 300 || q.MonthlyUsedBytes != 400 {
		t.Fatalf("maintenance overwrote concurrent usage: daily=%d monthly=%d, want 300/400", q.DailyUsedBytes, q.MonthlyUsedBytes)
	}
}
