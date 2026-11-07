# User Traffic Quota (Fix PR #308 Semantics)

- [x] Confirm new quota semantics: daily/monthly quota applies per user (aggregated across all tunnels), not per tunnel; overage pauses only that user's active forwards and blocks create/resume.
- [x] Backend schema: replace `tunnel_quota` usage with new `user_quota` persistence model + view types.
- [x] Repository: implement user quota read/write/increment/reset + daily/monthly window rollover.
- [x] Handler: wire quota accumulation into flow uploads, enforce overage (pause forwards + mark quota-disabled), and add admin reset API.
- [x] Jobs: run daily quota window rollover + release logic in existing 00:05 maintenance job.
- [x] Backup/import: persist quota config + quota-disable metadata on user backup payloads (not rolling usage).
- [x] Tests: update contract + handler job tests to validate quota blocking + reset window rollover.
- [x] Frontend: move quota inputs/usage/reset UI from tunnel management to user management; update API/types accordingly.
