# go-gost

Forwarding agent (forked GOST v3). Uses local `x/` module via `replace github.com/go-gost/x => ./x`.

## Structure

| File | Role |
|------|------|
| `main.go` | Entry point, reads `config.json`, starts reporter + service |
| `config.go` | Panel integration config loader (addr, secret, ports) |
| `program.go` | GOST runtime: parse config, run/reload services (SIGHUP) |
| `x/` | Local fork of `github.com/go-gost/x` (own `go.mod`) |

## Conventions

- Two config files: panel integration uses `config.json`; forwarding uses GOST config (`gost.{json,yaml}`).
- `x/` is the extension surface — add handlers/listeners/dialers there, not in vendored deps.
- Agent→panel: WebSocket (real-time commands) + HTTP (batch traffic reports).
- All panel communication uses AES encryption with node `secret` as PSK.

## Anti-patterns

- Don't edit `x/internal/util/grpc/proto/*.pb.go` (generated protobuf).

## Commands

```bash
go run .
go test ./...
go build .
```
