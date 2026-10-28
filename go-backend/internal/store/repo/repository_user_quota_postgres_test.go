package repo

import (
	"database/sql"
	"go-backend/internal/config"
	"go-backend/internal/store/model"
	"gorm.io/gorm"
	"net/url"
	"strconv"
	"testing"
	"time"
)

func TestPostgresDailyRollPreservesConcurrentUsage(t *testing.T) {
	r := openQuotaMaintenancePostgresTest(t)
	yesterday := time.Date(2026, 10, 26, 23, 59, 0, 0, time.UTC)
	today := yesterday.Add(time.Minute)
	if _, err := r.AddUserQuotaUsage(123, 100, yesterday); err != nil {
		t.Fatal(err)
	}
	read := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan error, 1)
	if err := r.DB().Callback().Query().After("gorm:query").Register("deep-review:pause-window-roll", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*[]model.UserQuota); ok && tx.Statement.Clauses["FOR"].Expression != nil {
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

func TestPostgresQuotaRollDoesNotLockUnchangedUsers(t *testing.T) {
	r := openQuotaMaintenancePostgresTest(t)
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	if _, err := r.AddUserQuotaUsage(101, 100, now); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddUserQuotaUsage(102, 100, now.AddDate(0, 0, -1)); err != nil {
		t.Fatal(err)
	}
	// Hold the current user's row as a slow billing transaction would.
	tx := r.DB().Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	if err := tx.Exec("UPDATE user_quota SET daily_used_bytes=daily_used_bytes+1 WHERE user_id=101").Error; err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := r.RollUserQuotaWindows(now); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		tx.Rollback()
		<-done
		t.Fatal("maintenance blocked on a user with no window change")
	}
	q, err := r.GetUserQuotaView(102, now)
	if err != nil || q.DailyUsedBytes != 0 {
		t.Fatalf("due user not rolled: %+v %v", q, err)
	}
}

func openQuotaMaintenancePostgresTest(t *testing.T) *Repository {
	t.Helper()
	dsn := config.IdentityEnv("FLYX_POSTGRES_TEST_DSN", "FLVX_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires temporary PostgreSQL")
	}
	schema := "quota_maintenance_" + strconv.FormatInt(time.Now().UnixNano(), 36)
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
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestPostgresQuotaRollRechecksCandidatesAfterConcurrentUpload(t *testing.T) {
	r := openQuotaMaintenancePostgresTest(t)
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	if _, err := r.AddUserQuotaUsage(101, 100, now.AddDate(0, 0, -1)); err != nil {
		t.Fatal(err)
	}
	read, release := make(chan struct{}), make(chan struct{})
	if err := r.DB().Callback().Query().After("gorm:query").Register("test:quota_candidate", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*[]model.UserQuota); ok && tx.Statement.Clauses["FOR"].Expression == nil {
			close(read)
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer r.DB().Callback().Query().Remove("test:quota_candidate")
	done := make(chan error, 1)
	go func() { _, err := r.RollUserQuotaWindows(now); done <- err }()
	select {
	case <-read:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("candidate query not reached")
	}
	_, addErr := r.AddUserQuotaUsage(101, 300, now)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if addErr != nil {
		t.Fatal(addErr)
	}
	q, err := r.GetUserQuotaView(101, now)
	if err != nil || q.DailyUsedBytes != 300 {
		t.Fatalf("stale candidate overwrote current usage: %+v %v", q, err)
	}
}
