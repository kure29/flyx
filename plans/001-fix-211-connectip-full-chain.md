# 001 Fix 211 ConnectIP Full Chain

## Checklist

- [x] Analyze connectIp/inIp full chain across diagnosis/runtime/redeploy paths.
- [x] Fix diagnosis target resolution to honor selected `connectIp` for chain hops.
- [x] Fix tunnel state reconstruction to preserve `connectIp` on chain/out nodes.
- [x] Add contract regression tests for normal + stream diagnosis target IP behavior.
- [x] Add handler regression test for redeploy state reconstruction preserving `connectIp`.
- [x] Run backend handler and contract test suites.

## Notes

- Diagnosis now uses `chain_tunnel.connect_ip` for both stream start preview and runtime probing.
- Redeploy/batch-redeploy no longer drops `connectIp` during `reconstructTunnelState`.
