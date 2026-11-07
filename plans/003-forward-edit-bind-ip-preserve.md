# 003 Forward Edit Bind IP Preserve

## Checklist

- [x] Confirm forward edit flow and identify why untouched listen IP gets overwritten.
- [x] Update frontend forward edit submit logic to only send `inIp` when user explicitly changes listen IP.
- [x] On tunnel switch in edit form, reset listen IP to default unless user reselects.
- [x] Update backend forward update logic to preserve existing `forward_port.in_ip` when request omits `inIp` and tunnel is unchanged.
- [x] Keep backend behavior explicit: if `inIp` is sent (including empty), apply requested value; if tunnel changed with no `inIp`, use default bind.
- [x] Add regression tests for preserved bind-IP reconstruction helper behavior.
- [x] Run focused frontend/backend checks for touched files.
