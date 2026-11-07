package repo

import (
	"database/sql"
	"go-backend/internal/config"
	"net/url"
	"strconv"
	"testing"
	"time"
)

func openTrafficPostgresTest(t *testing.T) *Repository {
	t.Helper()
	dsn := config.IdentityEnv("FLYX_POSTGRES_TEST_DSN", "FLVX_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires temporary PostgreSQL")
	}
	schema := "traffic_" + strconv.FormatInt(time.Now().UnixNano(), 36)
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
