# go-backend

Admin API for FLYX. Go + net/http + GORM (SQLite/PostgreSQL).

## Structure

| Dir | Role |
|-----|------|
| `cmd/paneld/main.go` | Entry point, HTTP server + WebSocket |
| `internal/http/router.go` | Route registration (`http.ServeMux`) + middleware chain |
| `internal/http/handler/` | API handlers |
| `internal/http/middleware/` | JWT, CORS, logging, recover |
| `internal/http/response/` | JSON envelope helpers |
| `internal/store/model/model.go` | All GORM models (single file) |
| `internal/store/repo/` | Repository layer (never access DB directly) |
| `internal/auth/` | Auth logic |
| `tests/contract/` | Integration tests |

## Conventions

- **Auth**: raw JWT in `Authorization` header — no `Bearer` prefix.
- **API envelope**: `{code, msg, data, ts}`, code 0 = success.
- **Repository pattern**: handlers call repo methods, never `repo.DB()` directly.
- **GORM**: `TableName()` on every model (GORM pluralizes by default).
- **GORM tags**: no `type:jsonb` or `type:serial` (SQLite incompatible).
- **SQLite**: `MaxOpenConns(1)`, WAL mode, `busy_timeout=5000`.
- **Schema**: created via `autoMigrateAll()` at startup, no hand-written DDL.
- **PostgreSQL**: set `DB_TYPE=postgres` and `DATABASE_URL` env vars.
- **Config**: all from environment variables.

## Commands

```bash
go run ./cmd/paneld           # SERVER_ADDR defaults to :6365
make build
go test ./...                 # includes contract tests
go test ./tests/contract/...  # contract tests only
```
