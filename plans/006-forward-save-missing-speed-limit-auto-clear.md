# 006 Forward Save Missing Speed Limit Auto Clear

## Checklist

- [x] Locate forward create/update speed limit validation path that blocks save when speed rule is deleted.
- [x] Change forward save behavior to auto-clear missing `speedId` instead of returning "限速规则不存在".
- [x] Add contract test coverage for editing a forward after its referenced speed limit is deleted.
- [x] Run focused contract tests for forward save behavior.
