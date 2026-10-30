package repo

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	gsqlite "github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"go-backend/internal/security"
	"go-backend/internal/store/model"
)

const (
	defaultPostgresMaxOpenConns = 32
	defaultPostgresMaxIdleConns = 8
	defaultPostgresConnMaxIdle  = 5 * time.Minute
	defaultPostgresConnMaxLife  = 30 * time.Minute
)

// ─── Type aliases for backward compatibility ─────────────────────────
// Handlers still reference repo.User, repo.BackupData, etc.

type User = model.User
type ViteConfig = model.ViteConfig
type Announcement = model.Announcement
type UserTunnelDetail = model.UserTunnelDetail
type UserForwardDetail = model.UserForwardDetail
type StatisticsFlow = model.StatisticsFlow
type Node = model.Node
type PeerShare = model.PeerShare
type PeerShareResource = model.PeerShareResource
type PeerShareRuntime = model.PeerShareRuntime
type FederationTunnelBinding = model.FederationTunnelBinding
type BackupData = model.BackupData
type UserBackup = model.UserBackup
type NodeBackup = model.NodeBackup
type TunnelBackup = model.TunnelBackup
type ChainTunnelBackup = model.ChainTunnelBackup
type ForwardBackup = model.ForwardBackup
type ForwardPortBackup = model.ForwardPortBackup
type UserTunnelBackup = model.UserTunnelBackup
type SpeedLimitBackup = model.SpeedLimitBackup
type TunnelGroupBackup = model.TunnelGroupBackup
type UserGroupBackup = model.UserGroupBackup
type PermissionBackup = model.PermissionBackup
type PermissionGrantBackup = model.PermissionGrantBackup
type ImportResult = model.ImportResult
type NodeMetric = model.NodeMetric
type TunnelMetric = model.TunnelMetric
type ServiceMonitor = model.ServiceMonitor
type ServiceMonitorResult = model.ServiceMonitorResult
type TunnelQuality = model.TunnelQuality

// ─── Repository ──────────────────────────────────────────────────────

type Repository struct {
	db     *gorm.DB
	dbPath string
}

type FlowUploadCounterDelta struct {
	ForwardID    int64
	UserID       int64
	UserTunnelID int64
	InFlow       int64
	OutFlow      int64
}

func (r *Repository) DB() *gorm.DB {
	if r == nil {
		return nil
	}
	return r.db
}

// ─── Open / Close ────────────────────────────────────────────────────

func Open(path string) (*Repository, error) {
	if err := ensureParentDir(path); err != nil {
		return nil, err
	}

	dsn := "file:" + path +
		"?_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)"

	db, err := gorm.Open(gsqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)

	if err := prepareSQLiteLegacyColumns(db); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("prepare sqlite legacy schema: %w", err)
	}
	if err := autoMigrateAll(db); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("auto migrate: %w", err)
	}

	seedData(db)

	if err := migrateSchema(db); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	if err := migrateSubscriptions(db, unixMilliNow()); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("migrate subscriptions: %w", err)
	}

	return &Repository{db: db, dbPath: path}, nil
}

func OpenPostgres(dsn string) (*Repository, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("empty postgres dsn")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	configurePostgresPool(sqlDB)
	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}

	if err := preparePostgresLegacySchema(db); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("prepare postgres legacy schema: %w", err)
	}

	if err := autoMigrateAll(db); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("auto migrate: %w", err)
	}

	seedData(db)

	if err := migrateSchema(db); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	if err := migrateSubscriptions(db, unixMilliNow()); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("migrate subscriptions: %w", err)
	}

	return &Repository{db: db}, nil
}

func configurePostgresPool(sqlDB *sql.DB) {
	if sqlDB == nil {
		return
	}
	sqlDB.SetMaxOpenConns(defaultPostgresMaxOpenConns)
	sqlDB.SetMaxIdleConns(defaultPostgresMaxIdleConns)
	sqlDB.SetConnMaxIdleTime(defaultPostgresConnMaxIdle)
	sqlDB.SetConnMaxLifetime(defaultPostgresConnMaxLife)
}

func (r *Repository) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	sqlDB, err := r.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func autoMigrateAll(db *gorm.DB) error {
	// Nodes configured before the column existed may own flvx tables.
	m := db.Migrator()
	backfillLegacyNft := m.HasTable(&model.NodeSSHConfig{}) && !m.HasColumn(&model.NodeSSHConfig{}, "legacy_nft_tables")
	if err := autoMigrateModels(db); err != nil {
		return err
	}
	if backfillLegacyNft {
		return db.Model(&model.NodeSSHConfig{}).Where("1 = 1").Update("legacy_nft_tables", true).Error
	}
	return nil
}

func autoMigrateModels(db *gorm.DB) error {
	if db.Dialector.Name() == "sqlite" {
		if err := prepareSQLiteNftablesColumns(db); err != nil {
			return err
		}
	}

	models := []interface{}{
		&model.User{},
		&model.Passkey{},
		&model.UserGithub{},
		&model.UserQuota{},
		&model.Forward{},
		&model.ForwardPort{},
		&model.Node{},
		&model.NodeSSHConfig{},
		&model.NodeUninstall{},
		&model.NftRuleBinding{},
		&model.NftCounterState{},
		&model.SpeedLimit{},
		&model.StatisticsFlow{},
		&model.TrafficCursor{},
		&model.Tunnel{},
		&model.ChainTunnel{},
		&model.UserTunnel{},
		&model.TunnelGroup{},
		&model.UserGroup{},
		&model.TunnelGroupTunnel{},
		&model.UserGroupUser{},
		&model.GroupPermission{},
		&model.GroupPermissionGrant{},
		&model.MonitorPermission{},
		&model.ViteConfig{},
		&model.PeerShare{},
		&model.PeerShareRuntime{},
		&model.PeerShareResource{},
		&model.FederationTunnelBinding{},
		&model.FederationPendingRelease{},
		&model.Announcement{},
		&model.SchemaVersion{},
		&model.NodeMetric{},
		&model.NodeTrafficBaseline{},
		&model.TunnelMetric{},
		&model.ServiceMonitor{},
		&model.ServiceMonitorResult{},
		&model.TunnelQuality{},
		&model.NodeKernel{},
		&model.NodeInbound{},
		&model.NodeInboundCounter{},
		&model.CounterStream{},
		&model.CounterEpoch{},
		&model.NodeInboundRelay{},
		&model.SubUser{},
		&model.Subscription{},
		&model.SubscriptionCounter{},
		&model.SubscriptionTraffic{},
		&model.NodeTraffic{},
		&model.RemoteSource{},
		&model.SubscriptionTemplate{},
		&model.NodeGroup{},
		&model.NodeLatencySample{},
	}

	if db.Dialector.Name() != "sqlite" {
		return db.AutoMigrate(models...)
	}

	m := db.Migrator()
	hasNode := m.HasTable(&model.Node{})
	hasTunnel := m.HasTable(&model.Tunnel{})
	hasForward := m.HasTable(&model.Forward{})

	for _, item := range models {
		if hasNode {
			if _, ok := item.(*model.Node); ok {
				continue
			}
		}
		if hasTunnel {
			if _, ok := item.(*model.Tunnel); ok {
				continue
			}
		}
		if hasForward {
			if _, ok := item.(*model.Forward); ok {
				continue
			}
		}
		if err := db.AutoMigrate(item); err != nil {
			return err
		}
	}

	return nil
}

func seedData(db *gorm.DB) {
	var adminCount int64
	if err := db.Model(&model.User{}).Where("id = ?", 1).Count(&adminCount).Error; err == nil && adminCount == 0 {
		adminPwd, err := security.HashPassword("admin_user")
		if err != nil {
			log.Printf("seed admin password hash failed: %v", err)
		} else {
			adminUser := model.User{
				ID: 1, User: "admin_user", Pwd: adminPwd,
				RoleID: 0, ExpTime: 2727251700000, Flow: 99999, InFlow: 0, OutFlow: 0,
				FlowResetTime: 1, Num: 99999, CreatedTime: 1748914865000,
				UpdatedTime:       sql.NullInt64{Int64: 1754011744252, Valid: true},
				Status:            1,
				PasswordChangedAt: 1748914865000,
			}
			db.Create(&adminUser)
		}
	}

	appNameConfig := model.ViteConfig{ID: 1, Name: "app_name", Value: "FLYX", Time: 1755147963000}
	db.Where("id = ?", 1).FirstOrCreate(&appNameConfig)
	// "flux" was the seeded name, hidden behind the old commercial license
	// (it could not be changed without one); show the product name instead.
	db.Model(&model.ViteConfig{}).Where("name = ? AND value = ?", "app_name", "flux").Update("value", "FLYX")
	// The project was renamed FLVX → FLYX; panels still on the old default
	// follow, a name the admin chose is kept.
	db.Model(&model.ViteConfig{}).Where("name = ? AND value = ?", "app_name", "FLVX").Update("value", "FLYX")
	now := time.Now().UnixMilli()
	for name, value := range map[string]string{
		"hide_footer_brand": "false",
	} {
		cfg := model.ViteConfig{Name: name, Value: value, Time: now}
		db.Where("name = ?", name).FirstOrCreate(&cfg)
	}
}

func nullableString(v sql.NullString) interface{} {
	if v.Valid {
		return v.String
	}
	return nil
}

func nullableInt64(v sql.NullInt64) interface{} {
	if v.Valid {
		return v.Int64
	}
	return nil
}

func unixMilliNow() int64 {
	return time.Now().UnixMilli()
}

func ensureParentDir(dbPath string) error {
	if dbPath == "" {
		return fmt.Errorf("empty db path")
	}
	dir := filepath.Dir(dbPath)
	if dir == "" || dir == "." {
		return nil
	}
	return osMkdirAll(dir)
}

var osMkdirAll = func(path string) error {
	return os.MkdirAll(path, 0o755)
}

// Suppress unused import warning for log
var _ = log.Printf
