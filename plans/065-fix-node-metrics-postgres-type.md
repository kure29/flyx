# Fix Node Metrics PostgreSQL Type Encoding

## Objective
Fix the PostgreSQL type encoding error (`failed to encode args[0]: unable to encode 5 into text format for text (OID 25)`) and `integer out of range` error when querying node metrics for time ranges greater than 1 hour.

## Tasks
- [x] Identify the problematic downsampled SQL aggregation in `GetNodeMetrics`.
- [x] Fix the `? AS node_id` placeholder which confused PostgreSQL's type inference by directly embedding the `nodeID` using `fmt.Sprintf("%d AS node_id")`.
- [x] Change all `CAST(X AS INTEGER)` to `CAST(X AS BIGINT)` to prevent 32-bit integer overflow on Unix millisecond timestamps in PostgreSQL.
- [x] Verify the build and tests pass.
- [ ] Commit all changes, create a new branch, push, create a Pull Request, merge the PR into `main`, and publish a new tag `2.1.9-rc9`.
