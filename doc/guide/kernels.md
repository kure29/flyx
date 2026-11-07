# 内核与协议

内核是服务器上真正跑代理协议的程序。面板支持三种内核，一台服务器可以同时装多个，每个入站属于其中一个内核。

## 安装内核

**服务器** 页 → **内核** 标签 → **安装内核** → 选择服务器和内核。

| 内核 | 版本 | 下载与校验 |
|------|------|------------|
| Xray | 默认最新版，可指定 | GitHub Release，用官方校验文件校验 |
| sing-box | 默认最新版，可指定 | GitHub Release，用 Release 资产的 SHA256 校验 |
| Snell | 5.0.1（默认）/ 6.0.0（测试版）/ 4.1.1 | 只从官方 `dl.nssurge.com` 下载，使用内置的校验值；6.0.0 没有 armv7 版本 |

内核按服务器分组显示。点击内核可以启停、重启、查看日志、更换版本或卸载；服务器重启后内核会自动恢复运行。国内服务器下载慢见 [安装服务器 - 国内服务器安装慢](./agent.md#国内服务器安装慢)。

## 怎么选

| 想要 | 选 |
|------|----|
| 不用域名和证书、通用性最好 | Xray 或 sing-box 的 **VLESS + Reality + Vision** |
| 弱网、高延迟、丢包多的线路 | sing-box 的 **Hysteria2** 或 **TUIC** |
| 抗量子加密（VLESS Encryption）、XHTTP | Xray |
| 经中转时让落地看到客户端真实 IP | Xray（PROXY protocol） |
| 自动申请证书（ACME）、NaïveProxy、AnyTLS、ShadowTLS | sing-box |
| 给 Surge 用 Snell | sing-box 内置 Snell，或 Snell 内核，区别见 [Snell 用哪个内核](#snell-用哪个内核) |

## 协议支持

| 协议 | Xray | sing-box | Snell 内核 |
|------|:----:|:--------:|:----------:|
| VLESS（TCP / WS / gRPC，Reality / TLS） | ✓ | ✓ | |
| VLESS + XHTTP | ✓ | | |
| VLESS Encryption | ✓ | | |
| VMess（TCP / WS / gRPC） | ✓ | ✓ | |
| Trojan（TCP / WS / gRPC） | ✓ | ✓ | |
| Shadowsocks / SS2022 | ✓ | ✓ | |
| SS2022 + ShadowTLS | | ✓ | |
| Hysteria2（可选端口跳跃） | | ✓ | |
| TUIC v5 | | ✓ | |
| AnyTLS | | ✓ | |
| NaïveProxy | | ✓ | |
| Snell / Snell + ShadowTLS | | ✓（1.14+） | ✓ |
| WireGuard | ✓ | ✓ | |
| SOCKS5 / HTTP 代理 | ✓ | ✓ | |

## 功能差异

| 功能 | Xray | sing-box | Snell 内核 |
|------|:----:|:--------:|:----------:|
| 自签证书 / 服务器上的证书文件 | ✓ | ✓ | — |
| 自动申请证书（ACME） | | ✓（1.14+） | — |
| 获取真实客户端 IP（PROXY protocol） | ✓ | | |
| [分流与链式出站](./routing.md) | ✓ | ✓ | |
| 作为别的入站的出站节点 | ✓ | ✓ | |
| 订阅用户单独凭据 | ✓ | ✓ | |
| 入站流量统计（nftables） | ✓ | ✓ | ✓ |

- Xray 作出站时只能连接 VLESS / VMess / Trojan / Shadowsocks（不含插件和 ShadowTLS）/ SOCKS5 / HTTP 节点；要连 Hysteria2、TUIC、AnyTLS 等请用 sing-box
- 订阅用户单独凭据只对支持的协议有效：Shadowsocks、Snell、WireGuard 在哪个内核上都共用凭据，见 [凭据、到期与流量](./subscription-users.md)
- 用到 1.14 功能（ACME、内置 Snell、经 Snell 节点出站）的 sing-box 不能换回 1.14 以前的版本，面板会拒绝

## Snell 用哪个内核

Snell 有两种跑法：装独立的 **Snell 内核**（Surge 官方的 snell-server），或用 **sing-box 1.14 起内置的 Snell**。两者客户端写法一样，客户端可以用 Surge，或 sing-box 1.14 及以上（SFA / SFI / SFM）。

| | sing-box 内置 Snell | Snell 内核（snell-server） |
|--|---------------------|---------------------------|
| 程序 | sing-box 1.14 及以上 | Surge 官方 snell-server 5.0.1 / 6.0.0（测试版）/ 4.1.1，闭源 |
| 客户端版本 | v4、v5、v6 同时可用 | 看装的版本：5.x 接受 v4 / v5，6.0.0 只接受 v6 |
| Snell v6 | ✓（PSK 至少 12 位，可选流量整形） | ✓（装 6.0.0 测试版，可选流量整形） |
| v5 的 QUIC 代理模式 | 不支持，UDP 改走 TCP 转发 | ✓（5.x；v6 已取消这个模式） |
| ShadowTLS | sing-box 自己处理，不用额外程序和端口 | 另装 shadow-tls，占用一个本机后端端口 |
| 分流、链式出站 | ✓ | |
| 和其他协议共用一个内核 | ✓ | 只跑 Snell |
| 多用户（userkey） | 不支持，订阅用户共用 PSK | 不支持，订阅用户共用 PSK |

怎么选：

- 想要 **v6**、要 **分流 / 链式出站**，或者服务器上已经装了 sing-box：用 **sing-box 内置 Snell**
- 依赖 **v5 的 QUIC 代理模式**（UDP 走 QUIC），或者只想用 Surge 官方程序：用 **Snell 内核**
- 想用 Surge 官方的 v6：Snell 内核装 **6.0.0**。注意它是测试版，**只接受 v6 客户端**（v4 / v5 客户端连不上），同一台服务器不能再跑 v4 / v5 入站；需要同时给新老客户端用，就再开一个 sing-box 内置 Snell 入站，或保留 5.0.1

新建入站时两个内核都能选 **Snell** / **Snell + ShadowTLS** 模板，在 **Snell 版本** 里选客户端版本：

- sing-box 上选 v4 或 v5 时，跑的都是 sing-box 的 v5 服务端（没有 QUIC 模式的 v5 和 v4 协议相同）；选 v6 跑 v6 服务端
- v6 可以选 **流量整形**：默认，或 unshaped（不做整形，更快但更容易被识别）；客户端必须选同样的模式，订阅和分享会自动带上
- Snell 内核上，入站的版本要和服务器安装的 Snell 对得上：v4 入站用 4.x 或 5.x，v5 入站用 5.x，v6 入站用 6.0.0；对不上时保存入站或更换内核版本会被拒绝。服务器装的是 6.0.0 时，新建入站默认就是 v6

::: info 为什么不支持多用户
sing-box 的 Snell 开启多用户后，只带 PSK、不带 userkey 的客户端会被直接断开，现有链接都会失效。所以面板和 Shadowsocks 一样只用一个 PSK：订阅用户共用凭据，停用后订阅失效，但已经导入的配置仍能连接。
:::

### 客户端和出站

- Snell 没有通用分享链接，**分享** 里显示 Surge 配置行（v6 为 `version=6`）
- **订阅**：Surge 订阅都包含 Snell；sing-box 订阅只给 1.14 及以上的客户端（面板按客户端上报的版本判断），旧客户端遇到 Snell 会整个配置加载失败，所以拿不到；Clash / Mihomo 只支持 Snell v1–v3，不包含
- **作为出站节点**：Snell 入站（两种内核搭的都行）可以被 sing-box 1.14 及以上内核的入站选作 [链式出站或分流去向](./routing.md)；Xray 不能连接 Snell
