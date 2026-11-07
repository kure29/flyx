# 常见问题

想先看看面板长什么样，可以打开 [在线演示](https://kure29.github.io/flyx/demo/)。这里没找到答案的话，可以到 [Telegram 交流群](https://t.me/flyx_chat) 问，或者在 GitHub [提交 issue](https://github.com/kure29/flyx/issues/new/choose)。

## 面板

### WHMCS 等集成登录提示“验证码校验失败”？

启用登录验证码并配置完整的 Cloudflare Turnstile 密钥后，所有密码登录都必须提交有效的 `captchaId`，包括 WHMCS 等集成。`X-FLVX-API-Client` 是客户端可自行填写的请求头，不再提供验证码豁免。依赖旧豁免的集成需要适配验证码流程；验证码令牌只能使用一次。

### 安装脚本提示 "Docker command not found"？
请确保您的系统已安装 Docker 和 Docker Compose。
- Ubuntu/Debian 安装 Docker: `curl -fsSL https://get.docker.com | bash`

### 面板无法访问 (Connection Refused)？
1. 检查防火墙是否放行了前端端口（默认 `6366`）。
2. 检查容器是否正常运行: `docker ps`。
3. 查看容器日志: `docker logs flux-panel-backend` 或 `docker logs vite-frontend`。

### 用 IP 访问时复制失败

浏览器只在 HTTPS 页面里完全开放复制功能。面板会自动改用兼容方式复制，仍失败时会提示；建议通过 HTTPS 域名访问面板，或长按文字手动复制。

### 面板自升级提示“部署docker-compose.yml不可用”

见 [升级](./upgrade.md#修复面板内升级)。

### IPv6 无法使用？
面板安装脚本会自动尝试配置 Docker 的 IPv6。如果失败，请手动检查 `/etc/docker/daemon.json` 配置，确保 `ipv6: true` 且分配了正确的 `fixed-cidr-v6` 子网。

## agent

### 服务器显示离线？
1. 检查服务器与面板服务器之间的网络连通性。
2. 确认在服务器端安装时输入的 **面板地址** 和 **密钥** 是否正确。
3. 检查服务器端服务状态：`systemctl status flyx-agent`（Alpine 用 `rc-service flyx-agent status`；3.4.0 之前安装的节点是 `flux_agent`）。
4. 查看服务器端日志：`journalctl -u flyx-agent -f`。

### 部署或升级服务器提示 “API rate limit exceeded”？

面板要向 GitHub 查询最新版本号。不登录的查询每个 IP 每小时只有 60 次，VPS 的 IP 常常和别人共用，可能早就被用完。面板会把查询结果缓存 10 分钟，查询失败时用上次的结果，正式版还会改用不受这个限制的方式查询，所以一般不会再遇到。

测试版或面板刚启动时仍可能出现，等一小时再试，或者给面板配一个 GitHub token：在 GitHub 的 Settings → Developer settings → Personal access tokens 新建一个 fine-grained token（只需公开仓库只读，不用勾任何权限），写进部署目录的 `.env`：

```bash
GITHUB_TOKEN=github_pat_xxx
```

然后 `docker compose up -d` 重建后端，限额变为每小时 5000 次。旧版 `docker-compose.yml` 需要在 `backend.environment` 里加上 `GITHUB_TOKEN: ${GITHUB_TOKEN:-}`。

### 只有 TCP 能通，UDP 不通？
请检查服务器防火墙和安全组（AWS/阿里云/腾讯云等）是否同时放行了对应端口的 **TCP 和 UDP** 协议。

## 内核与入站

### 内核状态显示“异常”

在“内核”标签里点击该内核 → “日志”查看原因，常见的有端口被占用、证书路径不存在、IPv6 被禁用（见 [新建入站 - 监听地址](./nodes.md#监听地址)）。

### Snell 用哪个版本、哪个内核

有两种：Snell 内核（官方 snell-server：5.x 支持 v4 / v5，含 v5 的 QUIC 代理模式；6.0.0 测试版只支持 v6）和 sing-box 1.14 起内置的 Snell（支持 v4 / v5 / v6、分流和链式出站，不支持 QUIC 模式）。对比和选择见 [Snell 用哪个内核](./kernels.md#snell-用哪个内核)。

用 Snell 内核时，入站的 Snell 版本要和服务器安装的版本对得上：v5 入站需要 Snell 5.x，v6 入站需要 Snell 6.0.0（测试版，只接受 v6 客户端）。

### sing-box 提示需要 1.14 及以上

自动申请证书（ACME）和内置 Snell 是 sing-box 1.14 才有的功能。在 **服务器 → 内核** 点该 sing-box → 更换版本，选最新版即可。已经有这类入站时，也不能把 sing-box 换回 1.14 以前的版本。

### ACME 一直申请不到证书

确认域名已解析到服务器；HTTP 验证时 80 端口不能被其他程序占用，并且要能从公网访问；也可以改用 DNS 验证。

### Reality 用 Clash / Mihomo 连不上

Xray 26.7.11 起，Reality 未设置“最低客户端版本”时默认只接受 26.3.27 及以上的客户端，而 Mihomo（Clash Verge、Stash、FlClash 等）上报的版本是 1.8.2，会被拒绝。面板默认把最低客户端版本设为 `1.0.0`，所有客户端都能连接；如需调整，在 Reality 设置里修改“最低客户端版本”。也可以在安装内核时选择旧版本的 Xray。

服务器装的是 Xray 25.5.16 及以上时，Mihomo 订阅里的 VLESS Reality 节点会带上 `support-x25519mlkem768: true`，启用抗量子的 X25519MLKEM768 密钥交换。这个选项需要服务端支持，所以 sing-box 内核、更旧的 Xray 和导入的节点都不会加；导入的节点保留订阅源本身的设置。

## 订阅与流量

### 流量一直是 0

检查服务器是否安装了 nftables；流量每分钟左右同步一次。订阅流量还需要 agent 和面板一起更新到最新版，Shadowsocks、Snell 等共用凭据的入站不按订阅计数，见 [凭据、到期与流量](./subscription-users.md#流量上限)。

### 某个客户端里少了几个节点

客户端不支持的协议会自动略过，例如 Surge 没有 VLESS，Clash 没有 Snell，1.14 以前的 sing-box 客户端也拿不到 Snell（升级客户端后重新更新订阅即可）。各格式不包含哪些协议见 [订阅与客户端](./subscription.md#各格式不包含的协议)。

## 延迟与回程

### 节点卡片上的延迟是从哪里测的

设置了 [延迟测试点](./latency.md) 时，是所在服务器到各测试点（各地运营商）的延迟；没设置时是面板服务器到入站的延迟，只能作为参考。导入的节点没有 agent，只有面板发起的连通性检测。

### 回程提示“agent 版本过旧”

回程检测需要新版 agent。到 **服务器** 页把服务器升级到最新版，升级后面板会马上重新检测，不用等第二天。

### 回程提示“没有 root / NET_RAW 权限”

路由追踪要发原始 ICMP 包，agent 需要以 root 运行，或者有 `CAP_NET_RAW` 权限。用面板生成的命令安装的 agent 默认是 root。

### 回程显示“未识别”

路由没有经过 CN2、163、9929、4837、CMI 这些骨干网（比如走的是普通 IX 或运营商没有回应），面板就认不出线路。点 **回程** 可以看每一跳的 IP。

## 数据库

### 如何切换到 PostgreSQL？
在 `.env` 文件中设置 `DB_TYPE=postgres`，并让 `DATABASE_URL` 与 `POSTGRES_*` 保持一致，然后执行 `docker compose up -d` 重启服务即可。使用安装脚本部署时，`POSTGRES_PASSWORD` 会自动随机生成并写入 `.env`。详见 [PostgreSQL](./postgresql.md)。

### 从 SQLite 迁移到 PostgreSQL 后数据丢失？
1. 确认迁移前已备份 SQLite 文件（`gost.db.bak`）。
2. 确认 `pgloader` 命令执行成功，检查其输出是否有报错。
3. 确认 `.env` 中 `DATABASE_URL` 的密码与 `POSTGRES_PASSWORD` 一致。
4. 详细迁移步骤参考 [PostgreSQL](./postgresql.md)。

### PostgreSQL 容器启动失败？
1. 检查 `POSTGRES_PASSWORD` 是否已设置（不能为空）。
2. 查看容器日志：`docker logs flux-panel-postgres`。
3. 如果是首次启动后修改了密码，需要删除旧的数据卷重新初始化：`docker volume rm postgres_data`。
