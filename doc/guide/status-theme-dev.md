# 状态页主题开发

状态页主题是一个独立的单页应用（任意框架），由面板在 `/status/` 下提供，数据来自面板的公开状态接口。机制参考了 [monitor](https://github.com/monitor-probe/monitor) 的公开页主题，但接口是 FLYX 自己的，monitor 的主题需要适配后才能使用。

内置主题的源码在仓库的 `vite-frontend/src/status-theme/`，可以作为参考。

## 主题包

发布 `theme.tar.gz`，解压后的根目录是：

```text
theme.json     主题信息（必需）
preview.png    后台主题列表里的缩略图（可选，建议 1280×720）
dist/          构建产物，至少有 dist/index.html
```

```bash
tar czf theme.tar.gz theme.json preview.png dist
```

不要把文件再套一层目录；GitHub 自动生成的 Source code 压缩包不是主题包。

### theme.json

```json
{
  "name": "LuminaPlus",
  "short": "luminaplus",
  "description": "高信息密度状态主题",
  "version": "1.0.0",
  "author": "kure29",
  "url": "https://github.com/kure29/Flyx-Theme-LuminaPlus",
  "flyxApi": 1
}
```

| 字段 | 说明 |
|------|------|
| `name` | 显示名称，必填 |
| `short` | 目录名，只能用小写字母、数字、`-`、`_`，最长 32 个字符；不能是 `default`。同一 `short` 再次安装会替换旧版本 |
| `version` | 版本号。「检查更新」把它和仓库最新 release 的 tag（去掉开头的 `v`）比较，相同则不更新 |
| `url` | 源码仓库，`https://github.com/作者/仓库` 形式时后台才显示「检查更新」 |
| `flyxApi` | 主题使用的公开接口版本，必填，当前为 `1`。比面板支持的版本新时面板拒绝安装 |
| `config` | 可选：主题设置表单，见下文 [主题设置](#主题设置)，格式与 monitor 主题的 `config` 相同 |

### 限制

包最大 32 MiB，解压后最多 64 MiB、2000 个条目、单个文件 8 MiB；只能包含普通文件和目录（不能有符号链接、硬链接），路径不能越出主题目录。

## 主题设置

在 `theme.json` 里用 `config` 声明设置项，后台主题卡片上会出现 **设置**，按声明生成表单；访客打开页面时由主题读取保存的值。

```json
"config": [
  { "key": "headline", "type": "string", "label": "标题文字", "default": "欢迎", "help": "显示在顶部" },
  { "type": "title", "label": "外观" },
  { "key": "view", "type": "select", "label": "默认视图", "default": "card",
    "options": [{ "value": "card", "label": "卡片" }, { "value": "list", "label": "列表" }] },
  { "key": "opacity", "type": "number", "label": "卡片透明度", "default": 90, "min": 0, "max": 100 },
  { "key": "dark", "type": "boolean", "label": "默认深色", "default": false },
  { "key": "footer", "type": "text", "label": "页脚说明", "default": "" }
]
```

| 字段 | 说明 |
|------|------|
| `type` | `string`（单行文字）、`text`（多行文字）、`number`、`boolean`、`select`，或 `title`（分组标题，只有 `label`） |
| `key` | 保存时用的键，不能重复 |
| `label`、`help` | 显示的名称和说明，可选；没有 `label` 时显示 `key` |
| `default` | 默认值，必须是这个类型能取的值（`select` 必须是某个选项的 `value`，`number` 必须在 `min`–`max` 内） |
| `options` | `select` 的选项，每项 `value` 为非空字符串，`label` 可选 |
| `min`、`max` | `number` 的范围，可选 |

不合规的项（类型未知、重复的 `key`、默认值不合法等）会被后台跳过，下面没有字段的标题也会跳过，不影响其他项。

后台只保存与默认值不同的项，所以主题之后改了某项的默认值，没改过这一项的站点会跟着变。读到的值可能来自旧版本的主题，**主题自己要检查类型和范围**，不合法时用默认值。

### `GET /api/v1/public/status/theme-config`

不需要登录，返回当前所用主题保存的设置；状态页未开启时 `code` 为 404。

```ts
interface PublicStatusThemeConfig {
  theme: string;                    // 当前所用主题的 short
  values: Record<string, unknown>;  // 只有改过的项；没改过的用 theme.json 里的默认值
}
```

设置对所有访客公开，不要用它存放密钥一类的内容。

## 构建

主题部署在 `/status/` 下，资源路径要以它为前缀。以 Vite 为例：

```ts
export default defineConfig({
  base: "/status/",
});
```

使用前端路由时把 basename 设为 `/status`（例如 React Router 的 `createBrowserRouter(routes, { basename: "/status" })`）。`/status/` 下不存在、且没有扩展名的路径会返回 `index.html`，由前端路由处理；有扩展名但不存在的文件返回 404。

缓存：`dist/assets/` 下的文件按带哈希的文件名处理，长期缓存；其他文件（包括 `index.html`）每次都会重新验证，所以切换或更新主题后刷新即可看到。

## 公开接口（flyxApi 1）

### `GET /api/v1/public/status`

不需要登录，返回面板统一的格式 `{code, msg, data, ts}`，`code` 为 `0` 表示成功。状态页未开启时 `code` 为 `404`，主题应显示相应提示。面板每 5 秒最多重新计算一次，建议主题每 10 秒左右刷新。

`data` 的结构（可选字段在没有数据时不出现）：

```ts
interface PublicStatus {
  title: string;          // 状态页标题
  timezone: string;       // 统计所用时区，如 "UTC+08:00"
  generatedAt: number;    // 毫秒时间戳
  days: string[];         // 近 30 天日期 "YYYY-MM-DD"，从旧到新
  showTraffic: boolean;   // 管理员是否允许显示网速和流量
  nodes: PublicStatusNode[];
  totals: {
    nodes: number; online: number;
    speedUp: number; speedDown: number;   // 字节/秒
    monthUp: number; monthDown: number;   // 字节
    d30: number | null;                   // 30 天平均可用率 %
  };
}

interface PublicStatusNode {
  id: number;
  name: string;           // 可能以国旗 emoji 开头，可据此按地区分组
  online: boolean;
  availability: {
    h24: number | null; d30: number | null;   // 可用率 %
    days: Array<number | null>;               // 与 days 对应的每日可用率
    down: Array<number | null>;               // 每日离线分钟数
  };
  cpu?: number; mem?: number; disk?: number;  // 使用率 %
  load?: [number, number, number];
  speedUp?: number; speedDown?: number;       // 字节/秒
  uptime?: number;                            // 秒
  tcp?: number; udp?: number;                 // 连接数
  cores?: number;
  memUsed?: number; memTotal?: number;        // 字节
  diskUsed?: number; diskTotal?: number;      // 字节
  monthUp?: number; monthDown?: number;       // 字节，showTraffic 为 false 时没有
  expiry?: string;                            // "YYYY-MM-DD"，管理员开启显示到期日时才有
  swap?: number;                              // Swap 使用率 %（agent 3.9.0 起；没有 Swap 时不出现）
  swapUsed?: number; swapTotal?: number;      // 字节
  procs?: number;                             // 进程数（agent 3.9.0 起）
  // 管理员开启「显示系统信息」时才有（agent 3.9.0 起），从不包含内核版本：
  os?: string;                                // 如 "Debian GNU/Linux 12 (bookworm)"
  arch?: string;                              // 如 "x86_64"、"aarch64"
  virtualization?: string;                    // 如 "kvm"、"lxc"、"openvz"、"docker"，物理机为 "none"
  cpuName?: string;
  ping?: Array<{
    name: string;         // 测试点名称
    ms: number | null;    // 最近一次延迟，null 为超时
    loss: number;         // 丢包率 %
    samples: number[];    // 近 1 小时的延迟，-1 为超时
    route?: string;       // 回程线路，如 "CN2 GIA"、"9929"、"4837"（面板 3.9.0 起，每天检测，检测过才有）
    routeV6?: string;     // 双栈服务器 IPv6 回程与 IPv4 不同时才有
  }>;
}
```

接口不会返回 IP、端口、密钥、用户和协议；回程只给线路名，不给每一跳的地址。

节点的 `id` 是它在状态页上的位置（从 1 开始），不是面板里的编号；服务器被隐藏或调整顺序后会变。

### `GET /api/v1/public/status/history?node=<id>&range=<1h|6h|24h|7d>`

一台服务器的资源历史，按时间段取平均。`node` 是上面的 `id`，`range` 默认 `1h`。
状态页未开启时 `code` 为 404；服务器不存在或被隐藏时 `code` 也为 404，两者回答相同；参数不对时为 400。
1h 的结果缓存 10 秒，其余 60 秒（响应带相应的 `Cache-Control`）。

```ts
interface PublicStatusMetricHistory {
  range: "1h" | "6h" | "24h" | "7d";
  bucketSec: number;      // 每个点的时间段：10、60、240、1680 秒
  points: Array<{
    t: number;            // 时间段开始的毫秒时间戳，从旧到新
    cpu: number; mem: number; swap: number; disk: number;   // 使用率 %
    load: number;         // 1 分钟负载
    tcp: number; udp: number; procs: number;
    netIn?: number; netOut?: number;  // 字节/秒，showTraffic 为 false 时没有
  }>;
}
```

没有数据的时间段不出现在 `points` 里（例如服务器离线时）。能查到多久以前取决于面板的监控数据保留天数。

### `GET /api/v1/public/status/ping-history?node=<id>&range=<1h|6h|24h|7d>`

同一台服务器各测试点的延迟历史，规则同上，最多 8 个测试点（固定的测试点在前）。

```ts
interface PublicStatusPingHistory {
  range: "1h" | "6h" | "24h" | "7d";
  bucketSec: number;
  points: Array<{
    name: string;         // 测试点名称，不含地址
    samples: Array<{
      t: number;
      ms: number | null;  // 这段时间里成功那几次的平均延迟；全部超时为 null
      loss: number;       // 平均丢包率 %
    }>;
  }>;
}
```

## 本地开发

1. 运行一个面板后端（`cd go-backend && go run ./cmd/paneld`，默认 `:6365`），在设置里开启状态页。
2. 在开发服务器里把 `/api` 代理到后端，例如 Vite：

   ```ts
   server: { proxy: { "/api": "http://127.0.0.1:6365" } }
   ```

3. 打包 `theme.tar.gz`，在 **设置 → 状态页 → 主题** 上传测试。

内置主题的开发方式：`cd vite-frontend && pnpm run dev:status-theme`，打开 `http://localhost:3001/status/`；`cd go-backend && make status-theme` 会构建它并放到后端嵌入的位置。

## 发布

在 GitHub 仓库的 release 里上传 `theme.tar.gz`，tag 与 `theme.json` 的 `version` 一致（`v1.2.3` 对 `1.2.3`）。用户在后台填仓库地址即可安装，之后点「检查更新」获取新版本。
