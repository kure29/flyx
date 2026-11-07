package repo

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"go-backend/internal/store/model"
)

// preparePostgresLegacySchema renames unique constraints that were created by
// the old schema.sql (which used inline UNIQUE column syntax) to the names
// expected by GORM's NamingStrategy ("uni_<table>_<column>"). Without this,
// GORM's AutoMigrate emits "DROP CONSTRAINT uni_..." against constraints that
// don't exist under that name, crashing startup on upgraded PostgreSQL installs.
func preparePostgresLegacySchema(db *gorm.DB) error {
	if db == nil || db.Dialector.Name() != "postgres" {
		return nil
	}

	type rename struct{ table, oldName, newName string }
	renames := []rename{
		{"vite_config", "vite_config_name_key", "uni_vite_config_name"},
		{"peer_share", "peer_share_token_key", "uni_peer_share_token"},
		{"peer_share_runtime", "peer_share_runtime_reservation_id_key", "uni_peer_share_runtime_reservation_id"},
		{"peer_share_runtime", "peer_share_runtime_resource_key_key", "uni_peer_share_runtime_resource_key"},
		{"federation_tunnel_binding", "federation_tunnel_binding_resource_key_key", "uni_federation_tunnel_binding_resource_key"},
	}

	for _, r := range renames {
		var count int64
		if err := db.Raw(
			`SELECT COUNT(*) FROM information_schema.table_constraints
			 WHERE constraint_schema = current_schema()
			   AND table_name = ?
			   AND constraint_name = ?
			   AND constraint_type = 'UNIQUE'`,
			r.table, r.oldName,
		).Scan(&count).Error; err != nil {
			return fmt.Errorf("check constraint %s.%s: %w", r.table, r.oldName, err)
		}
		if count == 0 {
			continue
		}
		if err := db.Exec(
			fmt.Sprintf(`ALTER TABLE %q RENAME CONSTRAINT %q TO %q`, r.table, r.oldName, r.newName),
		).Error; err != nil {
			return fmt.Errorf("rename constraint %s.%s→%s: %w", r.table, r.oldName, r.newName, err)
		}
	}
	return nil
}

// prepareSQLiteLegacyColumns adds the columns an older SQLite database is
// missing to node, tunnel and forward. autoMigrateModels skips those three
// tables once they exist (GORM's SQLite migrator would rebuild them), so a
// field added to the model reaches an existing panel only through here.
// Every field of the model is checked, so a new one cannot be forgotten
// (node.nft_error was, and adding a server failed on older panels).
func prepareSQLiteLegacyColumns(db *gorm.DB) error {
	if db == nil || db.Dialector.Name() != "sqlite" {
		return nil
	}
	m := db.Migrator()

	for _, t := range []struct {
		name  string
		model interface{}
	}{
		{"node", &model.Node{}},
		{"tunnel", &model.Tunnel{}},
		{"forward", &model.Forward{}},
	} {
		if !m.HasTable(t.model) {
			continue
		}
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(t.model); err != nil {
			return fmt.Errorf("parse %s model: %w", t.name, err)
		}
		for _, field := range stmt.Schema.Fields {
			if field.DBName == "" || field.PrimaryKey || m.HasColumn(t.model, field.DBName) {
				continue
			}
			if err := m.AddColumn(t.model, field.Name); err != nil {
				return fmt.Errorf("add %s.%s: %w", t.name, field.DBName, err)
			}
		}
	}

	return nil
}

func prepareSQLiteNftablesColumns(db *gorm.DB) error {
	if db == nil || db.Dialector.Name() != "sqlite" {
		return nil
	}
	if !db.Migrator().HasTable(&model.Node{}) {
		return nil
	}
	if !db.Migrator().HasColumn(&model.Node{}, "forward_mode") {
		if err := db.Exec("ALTER TABLE node ADD COLUMN forward_mode varchar(20) NOT NULL DEFAULT 'agent'").Error; err != nil {
			return err
		}
	}
	return nil
}

// ─── Migration ───────────────────────────────────────────────────────

const currentSchemaVersion = 6

var ensurePostgresIDDefaultsFn = ensurePostgresIDDefaults
var migrateViteConfigValueColumnTypeFn = migrateViteConfigValueColumnType
var migrateSpeedLimitTunnelBindingFn = migrateSpeedLimitTunnelBinding
var migratePostgresTrafficInt64ColumnsFn = migratePostgresTrafficInt64Columns
var migrateTunnelMetricBucketUniqueIndexFn = migrateTunnelMetricBucketUniqueIndex

func getSchemaVersion(db *gorm.DB) int {
	var v model.SchemaVersion
	if err := db.First(&v).Error; err != nil {
		db.Create(&model.SchemaVersion{Version: 0})
		return 0
	}
	return v.Version
}

func setSchemaVersion(db *gorm.DB, ver int) {
	db.Model(&model.SchemaVersion{}).Where("1=1").Update("version", ver)
}

func migrateSchema(db *gorm.DB) error {
	if db == nil {
		return errors.New("nil db")
	}

	if err := ensurePostgresIDDefaultsFn(db); err != nil {
		return err
	}

	ver := getSchemaVersion(db)
	if ver >= currentSchemaVersion {
		return nil
	}

	// Normalize strategy columns
	normalizeStrategy := func(modelRef interface{}, table, defaultValue string) error {
		result := db.Model(modelRef).Where("strategy IS NULL").Update("strategy", defaultValue)
		if result.Error != nil {
			msg := strings.ToLower(result.Error.Error())
			if strings.Contains(msg, "no such table") || (strings.Contains(msg, "relation") && strings.Contains(msg, "does not exist")) {
				return nil
			}
			return fmt.Errorf("normalize %s.strategy: %w", table, result.Error)
		}
		return nil
	}

	if err := normalizeStrategy(&model.Forward{}, "forward", "fifo"); err != nil {
		return err
	}
	if err := normalizeStrategy(&model.ChainTunnel{}, "chain_tunnel", "round"); err != nil {
		return err
	}
	if err := normalizeStrategy(&model.PeerShareRuntime{}, "peer_share_runtime", "round"); err != nil {
		return err
	}

	if ver < 3 {
		if err := migrateViteConfigValueColumnTypeFn(db); err != nil {
			return err
		}
	}

	if ver < 4 {
		if err := migrateSpeedLimitTunnelBindingFn(db); err != nil {
			return err
		}
	}

	if ver < 5 {
		if err := migratePostgresTrafficInt64ColumnsFn(db); err != nil {
			return err
		}
	}

	if ver < 6 {
		if err := migrateTunnelMetricBucketUniqueIndexFn(db); err != nil {
			return err
		}
	}

	setSchemaVersion(db, currentSchemaVersion)
	return nil
}

func migrateViteConfigValueColumnType(db *gorm.DB) error {
	if db == nil {
		return errors.New("nil db")
	}

	if !db.Migrator().HasTable(&model.ViteConfig{}) {
		return nil
	}

	if db.Dialector.Name() != "postgres" {
		return nil
	}

	type columnRow struct {
		DataType string `gorm:"column:data_type"`
	}

	var row columnRow
	if err := db.Raw(
		`SELECT data_type FROM information_schema.columns
		 WHERE table_schema = current_schema()
		   AND table_name = ?
		   AND column_name = ?`,
		"vite_config", "value",
	).Scan(&row).Error; err != nil {
		return fmt.Errorf("inspect vite_config.value type: %w", err)
	}

	if strings.EqualFold(row.DataType, "text") {
		return nil
	}

	if err := db.Exec(`ALTER TABLE "vite_config" ALTER COLUMN "value" TYPE TEXT`).Error; err != nil {
		return fmt.Errorf("alter vite_config.value to text: %w", err)
	}

	return nil
}

func migrateSpeedLimitTunnelBinding(db *gorm.DB) error {
	if db == nil {
		return errors.New("nil db")
	}

	if !db.Migrator().HasTable(&model.SpeedLimit{}) {
		return nil
	}

	if err := db.Model(&model.SpeedLimit{}).
		Where("tunnel_id IS NOT NULL OR tunnel_name IS NOT NULL").
		UpdateColumns(map[string]interface{}{
			"tunnel_id":   nil,
			"tunnel_name": nil,
		}).Error; err != nil {
		return fmt.Errorf("clear speed_limit tunnel binding: %w", err)
	}

	return nil
}

func migratePostgresTrafficInt64Columns(db *gorm.DB) error {
	if db == nil {
		return errors.New("nil db")
	}

	if db.Dialector.Name() != "postgres" {
		return nil
	}

	type trafficColumn struct {
		TableName  string
		ColumnName string
	}

	columns := []trafficColumn{
		{TableName: "user", ColumnName: "flow"},
		{TableName: "user", ColumnName: "in_flow"},
		{TableName: "user", ColumnName: "out_flow"},
		{TableName: "forward", ColumnName: "in_flow"},
		{TableName: "forward", ColumnName: "out_flow"},
		{TableName: "statistics_flow", ColumnName: "flow"},
		{TableName: "statistics_flow", ColumnName: "total_flow"},
		{TableName: "tunnel", ColumnName: "flow"},
		{TableName: "user_tunnel", ColumnName: "flow"},
		{TableName: "user_tunnel", ColumnName: "in_flow"},
		{TableName: "user_tunnel", ColumnName: "out_flow"},
		{TableName: "peer_share", ColumnName: "max_bandwidth"},
		{TableName: "peer_share", ColumnName: "current_flow"},
	}

	for _, column := range columns {
		if err := alterPostgresColumnToBigIntIfNeeded(db, column.TableName, column.ColumnName); err != nil {
			return err
		}
	}

	return nil
}

func migrateTunnelMetricBucketUniqueIndex(db *gorm.DB) error {
	if db == nil {
		return errors.New("nil db")
	}

	if !db.Migrator().HasTable(&model.TunnelMetric{}) {
		return nil
	}

	return db.Transaction(func(tx *gorm.DB) error {
		// Only do the heavier dedupe work when needed.
		var dupGroups int64
		q := `
			SELECT COUNT(1) AS cnt
			FROM (
				SELECT 1
				FROM tunnel_metric
				GROUP BY tunnel_id, node_id, timestamp
				HAVING COUNT(*) > 1
			) t
		`
		if err := tx.Raw(q).Scan(&dupGroups).Error; err != nil {
			return fmt.Errorf("inspect tunnel_metric duplicates: %w", err)
		}

		if dupGroups > 0 {
			switch tx.Dialector.Name() {
			case "postgres":
				sql := `
					WITH agg AS (
						SELECT MIN(id) AS keep_id,
						       tunnel_id,
						       node_id,
						       timestamp,
						       SUM(bytes_in) AS bytes_in,
						       SUM(bytes_out) AS bytes_out,
						       SUM(connections) AS connections,
						       SUM(errors) AS errors,
						       AVG(avg_latency_ms) AS avg_latency_ms
						FROM tunnel_metric
						GROUP BY tunnel_id, node_id, timestamp
						HAVING COUNT(*) > 1
					), updated AS (
						UPDATE tunnel_metric tm
						SET bytes_in = agg.bytes_in,
						    bytes_out = agg.bytes_out,
						    connections = agg.connections,
						    errors = agg.errors,
						    avg_latency_ms = agg.avg_latency_ms
						FROM agg
						WHERE tm.id = agg.keep_id
						RETURNING tm.id
					)
					DELETE FROM tunnel_metric tm
					USING agg
					WHERE tm.tunnel_id = agg.tunnel_id
					  AND tm.node_id = agg.node_id
					  AND tm.timestamp = agg.timestamp
					  AND tm.id <> agg.keep_id
				`
				if err := tx.Exec(sql).Error; err != nil {
					return fmt.Errorf("dedupe tunnel_metric buckets: %w", err)
				}
			default:
				// SQLite (and other) path.
				if err := tx.Exec(`DROP TABLE IF EXISTS tunnel_metric_dedupe`).Error; err != nil {
					return fmt.Errorf("prepare tunnel_metric dedupe table: %w", err)
				}
				if err := tx.Exec(`
					CREATE TEMP TABLE tunnel_metric_dedupe AS
					SELECT MIN(id) AS keep_id,
					       tunnel_id,
					       node_id,
					       timestamp,
					       SUM(bytes_in) AS bytes_in,
					       SUM(bytes_out) AS bytes_out,
					       SUM(connections) AS connections,
					       SUM(errors) AS errors,
					       AVG(avg_latency_ms) AS avg_latency_ms
					FROM tunnel_metric
					GROUP BY tunnel_id, node_id, timestamp
					HAVING COUNT(*) > 1
				`).Error; err != nil {
					return fmt.Errorf("build tunnel_metric dedupe table: %w", err)
				}
				if err := tx.Exec(`
					UPDATE tunnel_metric
					SET bytes_in = (SELECT bytes_in FROM tunnel_metric_dedupe d WHERE d.keep_id = tunnel_metric.id),
					    bytes_out = (SELECT bytes_out FROM tunnel_metric_dedupe d WHERE d.keep_id = tunnel_metric.id),
					    connections = (SELECT connections FROM tunnel_metric_dedupe d WHERE d.keep_id = tunnel_metric.id),
					    errors = (SELECT errors FROM tunnel_metric_dedupe d WHERE d.keep_id = tunnel_metric.id),
					    avg_latency_ms = (SELECT avg_latency_ms FROM tunnel_metric_dedupe d WHERE d.keep_id = tunnel_metric.id)
					WHERE id IN (SELECT keep_id FROM tunnel_metric_dedupe)
				`).Error; err != nil {
					return fmt.Errorf("update tunnel_metric deduped rows: %w", err)
				}
				if err := tx.Exec(`
					DELETE FROM tunnel_metric
					WHERE id IN (
						SELECT tm.id
						FROM tunnel_metric tm
						JOIN tunnel_metric_dedupe d
						  ON tm.tunnel_id = d.tunnel_id
						 AND tm.node_id = d.node_id
						 AND tm.timestamp = d.timestamp
						WHERE tm.id <> d.keep_id
					)
				`).Error; err != nil {
					return fmt.Errorf("delete tunnel_metric duplicates: %w", err)
				}
				_ = tx.Exec(`DROP TABLE IF EXISTS tunnel_metric_dedupe`).Error
			}
		}

		// Uniqueness is required for safe upsert on (tunnel_id, node_id, timestamp).
		if err := tx.Exec(
			`CREATE UNIQUE INDEX IF NOT EXISTS uidx_tunnel_metric_bucket ON tunnel_metric(tunnel_id, node_id, timestamp)`,
		).Error; err != nil {
			return fmt.Errorf("create tunnel_metric unique index: %w", err)
		}
		return nil
	})
}

func alterPostgresColumnToBigIntIfNeeded(db *gorm.DB, tableName, columnName string) error {
	if db == nil {
		return errors.New("nil db")
	}

	if tableName == "" || columnName == "" {
		return errors.New("empty table or column name")
	}

	type columnRow struct {
		DataType string `gorm:"column:data_type"`
	}

	var row columnRow
	if err := db.Raw(
		`SELECT data_type FROM information_schema.columns
		 WHERE table_schema = current_schema()
		   AND table_name = ?
		   AND column_name = ?`,
		tableName, columnName,
	).Scan(&row).Error; err != nil {
		return fmt.Errorf("inspect %s.%s type: %w", tableName, columnName, err)
	}

	if row.DataType == "" || strings.EqualFold(row.DataType, "bigint") {
		return nil
	}
	if !strings.EqualFold(row.DataType, "integer") {
		return nil
	}

	if err := db.Exec(fmt.Sprintf(
		"ALTER TABLE %s ALTER COLUMN %s TYPE BIGINT",
		quoteSQLIdentifier(tableName),
		quoteSQLIdentifier(columnName),
	)).Error; err != nil {
		return fmt.Errorf("alter %s.%s to bigint: %w", tableName, columnName, err)
	}

	return nil
}

func ensurePostgresIDDefaults(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		return nil
	}
	type idRow struct {
		TableSchema string
		TableName   string
	}
	var rows []idRow
	err := db.Table("information_schema.table_constraints AS tc").
		Select("c.table_schema, c.table_name").
		Joins("JOIN information_schema.key_column_usage AS kcu ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema").
		Joins("JOIN information_schema.columns AS c ON c.table_schema = kcu.table_schema AND c.table_name = kcu.table_name AND c.column_name = kcu.column_name").
		Where("tc.constraint_type = ?", "PRIMARY KEY").
		Where("kcu.column_name = ?", "id").
		Where("c.data_type IN ?", []string{"integer", "bigint"}).
		Where("c.is_identity = ?", "NO").
		Where("c.table_schema = current_schema()").
		Order("c.table_name ASC").
		Scan(&rows).Error
	if err != nil {
		return fmt.Errorf("discover postgres id columns: %w", err)
	}

	for _, r := range rows {
		if err := ensurePostgresTableIDDefault(db, r.TableSchema, r.TableName); err != nil {
			return fmt.Errorf("repair %s.%s id default: %w", r.TableSchema, r.TableName, err)
		}
	}
	return nil
}

func ensurePostgresTableIDDefault(db *gorm.DB, schemaName, tableName string) error {
	type defaultRow struct {
		ColumnDefault sql.NullString `gorm:"column:column_default"`
	}
	var row defaultRow
	err := db.Table("information_schema.columns").
		Select("column_default").
		Where("table_schema = ? AND table_name = ? AND column_name = 'id'", schemaName, tableName).
		Limit(1).
		Scan(&row).Error
	if err != nil {
		return err
	}
	defaultExpr := row.ColumnDefault

	hasNextvalDefault := defaultExpr.Valid && strings.Contains(strings.ToLower(defaultExpr.String), "nextval(")
	seqRef := ""
	if hasNextvalDefault {
		seqRef = extractNextvalRegclass(defaultExpr.String)
	}

	if !hasNextvalDefault || seqRef == "" {
		seqName := tableName + "_id_seq"
		if err := db.Exec(fmt.Sprintf("CREATE SEQUENCE IF NOT EXISTS %s.%s", quoteSQLIdentifier(schemaName), quoteSQLIdentifier(seqName))).Error; err != nil {
			return err
		}
		seqRef = schemaName + "." + seqName
		if err := db.Exec(fmt.Sprintf(
			"ALTER TABLE %s.%s ALTER COLUMN id SET DEFAULT nextval(%s::regclass)",
			quoteSQLIdentifier(schemaName), quoteSQLIdentifier(tableName), quoteSQLLiteral(seqRef),
		)).Error; err != nil {
			return err
		}
		if err := db.Exec(fmt.Sprintf(
			"ALTER SEQUENCE %s.%s OWNED BY %s.%s.id",
			quoteSQLIdentifier(schemaName), quoteSQLIdentifier(seqName),
			quoteSQLIdentifier(schemaName), quoteSQLIdentifier(tableName),
		)).Error; err != nil {
			return err
		}
	}

	return syncPostgresTableIDSequence(db, schemaName, tableName, seqRef)
}

func syncPostgresTableIDSequence(db *gorm.DB, schemaName, tableName, seqRef string) error {
	type maxRow struct {
		MaxID int64 `gorm:"column:max_id"`
	}
	var row maxRow
	qualifiedTable := fmt.Sprintf("%s.%s", quoteSQLIdentifier(schemaName), quoteSQLIdentifier(tableName))
	err := db.Table(qualifiedTable).
		Select("COALESCE(MAX(id), 0) AS max_id").
		Scan(&row).Error
	if err != nil {
		return err
	}
	maxID := row.MaxID

	setVal := maxID
	isCalled := true
	if maxID <= 0 {
		setVal = 1
		isCalled = false
	}

	return db.Exec(`SELECT setval(?::regclass, ?, ?)`, seqRef, setVal, isCalled).Error
}

func extractNextvalRegclass(defaultExpr string) string {
	nextvalIdx := strings.Index(strings.ToLower(defaultExpr), "nextval(")
	if nextvalIdx < 0 {
		return ""
	}
	expr := defaultExpr[nextvalIdx:]
	firstQuote := strings.Index(expr, "'")
	if firstQuote < 0 {
		return ""
	}
	expr = expr[firstQuote+1:]
	secondQuote := strings.Index(expr, "'")
	if secondQuote < 0 {
		return ""
	}
	return strings.TrimSpace(expr[:secondQuote])
}

func quoteSQLIdentifier(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

func quoteSQLLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
