# nftables 纯转发设计

**日期**: 2026-05-30
**状态**: 待审核
**作者**: Codex

## 概述

为 FLVX 增加一种不依赖 agent 的纯转发能力：节点可选择 `nftables` 转发模式，面板通过 SSH 在节点机器上下发和维护 nftables 规则。

第一阶段只支持端口级 DNAT/SNAT 纯转发。它不是 GOST 隧道能力的替代品，也不支持链路、限速、流量统计、连接数限制、Proxy Protocol、best exit 或 agent 诊断。目标是提供一个可靠、可回滚、可重建的轻量转发路径。

## 背景

当前 FLVX 的转发模型由三部分组成：

- `node` 表描述节点，现有本地节点通过 agent WebSocket 接收运行时命令。
- `tunnel` 表描述入口、出口和链路类型，`type=1` 表示端口转发，`type=2` 表示隧道转发。
- `forward` 表描述用户规则、入口端口和目标地址，运行时通过 GOST service 下发到入口节点。

nftables 模式的核心差异是没有 agent，因此不能复用现有 WebSocket command 通道，也不能依赖 agent 上报在线状态、流量和诊断结果。面板必须成为唯一控制面，通过 SSH 把数据库中的期望状态同步到远端 nftables。

## 用户决策

- 创建或编辑节点时选择转发模式。
- 选择 nftables 转发后，不需要安装 agent。
- nftables 转发不支持隧道、流量控制等能力，只支持纯转发。
- 规则由面板端维护，并通过 SSH 下放到节点。

## 推荐方案

新增节点运行时模式：

| 模式 | 含义 |
|------|------|
| `agent` | 默认模式，保持现有 GOST agent 行为 |
| `nftables` | 面板通过 SSH 管理 nftables 规则 |

业务层继续复用现有 `tunnel` 和 `forward` 概念，但对 nftables 模式加严格能力边界：

- nftables 节点只能创建端口转发隧道。
- nftables 隧道不能配置出口节点或转发链。
- 同一个隧道的入口节点必须全部是同一种运行时模式。
- nftables 转发规则创建、更新、删除时，由后端同步 SSH 规则。
- 面板提供节点级“测试 SSH”“重建规则”“清理 FLVX 规则”操作。

## 非目标

- 不支持 `tunnel.type=2` 隧道转发。
- 不支持多跳链路、远程面板共享节点和 federation runtime。
- 不支持 GOST service 能力：限速、每 IP 限速、最大连接数、Proxy Protocol、策略负载均衡。
- 不支持 agent 流量统计、实时系统指标、节点升级、回退、agent 安装命令。
- 不在第一阶段支持 HA 漂移、自动探活切换或复杂负载均衡。
- 不改写用户机器上的非 FLVX nftables 规则。

## 数据模型

### node 表

新增字段：

| 字段 | 类型 | 默认 | 说明 |
|------|------|------|------|
| `forward_mode` | string | `agent` | `agent` 或 `nftables` |

Go 模型使用 SQLite/PostgreSQL 兼容 tag：

```go
ForwardMode string `gorm:"column:forward_mode;type:varchar(20);not null;default:'agent'"`
```

### node_ssh_config 表

新增表保存 nftables 节点 SSH 配置。SSH 凭据不放进 `node` 主表，避免普通节点列表过度暴露敏感字段。

| 字段 | 说明 |
|------|------|
| `id` | 主键 |
| `node_id` | 关联节点，唯一 |
| `host` | SSH 主机，默认可使用 node.server_ip |
| `port` | SSH 端口，默认 22 |
| `username` | SSH 用户 |
| `auth_type` | `password` 或 `private_key` |
| `password` | 加密后密码，可为空 |
| `private_key` | 加密后私钥，可为空 |
| `passphrase` | 加密后私钥口令，可为空 |
| `sudo_mode` | `none` / `sudo` |
| `created_time` | 创建时间 |
| `updated_time` | 更新时间 |

第一阶段可使用现有配置密钥派生或面板本地密钥做对称加密；如果项目尚无统一密钥管理，应至少避免在列表 API 返回完整凭据。

### nft_rule_binding 表

记录面板认为已经应用到节点的规则状态，用于更新、删除、重建和错误展示。

| 字段 | 说明 |
|------|------|
| `id` | 主键 |
| `forward_id` | 转发规则 ID |
| `node_id` | 下发节点 ID |
| `in_port` | 入口端口 |
| `protocols` | 第一阶段固定 `tcp,udp` |
| `target_addr` | 目标地址 |
| `bind_ip` | 可选监听 IP |
| `rule_hash` | 当前期望规则 hash |
| `status` | `pending` / `applied` / `error` |
| `last_error` | 最近错误 |
| `applied_time` | 最近成功应用时间 |
| `created_time` | 创建时间 |
| `updated_time` | 更新时间 |

绑定表不是最终事实来源。最终期望状态仍从 `forward`、`forward_port`、`tunnel` 和 `chain_tunnel` 推导，绑定表只记录应用结果。

## API 行为

### 节点创建和更新

`/node/create` 和 `/node/update` 新增入参：

```json
{
  "forwardMode": "nftables",
  "sshConfig": {
    "host": "203.0.113.10",
    "port": 22,
    "username": "root",
    "authType": "private_key",
    "privateKey": "-----BEGIN OPENSSH PRIVATE KEY-----...",
    "passphrase": "",
    "sudoMode": "none"
  }
}
```

规则：

- `forwardMode` 缺省时按 `agent`。
- `agent` 节点保留现有字段和行为。
- `nftables` 节点要求 SSH 配置完整。
- 从 `agent` 切到 `nftables` 前，若该节点已有 agent 隧道链路或转发规则，应拒绝并提示先迁移或删除。
- 从 `nftables` 切回 `agent` 前，若存在 nftables 规则，应拒绝并提示先清理或迁移。

### 隧道创建和更新

创建 nftables 隧道仍使用 `/tunnel/create`，但后端根据入口节点模式校验能力。

规则：

- 入口节点为 nftables 时，`type` 必须为 `1`。
- 不允许提交 `outNodeId` 或 `chainNodes`。
- 入口节点必须在线的现有校验不能直接套用到 nftables 节点；应改为 SSH 可用性校验或允许保存后手动测试。
- 同一隧道入口节点不能混用 `agent` 和 `nftables`。
- 更新隧道时不允许改变运行时模式；需要通过迁移规则到新隧道实现。

### 转发创建和更新

选择 nftables 隧道时，`/forward/create` 和 `/forward/update` 强制收窄字段：

- `speedId` 必须为空。
- `ipSpeedId` 必须为空。
- `maxConn` 和 `ipMaxConn` 必须为 0。
- `proxyProtocol` 必须为 0。
- 第一阶段 `remoteAddr` 只允许单目标 `host:port`。
- `strategy` 固定为 `fifo` 或忽略。

创建流程：

1. 校验权限、隧道状态、端口占用和 nftables 能力边界。
2. 在数据库创建 `forward` 和 `forward_port`。
3. 通过 nftables runtime 对关联入口节点执行同步。
4. 若同步失败，回滚数据库创建，返回 SSH/nftables 错误。

更新流程：

1. 保存旧 forward 和端口绑定。
2. 更新数据库。
3. 同步 nftables 规则。
4. 若同步失败，回滚数据库状态并尝试恢复旧规则。

删除流程：

1. 先删除远端 nftables 规则。
2. 成功后删除数据库。
3. 如果远端删除失败，普通删除返回错误；强制删除可删除数据库并保留 binding 错误记录，提示用户稍后清理。

## 后端组件

新增 package：

```text
go-backend/internal/runtime/nftables/
```

建议拆分：

| 组件 | 职责 |
|------|------|
| `Manager` | 对 handler 暴露 Apply/Delete/Reconcile/Test 方法 |
| `Planner` | 从数据库记录生成节点级期望规则 |
| `Renderer` | 把期望规则渲染为 nftables 脚本 |
| `SSHRunner` | 负责 SSH 连接、sudo 包装、命令执行和超时 |
| `Parser` | 解析目标地址、协议和错误信息 |

handler 不直接执行 SSH，也不拼 nft 脚本；handler 只做业务校验并调用 runtime manager。

## nftables 规则设计

FLVX 只维护自己的 table，避免触碰用户已有规则：

```nft
table inet flvx {
  chain prerouting {
    type nat hook prerouting priority dstnat; policy accept;
  }

  chain postrouting {
    type nat hook postrouting priority srcnat; policy accept;
  }

  chain forward {
    type filter hook forward priority filter; policy accept;
  }
}
```

每条 forward 生成 TCP 和 UDP 规则：

```nft
tcp dport 12345 dnat to 198.51.100.20:443 comment "flvx forward:42 tcp"
udp dport 12345 dnat to 198.51.100.20:443 comment "flvx forward:42 udp"
```

第一阶段默认生成 masquerade：

```nft
masquerade comment "flvx masquerade"
```

原因是大多数纯 DNAT 场景需要回程可达；如果不做 SNAT，目标服务回包可能绕过转发节点导致连接失败。后续可增加高级开关允许用户关闭 masquerade。

### 原子同步策略

推荐节点级 reconcile，而不是逐条追加：

1. 从数据库查询该节点所有 nftables forward。
2. 生成完整 `table inet flvx` 脚本。
3. 通过 SSH 执行 `nft -f <tempfile>`。
4. 成功后更新所有相关 `nft_rule_binding` 状态和 hash。

这样可以避免局部更新导致规则漂移，也能让“重建规则”与创建/更新走同一条路径。

## SSH 执行策略

基础要求：

- 默认超时 10-15 秒。
- 支持密码和私钥认证。
- 支持 `sudo nft ...`。
- 执行前检查 `command -v nft`。
- 执行前检查 `nft --version`，错误时提示安装 nftables。
- 所有临时脚本写入 `/tmp/flvx-nft-<nonce>.nft`，执行后删除。

建议命令流程：

```sh
cat > /tmp/flvx-nft-xxxx.nft <<'EOF'
table inet flvx {
  ...
}
EOF
nft list table inet flvx >/dev/null 2>&1 && nft delete table inet flvx || true
nft -f /tmp/flvx-nft-xxxx.nft
rm -f /tmp/flvx-nft-xxxx.nft
```

如果目标 nft 版本支持 `destroy table`，也可以把删除动作放进脚本：

```nft
destroy table inet flvx
table inet flvx {
  ...
}
```

实现时应按目标 nft 版本兼容性选择 `destroy` 或 shell 中先检测 `nft list table inet flvx`。

## 前端体验

### 节点页

节点表单新增“转发模式”：

- `Agent 节点`：默认，现有表单不变。
- `nftables 节点`：显示 SSH 配置区块，隐藏 agent 安装相关提示。

nftables 节点列表操作：

- 测试 SSH
- 重建规则
- 清理 FLVX nftables 规则

隐藏或禁用：

- 安装命令
- 升级
- 回退
- agent 协议开关
- 实时 agent 指标入口

### 隧道页

隧道类型文案建议改为更明确的运行时说明：

- `Agent 端口转发`
- `Agent 隧道转发`
- `nftables 纯转发`

如果保持现有 `端口转发 / 隧道转发` 选择器，则在选择 nftables 入口节点后禁用隧道转发，并提示“不支持出口节点和转发链”。

### 转发页

选择 nftables 隧道后：

- 隐藏限速、每 IP 限速、最大连接数、Proxy Protocol。
- 目标地址输入提示“第一阶段仅支持单目标 host:port”。
- 创建/更新失败时显示远端 SSH 或 nftables 错误。

## 错误处理

- SSH 连接失败：返回“SSH 连接失败”，保留底层错误摘要。
- 认证失败：返回“SSH 认证失败，请检查用户名和凭据”。
- `nft` 不存在：返回“节点未安装 nftables”。
- nft 脚本失败：返回 nft stderr 摘要，并记录到 `nft_rule_binding.last_error`。
- 下发超时：标记 binding 为 `error`，允许用户重试“重建规则”。
- 数据库成功但远端失败时，创建/更新路径应回滚数据库；批量重建路径不回滚业务规则，只记录错误。

## 安全边界

- SSH 凭据只在创建/更新时接收，列表 API 不返回明文。
- 私钥和密码在数据库中加密保存。
- 后端日志不得打印完整私钥、密码或 passphrase。
- nft 脚本只由后端 renderer 生成，禁止直接拼接用户提交的自由文本。
- `remoteAddr` 必须严格解析为 host/IP + port，端口必须为 1-65535。
- `inPort` 仍复用现有端口占用校验。
- comment 中只放 forward ID 和协议，不放用户输入。

## 与现有功能的关系

- `node/install` 对 nftables 节点返回错误或前端隐藏入口。
- `node/check-status` 对 nftables 节点可返回 SSH 测试状态，而不是 agent 在线状态。
- `forward/batch-redeploy` 对 nftables 规则执行节点级 reconcile。
- `tunnel/batch-redeploy` 遇到 nftables 隧道时只重建相关 nftables 节点规则，不发送 GOST chain/service 命令。
- federation 导入/共享第一阶段不支持 nftables 节点。
- backup/import 应包含新增 node mode、SSH 配置和 binding 状态；导出时默认不导出 SSH 明文凭据。

## 测试计划

后端单元测试：

- nftables 节点不能创建隧道转发。
- nftables 隧道不能包含出口节点或转发链。
- agent 和 nftables 节点不能混在同一隧道。
- nftables forward 拒绝限速、连接限制和 Proxy Protocol。
- nftables forward 拒绝多目标 remoteAddr。
- renderer 为 TCP/UDP 生成稳定脚本和 comment。
- SSH runner 正确隐藏敏感信息并返回 stderr 摘要。

后端集成测试：

- 创建 nftables forward 时数据库和 binding 同步成功。
- runtime 下发失败时创建回滚。
- 更新失败时数据库和旧规则尽量恢复。
- 删除失败时普通删除返回错误，强制删除保留清理提示。

前端验证：

- 节点表单按转发模式切换字段。
- nftables 节点隐藏安装/升级/回退操作。
- 隧道表单阻止 nftables 隧道转发配置。
- 转发表单选择 nftables 隧道后隐藏不支持字段。

验证命令：

```bash
(cd go-backend && go test ./...)
(cd vite-frontend && pnpm run build)
```

## 实施顺序

1. 数据模型和 repository：新增字段、SSH 配置表、binding 表和查询方法。
2. nftables runtime：实现 planner、renderer、SSH runner、manager。
3. handler 校验：节点、隧道、转发 create/update/delete 接入 runtime。
4. 前端节点表单：增加转发模式和 SSH 配置。
5. 前端隧道/转发表单：按 nftables 能力收窄 UI。
6. 批量重建和清理操作：提供运维入口。
7. 测试与文案打磨。

## 第一阶段固定决策

本设计先固定以下选择，除非审核时调整：

- 第一阶段同时下发 TCP 和 UDP。
- 第一阶段只支持单目标。
- 第一阶段默认启用 masquerade。
- nftables 节点的“在线状态”以 SSH 测试为准，而不是常驻连接。
