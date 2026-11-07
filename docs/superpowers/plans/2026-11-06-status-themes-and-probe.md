# 状态页主题与探针：分期计划

目标：状态页做成可换主题（参考 [monitor](https://github.com/monitor-probe/monitor) 的公开页主题），
内置「FLYX 默认」主题，再把 [LuminaPlus](https://github.com/kure29/Monitor-Theme-LuminaPlus)
重构一份适配 FLYX 的版本；探针（agent 上报的数据）参考 monitor 补齐，让主题有数据可展示。

新对话接手时：先读本文件和 `doc/guide/status-theme-dev.md`（主题包格式与公开接口），
再看「当前状态」一节，从第一个未勾选的期开始。每期做完更新本文件的勾选和备注。

## 当前状态

- [x] 第一期：主题机制 + 内置默认主题（PR #215，已随 3.7.0 发布）
- [x] 第二期：探针补齐 + 公开历史接口 + 主题设置（计划随 3.8.0 发布）
  - [x] 2A 探针 + 历史接口 + 默认主题详情页（PR #216，已合并）
  - [x] 2B 主题设置（PR #217，已合并）
- [x] 第三期：LuminaPlus for FLYX（kure29/Flyx-Theme-LuminaPlus v1.0.0，已发布）
- [ ] 默认主题改版：shadcn/ui 风格小卡片 + 列表切换，公开接口加回程线路（分支 `feat/status-theme-shadcn`，计划随 3.9.0 发布）
- [ ] 第四期（可选）：节点公开字段（分组、地区、公开备注、价格与账单）

## 已确定的决定

- 状态页由**后端**提供：`/status/` 下返回当前所选主题，内置主题用 `go:embed` 打进 paneld；
  安装的主题放在数据目录 `themes/<short>/`（`STATUS_THEMES_DIR` 可改）。
- 主题包与 monitor 相同的结构：`theme.json` + `preview.png`（可选）+ `dist/`；
  `theme.json` 多一个必填的 `flyxApi`（公开接口版本，当前 1），`short` 不能是 `default`。
  monitor 的主题不能直接装：接口是 FLYX 自己的。
- 不在后端模拟 monitor 的接口（接口面太大、随 monitor 改版而变）；主题改用 FLYX 接口。
- 主题设置用 `theme.json` 的 `config` 声明，格式照搬 monitor（`string/text/number/boolean/select/title`），
  后台按声明画表单；主题**不**自带设置页（避免主题碰管理员 token）。
- 接口有破坏性变化时把 `statustheme.APIVersion` 加 1；只加字段不加版本。
- 安全：主题与面板同源，能读到浏览器里的登录 token。只允许管理员安装，界面和文档提示只装信任的主题。
  以后如有需要再考虑把状态页放到独立域名。
- 不做「模拟延迟」（LuminaPlus 原版有）：会误导访客。

## 第一期（已完成，PR #215）

- `go-backend/internal/statustheme/`：清单解析、安装（大小/条目/路径/链接限制，先解压校验再原子替换）、
  删除、预览、`Serve`（无扩展名路由回退 `index.html`，`assets/` 长缓存，其余 `no-cache` + ETag）。
- `go-backend/internal/http/handler/status_theme.go`：`/status`、`/status/*` 与
  `/api/v1/status-theme/{list,select,delete,upload,install,update,preview}`；所选主题存配置 `status_page_theme`。
- 默认主题：`vite-frontend/src/status-theme/`，`pnpm run build:status-theme` → `dist-status-theme/`，
  `make status-theme` 复制到 `go-backend/internal/statustheme/builtin/dist`（git 忽略）。
- 后端镜像从仓库根目录构建（Node 阶段构建主题）；nginx 反代 `/status`，上传放行 33 MiB。
- 顺带修复：SQLite 旧表缺列（`node.nft_error`）导致添加服务器失败，改为按模型全部字段补列。
- CI 新增「Backend Docker Image」：构建 amd64 镜像并检查 `/status/` 提供内置主题。

## 第二期：探针补齐 + 公开历史接口 + 主题设置

分两个 PR：**2A** 探针 + 历史接口 + 默认主题详情页（需要升级 agent），**2B** 主题设置（只升级面板）。
接口都是新增字段 / 新增路径，`flyxApi` 仍为 1。

### 2A-1 agent 上报（参考 monitor agent 的 facts + report 两类数据）

现状：agent（`go-gost/x/socket/websocket_reporter.go`，gopsutil v3）每 5 秒上报一次 `SystemInfo`：
uptime、累计收发字节（除 `lo*` 外所有网卡）、CPU/内存/磁盘（只看 `/`）使用率、load、TCP/UDP 连接数、网速、
核心数、内存和磁盘的已用/总量。monitor agent 另有 swap、进程数，以及连接时和每 60 秒复查一次的 facts
（系统、内核、架构、虚拟化、CPU 型号）。

**实时字段**（`SystemInfo` 新增，都是 `omitempty`，旧 agent 不报 = 无数据）：

| 字段 | 来源 |
|---|---|
| `swap_used` / `swap_total` | `mem.SwapMemory()` |
| `process_count` | `load.Misc().ProcsTotal`，失败时数 `/proc` 下的数字目录 |

**静态信息 facts**（`SystemInfo.host`，可选对象）：连上后第一次上报带上，之后每 60 秒重新采集一次，变了才再带，其余上报不带。

| 字段 | 来源 |
|---|---|
| `os` | `/etc/os-release` 的 `PRETTY_NAME`（读不到用 `host.Info().Platform + PlatformVersion`） |
| `kernel` | `host.Info().KernelVersion` |
| `arch` | `host.Info().KernelArch` |
| `cpu_name` | `cpu.Info()[0].ModelName` |
| `virtualization` | 先判断容器（照 monitor：`/proc/vz` 且无 `/proc/bc` → openvz；`/run/systemd/container`；`/.dockerenv` → docker；`/run/.containerenv` → podman；`/proc` 上挂了 lxcfs → lxc），否则 `host.Virtualization()` 的 guest 系统，裸机为 `none` |

不做：温度（大多数 VPS 没有）、GPU。

**面板**：
- `node` 表新增 `os`、`kernel_version`、`arch`、`cpu_name`、`virtualization`（text，默认 ''）。收到 `host` 才更新（值没变不写库）。
  SQLite 旧表由 `prepareSQLiteLegacyColumns` 自动补列。
- `statusLiveStore` 加 swap 和进程数。
- 后台服务器列表悬停（现在显示核心数和容量的地方）加系统、架构、虚拟化、CPU 型号。

### 2A-2 历史数据

现状：`node_metric` 每 5 秒一行，30 秒批量写入，保留 `monitor_retention_days` 天（默认 7）；
`node_latency_sample` 同样的保留期。只有管理员接口能读。

- `node_metric` 新增 `swap_usage`（%）、`process_count`。
- Repository 新增按时间桶聚合查询：`GROUP BY timestamp / bucket`，取 AVG（CPU、内存、Swap、磁盘、网速、load1、连接数、进程数）。
  每个范围最多约 360 个点（1 小时 10 秒一个点，7 天约 28 分钟一个点）。
- 新增公开接口（JWT 中间件 `shouldSkip` 放行）：
  - `GET /api/v1/public/status/history?node={id}&range=1h|6h|24h|7d`：`{range, bucketSec, points: [{t, cpu, mem, swap, disk, load, tcp, udp, procs, netIn, netOut}]}`。
    状态页「显示网速和流量」关闭时不返回 `netIn` / `netOut`。范围超出保留天数时只返回已有的数据。
  - `GET /api/v1/public/status/ping-history?node={id}&range=...`：`{points: [{name, samples: [{t, ms|null, loss}]}]}`，
    同一时间桶内取平均延迟、平均丢包；只给测试点名称，不给地址和错误信息。
  - 状态页未开启 → code 404；节点不存在、已删除、被隐藏 → 同样的 404（不暴露隐藏节点是否存在）；参数不合法 → 400。
  - 按 (节点, 范围) 缓存：1h 为 10 秒，其余 60 秒；响应带 `Cache-Control: public, max-age=…`，与 `/public/status` 一样能被 CDN 挡住。
- 契约测试：未开启、隐藏节点、正常、参数错误、关闭流量时没有网速字段。

### 2A-3 状态页接口和默认主题

- `/public/status` 的节点新增 `swap`、`swapUsed`、`swapTotal`、`procs`。
- 新开关「**显示系统信息**」（`status_page_show_host`，**默认关**）：打开后节点再带 `os`、`arch`、`virtualization`、`cpuName`。
  不公开内核版本：它对访客没用，却方便找可利用的漏洞。
- 默认主题新增节点详情页 `/status/node/{id}`（点卡片进入）：CPU / 内存 / Swap / 磁盘、网速、负载与连接数、各测试点延迟和丢包的历史图表，
  可切换 1h / 6h / 24h / 7d。图表用面板已有的 recharts。卡片上加 Swap 一项（agent 未上报时隐藏）。
- 文档：`status-theme-dev.md` 补新字段和两个历史接口；`status-page.md` 补详情页和「显示系统信息」；changelog 写明要升级 agent（不升级也能用，只是没有新数据）。

### 2B 主题设置

- `theme.json` 的 `config` 声明设置表单，格式照搬 monitor：`[{type: "title", label}, {key, type: string|text|number|boolean|select, label, help, default, min, max, options:[{value,label}]}]`。
- 后端：
  - `GET /api/v1/public/status/theme-config`：当前所选主题已保存的设置（JSON 对象，所有访客同一份）；状态页未开启时 404。
  - `POST /api/v1/status-theme/config/get {short}`、`POST /api/v1/status-theme/config/save {short, values}`（管理员）。
  - 存在配置 `status_theme_config_<short>`，上限 64 KiB；值不按声明校验（与 monitor 一样，由主题自己校验读到的值）。删除主题时一并删除它的设置。
- 前端：主题卡片加「设置」（主题声明了 `config` 才显示），弹窗按声明画表单。解析规则照 monitor `web-admin/src/lib/api.ts` 的
  `configForm` / `fits` / `configValues`：非法项、重复 key、默认值不合法的项跳过；下面没有字段的标题跳过；已保存的值不合法时显示默认值。
- 内置主题暂不声明设置。
- 文档：`status-theme-dev.md` 补 `config` 格式和 theme-config 接口。

### 不做 / 以后再说

- SSE 实时推送：现在每 10 秒轮询，后端 5 秒缓存，够用；LuminaPlus 需要秒级时再做。
- 网卡和磁盘统计口径：monitor 不统计 docker、veth、br-、tun、wg 等虚拟网卡，磁盘统计所有真实分区；FLYX 现在除 `lo` 外都算、磁盘只看 `/`。
  改口径会改变每台服务器的流量统计（`node_traffic`，影响本月流量和相关提醒），**第二期不改**，要改需单独决定。

## 第三期：LuminaPlus for FLYX

- 新仓库（建议 `kure29/Flyx-Theme-LuminaPlus`，需要用户在 GitHub 上创建并授权给会话），
  从 `kure29/Monitor-Theme-LuminaPlus` 起步，保留 MIT 许可和原作者署名（原版来自 Komari-Theme-LuminaPlus）。
- 改动范围，尽量只动数据层，组件少改，方便以后同步原版界面：
  - `src/services/api.ts`、`src/types/models.ts`：换成 FLYX 接口的适配层（`/public/status`、历史接口、theme-config），
    把 FLYX 数据映射成主题内部的 `NodeInfo` / `NodeRealtime` / `LoadRecord` 等结构。
  - 去掉 monitor 专有部分：`/api/ws`、Ping 任务分配、模拟延迟（`useFakePing`、`SimulatedPingBadge`）、
    自带的 `ThemeManage` 设置页（改为 `theme.json` 的 `config` 声明）、登录相关（`useAuth`）。
  - `vite.config.ts` 设 `base: "/status/"`，router basename `/status`；开发代理 `/api` 到 FLYX 后端。
  - 地区：FLYX 没有 region 字段，从节点名开头的国旗 emoji 推断（第四期有字段后改用字段）。
  - FLYX 没有的数据（价格、账单、标签、流量配额等）沿用原版「没数据自动隐藏」的逻辑。
- `theme.json`：`short: "luminaplus"`、`flyxApi: 1`、`url` 指向新仓库；release 工作流打 tag 生成 `theme.tar.gz`
  （tag 与 `version` 一致）。
- 在 FLYX 后台「从 GitHub 安装」实测：四种外观、背景图/视频、四种视图、详情页图表、移动端。
- 依赖第二期的历史接口和主题设置；可以先做首页（只用 `/public/status`），再接详情页。

## 第四期（可选）：节点公开字段

让 LuminaPlus 的分组、地区筛选、资产统计、费用能用。都要管理员在服务器编辑里填，且单独控制是否公开：

- `node` 加列：`public_group`、`region`（ISO 国家代码）、`public_remark`、`price`、`currency`、`billing_cycle`（已有 `renewal_cycle` 可复用）。
- 状态页设置加「显示价格」开关（默认关），`/public/status` 只在开关打开时下发价格。
- 现有的「显示到期日」开关逻辑保持。

## 参考

- monitor 主题托管：`src/frontend.rs`（安装/提供/删除）、`src/api.rs`（`update_theme`、`theme_config`、`save_theme_config`）、
  `web-admin/src/lib/api.ts`（主题设置表单解析）。
- LuminaPlus 结构：`src/services/api.ts`、`src/types/models.ts`、`src/hooks/*`、`src/components/{node,instance,shell}`，
  设计变量在 `src/styles/tokens.css`。
