# 070: 节点搭建（多内核可插拔，单用户自用）

## 背景与目标

FLVX 目前只做端口转发 / 隧道。目标是在**同一个面板**里增加"搭建代理节点"能力：

- 在已接入的节点（agent）上一键部署代理服务端，**内核可选**：Xray、sing-box、Snell，后续可扩展 Hysteria2 官方版、shadowsocks-rust 等
- **不使用 GOST 自带协议**做节点，GOST 继续只负责转发 / 隧道
- **单用户自用**：不做多用户、配额、到期、按用户计费
- 生成分享链接、二维码和订阅，客户端一键导入
- **核心场景：在落地机搭节点、在中转机做转发，一键对接**，订阅中自动给出中转入口（详见"落地 + 中转对接"）

### 非目标

- 多用户 / 用户同步 / 按用户流量统计
- 计费、套餐、在线 IP 限制
- 修改或删除现有多用户相关功能（只在新功能中不使用它们）
- 远程节点（`is_remote=1`）和 nftables 模式节点（`forward_mode=nftables`）上部署内核

## 总体架构

```
面板 go-backend
  ├─ node_kernel   节点上的内核实例（类型、版本、状态）
  ├─ node_inbound  入站（协议、端口、传输、TLS/Reality、凭据）
  ├─ 配置渲染：通用模型 → 每个内核的配置文本（在面板侧完成）
  ├─ 分享链接 / 订阅生成（只依赖协议，不依赖内核）
  └─ 通过现有 WebSocket 控制通道下发 Kernel* 命令
          │
          ▼
Agent go-gost/x/kernel（新包）
  ├─ Manager：安装、守护子进程、崩溃重启、日志环形缓冲
  ├─ Adapter：xray / singbox / snell / ...（描述二进制、启动参数、配置文件名）
  └─ nft 计数：独立表 inet flvx_kernel，按入站端口计数并上报
```

**关键决定：配置渲染放在面板侧。**
Agent 只负责"拿到配置文本，写文件，启动或重启进程"，不理解协议细节。好处是：

- 新增或调整协议时只改面板，不需要升级所有 agent
- 渲染逻辑可以在 `go test` 里用 golden 文件做单元测试
- Agent 的适配器很薄（每个约几十行）

## 数据模型

遵守项目约定：定义 `TableName()`，不用 `jsonb` / `serial`，JSON 存 `text`。新表加入 `repository.go` 的 AutoMigrate 列表。

### `node_kernel`

| 字段 | 类型 | 说明 |
|---|---|---|
| id | int64 PK | |
| node_id | int64, uniqueIndex(node_id, kernel_type) | 每个节点每种内核最多一个实例 |
| kernel_type | varchar(20) | `xray` / `singbox` / `snell` |
| version | varchar(50) | 期望版本，如 `25.9.11`、`1.12.8`、`5.0.0` |
| installed_version | varchar(50) | agent 上报的实际版本 |
| status | varchar(20) | `pending` / `installing` / `running` / `stopped` / `error` |
| last_error | text | |
| config_hash | varchar(64) | 最近一次下发配置的 sha256，用于判断是否需要重新下发 |
| enabled | int | 1=启用，0=停用 |
| created_time / updated_time | int64 | |

### `node_inbound`

| 字段 | 类型 | 说明 |
|---|---|---|
| id | int64 PK | |
| kernel_id | int64, index | 所属 node_kernel |
| node_id | int64, index | 冗余字段，便于做端口冲突检查和查询 |
| name | varchar(100) | 显示名，也用作订阅里的节点名 |
| protocol | varchar(30) | `vless` / `vmess` / `trojan` / `shadowsocks` / `hysteria2` / `tuic` / `snell` … |
| listen | varchar(100) | 默认 `::` |
| port | int | |
| network | varchar(20) | `tcp` / `udp` / `tcp,udp`，用于 nft 计数和防火墙 |
| transport | text(JSON) | `{type: tcp|ws|grpc|httpupgrade|xhttp, path, host, serviceName}` |
| security | text(JSON) | `{type: none|tls|reality, sni, alpn, certMode, realityPrivateKey, realityPublicKey, shortIds, dest, fingerprint}` |
| settings | text(JSON) | 协议凭据和参数：`uuid`、`flow`、`password`、`method`、`psk`、`obfs`、`version` 等 |
| extra | text(JSON) | 内核特有参数透传（高级用户使用，原样合并进渲染结果） |
| share_host | varchar(255) | 分享 / 订阅中使用的地址，为空时使用节点 `server_ip` |
| relay_only | int | 1=订阅中不输出直连节点 |
| allow_relay_only | int | 1=nft 白名单，仅允许中转 IP 访问（模式 A） |
| accept_proxy_protocol | int | 1=入站接收 PROXY protocol（与中转 Forward 联动） |
| enabled | int | |
| upload_bytes / download_bytes | int64 | 累计流量（由 nft 计数累加） |
| created_time / updated_time | int64 | |

后端为每种协议定义一个 Go 结构体，对 `settings`、`transport`、`security` 做强类型解析和校验，避免把 JSON 原样透传给前端编辑。

### `node_inbound_counter`（nft 计数快照）

结构参考 `NftCounterState`：`inbound_id + protocol + direction` 唯一，记录上次的计数值，用于计算增量，处理 agent 重启后计数器归零的情况。

### 订阅 token

存在现有的 `vite_config` 键值表里：`node_sub_token`。首次访问时自动生成，支持在页面上重置。

## 内核 × 协议能力矩阵（第一版）

| 协议 / 组合 | Xray | sing-box | Snell |
|---|---|---|---|
| VLESS + Reality + Vision | ✅ | ✅ | |
| VLESS + WS/gRPC + TLS | ✅ | ✅ | |
| VLESS + XHTTP + TLS/Reality | ✅ | ❌ | |
| VMess + WS + TLS | ✅ | ✅ | |
| Trojan + TLS | ✅ | ✅ | |
| Shadowsocks（AEAD / 2022） | ✅ | ✅ | |
| Hysteria2 | ⚠️ 需要确认版本支持 | ✅ | |
| TUIC v5 | | ✅ | |
| AnyTLS / ShadowTLS | | ✅ | |
| Snell v4 / v5 | | | ✅ |

该矩阵在面板后端的 `kernel/capabilities.go` 中以静态表维护，并通过 API 下发给前端。前端选择内核后，只显示该内核支持的协议和传输。

> 实施前需要对照各内核当前版本的官方文档，确认字段名和协议支持。sing-box 1.11 之后入站配置格式有调整，Xray 也在持续新增传输。

## 凭据与证书

全部在面板侧用 Go 标准库生成，不依赖各内核的命令行：

| 用途 | 生成方式 |
|---|---|
| UUID | `crypto/rand`，v4 |
| 密码 / Snell PSK | 32 字节随机，base64url |
| SS2022 密钥 | 按 method 生成 16 或 32 字节，base64 |
| Reality 密钥对 | `crypto/ecdh` X25519，私钥和公钥都用 base64url（无 padding） |
| Reality shortId | 8 字节随机，hex |

TLS 证书提供三种模式：

1. **Reality**（推荐默认）：不需要证书和域名
2. **自签证书**：面板生成 ECDSA 证书，随配置一起下发，分享链接带 `allowInsecure` / `insecure=1`。适合 Hysteria2、TUIC
3. **ACME**：只有 sing-box 内置 ACME，第一版只对 sing-box 开放；Xray 用户需要自己提供证书路径

### 预设模板

创建入站时先选模板，再自动填充参数，只需要用户填 SNI 或域名：

- VLESS-Reality-Vision（Xray / sing-box）
- Hysteria2 自签（sing-box）
- SS2022（Xray / sing-box）
- Trojan-TLS（sing-box ACME）
- Snell v5（Snell）

## 控制通道命令（面板 → agent）

复用 `ws.Server.SendCommand`，新增以下命令。agent 端加入 `routeCommand` 分支，并在 `isMutationCommand` 中登记，以获得变更锁：

| 命令 | 数据 | 说明 |
|---|---|---|
| `KernelInstall` | `{type, version, url, checksumUrl, archive: zip/tar.gz/raw, binPath}` | 下载、校验 sha256、解压到 `/etc/flux_agent/kernels/<type>/<version>/`。下载地址由面板生成（`{ARCH}` 占位符由 agent 替换），与 `UpgradeAgent` 的方式一致 |
| `KernelApply` | `{type, version, files: [{name, content, mode}], args, nftPorts: [{inboundId, port, network}]}` | 写入配置文件，原子替换（先写临时文件再 rename），然后重启进程，并同步 nft 计数规则 |
| `KernelControl` | `{type, action: start/stop/restart/remove}` | `remove` 同时删除二进制、配置和对应的 nft 规则 |
| `KernelStatus` | `{}` | 返回每个内核的运行状态、pid、版本、最近 N 行日志 |

agent 主动上报（新的消息类型）：

- `kernel_status`：进程状态变化时（启动、退出、崩溃重启）立即上报
- `kernel_traffic`：随 metric 周期（或每 60 秒）上报 nft 计数快照 `[{inboundId, proto, dir, bytes, packets}]`

### Agent 重连后的状态恢复

节点上线时（`SetNodeOnlineHook`），面板比较各 `node_kernel.config_hash` 与 agent 上报的 hash，不一致就重新执行 `KernelApply`。同时 agent 在本地持久化最后一次的 Apply 内容，所以 agent 重启时即使面板不在线，也能直接拉起内核。

## Agent 内核管理器（`go-gost/x/kernel/`）

```
kernel/
  manager.go      // 内核实例表、Apply/Control/Status，持久化 state.json
  process.go      // 子进程守护：启动、退出检测、指数退避重启、stdout/stderr 环形缓冲
  install.go      // 下载、sha256、解压（zip / tar.gz / 单文件）
  nft.go          // inet flvx_kernel 表的同步与计数读取
  adapters.go     // 适配器注册表
  adapter_xray.go
  adapter_singbox.go
  adapter_snell.go
```

适配器接口（agent 端很薄，配置内容由面板渲染）：

```go
type Adapter interface {
    Type() string
    BinaryName() string                    // xray / sing-box / snell-server
    ConfigFileName() string                // config.json / config.json / snell.conf
    Args(binPath, configPath string) []string
    Version(binPath string) (string, error) // 执行 -version 等命令并解析
}
```

| 内核 | 启动参数 | 版本命令 |
|---|---|---|
| xray | `run -c config.json` | `version` |
| sing-box | `run -c config.json` | `version` |
| snell | `-c snell.conf` | `-v` |

目录布局：

```
/etc/flux_agent/kernels/
  state.json                  // 最后一次 Apply 的内容（用于离线自启）
  xray/25.9.11/xray
  xray/run/config.json
  singbox/1.12.8/sing-box
  singbox/run/config.json
  snell/5.0.0/snell-server
  snell/run/snell.conf
```

进程以 root 运行（与 agent 相同）。实现时注意：

- 关闭 agent 时停止所有子进程（`Setpgid`，按进程组 kill）
- 进程退出后按 1s→2s→4s… 退避重启，最长 60 秒；连续失败时上报 `error` 和最后的日志

## 流量统计（nft，按入站端口）

**不复用**后端现有的 SSH 方式 nftables runtime。那套依赖 `NodeSSHConfig`，而且以 `forward_id` 为键。这里改由 agent 在本机直接管理一张独立的表：

```
table inet flvx_kernel {
  counter in_<id>_tcp  {}
  counter out_<id>_tcp {}
  counter in_<id>_udp  {}
  counter out_<id>_udp {}
  chain input  { type filter hook input  priority -150; policy accept;
                 tcp dport <port> counter name in_<id>_tcp ... }
  chain output { type filter hook output priority -150; policy accept;
                 tcp sport <port> counter name out_<id>_tcp ... }
}
```

- `in` = 客户端上传，`out` = 下发给客户端。只统计"客户端 ↔ 节点"这一段，不重复统计"节点 ↔ 目标网站"的流量
- 默认只计数，不过滤，不影响现有防火墙规则；唯一的例外是可选的"仅允许中转访问"白名单（见"落地 + 中转对接"）
- 每次 Apply 都整表重建（`nft -f` 原子替换）
- 机器上没有 `nft` 时降级：内核照常运行，面板显示"流量统计不可用"
- 面板侧沿用 `nftables_traffic.go` 的做法：根据快照和上次的值算增量，计数值变小时视为计数器被重置，然后累加到 `node_inbound.upload_bytes/download_bytes`

## 面板 API

所有接口都只允许管理员访问（复用现有的 admin 鉴权中间件）。响应格式沿用 `{code, msg, data, ts}`。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/kernel/capabilities` | 内核 × 协议 × 传输能力矩阵、各内核推荐版本 |
| GET | `/api/v1/kernel/list?nodeId=` | 节点上的内核实例及状态 |
| POST | `/api/v1/kernel/install` | `{nodeId, type, version}` |
| POST | `/api/v1/kernel/control` | `{kernelId, action}` |
| POST | `/api/v1/kernel/logs` | `{kernelId}`，实时拉取最近日志 |
| GET | `/api/v1/inbound/list` | 全部入站（含所属节点、内核、流量） |
| POST | `/api/v1/inbound/create` | 创建入站，后端补全凭据，然后渲染并下发 |
| POST | `/api/v1/inbound/update` | |
| POST | `/api/v1/inbound/delete` | |
| POST | `/api/v1/inbound/reset-traffic` | |
| POST | `/api/v1/inbound/regenerate` | 重新生成凭据（UUID、密码、Reality 密钥） |
| GET | `/api/v1/inbound/share?id=` | 分享链接和二维码数据 |
| POST | `/api/v1/inbound/sub/reset-token` | 重置订阅 token |
| GET | `/api/v1/sub/{token}?format=` | **公开接口**，靠 token 鉴权；默认根据 UA 自动识别客户端 |

### 端口冲突校验

创建或修改入站时，检查同一节点上：

- 其他入站的端口
- 现有转发和隧道正在使用的端口（复用 plan 014 中的端口占用校验逻辑）
- 节点 `port` 字段配置的转发端口范围：入站端口可以在范围外，但不能和已占用的端口重叠

## 配置渲染（`go-backend/internal/kernel/`）

```
kernel/
  capabilities.go     // 能力矩阵、推荐版本、下载地址模板
  model.go            // 协议设置的强类型结构体与校验
  credentials.go      // UUID / 密码 / X25519 / shortId / 自签证书
  render_xray.go      // []Inbound → config.json
  render_singbox.go
  render_snell.go     // 单个入站 → snell.conf（Snell 每个实例只有一个入站，见下文）
  share.go            // 单个入站 → URI（vless:// vmess:// ss:// trojan:// hysteria2:// tuic://）
  subscription.go     // []Inbound → clash/mihomo YAML、sing-box JSON、Surge conf、base64
  testdata/*.golden
```

不放在 `http/handler` 里，因为这部分是纯函数，便于单元测试。handler 只负责调用。

### Snell 的特殊处理

Snell 每个进程只能有一个监听端口和一个 PSK。处理方式：

- 每个 Snell 入站对应一个进程。agent 的实例键从 `type` 扩展为 `type + "-" + inboundId`，配置文件写到各自目录
- 对 `node_kernel` 来说仍然只有一条 snell 记录（管理版本和安装），入站各自对应一个子进程

### 渲染规则

- Xray / sing-box：一个节点、一种内核、一份配置，包含该内核下所有启用的入站。出站只配 `direct`（第一版不做分流）
- 关闭内核自带的统计 / API（不需要），日志级别默认 `warning`
- 合并 `extra` 时，内核特有字段以深合并方式覆盖默认值

## 分享链接与订阅

| 客户端格式 | 参数 | 包含的协议 |
|---|---|---|
| Base64 URI 列表 | `format=base64` | 除 Snell 外的全部 |
| Clash Meta / Mihomo | `format=clash` | 全部（Mihomo 支持 snell） |
| sing-box | `format=singbox` | 除 Snell 外的全部 |
| Surge | `format=surge` | Surge 支持的子集，包含 snell |

- 响应头带 `subscription-userinfo: upload=..; download=..; total=0; expire=0`，以及 `profile-update-interval`
- Clash / sing-box 格式附带一份最小可用的规则模板（全局代理 + 局域网直连），第一版不做规则定制
- 未传 `format` 时根据 UA 推断：`clash`/`mihomo`/`stash` → clash，`sing-box` → singbox，`surge` → surge，其余返回 base64

## 前端

新增页面 `/inbound`（侧边栏名称："节点服务"），组件全部从 `src/shadcn-bridge/heroui/*` 引入。

- **列表**：名称、节点、内核、协议/传输、端口、状态灯、上行/下行流量、操作（分享、编辑、启用/停用、删除）
- **新建 / 编辑弹窗**：
  1. 选节点（只列出非远程、非 nftables 模式的节点）
  2. 选内核（未安装时提示安装，并选择版本）
  3. 选模板或自定义协议，表单按能力矩阵动态生成
  4. 高级：自定义端口、`share_host`、`extra` JSON
- **分享弹窗**：URI、二维码、复制按钮
- **订阅卡片**（页面顶部）：订阅地址和各格式快捷链接、重置 token
- **节点详情页**增加"内核"标签页：版本、状态、启动/停止/重启/卸载、查看日志

API 封装放在 `src/api/` 中，与现有文件风格保持一致。

## 落地 + 中转对接（核心场景）

目标拓扑：**在落地机上搭节点，在中转机上做转发，订阅里给出中转入口。**

```
客户端 ──► 中转机 (GOST 转发入口 relay_ip:X) ──► 落地机 (内核入站 :Y) ──► 互联网
            现有 Forward / Tunnel                   本方案的 node_inbound
```

落地机和中转机都是面板里的普通节点（都装 agent）。"落地"和"中转"只是使用角色，不需要新增节点类型。

### 两种对接模式

| 模式 | 现有能力 | 落地入站监听 | 中转 → 落地链路 | 适用 |
|---|---|---|---|---|
| **A. 端口转发**（tunnel.type=1） | 中转入口直接 dial `落地IP:Y` | `::`（公网） | 原样 TCP/UDP 转发，开销最小 | 最常用；节点协议本身已加密（Reality/TLS） |
| **B. 隧道转发**（tunnel.type=2，出口为落地机） | 入口 → GOST 隧道 → 落地机上的出口 → dial `127.0.0.1:Y` | `127.0.0.1`（**不暴露公网**） | 经 GOST 隧道（tls/kcp 等），可多级链路 | 想隐藏落地端口、需要多跳，或中转↔落地线路需要改善 |

在模式 B 中，落地入站只监听本机，外部完全扫描不到。这是"落地机隐身"最干净的做法。实施时需要验证：隧道出口节点 dial `127.0.0.1` 时，Forward 的目标地址校验是否允许回环地址（如果被拦截，需要对"目标是本方案入站"的情况放行）。

### 落地入站白名单（模式 A 的补充）

模式 A 中，落地端口仍然暴露在公网。可以选择开启"仅允许中转访问"：agent 在 `inet flvx_kernel` 表里为该端口加入 `ip saddr != {中转IP集合} drop`。中转 IP 集合由面板根据绑定的转发自动计算（入口节点 IP 或隧道出站 IP）。这是本方案中**唯一会丢弃流量**的 nft 规则，默认关闭，只作用于该入站端口。

### 一键对接流程

在入站的"对接中转"操作中：

1. 选择一条现有隧道（或者新建：选中转节点作为入口；模式 B 还需要把落地节点设为出口）
2. 面板自动创建一条 Forward：
   - 模式 A：`remote_addr = 落地share_host或server_ip:Y`
   - 模式 B：`remote_addr = 127.0.0.1:Y`
   - 协议：TCP + UDP（Hysteria2 / TUIC 这类基于 UDP 的协议必须带 UDP，依赖 plan 069 修复后的 UDP 转发）
   - 入口端口：自动分配，或手动指定
3. 写入关联表 `node_inbound_relay`，订阅自动多出一个节点

### 数据模型调整

一个落地入站可以同时挂多个中转，所以用一对多的关联表：

#### `node_inbound_relay`

| 字段 | 类型 | 说明 |
|---|---|---|
| id | int64 PK | |
| inbound_id | int64, index | |
| forward_id | int64, uniqueIndex | 对应的 Forward（转发删除时级联删除这条关联） |
| name | varchar(100) | 订阅中的节点名，默认 `"{中转节点名} → {入站名}"` |
| share_host | varchar(255) | 为空时使用转发入口 IP（`ForwardPort.InIP`，或入口节点 `server_ip`） |
| managed | int | 1=由"一键对接"创建（删除入站时一起删除该 Forward）；0=绑定已有的转发（只解除关联） |
| created_time | int64 | |

### 订阅输出

每个入站输出：

- **直连节点**（可在入站上关闭："仅中转"，模式 B 下强制关闭）
- **每个中转一个节点**：地址和端口替换为中转入口，其余参数（UUID、SNI、Reality 公钥、传输等）与直连节点完全相同

节点命名示例：`🇯🇵 东京-Reality（直连）`、`🇭🇰 香港中转 → 东京-Reality`。Clash 订阅可以选择附带一个包含所有中转节点的 `url-test` 组。

### 中转场景下的注意事项

| 问题 | 处理 |
|---|---|
| Reality / TLS 经过中转 | 中转是 TCP 透明转发，SNI 和握手是端到端的，无需改动 |
| UDP 协议（Hysteria2/TUIC/SS UDP） | 中转 Forward 必须开启 UDP；模式 B 需要确认隧道协议支持 UDP 承载 |
| 落地机看到的客户端 IP | 是中转机的 IP。如果需要真实 IP，可以开启 Forward 的 `proxy_protocol_send`，同时在入站开启"接收 PROXY protocol"（Xray `acceptProxyProtocol` / sing-box 的对应字段）；两端必须同时开或同时关，面板负责联动 |
| 流量统计 | 落地入站的 nft 计数（含所有中转的流量）和转发自己的流量统计是两套数据，页面上分开显示，不相加 |
| 删除顺序 | 删除入站时，先删除 `managed=1` 的 Forward，再删除入站；删除转发时，只移除关联 |
| 落地 IP 变化 | 模式 A 下，Forward 的 remote_addr 来自落地节点 IP。节点 IP 变更时提示"同步对接的转发" |

## 与上游合并的策略

- 新代码集中放在：`go-backend/internal/kernel/`、`go-backend/internal/http/handler/kernel_*.go`、`go-gost/x/kernel/`、`vite-frontend/src/pages/inbound*`
- 对已有文件的改动控制在"注册一行"的程度：AutoMigrate 列表、路由注册、`routeCommand` 分支、`App.tsx` 路由、侧边栏菜单
- 不改现有表结构，不删除多用户功能

## 风险与待确认

| 项 | 说明 | 处理 |
|---|---|---|
| 内核配置格式变化 | sing-box 和 Xray 迭代快，字段经常调整 | 能力矩阵中记录每个版本区间；渲染和测试锁定在推荐版本 |
| Snell 闭源 | 下载地址和许可以官方为准，不允许把二进制打包进仓库 | agent 安装时从官方地址下载，下载地址做成配置项 |
| 下载被墙 | 国内节点访问 GitHub 困难 | 下载地址支持配置镜像前缀（与 agent 升级共用设置，如果存在的话） |
| 端口冲突 | 与 GOST 转发端口冲突 | 创建时校验；agent Apply 前再用 `net.Listen` 试探一次 |
| nft 与现有防火墙 | 可能与用户自定义规则相互影响 | 独立表，优先级 -150；默认只计数，白名单 drop 只在用户显式开启时作用于对应入站端口 |
| 证书 | Xray 没有内置 ACME | 第一版 Xray 只提供 Reality 和自签证书 |
| CI Go 版本 | CI 用 1.23 构建 | 只使用 1.23 已有的标准库（`crypto/ecdh` 从 1.20 开始提供） |

## 阶段与任务清单

### 第 1 阶段：框架 + Xray（目标：可用）✅

- [x] B1 新增模型 `NodeKernel`、`NodeInbound`、`NodeInboundCounter` 并加入 AutoMigrate
- [x] B2 `internal/kernel`：能力矩阵、协议模型与校验、凭据生成
- [x] B3 `render_xray.go` + golden 测试（VLESS-Reality-Vision、SS2022、VMess-WS-TLS、Trojan-gRPC、VLESS-XHTTP）
- [x] B4 kernel / inbound API（含端口冲突校验）+ contract 测试
- [x] B5 节点上线钩子：重新下发全部内核配置，并清理面板已删除的内核
- [x] A1 `x/kernel`：Manager、进程守护、安装（SHA256 校验）、state.json 自启
- [x] A2 xray 适配器 + `KernelApply/KernelControl/KernelStatus` 命令接入
- [x] A3 nft 计数表同步；面板每 60 秒拉取 `KernelStatus` 并按增量累加
- [x] F1 `/inbound` 页面：列表、新建/编辑（模板化）、启停删除、参数查看
- [x] F2 内核管理（安装、启停、重启、日志、卸载）

#### 实施记录（与上文设计的差异）

- **命令合并**：`KernelInstall` 并入 `KernelApply`（请求带下载信息，二进制缺失时自动安装），只保留 Apply / Control / Status 三个命令；内核命令不进入 agent 的变更队列，避免下载期间阻塞转发配置。
- **流量与状态改为面板拉取**：不新增 `kernel_status` / `kernel_traffic` 推送消息，面板每 60 秒发送 `KernelStatus`，WebSocket 服务端无需改动。计数快照带 epoch（nft 表重建时变化），面板据此判断计数器是否重置。
- **内核管理放在 `/inbound` 页面顶部**，没有改 `node.tsx`（3500+ 行，改动会增加与上游合并的冲突）。
- **进程生命周期**：内核以独立进程组运行，并设置 `Pdeathsig`，agent 异常退出时内核随之退出；agent 重启后从 `state.json` 恢复。
- **Xray 客户端兼容性（第 2 阶段注意）**：新版 Xray 客户端已移除 `allowInsecure`，自签证书的分享链接 / 订阅需要提供 `pinnedPeerCertSha256`（证书 DER 的 SHA256 十六进制）。

#### 验证记录

- 单元 / contract 测试：`go-backend` 全量 3 次通过；`go-gost/x/kernel` 在 `-race` 下通过。
- 用真实 Xray 26.3.27 校验渲染结果：全部 29 种 协议×传输×安全 组合 `xray run -test` 通过。
- 真实流量：VLESS-Reality-Vision、SS2022、Trojan-WS-TLS（自签）经 Xray 客户端访问成功；未授权探测 Reality 端口返回伪装站证书。
- 面板 + agent 端到端：经 API 安装 Xray（真实下载 + 校验）、创建入站、产生流量、面板记账、停用、agent `kill -9` 后内核随之退出并在重启后恢复、卸载后进程 / 文件 / nft 表全部清理。
- 浏览器（Playwright）走查：登录、安装内核、模板创建、编辑保留凭据、启停、删除、日志、移动端布局。

### 第 2 阶段：分享、订阅、对接中转（目标：完整可用）✅

- [x] B6 `share.go`：VLESS / VMess / Trojan / SS（SIP002，2022 用 SIP022 明文 userinfo）URI + golden 测试
- [x] B7 订阅：base64 / Clash(Mihomo) / sing-box / Surge，按 UA 识别，`Subscription-Userinfo` 头；节点名自动去重
- [x] B8 订阅 token（`node_sub_token`，标记为敏感 + 系统管理，普通用户不可读、配置接口不可改），支持重置
- [x] B9 `node_inbound_relay` + 一键对接（端口转发隧道 = 模式 A），失败时回滚已创建的转发
- [x] B10 订阅输出中转节点、`relay_only`；删除中转 / 入站 / 内核 / 转发时级联清理
- [x] B11 模式 B：隧道转发出口必须是落地节点，转发目标为 `127.0.0.1:端口`（管理员转发本就允许本地地址，无需改动校验）
- [x] B12 PROXY protocol 联动：入站开启后强制"仅中转"，Xray 渲染 `sockopt.acceptProxyProtocol`，该入站全部中转转发自动设为发送 v1；关闭时同步撤销
- [x] A4 nft 白名单：放行回环 + 中转入口节点的全部地址（域名在面板侧解析）+ 用户额外条目；agent 在 `flvx_kernel` 表内为该端口建 IPv4/IPv6 集合并丢弃其他来源，只影响该端口；中转增删时自动刷新
- [x] F3 分享弹窗（链接 + 二维码）、订阅卡片（各格式一键复制、二维码、重置）
- [x] F4 入站"中转"弹窗：按隧道类型自动判断可用性、创建 / 删除中转；入站卡片显示中转数量

#### 第 2 阶段验证记录

- 订阅文件用真实客户端校验：sing-box 1.14.3 `check` 通过，Mihomo（v1.19.32 源码构建）`-t` 通过。
- 真实流量：Xray 服务端 + sing-box / Mihomo 客户端，VLESS-Reality-Vision、SS2022、Trojan-WS-TLS（自签）、VMess-gRPC-TLS（自签）、VLESS-WS 五种全部连通。
- 面板 + 两个 agent（落地 / 中转）端到端：从公开订阅地址拉取 sing-box 配置，直连、模式 A（端口转发）、模式 B（隧道转发，含仅监听本机的 SS）4 个节点全部连通；删除中转 / 入站后中转机端口确实关闭。
- 浏览器走查：订阅复制（链接可直接拉取）、二维码、分享弹窗、中转创建 / 删除、仅本机入站只能选隧道转发、移动端。
- PROXY protocol 实测：客户端以 127.0.0.5 经中转访问，开启前落地内核看到中转地址 127.0.0.1，开启后看到 127.0.0.5（端口转发、隧道转发两种模式均验证）；开启后绕过中转直连被拒绝。
- 白名单实测：关闭时从 192.0.2.2 直连可用；开启后被丢弃，回环和两种中转仍可用；加入 192.0.2.0/24 后放行；关闭后集合移除。生成的规则在单元测试中经真实 `nft -c` 校验。
- 已知：Xray 新版客户端不再接受 `allowInsecure`，自签证书链接对 v2rayN 等仍有效（其自行映射），Mihomo 用 `skip-cert-verify`，sing-box 用 `insecure`。

### 第 3 阶段：更多内核（参考 mozisen/surge `vless-server.sh` v3.7.3）✅

参考脚本的协议 / 内核划分与本方案一致：Xray 负责 VLESS / VMess / Trojan / SS，sing-box 负责 Hysteria2 / TUIC / AnyTLS，Snell 等闭源协议独立进程。借鉴其中经过实战的参数与安全做法，不照搬多用户、WARP 分流、链式代理、acme.sh、Telegram 等与单用户自用无关的部分。

**sing-box 内核**
- [x] B13 `render_singbox.go` + golden：Hysteria2（`ignore_client_bandwidth`、可选 masquerade）、TUIC v5（`congestion_control: bbr`）、AnyTLS，以及 VLESS-Reality / VMess / Trojan / SS2022（与 Xray 共用协议模型，节点可任选内核）；QUIC 协议 ALPN 固定 `h3`，SNI 留空默认 `www.bing.com`（自签）
- [x] B14 证书：面板自签（链接带 insecure）/ 节点证书路径 / **sing-box 自动申请（ACME）**：1.14 起的 `certificate_providers`，同域名共用一个 provider，数据放在运行目录 `acme/`（重新下发不丢）；HTTP-01（需 80 端口）或 DNS-01（Cloudflare Token、阿里云 AccessKey）；DNS 凭据在列表接口中隐藏，编辑时留空保持不变；节点指定的 sing-box 版本低于 1.14 时拒绝
- [x] A5 sing-box 适配器：从 GitHub release 下载 `sing-box-{ver}-linux-{arch}.tar.gz`，用 release API 给出的资产 SHA256 校验；agent 侧对所有内核强制校验，拿不到校验值就拒绝安装
- [x] B15 Hysteria2 端口跳跃：agent 在 `flvx_kernel` 表加 nat prerouting/output 链（`fib daddr type local udp dport A-B redirect to :端口`），重定向发生在过滤前，计数与白名单照常生效；跳跃范围参与端口冲突校验，且不能与节点转发端口段重叠；只给直连地址输出（URI `mport`、Clash `ports`、sing-box `server_ports`、Surge `port-hopping`），中转只转一个端口所以不带。TUIC 客户端普遍不支持跳跃，未做

**Snell**
- [x] B16 `render_snell.go`：v4 / v5 每个入站一个 `snell-server` 进程（`listen`、`psk`、`ipv6`、`obfs = off`）；协议版本只影响客户端配置，入站版本须与节点安装的 Snell 主版本匹配（v5 需要 5.x）
- [x] A6 snell 适配器：只从官方 `dl.nssurge.com` 下载，内置已核对的 SHA256 表（4.1.1、5.0.1 × amd64 / aarch64 / armv7l），其他版本拒绝安装；内核卡片可选版本
- [x] v6 不开放：参考脚本的 v6 来自第三方仓库，不是官方渠道

**ShadowTLS v3**
- [x] B17 SS2022 + ShadowTLS：sing-box 原生 `shadowtls` 入站（v3，handshake 到伪装站 443），detour 到只监听 127.0.0.1 的 SS 后端
- [x] Snell + ShadowTLS：Snell 是独立进程，sing-box 无法 detour 过去，采用参考脚本方案——`ihciah/shadow-tls` 0.2.25 作为附加程序（内置 SHA256），`--v3 server --listen 公网端口 --server 127.0.0.1:后端 --tls 伪装站:443`；后端端口自动分配并参与端口冲突校验
- [x] agent 支持一个内核多个进程 + 附加程序（各自安装、校验、守护、崩溃重启），日志按进程加前缀并去掉 ANSI 颜色码

**订阅 / 分享**
- [x] B18 链接：`hysteria2://`、`tuic://`、`anytls://`、SS 带 `shadow-tls` 插件参数；Snell 无标准 URI，分享弹窗只给 Surge 行
- [x] B19 Clash(Mihomo)：hysteria2 / tuic / anytls / ss+shadow-tls（Mihomo 只支持 Snell v1–3，因此不输出 Snell）；sing-box：hysteria2 / tuic / anytls / ss+shadowtls（detour）；Surge：hysteria2 / tuic-v5 / anytls / ss / snell，带 ShadowTLS 时输出 `shadow-tls-password` / `shadow-tls-sni` / `shadow-tls-version=3`
- [x] 真实客户端连通性测试（见下）

#### 第 3 阶段验证记录

- sing-box 1.14.3 服务端 + sing-box / Mihomo 客户端：Hysteria2、TUIC v5、AnyTLS、SS2022+ShadowTLS 直连全部连通；经端口转发中转的 Hysteria2 / TUIC（UDP）同样连通，nft 计数（TCP 与 UDP）正确累加。
- 发现并修复：sing-box 订阅曾给 Hysteria2 / TUIC 出站带 uTLS，sing-box 客户端会拒绝（`unsupported usage for uTLS`），已去掉并加测试。
- Snell 4.1.1 / 5.0.1 + shadow-tls 0.2.25 在 agent 上实际下载、校验、启动、监听正常，杀进程后自动拉起；没有可用的 Snell v4/v5 命令行客户端，流量只能在 Surge 上实测。
- 浏览器走查：内核切换后模板列表随之变化（sing-box 8 个、Snell 2 个），Snell 版本 / PSK / ShadowTLS 表单，创建 TUIC、Snell+ShadowTLS 入站并下发。
- 注意：入站默认监听 `::`（双栈）；内核启动参数禁用了 IPv6 的机器（`ipv6.disable=1`）上 shadow-tls 会绑定失败，需把监听地址改为 `0.0.0.0`。

**NaïveProxy**
- [x] 用 sing-box 原生 `naive` 入站（不需要 Caddy），用户名 + 密码，监听 TCP + UDP（HTTP/3）；必须使用真实证书（ACME 或证书文件），不允许自签
- [x] 分享 `naive+https://user:pass@host:port`（经中转或用 IP 连接时带 `sni`）；sing-box 订阅输出 naive 出站（需 1.13+ 且带 cronet 的客户端）；Mihomo / Surge 不支持，不输出

#### 补充验证记录（ACME / NaïveProxy / 端口跳跃）

- ACME：本地 Pebble（Let's Encrypt 测试 CA）+ 按面板渲染格式配置的 sing-box 1.14.3，HTTP-01 实际签发成功，证书落在 `acme/`；渲染出的 DNS-01 配置通过 `sing-box check`。真实 Let's Encrypt / DNS 服务商无法在沙箱测试。
- NaïveProxy：自写的最小 naive 客户端（HTTP/2 CONNECT + padding）经面板下发的 sing-box 连通，错误密码返回 407；没有可用的官方 naiveproxy / cronet 客户端。
- 端口跳跃：面板 + agent 端到端，sing-box 客户端只知道 46000-46100（订阅里没有主端口）也能连通，入站 UDP 计数正常；Mihomo `-t`、sing-box `check` 均接受生成的跳跃配置；生成的 nft 脚本经真实 `nft -c` 校验。

**暂不做**
- TUIC 端口跳跃（客户端支持不普遍）
- 多用户、WARP / 分流、链式代理、负载均衡（单用户场景不需要）

### 验证（每个阶段）

- [ ] `(cd go-backend && go test ./...)`
- [ ] `(cd go-gost && go build ./...)`，以及 `x` 模块的单元测试
- [ ] `(cd vite-frontend && pnpm run build && pnpm run lint)`
- [ ] 在真实节点上实测：安装内核 → 创建入站 → 客户端导入订阅 → 能正常连接 → 流量计数增长 → agent 重启后内核自动恢复
- [ ] 中转实测：模式 A / B 各对接一次 → 订阅出现中转节点 → 经中转连通（TCP 和 UDP 协议各一个）→ 开启白名单后直连失败、中转正常

## 文件变更清单（预计）

| 文件 | 变更类型 |
|---|---|
| `go-backend/internal/store/model/node_kernel.go` | 新增（含 `NodeInboundRelay`） |
| `go-backend/internal/store/repo/repository.go` | 修改（AutoMigrate 列表） |
| `go-backend/internal/store/repo/node_kernel.go` | 新增（Repository 方法） |
| `go-backend/internal/kernel/*` | 新增 |
| `go-backend/internal/http/handler/kernel_*.go` | 新增（含 `kernel_relay.go` 对接中转） |
| `go-backend/internal/http/handler/mutations.go` | 可能修改（模式 B 回环目标校验放行） |
| `go-backend/internal/http/handler/handler.go` | 修改（路由注册） |
| `go-backend/internal/ws/server.go` | 修改（处理 `kernel_status` / `kernel_traffic` 消息） |
| `go-backend/tests/contract/kernel_*_test.go` | 新增 |
| `go-gost/x/kernel/*` | 新增 |
| `go-gost/x/socket/websocket_reporter.go` | 修改（命令路由、上报） |
| `vite-frontend/src/api/kernel.ts` | 新增 |
| `vite-frontend/src/pages/inbound.tsx`、`src/pages/inbound/*` | 新增 |
| `vite-frontend/src/pages/node/*` | 修改（内核标签页） |
| `vite-frontend/src/App.tsx`、侧边栏菜单 | 修改 |
