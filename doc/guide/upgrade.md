# 升级

## FLYX 命名兼容

新安装使用 `flyx-panel-backend`、`flyx-panel-frontend`、`flyx-panel-postgres` 容器，以及 `flyx-sqlite-data`、`flyx-postgres-data` 数据卷和 `flyx-network` 网络。安装脚本会把这些名称写入 `.env`。

旧部署升级时继续使用原容器、数据卷和数据库账号，不搬动已有数据。Compose 中保留的旧默认值用于兼容旧版面板直接下载新版配置的升级方式。手动部署如需使用新名称，应显式配置 `PANEL_BACKEND_CONTAINER`、`PANEL_FRONTEND_CONTAINER`、`PANEL_POSTGRES_CONTAINER`、`FLYX_SQLITE_VOLUME`、`FLYX_POSTGRES_VOLUME` 和 `FLYX_NETWORK`；已有部署请填写实际使用的名称，不能将数据卷改成一个空的新卷。自定义部署应在升级前核对这些参数。

版本变量统一为 `FLYX_VERSION`，升级脚本同时更新旧的 `FLUX_VERSION`，供旧模板或回退使用。其他配置优先读取 `FLYX_*`，未设置时兼容对应的 `FLUX_*` / `FLVX_*` 旧名称。通行证密钥的公开 origin 保持原值，浏览器的主题和列表偏好自动兼容旧键。

节点新安装使用 `flyx-agent` 服务及 `/etc/flyx-agent`。旧节点继续在 `/etc/flux_agent` 中运行 `flux_agent`，保留配置、内核状态、待上报流量及调优备份。新版程序按自身安装位置执行升级和回退。Docker 重装命令会识别已有容器的数据挂载，镜像拉取成功后才替换容器；发现新旧容器同时存在或挂载不明确时停止。

已有 nftables 表和计数 epoch 继续沿用，新建状态使用 FLYX 名称，避免仅因项目改名重置统计。只有升级前就已配置的 nftables 服务器会沿用旧的 `flvx` 表，之后新添加的服务器只使用 `flyx` 表。内部的用户凭据、数字标记及 API 数据字段保持兼容。旧镜像名作为过渡别名继续发布。

## 和 flux-panel / FLVX 装在同一台服务器

FLYX 3.4.0 之前和 flux-panel、FLVX 用的是同一套名字，所以安装脚本只在确认是 FLYX 自己的旧安装时才沿用旧名字，其他面板的安装不会被接管、改写或删除：

- **面板**：已有的 `flux-panel-backend` 容器只有镜像来自 `ghcr.io/kure29/`（或 3.9.0 发布时短暂使用的 `ghcr.io/seren233/`）时才当作 FLYX 的旧部署；新安装默认放在 `/opt/flyx-panel`（可用 `PANEL_DEPLOY_DIR` 指定），不会覆盖 `/root` 里其他面板的 `.env` 和 `docker-compose.yml`。默认端口 `6366` / `6365` 被占用时，脚本会建议下一个空闲端口。更新、卸载、迁移 PostgreSQL 只处理 FLYX 的部署
- **节点脚本安装**：`/etc/flux_agent` 只有 `config.json` 里的面板地址和本次安装的一致时才当作 FLYX 的旧节点，否则新装到 `/etc/flyx-agent`，两个 agent 各自运行；交互菜单里没有面板地址，更新或卸载前会先询问。脚本也不再删除 flux-panel 原版的 `gost.service` 和 `/usr/local/bin/gost`
- **节点 Docker 安装**：已有的 `flux_agent` 容器只有镜像来自 `ghcr.io/kure29/`（或 3.9.0 发布时短暂使用的 `ghcr.io/seren233/`）时才会被替换
- **nftables 转发**：FLVX 也用 `flvx` 表，新添加的服务器不会读写它

两个面板的 agent 共用宿主机网络，同一个端口只能给其中一个面板的转发使用。

回退前保留部署目录中的 `.env`、`docker-compose.yml` 及备份，使用该安装对应的原配置恢复；新安装回到不识别新资源参数的历史版本时，需要显式保留原数据卷挂载。API Skill 的迁移见 [AI Skill](../ai-skill.md)。

## 面板内升级

**设置** → **系统与升级** → **面板自升级** → **检查更新** → **立即升级**。

升级会拉取新镜像并重建前后端容器，约 1 分钟后刷新页面即可。页面没有变化时，通常是浏览器或 CDN 缓存，刷新或清理缓存即可。

## 脚本升级

重新运行安装脚本，选 `2. 更新面板`。脚本会按运行中容器的标签找到部署目录（新安装默认为 `/opt/flyx-panel`）；找不到时，在部署目录（存放 `docker-compose.yml` 和 `.env` 的目录）里运行：

```bash
curl -L https://github.com/kure29/flyx/releases/latest/download/panel_install.sh -o panel_install.sh && chmod +x panel_install.sh && ./panel_install.sh
```

数据会保留，更新前会自动备份到部署目录的 `backups/`。

## 指定版本

到 [Releases](https://github.com/kure29/flyx/releases) 复制对应版本的命令，例如：

```bash
curl -L https://github.com/kure29/flyx/releases/download/3.0.7/panel_install.sh -o panel_install.sh && chmod +x panel_install.sh && ./panel_install.sh
```

## agent 升级

**服务器** 页选择服务器 → **升级**，留空版本即使用最新版；点 **多选** 可以批量升级。也可以在服务器上重新运行服务器安装脚本。

面板升级后建议把 agent 也升级到同一版本：新功能（例如回程检测、订阅流量统计）需要新 agent，旧 agent 能照常转发，只是用不了这些功能。内核（Xray / sing-box / Snell）的版本在 **落地 → 内核** 里单独更换，见 [内核与协议](./kernels.md)。

### 流量可靠上报

先升级面板，再升级 agent。新版 agent 将已采集、待确认的转发流量保存在工作目录的 `traffic-*.json` 中，网络失败或重启后继续重试；面板按批次去重，规则流量、共享用量和统计记录一起提交。请保留该目录及文件，容器部署需要保留 agent 数据卷。

旧 agent 仍可向新版面板上报，但不能获得跨重试去重和本地待发送缓冲的保障。新版 agent 连接不支持 `/flow/upload/v2` 的旧面板时会保留待上报数据，升级面板后继续发送。缓冲保护已落盘的采样；进程突然退出前尚未采集或落盘的字节无法补回。

面板游标因旧备份恢复或数据库重建而丢失时，节点会保留当前待确认批次，重新建立上报流并继续发送。完整流量数据备份现在同时保存去重游标；导入旧格式完整备份会清理所恢复节点的旧游标，让节点自动重新建立上报流。部分数据导入不替换游标。已被节点确认并删除、但不在备份中的历史用量不能凭空恢复。

明确被面板拒绝的批次会拆分重试，正常条目继续入账。仍无效的单条记录或批次身份冲突会保存在同目录的 `traffic-*.json.rejected-*.json`，并打印日志；这些记录不自动重新入账，需要检查配置和账目后人工处理。留档或队列保存失败时保留原批次。普通网络错误、代理返回的错误页和无法识别的响应不会触发丢弃。

## 修复面板内升级

面板内升级提示“部署docker-compose.yml不可用”时：3.0.6 及更早版本的面板内升级会把后端容器的部署目录挂载弄丢，之后面板内升级会一直提示这个错误。数据不受影响，修复一次即可：

1. 在服务器上进入存放 `docker-compose.yml` 和 `.env` 的目录（常见为 `/root`，或当初运行安装脚本的目录），用 `ls` 确认这两个文件都在
2. 执行：

   ```bash
   docker compose up -d --force-recreate backend frontend
   ```

也可以在该目录运行 3.0.7 及以后的安装脚本，选“更新面板”，会顺带修复。

## 发布仓库是私有仓库

自己 fork 后把仓库设为私有时，GitHub 不再对外提供 Release 文件和镜像，面板内升级、服务器安装和 agent 升级都会失败。给面板配置一个令牌即可恢复（官方仓库 kure29/flyx 是公开的，不需要这一步）：

1. 在 GitHub **Settings** → **Developer settings** → **Personal access tokens (classic)** 新建令牌，勾选 `repo`；面板内升级还要拉取私有镜像，再勾选 `read:packages`。
2. 在面板部署目录的 `.env` 里加一行 `GITHUB_TOKEN=令牌`，然后 `docker compose up -d` 重建后端（旧版 `docker-compose.yml` 需要在 `backend.environment` 里加上 `GITHUB_TOKEN: ${GITHUB_TOKEN:-}`，见 [常见问题](./faq.md)）。令牌只在面板服务器上，不会显示在网页里，也不会进入备份。
3. **设置** → **服务器与监控** 打开 **GitHub 加速**，加速地址选 **面板中转**。服务器从面板下载 agent 和安装脚本，令牌只留在面板上，不会下发到服务器。

之后面板内升级、服务器 **升级** 按钮和面板生成的安装命令都和以前一样使用。用 Docker 方式部署的服务器拉取私有镜像前，需要在服务器上先 `docker login ghcr.io`（用户名是 GitHub 用户名，密码是带 `read:packages` 的令牌）。

::: tip
面板经中转下载私有仓库的文件需要 3.5.7 或更新的版本。面板还是旧版本时，先在部署目录手动升级一次：`docker login ghcr.io` 后，把 `.env` 里的 `FLYX_VERSION` 改成新版本号，再运行 `docker compose pull && docker compose up -d`。
:::

::: warning
面板中转对能访问面板地址的人开放，配置令牌后，私有仓库的 Release 文件（agent、安装脚本、compose 文件）也能经面板中转下载，源码不会。
:::

