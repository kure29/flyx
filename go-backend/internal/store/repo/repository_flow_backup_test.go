package repo

import (
	"gorm.io/gorm"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go-backend/internal/store/model"
)

func TestFlowBackupRestoresReceiptsAndOldBackupsResetThem(t *testing.T) {
	for _, open := range []struct {
		name string
		fn   func(*testing.T) *Repository
	}{
		{"sqlite", func(t *testing.T) *Repository {
			r, err := Open(filepath.Join(t.TempDir(), "backup.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = r.Close() })
			return r
		}},
		{"postgres", openTrafficPostgresTest},
	} {
		t.Run(open.name, func(t *testing.T) {
			r := open.fn(t)
			if err := r.DB().Create(&model.Node{ID: 10, Name: "node", Secret: "secret"}).Error; err != nil {
				t.Fatal(err)
			}
			cursor := model.TrafficCursor{Scope: "flow:10:" + strings.Repeat("a", 32), Sequence: 7, Digest: strings.Repeat("b", 64)}
			if err := r.DB().Create(&cursor).Error; err != nil {
				t.Fatal(err)
			}
			backup, err := r.ExportAll()
			if err != nil {
				t.Fatal(err)
			}
			if len(backup.FlowCursors) != 1 || backup.FlowCursors[0] != cursor {
				t.Fatalf("receipt omitted: %+v", backup.FlowCursors)
			}
			types := []string{"users", "nodes", "forwards", "userTunnels"}
			partial, err := r.ExportPartial(types)
			if err != nil || len(partial.FlowCursors) != 1 {
				t.Fatalf("complete accounting export: %v %+v", err, partial)
			}
			partial, err = r.ExportPartial([]string{"nodes"})
			if err != nil || len(partial.FlowCursors) != 0 {
				t.Fatalf("partial export contains receipts: %v", err)
			}
			if err := r.DB().Model(&model.TrafficCursor{}).Where("scope=?", cursor.Scope).Update("sequence", 9).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := r.Import(backup, types); err != nil {
				t.Fatal(err)
			}
			var restored model.TrafficCursor
			if err := r.DB().First(&restored, "scope=?", cursor.Scope).Error; err != nil || restored != cursor {
				t.Fatalf("receipt not restored: %+v %v", restored, err)
			}
			backup.FlowCursors = nil // Pre-protocol JSON backup.
			if _, err := r.Import(backup, []string{"nodes"}); err != nil {
				t.Fatal(err)
			}
			var count int64
			r.DB().Model(&model.TrafficCursor{}).Count(&count)
			if count != 1 {
				t.Fatal("partial restore removed receipt")
			}
			if _, err := r.Import(backup, types); err != nil {
				t.Fatal(err)
			}
			r.DB().Model(&model.TrafficCursor{}).Count(&count)
			if count != 0 {
				t.Fatal("old full backup kept incompatible receipt")
			}
			_, err = r.ApplyTrafficBatch(cursor.Scope, 8, cursor.Digest, func(*Repository) error { t.Fatal("gap must request a new stream"); return nil })
			if _, ok := err.(*TrafficSequenceGap); !ok {
				t.Fatalf("old restore not recoverable: %v", err)
			}
		})
	}
}

func TestPostgresFlowBackupBalancesAndReceiptsUseSameSnapshot(t *testing.T) {
	r := openTrafficPostgresTest(t)
	now := time.Now()
	id, err := r.CreateUser("backup", "hash", 1, 0, 100, 1, 100, 1, 0, now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	scope := "flow:10:" + strings.Repeat("a", 32)
	apply := func(seq int64) error {
		_, err := r.ApplyTrafficBatch(scope, seq, strings.Repeat("b", 64), func(tx *Repository) error {
			return tx.ApplyFlowUploadDeltasBatchAt([]FlowUploadCounterDelta{{UserID: id, InFlow: 100}}, now)
		})
		return err
	}
	if err := apply(1); err != nil {
		t.Fatal(err)
	}
	read, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	if err := r.DB().Callback().Query().After("gorm:query").Register("test:backup_snapshot", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*[]model.User); ok {
			close(read)
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer r.DB().Callback().Query().Remove("test:backup_snapshot")
	type result struct {
		backup *model.BackupData
		err    error
	}
	done := make(chan result, 1)
	go func() { b, err := r.ExportAll(); done <- result{b, err} }()
	select {
	case <-read:
	case <-time.After(5 * time.Second):
		t.Fatal("backup did not read users")
	}
	if err := apply(2); err != nil {
		t.Fatal(err)
	}
	once.Do(func() { close(release) })
	var got result
	select {
	case got = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("backup did not finish")
	}
	if got.err != nil {
		t.Fatal(got.err)
	}
	if len(got.backup.FlowCursors) != 1 || got.backup.FlowCursors[0].Sequence != 1 {
		t.Fatalf("receipt newer than balances: %+v", got.backup.FlowCursors)
	}
	for _, u := range got.backup.Users {
		if u.ID == id && u.InFlow != 100 {
			t.Fatalf("wrong snapshot balance: %d", u.InFlow)
		}
	}
}
