# Custom Best-Exit Probe Target Design

Date: 2026-05-01
Status: Approved design

## Goal

Allow each tunnel to define the TCP target used for exit-side quality probing instead of always probing `www.bing.com:443`.

The custom target must be used consistently by:

- `best` exit scoring: each exit probes the configured target to measure exit-to-public quality.
- Tunnel quality monitoring: the existing exit-side quality check probes the same configured target.

If a tunnel does not configure a target, behavior remains compatible with today: `www.bing.com:443`.

## Non-Goals

- Do not add HTTP/HTTPS request probing in this phase. The probe remains TCP host/port measurement.
- Do not add a global default target setting in this phase.
- Do not require existing tunnels to be edited or migrated manually.
- Do not change the `best` switching thresholds, confirmation rounds, cooldowns, or runtime chain ordering semantics.
- Do not add frontend test infrastructure.

## User-Facing Behavior

Each tunnel form gets a compact quality target section:

- Host input, placeholder `www.bing.com`.
- Port input, placeholder `443`.
- Helper text: this target is used for tunnel quality detection and `best` optimal-exit scoring; leaving it empty uses `www.bing.com:443`.

Tunnel list/get responses include the configured target so edit forms can round-trip it. The UI displays the effective target near quality/best-exit information as `测试目标：host:port`.

## Data Model

Add nullable/default-compatible fields to `model.Tunnel`:

- `ProbeTargetHost string` mapped to `probe_target_host`, `type:text`, default `''`.
- `ProbeTargetPort int` mapped to `probe_target_port`, default `0`.

Effective target resolution:

- If `ProbeTargetHost` is non-empty and `ProbeTargetPort` is valid, use it.
- Otherwise use `www.bing.com:443`.

The existing `TunnelQuality` persisted fields `exit_to_bing_latency` and `exit_to_bing_loss` remain unchanged for compatibility. They will semantically mean exit-to-configured-test-target after this change. API/UI labels should avoid saying `Bing` for new displays.

## Validation

On create/update:

- Empty host and empty/zero port are allowed and mean default target.
- If either host or port is set, validate both as a pair.
- Host is trimmed and must not contain URL scheme, path, query, or whitespace.
- Host can be a domain, IPv4, or IPv6 literal. Bracketed IPv6 input should be normalized by removing surrounding brackets.
- Port must be an integer from `1` to `65535`.
- Do not perform network probing during save; external network failures must not block configuration changes.

Errors should be specific, for example:

- `测试目标 Host 不能为空`
- `测试目标端口必须是 1-65535`
- `测试目标 Host 不能包含协议或路径`

## Backend Flow

Introduce a small value/helper near the tunnel quality and best-exit code:

```go
type tunnelProbeTarget struct {
    Host string
    Port int
}
```

Helpers:

- `defaultTunnelProbeTarget() tunnelProbeTarget` returns `www.bing.com:443`.
- `normalizeTunnelProbeTarget(host string, port int) (tunnelProbeTarget, bool, error)` validates user input; the boolean indicates whether the user explicitly configured a target.
- `effectiveTunnelProbeTarget(tunnel *model.Tunnel) tunnelProbeTarget` returns configured target or default.

Use the effective target in `tunnelQualityProber.probeTunnel`:

- Type 1 and unknown tunnel fallback probes entry node to effective target instead of hardcoded Bing.
- Type 2 probes the selected/current exit node to effective target instead of hardcoded Bing.
- `probeBestExitOwners` receives the effective target and passes it into best-exit owner scoring.

Use the effective target in `evaluateBestExitOwner`:

- Owner-to-exit measurement stays unchanged.
- Exit-to-public measurement probes `target.Host:target.Port` instead of `bestExitPublicTargetHost:bestExitPublicTargetPort`.
- The per-round public probe cache key must include node ID plus target host and port so future extensions cannot reuse measurements across different targets.

## API Shape

Tunnel list/get data includes:

```json
{
  "probeTargetHost": "example.com",
  "probeTargetPort": 443
}
```

For old/default tunnels, return empty host and `0` to represent `use default`. The edit form must preserve default-as-empty unless the user explicitly saves a custom target.

Quality monitoring response includes effective target display metadata:

```json
{
  "probeTargetHost": "www.bing.com",
  "probeTargetPort": 443
}
```

Existing `exitToBingLatency` and `exitToBingLoss` keys stay to avoid breaking frontend and external consumers.

## Frontend Flow

Extend `ChainTunnel` only if needed for node-level data; the target belongs to the tunnel, so `Tunnel` and `TunnelForm` get:

- `probeTargetHost?: string`
- `probeTargetPort?: number`

On edit:

- Populate form fields from tunnel response.
- Empty or zero means default target.

On submit:

- Trim host.
- Convert blank port to `0`.
- Send `probeTargetHost` and `probeTargetPort` with create/update payload.

Display:

- In the form helper, show default target behavior.
- In quality/best-exit display areas, avoid `Bing` wording; prefer `测试目标` or the concrete `host:port`.

## Error Handling

- Invalid target input returns a normal API error envelope with a specific message.
- Probe failures use existing quality error paths and best-exit scoring failure entries.
- If all exit-to-target probes fail, best-exit behavior remains the same as today when all Bing probes fail: no valid best decision is applied from that round.

## Testing

Backend tests:

- Normalize default target when host/port are empty.
- Reject partial host/port configuration and invalid port ranges.
- Reject host values with URL scheme/path/whitespace.
- Create/update tunnel persists `probeTargetHost` and `probeTargetPort`.
- `ListTunnels` returns target fields.
- `tunnelQualityProber` uses configured target instead of `www.bing.com:443`.
- `best` scoring uses configured target for exit-to-target probes.
- Empty target preserves old default `www.bing.com:443` behavior.

Frontend verification:

- `pnpm run build` passes.
- Manual UI check: create/edit tunnel with blank target and custom target, confirm payload and round-trip display.

## Rollout And Compatibility

- Existing tunnels continue using `www.bing.com:443` because empty target resolves to default.
- SQLite/PostgreSQL schema changes are handled by existing auto-migration.
- Historical `TunnelQuality` rows keep existing columns and are not rewritten.
- No runtime agent change is required; the panel already performs these quality probes through existing node ping APIs.

## Open Decisions

None. User-approved decisions:

- Per-tunnel fields are `host + port`.
- The target applies to both `best` scoring and tunnel quality monitoring.
- Probe type remains TCP host/port.
- Empty target defaults to `www.bing.com:443`.
