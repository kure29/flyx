# 订阅与客户端

**订阅** 页管理所有订阅链接。思路和 Sub-Store 一样：**来源 → 处理 → 输出**。页面上方分成 **订阅** 和 **节点组** 两个标签：

- **节点组**：从节点和订阅源里挑选，筛选、改名、排序后存成一份节点列表，见 [节点组与订阅源](./subscription-sources.md)
- **订阅**：选一个或几个节点组，再加分流规则、链式代理和模板，客户端拿到的格式按 UA 自动识别（也可以复制指定格式的链接）

同一个节点组可以给多个订阅用，改一次所有订阅都跟着变。

升级前的订阅链接会变成这里的“我的订阅”，链接不变；原来的订阅设置和共享用户也会自动迁移过来。升级到有节点组的版本时，每个订阅原来的来源、筛选、命名和脚本会搬进一个同名的节点组，订阅输出的节点不变。

## 管理订阅

所有订阅在同一个列表里，靠名称区分（例如“我的手机”“小明”）。

- 新建的订阅都有自己的凭据，可以设到期和流量上限，适合分给别人，见 [凭据、到期与流量](./subscription-users.md)

编辑订阅分成 **基本 / 节点 / 规则 / 预览** 四页，在任一页都能直接保存：

- **基本**：名称、到期、流量上限、每月重置、备注
- **节点**：选用哪些 [节点组](./subscription-sources.md)，按列表顺序输出，可以上下调整
- **规则**：[分流规则、按地区分组和链式代理](./subscription-rules.md)
- **预览**：打开就自动生成，看最终的节点和链式角色

链式代理默认折叠，标题旁显示当前设置了什么（例如“改名 2 条、国旗”），点开即可修改。

订阅列表右上角可以在列表和卡片之间切换，选择会记在浏览器里。

## 导入到客户端

点订阅打开详情，按客户端选择：**点客户端直接唤起 App 导入**，右侧按钮复制该格式的链接。也可以复制上面的通用链接，客户端按 UA 自动拿到对应格式。

| 客户端 | 格式 | 一键导入 | 链式代理 | 分流规则 |
|--------|------|----------|----------|----------|
| mihomo（Clash Verge Rev、Mihomo Party、FlClash 等） | Clash / Mihomo 配置 | `clash://` | ✓ | ✓（需 mihomo 内核） |
| sing-box（SFA / SFI / SFM） | sing-box 配置 | `sing-box://` | ✓ | ✓（需 1.12 及以上） |
| Shadowrocket | Clash 节点列表 | `shadowrocket://` | ✓（每个中转各生成一个“落地（经 中转）”节点） | ✓（下发完整配置） |
| Loon | Loon 节点 | `loon://` | ✗ | ✓（下发完整配置，复制链接导入） |
| Quantumult X | QX 节点 | `quantumult-x://` | ✗ | ✓（下发完整配置，复制链接导入） |
| Surge | Surge 配置 | `surge://` | ✓ | ✓ |
| Stash | Clash 配置 | `stash://` | ✓ | ✓（规则集用 YAML 格式） |
| 通用 | Base64 分享链接（v2rayN、NekoBox 等） | 仅复制 | ✗ | ✗ |

- 客户端不支持的协议会自动略过（完整列表见下表），例如 Loon 没有 TUIC，Quantumult X 没有 Hysteria2 / TUIC，Surge 没有 VLESS；VLESS Encryption 只在 Clash / Mihomo 和通用订阅里，WireGuard 只在 Clash / Mihomo、sing-box 和通用订阅里
- Loon 和 Quantumult X 的节点格式没有前置代理参数，订阅里的落地节点是直连
- 指定格式的链接：在订阅链接后加 `?format=clash`（或 `singbox`、`shadowrocket`、`loon`、`qx`、`surge`、`stash`、`base64`）

### 各格式不包含的协议

| 订阅格式 | 不包含 |
|----------|--------|
| Clash / Mihomo | XHTTP、Snell、NaïveProxy |
| sing-box | XHTTP、VLESS Encryption；Snell 只给 1.14 及以上的客户端 |
| Shadowrocket | XHTTP、Snell、NaïveProxy、WireGuard、VLESS Encryption |
| Loon | TUIC、gRPC、XHTTP、Snell、NaïveProxy、WireGuard、VLESS Encryption |
| Quantumult X | Hysteria2、TUIC、gRPC、XHTTP、ShadowTLS、Snell、NaïveProxy、WireGuard、VLESS Encryption |
| 通用 base64 | Snell、HTTP 代理 |
| Surge | VLESS、Reality、gRPC、XHTTP、NaïveProxy、WireGuard |

Snell 只在 Surge 和 sing-box 1.14+ 的订阅里，原因见 [Snell 用哪个内核 - 客户端和出站](./kernels.md#客户端和出站)。

## 其他

- 点订阅可以导入到客户端、复制各种格式的链接、编辑、重置链接或删除
- 所有订阅都包含在 [备份](./backup.md) 的“内核与入站”里
