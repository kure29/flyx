# 057 Hide Remote Nodes from Monitor View

## Goal
Do not display remote nodes on the Monitoring page.

## Changes Made
- Modified `ListMonitorNodes` in `go-backend/internal/store/repo/repository_monitor_nodes.go` by adding a `.Where("is_remote = ?", 0)` constraint so that remote nodes (imported via Federation feature) are entirely excluded from the returned payload for API `/api/v1/monitor/nodes`.
- This efficiently removes remote nodes from both the grid/list displaying Node stats in the Monitoring tab and also eliminates remote nodes from the selection dropdown when creating new Service Monitors.

## Checklist
- [x] Identify how "remote node" is defined in the database structure (`IsRemote` = 1 or 0).
- [x] Add SQL query constraint to filter out remote nodes from the `/monitor/nodes` API response.
- [x] Verify changes compile successfully.
