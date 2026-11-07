# 018 User Tunnel Disable Status Sync

## Checklist

- [x] Inspect the user tunnel permission edit flow and identify why disabling an assigned tunnel appears ineffective.
- [x] Return the real `user_tunnel.status` value from the admin permission list API instead of a hardcoded enabled state.
- [x] Add contract coverage for the user tunnel permission list status mapping and run focused backend verification.

## Test Record

- Command: `cd go-backend && go test ./tests/contract/...`
- Result: passed.
