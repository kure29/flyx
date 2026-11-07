# 005 Forward Invalid BindIP Fallback Default

## Checklist

- [x] Split forward service bind failures into address-in-use and cannot-assign classes.
- [x] Keep self-occupy release/rebind only for address-in-use conflicts.
- [x] Add fallback path for cannot-assign: switch to default listener bind and retry service creation.
- [x] Persist fallback result to DB by clearing `forward_port.in_ip` for affected node+port.
- [x] Return non-blocking warning in forward update response when fallback occurs.
- [x] Show warning toast in forward edit UI while still treating operation as success.
- [x] Run focused backend tests for touched handler/repo packages.
