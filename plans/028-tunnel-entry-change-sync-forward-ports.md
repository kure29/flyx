# 028 - Sync Forward Ports On Tunnel Entry Change

## Goal
When a tunnel's entry nodes change, automatically keep all forwards under that tunnel aligned by rebuilding `forward_port` rows to match the latest entry node set.

## Scope
- Backend only: update tunnel mutation flow to sync forward entry mappings.
- Preserve existing forward port and bind IP behavior:
  - Keep the existing forward port (choose the current min port in `forward_port`).
  - Preserve `in_ip` only when the tunnel has a single entry node; clear `in_ip` for multi-entry tunnels.

## Checklist
- [x] Capture old entry node IDs before tunnel update commits.
- [x] After commit, compare old/new entry node sets.
- [x] If changed, rebuild `forward_port` for all forwards in the tunnel.
- [x] Run `go test ./...` in `go-backend`.

## Notes
- Runtime redeploy/downlink is handled elsewhere; this change focuses on DB-level consistency of forward entry mappings.
