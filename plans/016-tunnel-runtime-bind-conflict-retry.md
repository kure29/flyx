# 016 Tunnel Runtime Bind Conflict Retry

## Checklist

- [x] Confirm tunnel `connectIp` precedence remains `connectIp > node tcp_listen_addr` for runtime service listen address.
- [x] Add tunnel runtime `address already in use` recovery that deletes the stale service and retries `AddService`.
- [x] Keep non-bind failures unchanged and avoid altering tunnel chain apply semantics.
- [x] Add regression tests for tunnel service address precedence and bind-conflict retry behavior.
- [x] Run focused backend handler tests and record the result.
- [ ] Add a contract test that simulates node-side `address already in use` during tunnel update and verifies retry success.
- [ ] Investigate whether forward update `address already in use` reports are only tunnel-redeploy linkage or also an independent forward path.
- [x] Add a contract test that simulates node-side `address already in use` during tunnel update and verifies retry success.
- [x] Investigate whether forward update `address already in use` reports are only tunnel-redeploy linkage or also an independent forward path.

## Test Record

- Command: `cd go-backend && go test ./internal/http/handler/...`
- Result: passed.
- Command: `cd go-backend && go test ./tests/contract/... -run 'TestTunnelUpdateRecoversFromAddressInUseContract|TestForwardCreateRollbackWhenServiceDispatchReturnsAddressInUseContract|TestForwardUpdateIgnoresDeletedSpeedLimitContract'`
- Result: passed.
- Command: `cd go-backend && go test ./tests/contract/... -run 'TestForwardUpdateRecoversFromAddressInUseContract|TestTunnelUpdateRecoversFromAddressInUseContract'`
- Result: passed.
- Command: `cd go-backend && go test ./internal/http/handler/... && go test ./tests/contract/... -run 'TestForwardUpdateRecoversFromAddressInUseContract|TestTunnelUpdateRecoversFromAddressInUseContract'`
- Result: passed.

## Investigation Note

- Forward update still has its own independent `address already in use` recovery path in `syncForwardServicesWithWarnings` / `rebindForwardServiceOnSelfOccupiedPort`; tunnel update linkage is not the only possible source of the symptom.
- Tunnel update also triggers downstream forward `UpdateService` for bound forwards, so users can still observe the same error around a tunnel edit even when the failing runtime is on the tunnel side.
- Real node output can collapse spaces into variants like `address alreadyin use` / `cannotassignrequestedaddress`; bind-conflict detection now normalizes whitespace before classifying the error.
- Forward self-heal cleanup now deletes every candidate runtime name variant instead of stopping after the first successful delete, which avoids leaving sibling `_tcp`/`_udp` services behind to keep the port occupied.
