# 055 - 隧道质量定时探测 + 实时展示 + 历史图表

## 背景
当前隧道质量检测是手动触发的：用户点击"诊断"按钮 → 后端调用节点 TcpPing → 返回结果。
需求：改为**后端定时（每10秒）自动探测**所有启用隧道的质量（入口→出口延迟、出口→Bing延迟），
结果保留历史（24h），前端隧道 Tab 实时展示 + 图表历史趋势。

## 设计原则：与服务监控复用

| 复用点 | 服务监控 | 隧道质量 |
|--------|---------|---------|
| 调度方式 | `health.Checker.Start(ctx)` via `jobs.go` | `tunnelQualityProber.Start(ctx)` via `jobs.go` |
| 存储模式 | `service_monitor_result` (history, insert) | `tunnel_quality` (history, insert) |
| 清理方式 | `PruneServiceMonitorResults(olderThanMs)` | `PruneTunnelQualityResults(olderThanMs)` |
| 最新查询 | `GetLatestServiceMonitorResults()` (window func) | `GetLatestTunnelQualities()` (window func) |
| 历史查询 | `GetServiceMonitorResults(id, limit)` | `GetTunnelQualityHistory(id, start, end)` |
| API 模式 | `GET /monitor/services/{id}/results` | `GET /monitor/tunnels/{id}/quality` |
| 前端图表 | Recharts LineChart (延迟趋势) | Recharts LineChart (同样模式) |

## 任务清单

- [x] 1. `TunnelQuality` model 改为历史存储（composite index, 非 unique）
- [x] 2. Repo 改为 insert（非 upsert），复用服务监控的查询模式
- [x] 3. 添加 `PruneTunnelQualityResults` + `GetLatestTunnelQualities` + `GetTunnelQualityHistory`
- [x] 4. Prober 生命周期集成到 `jobs.go`（与 healthCheck 同级）
- [x] 5. Prober 添加 24h 清理周期
- [x] 6. 添加 API `GET /monitor/tunnels/{id}/quality` 返回历史
- [x] 7. 前端添加 `getMonitorTunnelQualityHistory()` API
- [x] 8. 前端详情页添加质量趋势图表（复用服务监控图表组件模式）
- [x] 9. Go 编译 + 测试通过
- [x] 10. TypeScript 编译通过
