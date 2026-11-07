# 008 Frontend Missing Speed Limit Consistency

## Checklist

- [x] Review forward and user tunnel submit flows for missing speed limit behavior.
- [x] Make frontend normalize deleted `speedId` to `null` before submit in both pages.
- [x] Add consistent non-blocking warning toast when deleted speed rule is auto-cleared.
- [x] Verify touched frontend files pass lint checks.
