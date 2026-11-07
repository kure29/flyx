# BACKEND HTTP HANDLER KNOWLEDGE BASE

**Generated:** Fri Mar 20 2026
**Commit:** f45f960
**Branch:** main
**Tag:** 2.1.9-beta6

## OVERVIEW
HTTP request handlers for FLYX Admin API. Core business logic layer.
**Stack:** Go 1.24, net/http, GORM via Repository pattern.

## STRUCTURE
```
handler/
├── handler.go        # Main Handler struct, login/captcha, job scheduling
├── control_plane.go  # Record lookups, access checks, sending commands to nodes
├── forward_services.go # Building and syncing a forward's services on nodes
├── forward_limiters.go # Speed/connection limiters on nodes
├── diagnosis.go      # Forward/tunnel diagnosis (ping via nodes, path checks)
├── federation.go     # Node import, federated tunnel create, remote status sync
├── federation_share.go    # Sharing a node with another panel (provider side CRUD)
├── federation_provider.go # Runtime API a consumer panel calls (reserve port, apply role, command)
├── federation_auth.go     # Peer auth, client IP and allow-list checks
├── flow_policy.go    # Traffic policy API
├── jobs.go           # Background job management (sync, cleanup)
├── user_mutations.go     # Users and user↔tunnel grants
├── node_mutations.go     # Node CRUD, install, ordering
├── tunnel_mutations.go   # Tunnel CRUD, ports, batch ops
├── tunnel_state.go       # Building/validating tunnel state and chains
├── tunnel_runtime.go     # Pushing tunnels to nodes (chains, services, dial addresses)
├── federation_runtime.go # Federation runtime apply/cleanup
├── forward_mutations.go  # Forward CRUD, pause/resume, batch ops, forward ports
├── group_mutations.go    # Groups, permissions, speed limits
├── request_helpers.go    # Request-body parsing helpers, batch results
└── upgrade.go        # System upgrade API
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| **User/Tunnel/Forward CRUD** | `*_mutations.go` | One file per resource; tunnel runtime in `tunnel_runtime.go` |
| **Login/Captcha** | `handler.go` | Login flow, captcha verification |
| **Federation** | `federation*.go` | Shares and provider API in `federation_share.go` / `federation_provider.go`; consumer side in `federation_runtime.go` |
| **Traffic Policies** | `flow_policy.go` | Flow limiting, quota management |
| **Background Jobs** | `jobs.go` | Scheduled sync/cleanup tasks |
| **Node commands** | `control_plane.go` | `sendNodeCommand*`; forward services in `forward_services.go` |

## CONVENTIONS
- Inherits from parent: GORM via Repository pattern, JWT in Authorization header.
- Keep files per domain; split a file when it grows past ~1500 lines instead of adding to a catch-all.
- Uses `repo.Repository` for DB access via `h.repo.XXX()` methods.
- Handlers never call `repo.DB()` directly — all queries go through Repository methods.
- Domain-driven file split: one file per functional area (federation, jobs, etc.).

## ANTI-PATTERNS
- Do NOT let handlers call `repo.DB()` directly — add a Repository method instead.
- Do NOT change handler signatures without updating router.go.

## COMMANDS
```bash
cd go-backend
go test ./internal/http/handler/...
```
