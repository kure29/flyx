# 019 Federation Share Traffic Bigint Migration

## Checklist

- [x] Inspect federation share creation failure and identify the PostgreSQL `int4` overflow source.
- [x] Audit other traffic-related legacy PostgreSQL columns that may still be `integer` despite Go models using `int64`.
- [x] Add a schema migration that widens legacy traffic/quota columns from `integer` to `bigint`.
- [x] Add migration tests covering the new schema version branch and error propagation.
- [x] Run focused backend verification for the migration changes.

## Notes

- The reported failing value `536870912000` is 500 GiB in bytes and overflows PostgreSQL `int4`.
- The fix widens historical PostgreSQL traffic columns in `user`, `forward`, `statistics_flow`, `tunnel`, `user_tunnel`, and `peer_share` to `BIGINT` when needed.

## Test Record

- Command: `cd go-backend && go test ./internal/store/repo/...`
- Result: passed.
- Command: `cd go-backend && go test ./tests/contract/...`
- Result: passed.
