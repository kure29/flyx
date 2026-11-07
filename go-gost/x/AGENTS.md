# go-gost/x

Local fork of `github.com/go-gost/x`. Standalone Go module, used by `go-gost/` via `replace => ./x`.

## Key packages

| Dir | Role |
|-----|------|
| `handler/` | Protocol handlers (socks, http, tunnel, relay, ...) |
| `listener/` | Inbound listeners (tcp, udp, tun, tap, redirect, ...) |
| `dialer/` | Outbound dialers (tcp, tls, ws, quic, ...) |
| `connector/` | Outbound connect implementations |
| `service/` | Service wrappers + reporting hooks |
| `socket/` | WebSocket reporter / panel integration |
| `config/` | Config model + parsing/load/reload |
| `registry/` | Component registries (`Register{Type}(name, creator)`) |
| `api/` | Gin management API + embedded swagger docs |
| `limiter/` | Traffic/rate/conn limiters |
| `internal/` | gRPC proto, net utils, sniffing, TLS |

## Conventions

- Each protocol follows `{type}.go` + `metadata.go` pattern.
- OS-specific code uses `name_[os].go` suffix (e.g. `tun_linux.go`).
- Run Go tooling from this directory for module resolution issues.

## Anti-patterns

- Don't edit `internal/util/grpc/proto/*.pb.go` or `*_grpc.pb.go` (generated).

## Commands

```bash
go test ./...
```
