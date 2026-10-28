# AGENTS

FLYX — traffic forwarding panel: Go admin API + Vite/React UI + Go agent.

## Structure

| Dir | Role | Entry |
|-----|------|-------|
| `go-backend/` | Admin API (GORM + SQLite/PG, net/http) | `cmd/paneld/main.go` |
| `go-gost/` | Forwarding agent (forked GOST) | `main.go` |
| `go-gost/x/` | Protocol handlers/dialers/listeners (own module) | — |
| `vite-frontend/` | React dashboard (shadcn bridge + Tailwind v4) | `src/App.tsx` |
| `doc/` | User docs (VitePress, deployed to GitHub Pages) | `.vitepress/config.mts` |

`go-gost/go.mod` uses `replace github.com/go-gost/x => ./x`.

## Commands

```bash
# Backend
(cd go-backend && go run ./cmd/paneld)        # SERVER_ADDR defaults to :6365
(cd go-backend && make build)
(cd go-backend && go test ./...)

# Frontend
(cd vite-frontend && pnpm install)
(cd vite-frontend && pnpm run dev)             # host 0.0.0.0:3000
(cd vite-frontend && pnpm run build)           # tsc && vite build
(cd vite-frontend && pnpm run lint)            # eslint --fix (no typecheck command)

# Agent
(cd go-gost && go run .)

# Docs
(cd doc && npm ci && npm run dev)            # npm run build -> doc/.vitepress/dist
```

## Conventions

- **Auth**: raw JWT in `Authorization` header — **no `Bearer` prefix** (both frontend and backend).
- **API envelope**: all responses `{code, msg, data, ts}` (code 0 = success).
- **Frontend UI**: import from `src/shadcn-bridge/heroui/*`, never `@heroui/*` or `@nextui-org/*`.
- **Tailwind theme**: `globals.css` must import `tailwind-theme.pcss` or semantic classes break.
- **Backend DB**: handlers use Repository methods, never `repo.DB()` directly.
- **GORM models**: always define `TableName()` (GORM pluralizes by default).
- **GORM tags**: no `type:jsonb` or `type:serial` (SQLite incompatible).
- **Commits**: no `Co-Authored-By: Claude …` trailer — the owner doesn't want Claude listed as a contributor. Commit as the owner (set the repo's `user.name` / `user.email` to the owner's GitHub identity first; a Claude author makes GitHub add the co-author line back on squash merge).
- **Go versions**: `go.mod` says 1.25.0 for all three modules; CI and the Docker images build with Go 1.25.

## Releases

- Before triggering a release (`docker-build.yml`, input `version`), add a `## <version>` section to `doc/changelog.md` (Chinese, user-facing, date line under the heading, a **升级** line when node agents must be upgraded) and merge it: the release notes take that section, falling back to the commit list since the previous tag (`.github/scripts/release-notes.sh`).

## Anti-patterns

- `install.sh` / `panel_install.sh`: `REPO` must stay `kure29/flyx` (this repo is independent of upstream; don't sync it back). The release job rewrites `REPO`, the version and `PINNED_VERSION` in the release copies, so don't hand-edit those for a release.
- Don't edit `go-gost/x/internal/util/grpc/proto/*.pb.go` (generated).
- Don't add frontend tests (no Vitest/Jest configured).
- Don't reintroduce `@heroui/*` or `@nextui-org/*` packages.

## Testing

- Backend: `(cd go-backend && go test ./...)` — includes contract tests in `tests/contract/`.
- Frontend: no test infrastructure.
- CI runs `gofmt -l`, `go vet` and `go test ./...` on `go-backend`, `eslint .` on the frontend, plus one PostgreSQL contract test (env var `FLYX_POSTGRES_TEST_DSN`, legacy `FLVX_POSTGRES_TEST_DSN` accepted).

## Build quirks

- `vite-frontend` uses `rolldown-vite` (Rust bundler), not standard Vite.
- `vite.config.ts`: PWA via `vite-plugin-pwa`; build uses default minify and treeshake, no sourcemaps.
- CI builds `go-gost` with `CGO_ENABLED=0` then compresses with UPX `--best --lzma`.
- The release also pushes the node agent image `ghcr.io/<owner>/flyx-agent` from `go-gost/docker/` (same UPX binaries). In the container (`FLYX_AGENT_CONTAINER=1` (legacy `FLUX_AGENT_CONTAINER` accepted)) there is no systemd: upgrade/rollback swap the running agent binary (`/etc/flyx-agent/flyx-agent` for new installs, legacy path retained for upgrades) and stop the agent so the restart policy brings it back.
