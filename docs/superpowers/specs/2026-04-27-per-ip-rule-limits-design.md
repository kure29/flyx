# 规则每 IP 连接数与限速设计

**日期**: 2026-04-27
**状态**: 待审核
**作者**: AI Assistant

## 概述

在转发规则的高级设置中新增两类每客户端 IP 限制：每 IP 最大连接数、每 IP 带宽限速。保留现有总量限制语义不变，新增字段只在用户显式配置时生效。

实现优先复用 GOST 已有能力：`climiters` 的 `$$ N` 表示每个客户端 IP 独立最大连接数；`limiters` 支持 IP/CIDR 级带宽桶，可用 `0.0.0.0/0` 和 `::/0` 实现默认覆盖所有 IPv4/IPv6 客户端的每 IP 带宽限速。

## 背景

当前 FLVX 已经支持规则级最大连接数和规则级限速，但这两个限制都是规则总量：

- `maxConn` 下发为 GOST `climiters` 的 `$ N`，限制整条规则的总并发连接数。
- `speedId` 下发为 GOST `limiters` 的 `$ in out`，限制整条规则的总带宽。

用户需要的是按客户端 IP 隔离的限制，例如每个 IP 最多 5 个连接、每个 IP 最多 10 Mbps，而不是所有客户端共享同一个总量。

## GOST 能力确认

### 连接数限制

`go-gost/x/limiter/conn/conn.go` 已内置以下语义：

| Key | 含义 |
|-----|------|
| `$` | 全局连接数限制，所有客户端共享一个 limiter |
| `$$` | 每个客户端 IP 独立连接数限制，每个 IP 创建自己的 limiter |
| `IP` / `CIDR` | 指定 IP 或 CIDR 的连接数限制 |

因此每 IP 连接数无需新增 agent 限制器，只需后端下发 `$$ N`。

### 带宽限制

`go-gost/x/limiter/traffic/traffic.go` 已内置以下语义：

| Key | 含义 |
|-----|------|
| `$` | 服务级总带宽限制 |
| `$$` | 连接级带宽限制 |
| `IP` / `CIDR` | 客户端 IP 或 CIDR 级带宽限制 |

CIDR 级限制使用 generator，为命中的客户端 IP 创建独立 limiter。使用 `0.0.0.0/0` 和 `::/0` 可以覆盖所有 IPv4/IPv6 客户端，实现每 IP 带宽限速。

### 现有缺口

TCP listener 已在 Accept 后用客户端地址包装连接级 traffic limiter，路径可用于每 IP 带宽。UDP listener 当前只在 PacketConn 上应用服务级 limiter，没有在 `Accept()` 后按客户端 UDP pseudo-connection 包装 limiter，也没有挂接 connection limiter。因此要让 UDP 与 TCP 语义一致，需要补齐 UDP listener 的 per-client wrapper。

## 目标

1. 保留现有 `maxConn` 和 `speedId` 的总量语义。
2. 在规则上新增每 IP 最大连接数。
3. 在规则上新增每 IP 带宽限速。
4. 同一规则允许同时配置总量限制和每 IP 限制。
5. 普通用户不能设置或修改限速规则字段，保持现有权限模型。
6. TCP 和 UDP 入口都尽量遵循相同限制语义。

## 非目标

1. 不新增按用户组、节点组、国家地区、ASN 的限制。
2. 不新增请求频率限制；本次“每个 IP 限速”指带宽限速，不是新建连接频率。
3. 不改变已有 speed limit 规则表的单位和含义。
4. 不把用户级默认最大连接数改成每 IP 语义；用户级 `maxConn` 继续作为默认总连接数。

## 数据模型

在 `forward` 表新增两个字段：

| 字段 | 类型 | 默认 | 说明 |
|------|------|------|------|
| `ip_max_conn` | int | `0` | 每 IP 最大连接数，`0` 表示不启用 |
| `ip_speed_id` | nullable int64 | `NULL` | 每 IP 带宽限速规则 ID，`NULL` 表示不启用 |

Go 模型新增：

```go
IPMaxConn int           `gorm:"column:ip_max_conn;not null;default:0"`
IPSpeedID sql.NullInt64 `gorm:"column:ip_speed_id"`
```

字段会通过现有 auto-migrate 机制创建，保持 SQLite/PostgreSQL 兼容，不使用 SQLite 不兼容的 GORM tags。

## API 行为

### 创建规则

`/forward/create` 新增入参：

```json
{
  "ipMaxConn": 5,
  "ipSpeedId": 123
}
```

规则：

- `ipMaxConn` 缺省或小于等于 `0` 时按 `0` 存储，不启用每 IP 连接数限制。
- `ipSpeedId` 缺省或不存在时存为 `NULL`，不启用每 IP 带宽限速。
- `ipSpeedId` 指向不存在的限速规则时按 `NULL` 处理，沿用现有 `speedId` 的容错策略。
- 普通用户提交非空 `ipSpeedId` 时返回错误，保持与 `speedId` 一致的权限边界。

### 更新规则

`/forward/update` 新增入参：

```json
{
  "ipMaxConn": 5,
  "ipSpeedId": 123
}
```

规则：

- 未提交 `ipMaxConn` 时保留原值；提交空值或 `0` 时清除每 IP 连接数限制。
- 未提交 `ipSpeedId` 时保留原值；提交 `null` 时清除每 IP 带宽限速。
- 普通用户不能把 `ipSpeedId` 改成不同的非空值。
- 更新后重新同步运行时服务和 limiter。

### 列表返回

`/forward/list` 返回项新增：

```json
{
  "ipMaxConn": 5,
  "ipSpeedId": 123,
  "ipSpeedLimitName": "每IP 10Mbps"
}
```

`ipSpeedLimitName` 可选，但建议返回，便于前端显示缺失或已删除的限速规则。

## 后端运行时同步

### 连接数限制器

将现有连接限制器构建从单一总量扩展为组合规则。

当前行为：

```json
{
  "name": "rule_conn_limit_42",
  "limits": ["$ 100"]
}
```

新增行为：

```json
{
  "name": "rule_conn_limit_42",
  "limits": ["$ 100", "$$ 5"]
}
```

规则：

- `maxConn > 0` 时追加 `$ maxConn`。
- `ipMaxConn > 0` 时追加 `$$ ipMaxConn`。
- 如果规则未配置 `maxConn` 且用户有 `MaxConn > 0`，继续继承用户级总连接数，追加 `$ user.MaxConn`。
- 如果两者都没有，则不下发 `climiter`，服务不引用 `climiter`。
- limiter 名称继续优先使用 `rule_conn_limit_<forwardID>`；只有用户级默认总连接数且规则没有任何连接限制时可继续使用 `user_conn_limit_<userID>`，避免不必要的 per-rule limiter。

### 带宽限制器

将现有规则限速从单一 `speedId` 扩展为组合 limiter。

当前行为：

```json
{
  "name": "123",
  "limits": ["$ 1.3MB 1.3MB"]
}
```

新增每 IP 行为：

```json
{
  "name": "rule_traffic_limit_42",
  "limits": [
    "$ 1.3MB 1.3MB",
    "0.0.0.0/0 1.3MB 1.3MB",
    "::/0 1.3MB 1.3MB"
  ]
}
```

规则：

- 只有总量 `speedId` 时，保持现有名称和下发路径，服务继续引用 `speedId` 字符串。
- 只有每 IP `ipSpeedId` 时，创建 `rule_traffic_limit_<forwardID>`，只包含 IPv4/IPv6 CIDR 行。
- 总量和每 IP 同时存在时，创建 `rule_traffic_limit_<forwardID>`，同时包含 `$` 和 CIDR 行。
- 如果规则没有 `speedId`，则总量仍可继承 user tunnel 的 `speedId`，保持现有 fallback 语义；当继承的总量限速与 `ipSpeedId` 同时存在时，也使用 `rule_traffic_limit_<forwardID>` 组合 limiter。
- 每 IP 限速不从 user tunnel 继承，只由规则字段控制。
- `AddLimiters` 失败且提示已存在时，使用 `UpdateLimiters` 更新。

### 服务配置

`buildForwardServiceConfigs` 需要从当前 `limiterID *int64` / `cLimiterName string` 扩展为更明确的运行时限制描述，例如：

```go
type forwardRuntimeLimiters struct {
    TrafficLimiter string
    ConnLimiter    string
}
```

服务配置只关心最终引用的 limiter 名称：

- `service["limiter"] = runtimeLimiters.TrafficLimiter`
- `service["climiter"] = runtimeLimiters.ConnLimiter`

这样可以把“如何构建 limiter payload”的逻辑和“如何构建 service JSON”的逻辑分开。

## Agent/GOST 调整

### WebSocket 命令

当前 agent WebSocket 已支持：

- `AddLimiters` / `UpdateLimiters` / `DeleteLimiters`
- `AddCLimiters` / `UpdateCLimiters` / `DeleteCLimiters`

本设计无需新增命令类型。

### UDP listener

补齐 `go-gost/x/listener/udp/listener.go` 的 `Accept()` 包装逻辑，使 UDP pseudo-connection 与 TCP listener 一致：

- 对 `l.options.ConnLimiter` 按客户端地址应用连接数限制。
- 对 `l.options.TrafficLimiter` 按 `conn.RemoteAddr().String()` 应用连接级 traffic wrapper。

需要注意 UDP pseudo-connection 的生命周期由内部 UDP listener 的 TTL/keepalive 控制；connection limiter 必须在 pseudo-connection 关闭时释放计数。

## 前端设计

在 `vite-frontend/src/pages/forward.tsx` 的规则高级设置中新增两个控件：

1. `每 IP 最大连接数`
- 类型：number input。
- 文案：`每个客户端 IP 可同时建立的最大连接数；0 或空表示不限制。`
- 字段：`ipMaxConn`。

2. `每 IP 限速`
- 类型：Select，复用现有限速规则列表。
- 文案：`每个客户端 IP 独享该带宽限制；不选择表示不限制。`
- 字段：`ipSpeedId`。
- 只对管理员显示，保持与 `规则限速` 一致。

前端类型需要同步更新：

- `ForwardApiItem`
- `ForwardMutationPayload`
- `ForwardForm` 或页面内等价类型

## 错误处理与兼容性

1. 旧数据默认 `ip_max_conn=0`、`ip_speed_id=NULL`，行为与当前版本一致。
2. 现有 agent 已支持 limiter 命令和 GOST limiter 语法；发布时需要包含 UDP 修复，才能让 TCP/UDP 都获得完整语义。
3. 节点离线时沿用现有 warning 行为，规则仍可保存，在线节点跳过下发。
4. 如果每 IP speed limit ID 被删除，更新时按 `NULL` 处理，列表页可提示或自动清除，和现有 `speedId` 行为一致。
5. 如果 IPv6 CIDR 在某些监听路径未命中，IPv4 行仍正常生效；测试应覆盖 IPv4，IPv6 通过 payload 合同保证下发。

## 测试计划

### 后端 contract 测试

新增或扩展 `go-backend/tests/contract/max_conn_limit_contract_test.go`：

1. 创建规则时设置 `ipMaxConn=5`，断言 `AddCLimiters` payload 包含 `$$ 5`。
2. 同时设置 `maxConn=100` 和 `ipMaxConn=5`，断言 payload 包含 `$ 100` 和 `$$ 5`。
3. 用户级 `MaxConn` 存在且规则 `ipMaxConn=5` 时，断言 payload 包含 `$ userMaxConn` 和 `$$ 5`。

新增每 IP 限速 contract 测试：

1. 创建规则时设置 `ipSpeedId`，断言 `AddLimiters` payload 包含 `0.0.0.0/0 ...` 和 `::/0 ...`。
2. 同时设置 `speedId` 和 `ipSpeedId`，断言组合 limiter 包含 `$ ...` 与两个 CIDR 行，服务引用 `rule_traffic_limit_<forwardID>`。
3. 普通用户提交 `ipSpeedId` 返回错误。

### Repository/API 测试

1. `CreateForwardTx`、`UpdateForward`、列表查询读写 `ip_max_conn` 和 `ip_speed_id`。
2. `/forward/list` 返回 `ipMaxConn`、`ipSpeedId`。

### GOST/x 测试

1. `go-gost/x/limiter/conn`：验证 `$$ N` 为不同 IP 创建独立 limiter。
2. `go-gost/x/limiter/traffic`：验证 `0.0.0.0/0` 为不同 IPv4 创建独立 limiter。
3. UDP listener：验证 Accept 返回的 UDP pseudo-connection 关闭后释放 connection limiter。

### 验证命令

```bash
(cd go-backend && go test ./...)
(cd go-gost/x && go test ./limiter/... ./listener/udp/...)
(cd vite-frontend && pnpm run build)
```

## 推荐实施顺序

1. 后端模型、repo DTO、API 字段读写。
2. 后端 limiter payload 构建与服务引用重构。
3. Contract 测试覆盖连接数和带宽 payload。
4. GOST UDP listener per-client wrapper 与相关测试。
5. 前端高级设置表单和类型更新。
6. 运行后端测试、GOST/x 相关测试、前端构建。

## 风险

1. UDP pseudo-connection 生命周期和 TCP 连接不同，连接数释放必须依赖 Close 包装正确执行。
2. 总带宽和每 IP 带宽组合时 limiter 名称从纯 speed ID 变为 rule-level 名称，需要确保更新已有规则时不会留下错误引用。
3. 旧节点如果没有 UDP wrapper 修复，TCP 生效但 UDP 每 IP 语义可能不完整；发布时应要求 agent 同步升级。
4. 每 IP 带宽是每个入口节点本地独立限制，不是跨节点全局聚合限制。
